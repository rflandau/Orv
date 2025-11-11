package vaultkeeper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/client"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"google.golang.org/protobuf/proto"
)

// File requests.go implements vk methods to wrap the client requests.

// Join directs the vk to attempt to join the VK at target.
// Sends a HELLO, followed by a JOIN.
//
// On success, the VK's parent info will be updated and the parent will be notified of all known services.
// Returns nil on success.
func (vk *VaultKeeper) Join(ctx context.Context, target netip.AddrPort) (err error) {
	if ctx == nil {
		return slims.ErrNilCtx
	}

	// send the HELLO
	if _, _, _, err := client.Hello(ctx, vk.ID(), target); err != nil {
		return fmt.Errorf("HELLO: %w", err)
	}
	// send the JOIN
	vk.structure.mu.Lock()
	parentID, _, err := client.Join(ctx, vk.id, target, client.JoinInfo{IsVK: true, VKAddr: vk.addr, Height: vk.structure.height})
	if err != nil {
		vk.structure.mu.Unlock()
		return err
	}
	// update our parent
	vk.structure.parentAddr = target
	vk.structure.parentID = parentID
	vk.structure.mu.Unlock()
	// send all of our services (and their providers) up to our parent
	for svc, providers := range vk.children.allServices {
		for _, addr := range providers {
			// if our parent has changed, quit
			vk.structure.mu.Lock()
			curParent := vk.structure.parentAddr
			vk.structure.mu.Unlock()
			if curParent != target {
				return errors.New("parent changed while registering services")
			}
			// register this service+provider
			_, _, err := client.Register(ctx, vk.id, target, svc, addr, 0)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Merge requests to merge with the target vk.
// If agreed to, this vk will become the new root and will notify all pre-existing children.
//
// Returns errors that occur during the initial merge stage.
// Logs and swallows errors that occur during the increment stage.
func (vk *VaultKeeper) Merge(target netip.AddrPort) error {
	// send the HELLO
	targetVKID, _, _, err := client.Hello(vk.net.ctx, vk.ID(), target)
	if err != nil {
		return fmt.Errorf("HELLO: %w", err)
	}
	// send the MERGE request
	reqHdr := protocol.Header{
		Version: protocol.SupportedVersions().HighestSupported(),
		Type:    pb.MessageType_MERGE,
		ID:      vk.id,
	}
	reqBody := pb.Merge{}
	vk.net.mu.RLock()
	UDPAddr := net.UDPAddrFromAddrPort(target)
	vk.net.mu.RUnlock()

	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return err
	}

	if _, err := protocol.WritePacket(vk.net.ctx, conn, reqHdr, &reqBody); err != nil {
		return err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, vk.net.ctx)
	if err != nil {
		return err
	}
	if respHdr.Type == pb.MessageType_FAULT {
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return err
		}
		return err
	}
	// add child, update height, notify all other children
	vk.structure.mu.Lock()
	defer vk.structure.mu.Unlock()
	vk.structure.height += 1

	// add the target as a childVK
	if !vk.addCVK(targetVKID, target) {
		return fmt.Errorf("merge target @ %v (ID: %v) could not be added as a child: it is already a leaf",
			target, targetVKID)
	}
	// message INCREMENT down all other branches.
	// If an error occurs, it is logged and processing moves onto the next cVK.
	vk.children.cvks.RangeLocked(func(id slims.NodeID, s struct {
		services map[string]netip.AddrPort
		addr     netip.AddrPort
	}) (next bool) {
		next = true // in all cases, we want to continue to the next child
		// pre-prepare a warning log message to ensure it has all the fields we want
		eLog := vk.log.Warn().Uint64("cVK ID", id).Str("origin", "notifying child to INCREMENT")

		if id == targetVKID {
			return
		}
		UDPAddr := net.UDPAddrFromAddrPort(s.addr)
		conn, err := net.DialUDP("udp", nil, UDPAddr)
		if err != nil {
			eLog.Err(err).Msg("failed to generate UDP dialer")
			return
		}

		if _, err := protocol.WritePacket(vk.net.ctx, conn, protocol.Header{
			Version: vk.versionSet.HighestSupported(),
			Type:    pb.MessageType_INCREMENT,
			ID:      vk.id,
		}, &pb.Increment{NewHeight: uint32(vk.structure.height) - 1}); err != nil {
			eLog.Err(err).Msg("failed to INCREMENT child")
			return
		}
		// await an INCREMENT_ACK
		_, _, hdr, bd, err := protocol.ReceivePacket(conn, vk.net.ctx)
		if err != nil {
			eLog.Err(err).Msg("failed to receive INCREMENT_ACK")
			return
		} else if hdr.Type == pb.MessageType_FAULT {
			var f *pb.Fault
			if err := pbun.Unmarshal(bd, f); err != nil {
				eLog.Err(err).Msg("failed to receive INCREMENT_ACK: failed to unmarshal fault")
				return
			}
			eLog.Err(slims.FormatFault(f)).Msg("failed to receive INCREMENT_ACK")
		} else if hdr.Type != pb.MessageType_INCREMENT_ACK {
			eLog.Str("response message type", hdr.Type.String()).Msg("failed to receive INCREMENT_ACK: bad response message type")
		} else if bd != nil { // it isn't strictly necessary for the child to echo its new height
			// unpack the body and sanity check it
			var ack pb.IncrementAck
			if err := pbun.Unmarshal(bd, &ack); err != nil {
				eLog.Err(err).Msg("failed to sanity check INCREMENT_ACK: failed to unmarshal as such")
				return
			} else if ack.NewHeight != uint32(vk.structure.height)-1 {
				eLog.Uint32("expected height", uint32(vk.structure.height)-1).Uint32("response height", ack.NewHeight).Msg("INCREMENT_ACK returned back response height")
			}
		}
		return
	})
	return nil
}

// Leave makes the vaultkeeper leave (and notify) its current parent.
// No-op if this vaultkeeper does not have a parent.
//
// If an errors occurs (which can only occur while notifying parent), it is logged and swallowed.
func (vk *VaultKeeper) Leave(timeout time.Duration) {
	var (
		pAddr    netip.AddrPort
		pID      slims.NodeID
		leaveCtx context.Context
		cnl      func()
	)
	vk.structure.mu.Lock()
	{
		// cache and clear
		pAddr = vk.structure.parentAddr
		pID = vk.structure.parentID
		vk.structure.parentAddr = netip.AddrPort{}
		vk.structure.parentID = 0

		// derive child context
		vk.net.mu.RLock()
		pCtx := vk.net.ctx
		if vk.net.ctx == nil { // edge case: vk is not active and therefore does not have a context
			pCtx = context.Background()
		}
		leaveCtx, cnl = context.WithTimeout(pCtx, timeout)
		vk.net.mu.RUnlock()
		defer cnl()
	}
	vk.structure.mu.Unlock()
	if !pAddr.IsValid() {
		return
	}

	vk.log.Info().Str("former parent address", pAddr.String()).Uint64("former parent address", pID).Msg("leaving parent...")
	if err := client.Leave(leaveCtx, pAddr, vk.id); err != nil {
		vk.log.Warn().Err(err).Msg("failed to tell parent we are leaving")
	}
}

// HeartbeatParent sends a VK_HEARTBEAT to the parent of this vk.
// VKs do this automatically; you only need to call this function manually if you have disabled automated heartbeats in the VK.
//
// ! Does NOT alter the parent information in this VK on a bad or lost heartbeat.
func (vk *VaultKeeper) HeartbeatParent() error {
	respHdr, respBody, err := vk.messageParent(pb.MessageType_VK_HEARTBEAT, nil)
	if err != nil {
		return err
	} else if respHdr.Type != pb.MessageType_VK_HEARTBEAT_ACK {
		return fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
	if len(respBody) > 0 {
		vk.log.Warn().Int("body length", len(respBody)).Bytes("body", respBody).Msg("VK_HEARTBEAT has a non-zero body")
	}
	return nil
}

// MessageToParent sends the header and body to the parent, if they exist.
// Returns a client.ErrInvalidAddrPort if parentAddr is invalid (or empty).
//
// respHdr and body will not be of type FAULT; FAULTs will be returned as an error per FormatFault.
//
// Acquires a structure read lock just long enough to cache the parent's address.
func (vk *VaultKeeper) messageParent(typ pb.MessageType, payload proto.Message) (respHdr protocol.Header, respBody []byte, _ error) {
	vk.structure.mu.RLock()
	UDPParentAddr := net.UDPAddrFromAddrPort(vk.structure.parentAddr)
	vk.structure.mu.RUnlock()
	if UDPParentAddr == nil {
		return protocol.Header{}, nil, client.ErrInvalidAddrPort
	}
	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPParentAddr)
	if err != nil {
		return protocol.Header{}, nil, err
	}
	// send
	if _, err := protocol.WritePacket(
		vk.net.ctx, conn,
		protocol.Header{
			Version: protocol.SupportedVersions().HighestSupported(),
			Type:    typ,
			ID:      vk.ID()},
		payload); err != nil {
		return protocol.Header{}, nil, err
	}
	// receive
	_, _, respHdr, respBody, err = protocol.ReceivePacket(conn, vk.net.ctx)
	if err != nil {
		return protocol.Header{}, nil, err
	}
	if respHdr.Type == pb.MessageType_FAULT {
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return protocol.Header{}, nil, err
		}
		return protocol.Header{}, nil, slims.FormatFault(f)
	}
	return respHdr, respBody, nil
}
