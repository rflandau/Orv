package orv

import (
	"bytes"
	"context"

	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/udp"
	"github.com/rflandau/Orv/implementations/slims/slims/pb"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"google.golang.org/protobuf/proto"
)

// The client file provides static subroutines for interacting with a Vault Keeper.
// These can be called from any client, be they a leaf, VK, or neither.

// Status sends a STATUS packet to the given address. Blocks until a response is received or fails.
// vkAddr is the address and port to send the request to (of the form <IP>:<port>).
// ctx is passed to the dialer, enabling timeouts.
//
// The response is unmarshaled if it can be; an error is returned otherwise.
// The raw response is returned in any case.
func Status(vkAddr string, ctx context.Context) (*pb.StatusResp, error) {
	// generate a request header
	hdr := protocol.Header{
		Version:   protocol.HighestSupported,
		Shorthand: true,
		Type:      mt.Status,
	}
	hdrSrl, err := hdr.Serialize()
	if err != nil {
		return nil, err
	}

	// submit request
	conn, err := udp.Dial(vkAddr)
	if err != nil {
		return nil, err
	}
	resp, err := conn.Post(ctx, "/", message.AppOctets, bytes.NewReader(hdrSrl))
	if err != nil {
		return nil, err
	}
	raw, err := resp.ReadBody()
	if err != nil {
		return nil, err
	}

	rd := bytes.NewReader(raw)
	// stream the Orv header
	respHdr := protocol.Header{}
	if err := respHdr.Deserialize(rd); err != nil {
		return nil, err
	}

	var sr pb.StatusResp
	if err := proto.Unmarshal(raw[protocol.LongHeaderLen:], &sr); err != nil {
		return nil, err
	}
	// assuming we parsed the Orv header properly, we should be able to drop the first X bytes
	return &sr, err
}
