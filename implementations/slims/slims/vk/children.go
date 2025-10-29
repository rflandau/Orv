package vaultkeeper

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/netip"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
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

	// on timeout/cleanup, also remove this cVK as a provider
	cleanup := func(id slims.NodeID, s struct {
		services map[string]netip.AddrPort
		addr     netip.AddrPort
	}) {
		vk.children.mu.Lock()
		defer vk.children.mu.Unlock()
		vk.pruneProvider(maps.Keys(s.services), cID)
	}

	vk.children.cvks.Store(cID, struct {
		services map[string]netip.AddrPort
		addr     netip.AddrPort
	}{services: make(map[string]netip.AddrPort), addr: addr},
		vk.pruneTime.ChildVK,
		cleanup,
	)
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
		// Tests the leaf after servicelessPruneTime has elapsed to check that it has registered at least one service;
		// if it has not, trim the leaf out of the set of children.
		servicelessPruner: time.AfterFunc(vk.pruneTime.ServicelessLeaf, func() {
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
		}),
		services: make(map[string]struct {
			pruner *time.Timer
			stale  time.Duration
			addr   netip.AddrPort
		}),
	}
	return false
}

// helper function intended to be given to time.AfterFunc.

// Adds a new service under the child, be they a leaf or a vk.
// Stale is only used for leaf services.
//
// Acquires the children lock.
//
// ! Assumes that parameters, other than childID, have already been validated.
func (vk *VaultKeeper) addService(childID slims.NodeID, service string, addr netip.AddrPort, stale time.Duration) (erred bool, code pb.Fault_Errnos, extraInfo []string) {
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
		}{pruner: time.AfterFunc(stale, func() { pruneServiceFromLeaf(vk, childID, service) }),
			stale: stale,
			addr:  addr}
	} else if cvk, found := vk.children.cvks.Load(childID); found { // add service to cvk
		// refresh the cvk's prune timer
		if !vk.children.cvks.Refresh(childID, vk.pruneTime.ChildVK) {
			return true, pb.Fault_UNSPECIFIED, []string{fmt.Sprintf("failed to register service %s to child vk %d: child vk was pruned during look up", service, childID)}
		}
		cvk.services[service] = addr // update or set our info
		vk.log.Debug().
			Uint64("child", childID).
			Str("service", service).
			Str("service address", addr.String()).
			Msgf("registered/updated service %s on child vk %d", service, childID)
	} else {
		return true, pb.Fault_UNKNOWN_CHILD_ID, nil
	}

	// add this child as a provider of the service
	if providers, found := vk.children.allServices[service]; found {
		providers[childID] = addr // update or insert ID -> addr
		vk.log.Debug().Msgf("associated child %d as a provider of service %s at %v", childID, service, addr)
	} else { // totally new service with no other providers
		vk.log.Info().
			Uint64("provider/childID", childID).
			Str("service", service).
			Msg("adding new service to list of all services")
		vk.children.allServices[service] = map[slims.NodeID]netip.AddrPort{
			childID: addr,
		}
	}

	return false, 0, nil
}

// pruneServiceFromLeaf is called whenever a service's stale timer is triggered (which can only occurs on leaves as cvk's services do not have stale timers).
// The service is removed from the leaf's list of services and the leaf is removed from the list of providers of the service.
// If this was the last service offered by the leaf, the leaf's serviceless prune timer is restarted.
func pruneServiceFromLeaf(vk *VaultKeeper, childID slims.NodeID, service string) {
	// if this timer ever fires, it means the service was not refreshed quickly enough and thus this service is considered stale (and can be removed)
	vk.children.mu.Lock()
	defer vk.children.mu.Unlock()

	if _, found := vk.children.allServices[service]; found {
		// remove this leaf as a provider of the service
		delete(vk.children.allServices[service], childID)
	}

	// remove this service from the leaf's list of services
	if _, found := vk.children.leaves[childID]; !found {
		vk.log.Info().
			Uint64("child", childID).
			Str("service", service).
			Msg("failed to pruned service from leaf: child no longer exists")
		return
	}
	delete(vk.children.leaves[childID].services, service)
	vk.log.Info().
		Uint64("child", childID).
		Str("service", service).
		Msg("pruned service from leaf")
	// if that was the last service offered by this leaf, restart the leaf's prune timer
	if len(vk.children.leaves[childID].services) == 0 {
		if vk.children.leaves[childID].servicelessPruner.Reset(vk.pruneTime.ServicelessLeaf) {
			vk.log.Warn().
				Uint64("leaf ID", childID).
				Str("pruned service", service).
				Msg("restarted serviceless timer, but timer was already running")
		}
	}
	// notify our parent
	respHdr, respBody, err := vk.messageParent(pb.MessageType_DEREGISTER, &pb.Deregister{Service: service})
	// log the result, but do not otherwise act on it (as we don't care about retries)
	if err != nil && !errors.Is(err, client.ErrInvalidAddrPort) { // swallow invalid address errors as we are probably root
		vk.log.Warn().Err(err).Msg("failed to deregister message to parent")
		return
	} else if respHdr.Type != pb.MessageType_DEREGISTER_ACK {
		vk.log.Error().Msgf("unhandled message type ('%s') received in response to DEREGISTER", respHdr.Type.String())
		return
	}
	vk.log.Info().Str("service", service).Msg("propagated deregister to parent")
	// sanity check the body
	if len(respBody) > 0 {
		var b pb.DeregisterAck
		if err := pbun.Unmarshal(respBody, &b); err != nil {
			vk.log.Error().Err(err).Msg("failed to unmarshal DEREGISTER_ACK body")
			return
		} else if b.Service != service {
			vk.log.Warn().Str("expected service", service).Str("deregistered service", b.Service).Msg("incorrect service acknowledged by DEREGISTER_ACK")
		}
	}
}

