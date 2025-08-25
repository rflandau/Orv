package vaultkeeper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/rflandau/Orv/implementations/slims/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/pb"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol/mt"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// File requests.go contains methods for vaultkeepers to make requests and send packets to other vaultkeepers.
// These subroutines are used to communicate with other nodes.
// Request payloads are encoded via protobuf.

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
func (vk *VaultKeeper) Hello(addrPort string, ctx context.Context) (response *pb.HelloAck, err error) {
	if !vk.alive.Load() {
		return nil, ErrDead
	}

	// compose the body
	body, err := proto.Marshal(&pb.Hello{Id: vk.id})
	if err != nil {
		return nil, err
	}

	// only sanity check length in debug mode
	if vk.log.GetLevel() == zerolog.DebugLevel {
		if len(body) > int(protocol.MaxPayloadLength) {
			vk.log.Error().Int("body length", len(body)).Uint16("max payload length", protocol.MaxPayloadLength).Msg("body length exceeds max payload length")
		}
	}

	// compose the header
	reqHdr := protocol.Header{
		Version:       protocol.HighestSupported,
		HopLimit:      1,
		PayloadLength: uint16(len(body)),
		Type:          mt.Hello,
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

	resp, err := conn.Post(ctx, "/", orv.ResponseMediaType(), bytes.NewReader(append(reqHdrB, body...)))
	respBody := resp.Body()
	var hdrBytes = make([]byte, protocol.FixedHeaderLen)
	if n, err := respBody.Read(hdrBytes); err != nil {
		return nil, err
	} else if n != int(protocol.FixedHeaderLen) {
		return nil, fmt.Errorf("incorrect read count (%d, expected %d)", n, protocol.FixedHeaderLen)
	}

	// read the header
	respHeader := protocol.Header{}
	if err := respHeader.Deserialize(bytes.NewBuffer(hdrBytes)); err != nil {
		return nil, err
	}
	// read the payload
	// if the packet type is FAULT, unmarshal as a fault
	if respHeader.Type == mt.Fault {

	} else if respHeader.Type == mt.HelloAck {

	} else {
		return nil, errors.New("unexpected response message type: " + respHeader.Type.String())
	}
	// TODO the response body should already be read forward 5 bytes, but we need to confirm that
	var ret pb.HelloAck
	if respBody, err := io.ReadAll(respBody); err != nil {
		return nil, err
	} else {
		var r pb.HelloAck
		if err := proto.Unmarshal(respBody, &r); err != nil {
			return nil, err
		}
		// narrow the type to a HelloAck
		ret = pb.HelloAck{
			Id:      r.Id,
			Height:  r.Height,
			Version: r.Version,
		}
	}
	return &ret, nil
}
