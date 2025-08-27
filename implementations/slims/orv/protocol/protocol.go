/*
Package protocol contains tools for interacting with the L5 orv header.

Includes structs that can be composed into a fixed header; you should never have to interact with the raw bits or endian-ness of the header.
*/
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/rflandau/Orv/implementations/slims/orv"
	"github.com/rflandau/Orv/implementations/slims/orv/protocol/mt"
	"github.com/rs/zerolog"
)

const (
	// FixedHeaderLen is the length (in bytes) of the Orv fixed header.
	FixedHeaderLen uint = 12
	// the maximum length the payload can be to fit into a single UDP datagram (without IPv6's jumbograms).
	MaxPayloadLength uint16 = math.MaxUint16 - uint16(FixedHeaderLen)
)

// A Header represents a deconstructed Orv packet header.
// The state of Header is never guaranteed; call .Validate() to verify before using..
type Header struct {
	// Version of Orv Slims this message intends to use.
	Version Version
	// Does this packet omit the ID field?
	Shorthand bool
	// Type of message.
	// See PACKETS.md for the available message types.
	Type mt.MessageType
	// Unique identifier of the sender. May be anything that fits into 8B, but is treated as an unsigned int in this implementation.
	// Ignored if Shorthand is set.
	ID orv.NodeID
}

//#region errors

var (
	ErrInvalidVersionMajor = errors.New("major version must be 0 <= x <= 15")
	ErrInvalidVersionMinor = errors.New("minor version must be 0 <= x <= 15")
	ErrInvalidMessageType  = errors.New("message type must be representable with 7 bits and must not be 0")
	ErrShorthandID         = errors.New("ID is ignored when shorthand is set")
)

//#endregion errors

// Serialize returns an Orv header in network-byte-order.
//
// NOTE: Does NOT imply .Validate() and thus does NOT error on invalid data.
// Serialize can formulate an invalid packet if given bad data; it is the caller's responsibility to guarantee header's fields.
//
// Performs a single allocation of FixedHeaderLen size.
func (hdr *Header) Serialize() ([]byte, error) {
	out := make([]byte, FixedHeaderLen)

	var data []byte
	if hdr.Shorthand {
		data = []byte{
			hdr.Version.Byte(),
			0b10000000 | byte(hdr.Type),
		}
	} else {
		data = []byte{
			hdr.Version.Byte(),
			0b00000000 | byte(hdr.Type),
			byte(hdr.ID & 0xFF00000000000000),
			byte(hdr.ID & 0x00FF000000000000),
			byte(hdr.ID & 0x0000FF0000000000),
			byte(hdr.ID & 0x000000FF00000000),
			byte(hdr.ID & 0x00000000FF000000),
			byte(hdr.ID & 0x0000000000FF0000),
			byte(hdr.ID & 0x000000000000FF00),
			byte(hdr.ID & 0x00000000000000FF),
		}
	}

	// compose data into out
	if count, err := binary.Encode(out, binary.BigEndian, data); err != nil {
		return nil, err
	} else if count != int(FixedHeaderLen) {
		var moreOrFewer = "fewer"
		if count > int(FixedHeaderLen) {
			moreOrFewer = "more"
		}
		return nil, fmt.Errorf("encoded %s bytes (%d) than expected (%d)", moreOrFewer, count, FixedHeaderLen)
	}
	return out, nil
}

// Deserialize populates hdr's fields from the given reader.
// Reads 2 bytes if Shorthand, 12 otherwise.
// Clobbers existing data.
//
// Does NOT validate fields. Does not drain rd.
//
// If an error occurs, hdr will not be altered and its state is considered undefined.
func (hdr *Header) Deserialize(rd io.Reader) (err error) {
	// set up a function to peel off a byte at a time
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
		return err
	} else if done {
		return errors.New("short read on byte 1 (Version)")
	} else {
		hdr.Version = VersionFromByte(b)
	}

	// Shorthand and Message Type
	if b, done, err := readByte(); err != nil {
		return err
	} else if done {
		return errors.New("short read on byte 2 (composite of Shorthand and Type)")
	} else {
		hdr.Shorthand = (b & 0b10000000) == 1
		hdr.Type = mt.MessageType(b & 0b01111111)
	}

	// ID (only if !shorthand)
	if !hdr.Shorthand {
		for i := range 8 {
			if b, done, err := readByte(); err != nil {
				return err
			} else if done {
				return fmt.Errorf("short read on byte %d (within ID bytes)", i+2)
			} else {
				hdr.ID = hdr.ID | (uint64(b) << ((7 - i) * 8))
			}
		}

	}

	return nil
}

// Validate tests each field in header, returning a list of issues.
func (hdr *Header) Validate() (errors []error) {
	// Version
	if hdr.Version.Major > 15 {
		errors = append(errors, ErrInvalidVersionMajor)
	}
	if hdr.Version.Minor > 15 {
		errors = append(errors, ErrInvalidVersionMinor)
	}
	// Type
	if hdr.Type > 127 || hdr.Type == 0 { // type only has 7 bits available
		errors = append(errors, ErrInvalidMessageType)
	}
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
