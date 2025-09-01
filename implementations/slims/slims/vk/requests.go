package vaultkeeper

import (
	"context"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
)

// File requests.go implements vk methods to wrap the client requests.

// Join directs the vk to attempt to join the VK at target.
// Returns nil on success
func (vk *VaultKeeper) Join(ctx context.Context, target netip.AddrPort) (err error) {
	if ctx == nil {
		return slims.ErrCtxIsNil
	}

	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()

	parentID, _, err := client.Join(ctx, vk.id, target, struct {
		IsVK   bool
		VKAddr netip.AddrPort
		Height uint16
	}{true, vk.addr, vk.structure.height})
	if err != nil {
		return err
	}
	// update our parent
	vk.structure.parentAddr = target
	vk.structure.parentID = parentID
	return nil
}
