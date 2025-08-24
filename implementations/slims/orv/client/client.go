package orv

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/rflandau/Orv/implementations/slims/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol"
)

// The client file provides static subroutines for interacting with a Vault Keeper.
// These can be called from any client, be they a leaf, VK, or neither.

// Status sends a STATUS packet to the given address. Blocks until a response is received or fails.
// vkAddr is the address and port to send the request to (of the form <IP>:<port>).
// ctx is passed to the dialer, enabling timeouts.
//
// The response is unmarshaled if it can be; an error is returned otherwise.
// The raw response is returned in any case.
func Status(vkAddr string, ctx context.Context) (sr orv.StatusResponse, rawJSON []byte, _ error) {
	// generate a request header
	hdr := protocol.Header{
		Version:       protocol.HighestSupported,
		HopLimit:      1,
		PayloadLength: 0,
		Type:          protocol.Status,
	}
	hdrSrl, err := hdr.Serialize()
	if err != nil {
		return orv.StatusResponse{}, nil, err
	}

	// submit request
	conn, err := udp.Dial(vkAddr)
	if err != nil {
		return orv.StatusResponse{}, nil, err
	}
	resp, err := conn.Post(ctx, "/", message.AppOctets, bytes.NewReader(hdrSrl))
	if err != nil {
		return orv.StatusResponse{}, nil, err
	}
	raw, err := resp.ReadBody()
	if err != nil {
		return orv.StatusResponse{}, nil, err
	}
	rd := bytes.NewReader(raw)
	// stream the Orv header
	respHdr := protocol.Header{}
	if err := respHdr.Deserialize(rd); err != nil {
		return orv.StatusResponse{}, nil, err
	}

	// the remainder should be the payload as JSON
	dc := json.NewDecoder(rd)
	err = dc.Decode(&sr)
	// assuming we parsed the Orv header properly, we should be able to drop the first X bytes
	return sr, raw[protocol.FixedHeaderLen:], err
}
