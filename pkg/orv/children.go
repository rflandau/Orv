package orv

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"time"

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
	services   map[serviceName]bool // what services are available via this child's branch? Hashset; value is unused
	addr       netip.AddrPort       // how do we reach this child? Only used for INCRs
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
			services: make(map[serviceName]bool),
			addr:     addr}
	}

	return

}

// Adds a new service under the given child.
// If the service already exists as a leaf, it will take the new stale time.
// Does not echo the REGISTER up the tree; expects the caller to do so.
func (c *children) addService(cID childID, svc serviceName, addr netip.AddrPort, staleTime time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		//newSvc bool
		err error
	)
	// identify what kind of child to associate the service under
	if _, exists := c.leaves[cID]; exists {
		_, err = c.addServiceToLeaf(cID, svc, addr, staleTime)
		if err != nil {
			return err
		}
	} else if _, exists := c.vks[cID]; exists {
		_, err = c.addServiceToVK(cID, svc)
		if err != nil {
			return err
		}
	} else {
		// the specified child DNE
		return ErrUnknownCID(cID)
	}

	// add this node as a provider of the service
	if providers, exists := c.services[svc]; exists {
		// check if it already exists
		for i, p := range providers {
			if p.cID == cID {
				c.log.Debug().
					Uint64("child ID", cID).
					Str("service", svc).
					Str("old address", p.addr.String()).
					Str("new address", addr.String()).
					Msgf("service is already known to be provided by the child. Updating address...")
				// this service is already associated to the child
				// update the address
				c.services[svc][i].addr = addr
				return nil
			}
		}
		c.log.Debug().
			Uint64("child ID", cID).
			Str("service", svc).
			Str("address", addr.String()).
			Msgf("registering new service provider")
		// if we made it this far, then the child is not a known provider of the service
		// add it
		c.services[svc] = append(c.services[svc], struct {
			cID  childID
			addr netip.AddrPort
		}{cID, addr})
	} else {
		c.log.Info().Uint64("provider", cID).Str("service", svc).Msg("adding new service to list of services")
		// this is a totally new service
		c.services[svc] = []struct {
			cID  childID
			addr netip.AddrPort
		}{
			{cID: cID, addr: addr},
		}
	}

	return nil

}

// Helper function for addService() once it figures out that the new service should belong to a leaf.
// Adds the service under the leaf. Does not associate this leaf as a provider of the service.
// Assumes the caller already owns the child lock.
// Returns true if it is a new service.
func (c *children) addServiceToLeaf(cID childID, svc serviceName, addr netip.AddrPort, staleTime time.Duration) (newService bool, err error) {
	leaf, exists := c.leaves[cID]
	if !exists {
		return false, fmt.Errorf("did not find a leaf associated to id %d", cID)
	}
	// validate parameters
	if staleTime <= 0 {
		return false, errors.New("stale time must be a valid Go time greater than 0")
	} else if !addr.IsValid() {
		return false, ErrBadAddr(addr)
	}

	// if the service already exists, stop the current timer and allow the service to be replaced with the new info
	if svcInfo, exists := leaf[svc]; exists {
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Dur("old stale time", svcInfo.staleTime).
			Dur("new stale time", staleTime).
			Msg("re-REGISTERing service")
		svcInfo.pruneTimer.Stop()
		newService = false
	} else {
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Dur("stale time", staleTime).
			Msg("REGISTERing new service")
		newService = true
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
		addr:      addr,
	}
	return newService, nil
}

// Helper function for addService() once it figures out that the new service should belong to a cVK.
// Adds the service under the cVK if we do not already know about it. Does not associate this VK as a provider of the service.
// Also refreshes the heartbeat timer for this cVK.
// Assumes the caller already owns the child lock.
// Returns true if it is a new service.
func (c *children) addServiceToVK(cID childID, svc serviceName) (newService bool, err error) {
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
	c.vks[cID].services[svc] = true
	return true, nil
}
