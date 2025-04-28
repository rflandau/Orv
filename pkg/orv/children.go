package orv

/*
This file revolves around the handling of child node information for a vk.
The children struct manages two hashtables, one for leaves and one for child VKs, and is responsible for spinning up self-destructing service timers (the prune timers).
The leaf table maps each leaf cID to a map of the services it offers and their prune timers.
The cVK table maps each cVK cID to the child's prune timer, address, and the services it has bubbled up.
For efficiency's sake, it also maintains a third hashtable of services which carries redundant data from the leaves and VKs, but provides constant time access for service requests.

The struct manages its own, coarse-grained locking, which it acquires for all operations.
*/

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

// tracks the state of all children of the VK, using multiple hashtables for rapid access.
// mu must be held for all operations on children.
type children struct {
	log      zerolog.Logger // child logger of vk.log
	mu       sync.Mutex
	services map[serviceName][]struct {
		cID  childID
		addr netip.AddrPort
	} // service name -> list of each provider of the service and the child that told us about it
	servicelessLeafPruneTime time.Duration // how long should a leaf be allowed to exist without a service
	cvkPruneTime             time.Duration // how long should a cVK be allowed to exist without a heartbeat
	leaves                   map[childID]map[serviceName]leafService
	vks                      map[childID]childVK
}

// A service provided by a directly descendent of the VK, a leaf node.
type leafService struct {
	// the timer currently tracking how much time remains until the service is pruned (unless a heartbeat refreshes it).
	// Refreshed by a SERVICE_HEARTBEAT.
	// uses leafService.staleTime.
	// If the timer is allowed to fire, it will prune this service
	pruneTimer *time.Timer
	staleTime  time.Duration  // how long w/o a heartbeat until this service is considered stale
	addr       netip.AddrPort // what address to give to requesters for how to access this service
}

// A child VaultKeeper and the services that are available on its branch.
type childVK struct {
	// the timer currently tracking how much time remains until this cVK is considered dead.
	// refreshed by a VK_HEARTBEAT.
	// uses children.cvkPruneTime.
	// If the timer is allowed to fire, it will prune this child and all services it provides
	pruneTimer *time.Timer
	services   map[serviceName]netip.AddrPort // service -> downstream leaf's given ip:port for this service
	addr       netip.AddrPort                 // how do we reach this child? Only used for INCRs
}

// Returns a ready-to-use children struct with the top level maps initialized.
// leafPruneTime is how much time a new leaf has from the moment it is added to the map until it is pruned
// (if it does not add a service).
func newChildren(parentLogger *zerolog.Logger, leafPruneTime, cvkPruneTime time.Duration) *children {
	return &children{
		log: parentLogger.With().Str("sublogger", "children").Logger(),
		services: make(map[string][]struct {
			cID  childID
			addr netip.AddrPort
		}),
		servicelessLeafPruneTime: leafPruneTime,
		cvkPruneTime:             cvkPruneTime,
		leaves:                   make(map[childID]map[serviceName]leafService),
		vks:                      make(map[childID]childVK),
	}
}

// Adds a new leaf if the leaf does not already exist (as a leaf or VK).
// Returns what the cID already existed as. Both returns will be false if a new leaf was actually added.
// It should not be possible for both to return true; if they do, something has gone horribly wrong prior to this call.
//
// NOTE(rlandau): When the leaf is added, a timer is started that attempts to prune the leaf (if it has no services) when it fires.
// This is a one-shot timer; you must manually test for a lack of remaining services whenever a service is deregistered from a leaf.
func (c *children) addLeaf(cID childID) (wasVK, wasLeaf bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.leaves[cID]; exists {
		wasLeaf = true
	}
	if _, exists := c.vks[cID]; exists {
		wasVK = true
	}
	// if it did not exist, prepare it and set a timer for it to register services
	if !wasLeaf && !wasVK {
		c.leaves[cID] = make(map[serviceName]leafService)
		// spawn a timer function to self-prune
		time.AfterFunc(c.servicelessLeafPruneTime, func() {
			// acquire lock
			c.mu.Lock()
			defer c.mu.Unlock()
			// check that the leaf has at least one service associated to it
			svcs, exists := c.leaves[cID]
			if !exists {
				// the child was already pruned. Job's done.
				return
			}
			if len(svcs) == 0 { // prune the child
				c.log.Info().Uint64("child", cID).Msg("no services found after prune time. pruning...")
				delete(c.leaves, cID)
			}
		})
	}

	return
}

