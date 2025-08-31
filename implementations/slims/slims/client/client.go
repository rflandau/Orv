// Package client provides static subroutines for interacting with a vault and vaultkeeper.
// Each subroutine lists if it is intended to be used only by leaves, only by vks, or by any node (even those not associated to the tree).
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidAddrPort = errors.New("target must be a valid address+port")

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
// Sends the packet as protocol.SupportedVersions().HighestSupported().
//
// This subroutine can be invoked by any node.
func Hello(ctx context.Context, myID slims.NodeID, target netip.AddrPort) (vkID slims.NodeID, vkVersion version.Version, _ *pb.HelloAck, err error) {
	// validate parameters
	if ctx == nil {
		return 0, version.Version{}, nil, slims.ErrCtxIsNil
	} else if !target.IsValid() {
		return 0, version.Version{}, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, version.Version{}, nil, ErrInvalidAddrPort
	}
	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, version.Version{}, nil, err
	}
	// send
	if _, err := protocol.WritePacket(ctx, conn,
		protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Type: mt.Hello, ID: myID}, nil); err != nil {
		return 0, version.Version{}, nil, err
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, version.Version{}, nil, err
	}
	switch respHdr.Type {
	case mt.Fault:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, respHdr.Version, nil, err
		}
		return respHdr.ID, respHdr.Version, nil, errors.New(f.Reason)
	case mt.HelloAck:
		ha := &pb.HelloAck{}
		if err := proto.Unmarshal(respBody, ha); err != nil {
			return respHdr.ID, respHdr.Version, nil, err
		}
		return respHdr.ID, respHdr.Version, ha, nil
	default:
		return respHdr.ID, respHdr.Version, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// #region client requests

// Status sends a STATUS packet to the given address and returns its answer (or an error).
// ID is optional; if given, the STATUS packet will be sent long-form.
// If omitted, the STATUS packet will be sent shorthand.
//
// This subroutine can be invoked by any node.
func Status(target netip.AddrPort, ctx context.Context, senderID ...slims.NodeID) (vkID slims.NodeID, _ *pb.StatusResp, _ error) {
	var sr *pb.StatusResp
	if !target.IsValid() {
		return 0, sr, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, sr, ErrInvalidAddrPort
	}

	// generate a header
	reqHdr := protocol.Header{Version: protocol.SupportedVersions().HighestSupported(), Shorthand: true, Type: mt.Status}
	if len(senderID) > 0 {
		reqHdr.Shorthand = false
		reqHdr.ID = senderID[0]
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, sr, err
	}

	if _, err := protocol.WritePacket(ctx, conn, reqHdr, nil); err != nil {
		return 0, sr, err
	}

	// await a response
	// if a response did not arrive in time, try again
	// TODO

	_, _, respHdr, bd, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, sr, err
	}
	switch respHdr.Type {
	case mt.Fault:
		f := pb.Fault{}
		if err := proto.Unmarshal(bd, &f); err != nil {
			return respHdr.ID, sr, err
		}
		return respHdr.ID, nil, errors.New(f.Reason)
	case mt.StatusResp:
		sr := &pb.StatusResp{}
		if err := proto.Unmarshal(bd, sr); err != nil {
			return respHdr.ID, nil, err
		}
		// TODO translate struct away from a pb struct to pass by value
		return respHdr.ID, sr, nil
	default:
		return respHdr.ID, sr, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

// #endregion client requests
