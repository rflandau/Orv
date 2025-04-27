package orv

import (
	"errors"
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
	services map[string][]struct {
		cID  childID
		addr netip.AddrPort
	} // service name -> list of each provider of the service and the child that told us about it
	leafPruneTime time.Duration
	cvkPruneTime  time.Duration
	leaves        map[childID]map[serviceName]struct {
		tilPrune  *time.Timer
		staleTime time.Duration
	} // child ID -> each service it offers and how much time until it is considered stale
	vks map[childID]struct {
		prune    *time.Timer          // time until this child is considered dead
		services map[serviceName]bool // hashset; value is unused
	}
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
		leafPruneTime: leafPruneTime,
		cvkPruneTime:  cvkPruneTime,
		leaves: make(map[childID]map[serviceName]struct {
			tilPrune  *time.Timer
			staleTime time.Duration
		}),
		vks: make(map[childID]struct {
			prune    *time.Timer
			services map[serviceName]bool
		}),
	}
}

// Adds a new leaf if the leaf does not already exist (as a leaf or VK).
// Returns what the leaf already existed as. Both returns will be false if a new leaf was actually added.
// It should not be possible for both to return true; if they do, something has gone horribly wrong prior to this call.
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
		c.leaves[cID] = make(map[serviceName]struct {
			tilPrune  *time.Timer
			staleTime time.Duration
		})
		// spawn a timer function to self-prune
		time.AfterFunc(c.leafPruneTime, func() {
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

// Adds a new service under the given child.
// If the service already exists as a leaf, it will take the new stale time.
// Does not echo the REGISTER up the tree; expects the caller to do so.
func (c *children) addService(cID childID, svc serviceName, staleTime time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if leaf, exists := c.leaves[cID]; exists {
		// validate stale time
		if staleTime <= 0 {
			return errors.New("stale time must be a valid Go time greater than 0")
		}

		// if the service already exists, use the new stale time and refresh the timer
		if svcInfo, exists := leaf[svc]; exists {
			c.log.Debug().
				Uint64("child", cID).
				Str("service", svc).
				Dur("old stale time", svcInfo.staleTime).
				Dur("new stale time", staleTime).
				Msg("duplicate REGISTER. refreshing prune time...")
			// refresh the timer with the given staleTime and return success
			svcInfo.tilPrune.Reset(svcInfo.staleTime)
			leaf[svc] = struct {
				tilPrune  *time.Timer
				staleTime time.Duration
			}{
				svcInfo.tilPrune,
				staleTime,
			}
			return nil
		}
		// if the service dne, create it and start a new timer
		c.log.Debug().
			Uint64("child", cID).
			Str("service", svc).
			Dur("stale time", staleTime).
			Msg("REGISTERing new service")
		leaf[svc] = struct {
			tilPrune  *time.Timer
			staleTime time.Duration
		}{
			time.AfterFunc(staleTime, func() {
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
			staleTime,
		}
		return nil
	}
	if cvk, exists := c.vks[cID]; exists {
		// check if the service already exists under the named vk
		if _, exists := cvk.services[svc]; exists {
			// nothing to do; we already know about this service
			return nil
		}

		// new service for this cVK
		c.vks[cID] = struct {
			prune    *time.Timer
			services map[serviceName]bool
		}{prune: time.AfterFunc(c.cvkPruneTime, func() {
			// acquire children lock
			c.mu.Lock()
			defer c.mu.Unlock()
			c.log.Debug().
				Uint64("child VK", cID).
				Msg("child VK timed out")
			// check that the child still exists
			if childVK, exists := c.vks[cID]; exists {
				// deregister all services associated to this child VK
				for sn, _ := range childVK.services {
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
						// if this service now has no providers, send a DEREGISTER up the tree and remove it from the known services
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
			}
		})}

	}

	// if we made it this far, the specified child DNE
	return ErrUnknownCID(cID)

}
