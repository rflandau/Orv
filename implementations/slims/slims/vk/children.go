package vaultkeeper

import (
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims"
)

// addCVK installs the given information into the cvk table, failing if the ID is already registered to a leave and refreshing the timer if it is for a previously-known cvk.
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
