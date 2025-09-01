package vaultkeeper

import (
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
	/*if _, found := vk.children.leaves.Load(cID); found {
		return true
	}*/ // TODO
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
		servicelessPruner: vk.startPruneServicelessTimer(lID),
		services: make(map[string]struct {
			pruner *time.Timer
			stale  time.Duration
			addr   netip.AddrPort
		}),
	}
	return false
}

// helper function intended to be given to time.AfterFunc.
// Tests the ID after servicelessPruneTime has elapsed to check that it has registered at least one service;
// if it has not, trim the leaf out of the set.
func (vk *VaultKeeper) startPruneServicelessTimer(lID slims.NodeID) *time.Timer {
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
