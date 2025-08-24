package vaultkeeper

import (
	"bytes"
	"context"

	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/rflandau/Orv/implementations/slims/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
	"github.com/rs/zerolog"
)

// File requests.go contains methods for vaultkeepers to make requests and send packets to other vaultkeepers.
// These subroutines are used to communicate with other nodes.
// Request payloads are encoded via protobuf.

type Hello struct {
	id orv.NodeID
}

type HelloAck struct {
	id      orv.NodeID
	height  uint16
	version byte // major+minor as a byte
}

// Hello sends a HELLO packet to the given address, returning the target node's response or an error.
func (vk *VaultKeeper) Hello(addrPort string, ctx context.Context) (response HelloAck, err error) {
	if !vk.alive.Load() {
		return HelloAck{}, ErrDead
	}

	// compose the body
	var body []byte
	reqBody := Hello{}
	// TODO serialize via protobuf

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

	// read the header
	respHeader := protocol.Header{}
	if err := respHeader.Deserialize(resp.Body()); err != nil {
		return HelloAck{}, err
	}
	// read the payload
	// TODO

}
