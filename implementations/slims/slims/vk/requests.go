package vaultkeeper

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"google.golang.org/protobuf/proto"
)

// File requests.go implements vk methods to wrap the client requests.

// Join directs the vk to attempt to join the VK at target.
// Returns nil on success
func (vk *VaultKeeper) Join(ctx context.Context, target netip.AddrPort) (err error) {
	if ctx == nil {
		return slims.ErrNilCtx
	}

	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()

	parentID, _, err := client.Join(ctx, vk.id, target, client.JoinInfo{IsVK: true, VKAddr: vk.addr, Height: vk.structure.height})
	if err != nil {
		return err
	}
	// update our parent
	vk.structure.parentAddr = target
	vk.structure.parentID = parentID
	return nil
}

// HeartbeatParent sends a VK_HEARTBEAT to the parent of this vk.
// VKs do this automatically; you only need to call this function manually if you have disabled automated heartbeats in the VK.
//
// ! Does NOT alter the parent information in this VK on a bad or lost heartbeat.
func (vk *VaultKeeper) HeartbeatParent() error {
	vk.structure.mu.RLock()
	UDPParentAddr := net.UDPAddrFromAddrPort(vk.structure.parentAddr)
	vk.structure.mu.RUnlock()
	if UDPParentAddr == nil {
		return client.ErrInvalidAddrPort
	}
	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPParentAddr)
	if err != nil {
		return err
	}
	// send
	if _, err := protocol.WritePacket(context.Background(), conn,
		protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Type: pb.MessageType_VK_HEARTBEAT, ID: vk.ID()}, nil); err != nil {
		return err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, context.Background())
	if err != nil {
		return err
	}
	switch respHdr.Type {
	case pb.MessageType_FAULT:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return err
		}
		return slims.FormatFault(f)
	case pb.MessageType_VK_HEARTBEAT_ACK:
		if len(respBody) > 0 {
			vk.log.Warn().Int("body length", len(respBody)).Bytes("body", respBody).Msg("VK_HEARTBEAT has a non-zero body")
		}
		return nil
	default:
		return fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}

}
