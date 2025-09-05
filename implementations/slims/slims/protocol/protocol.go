/*
Package protocol contains tools for interacting with the L5 orv header and sending Orv packets.

Includes structs that can be composed into a fixed header; you should never have to interact with the raw bits or endian-ness of the header.
*/
package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/rflandau/Orv/implementations/slims/slims"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/mt"
	"github.com/rflandau/Orv/implementations/slims/slims/protocol/version"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

const (
	// LongHeaderLen is the length (in bytes) of the full Orv header (when !Shorthand).
	LongHeaderLen uint8 = 10
	// ShortHeaderLen is the length (in bytes) of the Orv header when shorthand is set.
	ShortHeaderLen uint8 = 2
)

var versions = version.NewSet(version.Version{Major: 1, Minor: 0})

// SupportedVersions returns the set of Orv versions supported by this library.
// Currently pretty redundant.
func SupportedVersions() version.Set {
	return versions
}

// A Header represents a deconstructed Orv packet header.
// The state of Header is never guaranteed; call .Validate() to verify before using.
type Header struct {
	// Version of Orv Slims this message intends to use.
	// As a requestor, this is typically the version you want to use/the version previously agreed upon via HELLO.
	// As a sender, this typically echos the version sent to you (if you support that version).
	Version version.Version
	// Does this packet omit the ID field?
	Shorthand bool
	// Type of message.
	// See PACKETS.md for the available message types.
	Type mt.MessageType
	// Unique identifier of the sender. May be anything that fits into 8B, but is treated as an unsigned int in this implementation.
	// Ignored if Shorthand is set.
	ID slims.NodeID
}

//#region errors

var (
	ErrInvalidVersionMajor = errors.New("major version must be 0 <= x <= 15")
	ErrInvalidVersionMinor = errors.New("minor version must be 0 <= x <= 15")
	ErrInvalidMessageType  = errors.New("message type must be representable with 7 bits, must not be 0, and must be an enumerated packet type")
	// both ID and shorthand were set, which will cause ID to be ignored
	ErrShorthandID = errors.New("ID is ignored when shorthand is set")
	// a nil connection was given as a parameter
	ErrConnIsNil = errors.New("PacketConn is nil")
)

//#endregion errors

// Serialize returns an Orv header in network-byte-order.
//
// NOTE: Does NOT imply .Validate() and thus does NOT error on invalid data.
// Serialize can formulate an invalid packet if given bad data; it is the caller's responsibility to guarantee header's fields.
//
// Performs a single allocation of ShortHeaderLen or LongHeaderLen (depending on hdr.Shorthand).
func (hdr Header) Serialize() ([]byte, error) {
	// prep output buffer and build the input data
	var (
		out, data []byte
		bufLen    uint8
	)
	if hdr.Shorthand {
		bufLen = ShortHeaderLen
		out = make([]byte, bufLen)
		data = []byte{
			hdr.Version.Byte(),
			0b10000000 | byte(hdr.Type),
		}
	} else {
		bufLen = LongHeaderLen
		out = make([]byte, bufLen)
		data = []byte{
			hdr.Version.Byte(),
			0b01111111 & byte(hdr.Type),
			byte(hdr.ID >> 56),
			byte(hdr.ID >> 48),
			byte(hdr.ID >> 40),
			byte(hdr.ID >> 32),
			byte(hdr.ID >> 24),
			byte(hdr.ID >> 16),
			byte(hdr.ID >> 8),
			byte(hdr.ID),
		}
	}

	// compose and error check
	if count, err := binary.Encode(out, binary.BigEndian, data); err != nil {
		return nil, err
	} else if count != int(bufLen) {
		var moreOrFewer = "fewer"
		if count > int(bufLen) {
			moreOrFewer = "more"
		}
		return nil, fmt.Errorf("encoded %s bytes (%d) than expected (%d)", moreOrFewer, count, bufLen)
	}
	return out, nil
}

