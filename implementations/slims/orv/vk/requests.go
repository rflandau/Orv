package vaultkeeper

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/rflandau/Orv/implementations/slims/orv"
	payloads_proto "github.com/rflandau/Orv/implementations/slims/orv/pb"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// File requests.go contains methods for vaultkeepers to make requests and send packets to other vaultkeepers.
// These subroutines are used to communicate with other nodes.
// Request payloads are encoded via protobuf.

type HelloAck struct {
	ID      orv.NodeID
	Height  uint16
	Version byte // major+minor as a byte
}

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
func (vk *VaultKeeper) Hello(addrPort string, ctx context.Context) (response HelloAck, err error) {
	if !vk.alive.Load() {
		return HelloAck{}, ErrDead
	}

	// compose the body
	body, err := proto.Marshal(&payloads_proto.Hello{Id: vk.id})
	if err != nil {
		return HelloAck{}, err
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
		Type:          protocol.Hello,
	}
	reqHdrB, err := reqHdr.Serialize()
	if err != nil {
		return HelloAck{}, err
	}

	conn, err := udp.Dial(addrPort)
	if err != nil {
		return HelloAck{}, err
	}
	defer conn.Close()

	resp, err := conn.Post(ctx, "/", orv.ResponseMediaType(), bytes.NewReader(append(reqHdrB, body...)))
	respBody := resp.Body()
	var hdrBytes = make([]byte, protocol.FixedHeaderLen)
	if n, err := respBody.Read(hdrBytes); err != nil {
		return HelloAck{}, err
	} else if n != int(protocol.FixedHeaderLen) {
		return HelloAck{}, fmt.Errorf("incorrect read count (%d, expected %d)", n, protocol.FixedHeaderLen)
	}

	// read the header
	respHeader := protocol.Header{}
	if err := respHeader.Deserialize(bytes.NewBuffer(hdrBytes)); err != nil {
		return HelloAck{}, err
	}
	// read the payload
	// TODO the response body should already be read forward 5 bytes, but we need to confirm that
	var ret HelloAck
	if respBody, err := io.ReadAll(respBody); err != nil {
		return HelloAck{}, err
	} else {
		var r payloads_proto.HelloAck
		if err := proto.Unmarshal(respBody, &r); err != nil {
			return HelloAck{}, err
		}
		// narrow the type to a HelloAck
		ret = HelloAck{
			ID:      r.Id,
			Height:  uint16(r.Height),
			Version: byte(r.Version),
		}
	}
	return ret, nil
}