// Adds a new child VK if the cID does not already exist (as a leaf or VK).
// Expects addr to be the port that the VK service is bound to on the child so we can send them INCRs.
// Returns what the cID already existed as. Both returns will be false if a new cVK was actually added.
// It should not be possible for both to return true; if they do, something has gone horribly wrong prior to this call.
func (c *children) addVK(cID childID, addr netip.AddrPort) (wasVK, wasLeaf bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.leaves[cID]; exists {
		wasLeaf = true
	}
	if _, exists := c.vks[cID]; exists {
		wasVK = true
	}
	if !wasLeaf && !wasVK {
		c.log.Info().Uint64("child ID", cID).Str("VK address", addr.String()).Msg("adding new child VK")
		c.vks[cID] = childVK{
			pruneTimer: time.AfterFunc(
				c.cvkPruneTime,
				func() {
					// acquire children lock
					c.mu.Lock()
					defer c.mu.Unlock()
					c.log.Debug().
						Uint64("child VK", cID).
						Msg("child VK timed out")
					// check that the child still exists
					if childVK, exists := c.vks[cID]; exists {
						// deregister all services associated to this child VK
						for sn := range childVK.services {
							// if this child is a provider of a given service, remove it
							providers, exists := c.services[sn]
							if exists {
								c.log.Debug().
									Uint64("child VK", cID).
									Str("service", sn).
									Msg("removing service provider")
								c.services[sn] = slices.DeleteFunc(providers, func(s struct {
									cID  childID
									addr netip.AddrPort
								}) bool {
									return s.cID == cID
								})
								// if this service now has no providers remaining, send a DEREGISTER up the tree and remove it from the known services
								if len(c.services[sn]) == 0 {
									c.log.Debug().
										Uint64("child VK", cID).
										Str("service", sn).
										Msg("no remaining service providers. Sending DEREGISTER...")
									// TODO send DEREGISTER
									delete(c.services, sn)
								}
							}
						}
						// remove self from map
						delete(c.vks, cID)
					} else {
						c.log.Debug().
							Uint64("child VK", cID).
							Msg("failed to prune dead cVK: DNE")
					}

				},
			),
			services: make(map[serviceName]netip.AddrPort),
			addr:     addr}
	}

	return

}

// Adds a new service under the given child.
// staleTime is only used if cID resolves to a leaf. If this service already exists under a leaf, the service will take on the new staleTime iff it is valid.
// Does not echo the REGISTER up the tree; expects the caller to do so.
// Assumes all parameters (other than staleTime) have already been validated.
func (c *children) addService(cID childID, svcName serviceName, addr netip.AddrPort, staleTimeStr string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		//newSvc bool
		err error
	)
	// identify what kind of child to associate the service under
	if _, exists := c.leaves[cID]; exists {
		_, err = c.addServiceToLeaf(cID, svcName, addr, staleTimeStr)
		if err != nil {
			return err
		}
	} else if _, exists := c.vks[cID]; exists {
		_, err = c.addServiceToVK(cID, svcName, addr)
		if err != nil {
			return err
		}
	} else {
		// the specified child DNE
		return ErrUnknownCID(cID)
	}

	// add this node as a provider of the service
	if providers, exists := c.services[svcName]; exists { // this is an existing service
		for i, p := range providers { // see if this child is already known to provide this service
			if p.cID == cID {
				c.log.Debug().
					Uint64("child ID", cID).
					Str("service", svcName).
					Str("old address", p.addr.String()).
					Str("new address", addr.String()).
					Msgf("service is already known to be provided by the child. Updating address...")
				// this service is already associated to the child
				// update the address
				c.services[svcName][i].addr = addr
				return nil
			}
		}
		// if we made it this far, then the child is not a known provider of the service
		// add it
		c.log.Debug().
			Uint64("child ID", cID).
			Str("service", svcName).
			Str("address", addr.String()).
			Msgf("registering new service provider")
		c.services[svcName] = append(c.services[svcName], struct {
			cID  childID
			addr netip.AddrPort
		}{cID, addr})
	} else { // this is a totally new service
		c.log.Info().Uint64("provider", cID).Str("service", svcName).Msg("adding new service to list of services")
		c.services[svcName] = []struct {
			cID  childID
			addr netip.AddrPort
		}{
			{cID: cID, addr: addr},
		}
	}

	return nil

}