// Validate tests each field in header, returning a list of issues.
func (hdr Header) Validate() (errors []error) {
	// Version
	if hdr.Version.Major > 15 {
		errors = append(errors, ErrInvalidVersionMajor)
	}
	if hdr.Version.Minor > 15 {
		errors = append(errors, ErrInvalidVersionMinor)
	}
	// Type
	if hdr.Type.String() == "UNKNOWN" || hdr.Type == 0 { // type only has 7 bits available
		errors = append(errors, ErrInvalidMessageType)
	}
	// Shorthand+ID
	if hdr.Shorthand && hdr.ID != 0 {
		errors = append(errors, ErrShorthandID)
	}

	return errors
}

// Zerolog attaches header's fields to the given log event.
// Intended to be given to *zerolog.Event.Func().
func (hdr *Header) Zerolog(ev *zerolog.Event) {
	ev.Str("version", fmt.Sprintf("%d.%d", hdr.Version.Major, hdr.Version.Minor)).
		Bool("shorthand", hdr.Shorthand).
		Str("type", hdr.Type.String())
	if !hdr.Shorthand {
		ev.Uint64("ID", hdr.ID)
	}
}

// Serialize consumes the given header+body and returns it in a wire-ready format.
// id will be ignored if shorthand is set.
//
// Does not validate the header, the payload, or that the combination is valid.
//
// If you just want a header, use header.Serialize().
func Serialize(v version.Version, shorthand bool, typ mt.MessageType, id slims.NodeID, payload proto.Message) ([]byte, error) {
	// generate header
	hdrB, err := Header{Version: v, Shorthand: shorthand, Type: typ, ID: id}.Serialize()
	if err != nil {
		return nil, err
	} else if payload == nil {
		return hdrB, nil
	}
	// generate body
	payloadB, err := proto.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append(hdrB, payloadB...), nil

}

// Deserialize greedily populates hdr's fields from the given reader.
// Reads 2 bytes if Shorthand, 12 otherwise.
// If an error occurs, hdr will be left in a partially-set state which is considered undefined.
//
// ! Does NOT validate fields. Does not drain rd. A payload may still be in rd; Deserialize only handles the header.
func Deserialize(rd io.Reader) (hdr Header, err error) {
	// set up a function to peel data byte by byte
	var buf = make([]byte, 1)
	readByte := func() (b byte, done bool, err error) {
		if read, err := rd.Read(buf); err != nil {
			if errors.Is(err, io.EOF) { // short read
				return 0, true, nil
			}
			return 0, false, err // error occurred
		} else if read != 1 { // short read
			return 0, true, nil
		}
		return buf[0], false, nil // success
	}

	// Version
	if b, done, err := readByte(); err != nil {
		return hdr, err
	} else if done {
		return hdr, errors.New("short read on byte 1 (Version)")
	} else {
		hdr.Version = version.FromByte(b)
	}

	// Shorthand and Message Type
	if b, done, err := readByte(); err != nil {
		return hdr, err
	} else if done {
		return hdr, errors.New("short read on byte 2 (composite of Shorthand and Type)")
	} else {
		hdr.Shorthand = (b & 0b10000000) != 0
		hdr.Type = mt.MessageType(b & 0b01111111)
	}

	// ID (only if !shorthand)
	if !hdr.Shorthand {
		for i := range 8 {
			if b, done, err := readByte(); err != nil {
				return hdr, err
			} else if done {
				return hdr, fmt.Errorf("short read on byte %d (within ID bytes)", i+2)
			} else {
				hdr.ID = hdr.ID | (uint64(b) << ((7 - i) * 8))
			}
		}

	}

	return hdr, nil
}

// DeserializeWithBody operates the same as Deserialize, but also drains the reader into a byte array and trims off null bytes.
func DeserializeWithBody(rd io.Reader) (hdr Header, nBody int, body []byte, err error) {
	hdr, err = Deserialize(rd)
	if err != nil {
		return Header{}, 0, nil, err
	}
	var bd []byte
	n, err := rd.Read(bd)
	if err != nil {
		return Header{}, 0, nil, err
	} else {
		bd = bd[:n]
	}
	return hdr, n, bd, nil
}

