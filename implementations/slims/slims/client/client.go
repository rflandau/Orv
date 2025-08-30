// Package client provides static subroutines for interacting with a vault and vaultkeeper.
// Each subroutine lists if it is intended to be used only by leaves, only by vks, or by any node (even those not associated to the tree).
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"google.golang.org/protobuf/proto"
)

var ErrInvalidAddrPort = errors.New("target must be a valid address+port")

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
// Sends the packet as protocol.SupportedVersions().HighestSupported().
//
// This subroutine can be invoked by any node.
func Hello(id slims.NodeID, target netip.AddrPort, ctx context.Context) (vkID slims.NodeID, _ *pb.HelloAck, err error) {
	// validate parameters
	if ctx == nil {
		return 0, nil, slims.ErrCtxIsNil
	} else if !target.IsValid() {
		return 0, nil, ErrInvalidAddrPort
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, nil, ErrInvalidAddrPort
	}

	// generate a packet
	pkt, err := protocol.Serialize(protocol.SupportedVersions().HighestSupported(), false, mt.Hello, id, nil)
	if err != nil {
		return 0, nil, err
	}
	// generate a dialer
	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, nil, err
	}
	// send
	if wroteN, err := ctxWrite(ctx, conn, pkt); err != nil {
		return 0, nil, err
	} else if len(pkt) != wroteN {
		return 0, nil, fmt.Errorf("unexpected byte count written to target. Expected %dB, wrote %dB", len(pkt), wroteN)
	}
	// receive
	_, _, respHdr, respBody, err := protocol.ReceivePacket(conn, ctx)
	if err != nil {
		return 0, nil, err
	}
	switch respHdr.Type {
	case mt.Fault:
		f := &pb.Fault{}
		if err := proto.Unmarshal(respBody, f); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, nil, errors.New(f.Reason)
	case mt.HelloAck:
		ha := &pb.HelloAck{}
		if err := proto.Unmarshal(respBody, ha); err != nil {
			return respHdr.ID, nil, err
		}
		return respHdr.ID, ha, nil
	default:
		return respHdr.ID, nil, fmt.Errorf("unhandled message type from response: %s", respHdr.Type.String())
	}
}

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
	hdrB, err := reqHdr.Serialize()
	if err != nil {
		return 0, sr, err
	}

	conn, err := net.DialUDP("udp", nil, UDPAddr)
	if err != nil {
		return 0, sr, err
	}

	if n, err := conn.Write(hdrB); err != nil { // TODO include context
		return 0, sr, err
	} else if n != len(hdrB) {
		return 0, sr, fmt.Errorf("unexpected byte count written to target. Expected %dB, wrote %dB", len(hdrB), n)
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

// ctxWrite writes a bytestream over the given connection, but also watches for context cancellation to return early.
func ctxWrite(ctx context.Context, conn *net.UDPConn, b []byte) (int, error) {
	if ctx == nil {
		return 0, slims.ErrCtxIsNil
	}
	// clear out any existing deadline and ensure we do the same on exist
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		return 0, err
	}
	defer conn.SetWriteDeadline(time.Time{})

	// spin up a channel to receive the write results
	resCh := make(chan struct {
		n   int
		err error
	}, 1) // buffer the channel so we do not leak the goroutine
	go func() {
		n, err := conn.Write(b)
		resCh <- struct {
			n   int
			err error
		}{n, err}
		close(resCh)
	}()

	select {
	case <-ctx.Done(): //handle if the context is cancelled
		conn.SetWriteDeadline(time.Now())
		return 0, ctx.Err()
	case res := <-resCh:
		return res.n, res.err
	}
}