// Helper function for addService() once it figures out that the new service should belong to a leaf.
// Adds the service under the leaf. Does not associate this VK as a provider of the service.
// Assumes cID, svc, and ap have already been validated.
// staleTime must resolve to a valid time.Duration iff this is a new service.
// Assumes the caller already owns the child lock.
// Returns true if it is a new service.
func (c *children) addServiceToLeaf(cID childID, svc serviceName, ap netip.AddrPort, staleTimeStr string) (newService bool, err error) {
	leaf, exists := c.leaves[cID]
	if !exists {
		return false, fmt.Errorf("did not find a leaf associated to id %d", cID)
	}

	// pre-prepare stale time
	staleTime, staleTimeErr := time.ParseDuration(staleTimeStr)

	// if the service already exists, stop the current timer and allow the service to be replaced with the new info
	if svcInfo, exists := leaf[svc]; exists {
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Dur("old stale time", svcInfo.staleTime).
			Str("potential new stale time", staleTimeStr).
			AnErr("stale time parse error", staleTimeErr).
			Msg("re-REGISTERing service")
		svcInfo.pruneTimer.Stop()
		newService = false
		// if given staleTime is invalid, use the existing stale time
		if staleTimeErr != nil {
			staleTime = svcInfo.staleTime
		}
	} else {
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Dur("stale time", staleTime).
			Msg("REGISTERing new service")
		newService = true
		// stale time must be valid
		if staleTimeErr != nil {
			return true, errors.New("stale must resolve to a valid Go duration when registering a new service (given " + staleTimeStr + "). Error: " + staleTimeErr.Error())
		}
	}

	// associate the service to this leaf
	c.leaves[cID][svc] = leafService{
		// associate a function to destruct the service if this timer ever fires
		pruneTimer: time.AfterFunc(staleTime, func() {
			// acquire children lock
			c.mu.Lock()
			defer c.mu.Unlock()
			// check that the child still exists
			if _, exists := c.leaves[cID]; exists {
				// remove service from the child
				delete(c.leaves[cID], svc)
				// send DEREGISTER up the tree
				// TODO

				c.log.Debug().
					Uint64("child", cID).
					Str("service", svc).
					Dur("stale time", staleTime).
					Msg("pruned service from child")
			} else {
				c.log.Debug().
					Uint64("child", cID).
					Str("service", svc).
					Msg("failed to prune service from child: child no longer exists")
			}
		}),
		staleTime: staleTime,
		addr:      ap,
	}

	return newService, nil
}