// ReceivePacket reads from the given connection, unmarshals the prefix into a header, and returns the rest as a body.
// The context can be used to cancel or timeout the read. If this occurs, all values will be zero except for err, which will equal ctx.Err().
//
// ! pconn's read deadline is destructively set according to the given context and NOT reset on exit.
// If the given context has a deadline, it will supplant pconn's read deadline.
// If the given context does not have a deadline, pconn's read deadline will be removed.
func ReceivePacket(pconn net.PacketConn, ctx context.Context) (n int, origAddr net.Addr, hdr Header, body []byte, err error) {
	// validate params
	if pconn == nil {
		return 0, nil, Header{}, nil, ErrConnIsNil
	} else if ctx == nil {
		return 0, nil, Header{}, nil, slims.ErrNilCtx
	} else if err := ctx.Err(); err != nil {
		return 0, nil, Header{}, nil, err
	}
	// set a deadline (if one was given)
	if ddl, set := ctx.Deadline(); set {
		if err := pconn.SetReadDeadline(ddl); err != nil {
			return 0, nil, Header{}, nil, err
		}
	} else {
		pconn.SetReadDeadline(time.Time{})
	}

	// spin up a channel to receive the packet when it arrives over the connection
	pktCh := make(chan struct {
		n    int
		buf  []byte
		addr net.Addr
		err  error
	}, 1) // buffer the channel so our goro doesn't block if cancelled prior to the read

	go func() { // read the next packet and send it along our channel
		var buf = make([]byte, slims.MaxPacketSize)
		n, senderAddr, err := pconn.ReadFrom(buf)
		if err == nil {
			buf = buf[:n] // trim off excess capacity
		}

		pktCh <- struct {
			n    int
			buf  []byte
			addr net.Addr
			err  error
		}{n, buf, senderAddr, err}
	}()

	// await cancellation or the packet
	select {
	case <-ctx.Done():
		// ensure the read is cancelled
		pconn.SetReadDeadline(time.Now())
		return 0, nil, Header{}, nil, ctx.Err()
	case pkt := <-pktCh:
		// check result
		if pkt.err != nil {
			return 0, nil, Header{}, nil, pkt.err
		}

		var rd = bytes.NewBuffer(pkt.buf)
		hdr, err = Deserialize(rd)
		if err != nil {
			return n, nil, Header{}, nil, err
		}
		return pkt.n, pkt.addr, hdr, rd.Bytes(), nil
	}
}

// WritePacket generates and sends a packet via pconn, to the pre-connected address.
func WritePacket(ctx context.Context, pconn *net.UDPConn, hdr Header, payload proto.Message) (n int, _ error) {
	if ctx == nil {
		return 0, slims.ErrNilCtx
	}

	// clear out any existing deadline and ensure we do the same on exit
	if err := pconn.SetWriteDeadline(time.Time{}); err != nil {
		return 0, err
	}
	defer pconn.SetWriteDeadline(time.Time{})

	// compose the packet
	pkt, err := Serialize(hdr.Version, hdr.Shorthand, hdr.Type, hdr.ID, payload)
	if err != nil {
		return 0, err
	}

	// spin up a channel to receive the write results
	resCh := make(chan struct {
		n   int
		err error
	}, 1) // buffer the channel so we do not leak the goroutine
	go func() {
		n, err := pconn.Write(pkt) // use the pre-connected address
		resCh <- struct {
			n   int
			err error
		}{n, err}
		close(resCh)
	}()

	select {
	case <-ctx.Done(): //handle if the context is cancelled
		pconn.SetWriteDeadline(time.Now())
		return 0, ctx.Err()
	case res := <-resCh:
		return res.n, res.err
	}
}
