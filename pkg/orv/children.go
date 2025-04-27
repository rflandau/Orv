package orv

import (
	"net/netip"
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
	leaves        map[childID]map[serviceName]time.Timer // child ID -> each service it offers and how much time until it is considered stale
	vks           map[childID]struct {
		prune    time.Timer           // time until this child is considered dead
		services map[serviceName]bool // hashset; value is unused
	}
}

// Returns a ready-to-use children struct with the top level maps initialized.
// leafPruneTime is how much time a new leaf has from the moment it is added to the map until it is pruned
// (if it does not add a service).
func newChildren(parentLogger *zerolog.Logger, leafPruneTime time.Duration) *children {
	return &children{
		log: parentLogger.With().Str("sublogger", "children").Logger(),
		services: make(map[string][]struct {
			cID  childID
			addr netip.AddrPort
		}),
		leafPruneTime: leafPruneTime,
		leaves:        make(map[childID]map[serviceName]time.Timer),
		vks: make(map[childID]struct {
			prune    time.Timer
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
		c.leaves[cID] = make(map[serviceName]time.Timer)
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