// Helper function for addService() once it figures out that the new service should belong to a cVK.
// Adds the service under the cVK if we do not already know about it. Does not associate this VK as a provider of the service.
// Also refreshes the heartbeat timer for this cVK.
// Assumes cID, svc, and ap have already been validated.
// ap should be the address of the leaf that offers this service, which should have been passed up by downstream VKs.
// Assumes the caller already owns the child lock.
// Returns true if it is a new service.
func (c *children) addServiceToVK(cID childID, svc serviceName, ap netip.AddrPort) (newService bool, err error) {
	cvk, exists := c.vks[cID]
	if !exists {
		return false, fmt.Errorf("did not find a child VK associated to id %d", cID)
	}

	// refresh the prune timer
	cvk.pruneTimer.Reset(c.cvkPruneTime)

	// check if the service already exists under the named vk
	if _, exists := cvk.services[svc]; exists {
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Msg("duplicate REGISTER from cVK")
		// nothing to do; we already know about this service
		return false, nil
	}

	c.log.Debug().
		Uint64("cVK", cID).
		Str("service", svc).
		Msg("REGISTERed new service")
	// new service for this cVK
	c.vks[cID].services[svc] = ap
	return true, nil
}

// Used by Snapshot to take a point-in-time snapshot of the children and their services.
type ChildrenSnapshot struct {
	// cID -> (service -> ip:addr)
	Leaves map[childID]map[serviceName]string `json:"leaves"`
	// cID -> [services]
	CVKs map[childID][]string `json:"child-VKs"`
	// service -> ["cID(ip:port)"]
	Services map[serviceName][]string `json:"services"`
}

// Refreshes the prune timer of the cVKL associated to the given cID.
func (c *children) HeartbeatCVK(cID childID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cvk, exists := c.vks[cID]
	if !exists {
		return huma.Error400BadRequest(fmt.Sprintf("no child VK associated to ID %d", cID))
	}
	cvk.pruneTimer.Reset(c.cvkPruneTime)
	c.log.Debug().Str("child type", "vk").Uint64("child ID", cID).Msg("refreshed prune timer")
	return nil
}

// Refreshes the prune timer of the cVKL associated to the given cID.
// If an error is returned, it will a huma error (thus including the response status code).
func (c *children) HeartbeatLeafService(cID childID, svcNames []serviceName) (refreshedServices []serviceName, herr error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// initialize return array
	refreshedServices = make([]serviceName, 0)

	services, exists := c.leaves[cID]
	if !exists {
		return nil, huma.Error404NotFound(fmt.Sprint("no leaf associated to ID ", cID))
	}

	for _, sn := range svcNames {
		// validate the service name
		sn = strings.TrimSpace(sn)
		if sn == "" {
			continue
		}

		// find the leaf's service by the given name
		lfs, exists := services[sn]
		if !exists {
			continue
		}
		lfs.pruneTimer.Reset(lfs.staleTime)
		c.log.Debug().Str("child type", "leaf").Uint64("child ID", cID).Str("service", sn).Msg("refreshed prune timer")
		refreshedServices = append(refreshedServices, sn)
	}

	if len(refreshedServices) == 0 {
		return nil, huma.Error400BadRequest("no services were refreshed")
	}

	return refreshedServices, nil
}

// Returns the name of every service currently offered.
func (c *children) Services() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return slices.Collect(maps.Keys(c.services))
}

// Returns a JSON-encodable struct of the child nodes and their services.
// It is a point-in-time snapshot and requires locking the children struct.
func (c *children) Snapshot() ChildrenSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	snap := ChildrenSnapshot{
		Leaves:   make(map[childID]map[serviceName]string),
		CVKs:     make(map[childID][]string),
		Services: make(map[serviceName][]string),
	}

	// copy the leaves
	for cid, svcs := range c.leaves {
		snap.Leaves[cid] = make(map[serviceName]string)
		for sn, v := range svcs {
			snap.Leaves[cid][sn] = v.addr.String()
		}
	}
	// copy the child VKs
	for cid, cvk := range c.vks {
		snap.CVKs[cid] = slices.Collect(maps.Keys(cvk.services))
	}

	// copy the services
	for sn, providers := range c.services {
		snap.Services[sn] = []string{}
		for _, p := range providers {
			// collect each provider
			snap.Services[sn] = append(snap.Services[sn], fmt.Sprintf("%v(%s)", p.cID, p.addr.String()))
		}
	}
	return snap
}
