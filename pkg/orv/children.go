package orv

import (
	"net/netip"
	"sync"
	"time"
)

// tracks the state of all children of the VK, using multiple hashtables for rapid access.
// mu must be held for all operations on children.
type children struct {
	mu       sync.Mutex
	services map[string][]struct {
		cID  childID
		addr netip.AddrPort
	} // service name -> list of each provider of the service and the child that told us about it
	leaves map[childID]map[serviceName]time.Timer // child ID -> each service it offers and how much time until it is considered stale
	vks    map[childID]struct {
		prune    time.Timer           // time until this child is considered dead
		services map[serviceName]bool // hashset; value is unused
	}
}

// Returns a ready-to-use children struct with the top level maps initialized.
func newChildren() *children {
	return &children{
		services: make(map[string][]struct {
			cID  childID
			addr netip.AddrPort
		}),
		leaves: make(map[childID]map[serviceName]time.Timer),
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
	// if it did not exist, prepare it
	if !wasLeaf && !wasVK {
		c.leaves[cID] = make(map[serviceName]time.Timer)
	}

	return
}
