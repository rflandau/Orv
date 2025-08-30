// Package client provides static subroutines for interacting with a vault and vaultkeeper.
// These subroutines can be called from any node (vk, leaf, or external)
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
	"google.golang.org/protobuf/proto"
)

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
func Hello(id slims.NodeID, addrPort string, ctx context.Context) (_ *pb.HelloAck, err error) {
	/*
	   // compose the body
	   body, err := proto.Marshal(&pb.Hello{Id: id})

	   	if err != nil {
	   		return nil, err
	   	}

	   // compose the header

	   	reqHdr := protocol.Header{
	   		Version: protocol.HighestSupported,
	   		Type:    mt.Hello,
	   	}

	   reqHdrB, err := reqHdr.Serialize()

	   	if err != nil {
	   		return nil, err
	   	}

	   conn, err := udp.Dial(addrPort)

	   	if err != nil {
	   		return nil, err
	   	}

	   defer conn.Close()

	   resp, err := conn.Post(ctx, "/", ResponseMediaType(), bytes.NewReader(append(reqHdrB, body...)))
	   var hdrBytes = make([]byte, protocol.LongHeaderLen)

	   	if n, err := resp.Body().Read(hdrBytes); err != nil {
	   		return nil, err
	   	} else if n != int(protocol.LongHeaderLen) {

	   		return nil, fmt.Errorf("incorrect read count (%d, expected %d)", n, protocol.LongHeaderLen)
	   	}

	   // read the header
	   respHeader := protocol.Header{}

	   	if err := respHeader.Deserialize(bytes.NewBuffer(hdrBytes)); err != nil {
	   		return nil, err
	   	}

	   // read the payload
	   var respBody bytes.Buffer

	   	if _, err := io.Copy(&respBody, resp.Body()); err != nil {
	   		return nil, err
	   	}

	   // if the packet type is FAULT, unmarshal as a fault
	   switch respHeader.Type {
	   case mt.Fault:

	   	// fetch the reason
	   	var f pb.Fault
	   	if err := proto.Unmarshal(respBody.Bytes(), &f); err != nil {
	   		return nil, err
	   	}
	   	f.Reason = strings.TrimSpace(f.Reason)
	   	if f.Reason == "" {
	   		return nil, errors.New("FAULT occurred, but no reason was given")
	   	}
	   	return nil, errors.New(f.Reason)

	   case mt.HelloAck:

	   		var r pb.HelloAck
	   		if err := proto.Unmarshal(respBody.Bytes(), &r); err != nil {
	   			return nil, err
	   		}
	   		return &pb.HelloAck{
	   			Id:      r.Id,
	   			Height:  r.Height,
	   			Version: r.Version,
	   		}, nil
	   	}

	   return nil, errors.New("unexpected response message type: " + respHeader.Type.String())
	*/
	// TODO
	return nil, nil
}

// Status sends a STATUS packet to the given address and returns its answer (or an error).
// ID is optional; if given, the STATUS packet will be sent long-form.
// If omitted, the STATUS packet will be sent shorthand.
func Status(target netip.AddrPort, ctx context.Context, senderID ...slims.NodeID) (vkID slims.NodeID, _ *pb.StatusResp, _ error) {
	var sr *pb.StatusResp
	if !target.IsValid() {
		return 0, sr, errors.New("ap must be a valid address+port")
	}
	UDPAddr := net.UDPAddrFromAddrPort(target)
	if UDPAddr == nil {
		return 0, sr, errors.New("ap must be a valid address+port")
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
