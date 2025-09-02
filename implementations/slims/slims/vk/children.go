package vaultkeeper

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
)

// addCVK installs the given information into the cvk table, failing if the ID is already registered to a leave and refreshing the timer if it is for a previously-known cvk.
// Acquires the children lock.
func (vk *VaultKeeper) addCVK(cID slims.NodeID, addr netip.AddrPort) (isLeaf bool) {
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()
	// ensure this ID is not already owned by a leaf
	if _, found := vk.children.leaves[cID]; found {
		return true
	}
	vk.children.cvks.Store(cID, struct {
		services map[string]netip.AddrPort
		addr     netip.AddrPort
	}{services: make(map[string]netip.AddrPort), addr: addr}, vk.pruneTime.cvk)
	return false
}

// a leaf represents a single leaf child that has successfully joined a vk.
type leaf struct {
	servicelessPruner *time.Timer // runs whenever the leaf has no services to prune the leaf if it remains that way
	services          map[string]struct {
		// the timer currently tracking how much time remains until the service is pruned (unless a heartbeat refreshes it).
		// Refreshed by a SERVICE_HEARTBEAT.
		// uses leafService.staleTime.
		// If the timer is allowed to fire, it will prune this service
		pruner *time.Timer
		stale  time.Duration  // how long w/o a heartbeat until we can consider this service stale
		addr   netip.AddrPort // address this service is accessible at
	}
}

// addLeaf installs a new leaf with the given ID.
// Acquires the children lock.
func (vk *VaultKeeper) addLeaf(lID slims.NodeID) (isCVK bool) {
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()

	// ensure this ID is not already owned by a cvk
	if _, found := vk.children.cvks.Load(lID); found {
		return true
	} else if _, found := vk.children.leaves[lID]; found { // it is is already a known leaf, our job is already done
		return false
	}
	// install the new leaf
	vk.children.leaves[lID] = leaf{
		servicelessPruner: vk.servicelessLeafPrune(lID),
		services: make(map[string]struct {
			pruner *time.Timer
			stale  time.Duration
			addr   netip.AddrPort
		}),
	}
	return false
}

// helper function intended to be given to time.AfterFunc.
// Tests the leaf after servicelessPruneTime has elapsed to check that it has registered at least one service;
// if it has not, trim the leaf out of the set of children.
func (vk *VaultKeeper) servicelessLeafPrune(lID slims.NodeID) *time.Timer {
	return time.AfterFunc(vk.pruneTime.servicelessLeaf, func() {
		// acquire lock
		vk.children.mu.Lock()
		defer vk.children.mu.Unlock()
		// check if the ID still exists
		l, found := vk.children.leaves[lID]
		if !found {
			return
		}
		if len(l.services) == 0 {
			vk.log.Debug().Uint64("leaf", lID).Msg("no services found after prune time. Pruning...")
			delete(vk.children.leaves, lID)
		}
	})
}

// Adds a new service under the child, be they a leaf or a vk.
// Stale is only used for leaf services.
//
// ! Assumes that parameters, other than childID, have already been validated.
func (vk *VaultKeeper) addService(childID slims.NodeID, service string, addr netip.AddrPort, stale time.Duration) error {
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()

	// figure out which kind of child it is
	if leaf, found := vk.children.leaves[childID]; found { // add service to leaf
		// if the service already exists, just stop its timer; it will be replaced with updated info
		if serviceInfo, found := leaf.services[service]; found {
			vk.log.Debug().
				Uint64("child", childID).
				Str("service", service).
				Dur("old stale time", serviceInfo.stale).
				Str("old service address", serviceInfo.addr.String()).
				Msg("replacing existing service")
			serviceInfo.pruner.Stop()
		}
		// install the new service
		vk.log.Debug().
			Uint64("child", childID).
			Str("service", service).
			Dur("stale time", stale).
			Str("service address", addr.String()).
			Msg("registering service to leaf")
		leaf.services[service] = struct {
			pruner *time.Timer
			stale  time.Duration
			addr   netip.AddrPort
		}{pruner: time.AfterFunc(stale, func() {
			// if this timer ever fires, it means the service was not refreshed quickly enough and thus this service is considered stale (and can be removed)
			vk.children.mu.Lock()
			defer vk.children.mu.Unlock()

			if _, found := vk.children.leaves[childID]; found { // if the child still exists...
				delete(vk.children.leaves[childID].services, service)
				// TODO send a DEREGISTER up the tree
				vk.log.Info().
					Uint64("child", childID).
					Str("service", service).
					Dur("stale time", stale).
					Msg("pruned service from leaf")
			} else {
				vk.log.Info().
					Uint64("child", childID).
					Str("service", service).
					Msg("failed to pruned service from leaf: child no longer exists")
			}
		}),
			stale: stale,
			addr:  addr}
	} else if cvk, found := vk.children.cvks.Load(childID); found { // add service to cvk
		// refresh the cvk's prune timer
		if !vk.children.cvks.Refresh(childID, vk.pruneTime.cvk) {
			return fmt.Errorf("failed to register service %s to child vk %d: child vk was pruned during look up", service, childID)
		}
		cvk.services[service] = addr // update or set our info
		vk.log.Debug().
			Uint64("child", childID).
			Str("service", service).
			Str("service address", addr.String()).
			Msgf("registered/updated service %s on child vk %d", service, childID)
	} else {
		return fmt.Errorf("id %d does not correspond to any known child", childID)
	}

	// add this child as a provider of the service
	if providers, found := vk.children.allServices[service]; found {
		// providers already exist for this service
		for i, p := range providers { // check if this child is already a known provider
			if p.childID == childID {
				vk.log.Debug().
					Uint64("provider/childID", p.childID).
					Str("service", service).
					Str("old address", p.addr.String()).
					Str("new address", addr.String()).
					Msgf("service is already known to be provided by the child. Updating address...")

				// just make sure the latest address is known
				vk.children.allServices[service][i].addr = addr
				return nil
			}
		}
		// if we made it this far, then providers exist for the service, but this child is not one of them.
		// So add it.
		vk.log.Debug().
			Uint64("provider/childID", childID).
			Str("service", service).
			Str("address", addr.String()).
			Msgf("registering new service provider")
		vk.children.allServices[service] = append(vk.children.allServices[service], struct {
			childID slims.NodeID
			addr    netip.AddrPort
		}{childID: childID, addr: addr})
	} else { // totally new service with no other providers
		vk.log.Info().
			Uint64("provider/childID", childID).
			Str("service", service).
			Msg("adding new service to list of all services")
		vk.children.allServices[service] = []struct {
			childID slims.NodeID
			addr    netip.AddrPort
		}{
			{childID: childID, addr: addr},
		}
	}

	return nil
}