// pruneProvider iterates the list of services, removing the ID as a provider of each (if found).
// If a service isn't found in the list of allServices it is skipped.
// If the last provider of a service is pruned, the service is removed from the list of services and this VK's parent notified via DEREGISTER.
//
// ! Expects the caller to hold the child lock.
func (vk *VaultKeeper) pruneProvider(services iter.Seq[string], childID slims.NodeID) {
	for svc := range services {
		if m, found := vk.children.allServices[svc]; !found {
			continue
		} else if _, found := m[childID]; !found {
			continue
		}
		vk.log.Debug().Msgf("pruned provider %v from service %v", childID, svc)
		delete(vk.children.allServices[svc], childID)
		if len(vk.children.allServices[svc]) == 0 {
			delete(vk.children.allServices, svc)
			// notify parent
			if respHdr, respBody, err := vk.messageParent(pb.MessageType_DEREGISTER, &pb.Deregister{Service: svc}); err != nil || respHdr.Type == pb.MessageType_FAULT {
				ev := vk.log.Warn().Uint64("last provider's cID", childID).Str("service", svc)
				if err != nil {
					ev = ev.Err(err)
				} else { // fault returned, unpack the body
					var f pb.Fault
					if err := pbun.Unmarshal(respBody, &f); err != nil {
						vk.log.Error().Err(err).Msg("failed to unpack fault message from sending a register to parent")
						ev = ev.Str("errno", "UNKNOWN")
					} else {
						ev = ev.Str("errno", f.Errno.String())
					}
				}
				ev.Msg("failed to deregister service")
			}
		}
	}
}

// RemoveCVK attempts to delete the cVK associated to the given ID
// If the ID is found, the cVK is also removed as a provider of its services (potentially pruning those services).
//
// Acquires the children lock iff lock is set.
func (vk *VaultKeeper) RemoveCVK(id slims.NodeID, lock bool) (found bool) {
	if lock {
		vk.children.mu.Lock()
		defer vk.children.mu.Unlock()
	}

	v, found := vk.children.cvks.Load(id)
	defer vk.pruneProvider(maps.Keys(v.services), id)
	if !found {
		return false
	}
	found = vk.children.cvks.Delete(id)
	vk.log.Info().Msgf("dropped cVK %d", id)
	return found
}

// RemoveLeaf deletes the leaf associated to the given ID,
// stops its stale timer and the stale timer of each of its services, and prunes the leaf as a known provider.
//
// Acquires the children lock iff lock is set.
func (vk *VaultKeeper) RemoveLeaf(id slims.NodeID, lock bool) (found bool) {
	if lock {
		vk.children.mu.Lock()
		defer vk.children.mu.Unlock()
	}

	l, found := vk.children.leaves[id]
	defer vk.pruneProvider(maps.Keys(l.services), id) // ensure we prune these services, even if the leaf isn't found
	if !found {
		return false
	}
	l.servicelessPruner.Stop()
	// stop each service's timer
	for _, inf := range l.services {
		inf.pruner.Stop()
	}
	delete(vk.children.leaves, id)
	vk.log.Info().Msgf("dropped leaf %d", id)
	return true
}
