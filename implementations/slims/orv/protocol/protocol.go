/*
Package proto contains tools for interacting with the L5 orv header.

Includes structs that can be composed into a fixed header; you should never have to interact with the raw bits or endian-ness of the header.

	    0               1               2               3
	    0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7

		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		| Major | Minor |   Hop Limit   |    Total Length in Octets   |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|     Type      |
		+-+-+-+-+-+-+-+-+
*/
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/rflandau/Orv/implementations/slims/orv/protocol/mt"
	"github.com/rs/zerolog"
)

const (
	// FixedHeaderLen is the length (in bytes) of the Orv fixed header.
	FixedHeaderLen   uint   = 5
	MaxPayloadLength uint16 = math.MaxUint16 - uint16(FixedHeaderLen)
)

// A Header represents a deconstructed Orv packet header.
// The state of Header is never guaranteed; call .Validate() to verify before using..
type Header struct {
	// REQUIRED.
	// The highest major and minor version of Orv the sender speaks.
	Version Version
	// The maximum number of hops this packet may traverse before being dropped.
	// Defaults to DefaultHopLimit.
	HopLimit uint8
	// Length of the payload in bytes.
	// Must be <= (65535-FixedHeaderLen).
	// Defaults to zero, thus telling the receiver not to scan any data after the header.
	PayloadLength uint16
	// REQUIRED.
	// Type of message.
	// See PACKETS.md for the available message types.
	Type mt.MessageType
}

//#region errors

var (
	ErrInvalidVersionMajor = errors.New("major version must be 0 <= x <= 15")
	ErrInvalidVersionMinor = errors.New("minor version must be 0 <= x <= 15")
	// ErrInvalidHopLimit means that the given hop limit was out of bounds.
	ErrInvalidPayloadLength = errors.New("payload length must be 0 <= x <= " + strconv.FormatUint(uint64(65535-FixedHeaderLen), 10) + " (uint16MAX - length of the fixed header)")
	ErrUnknownMessageType   = errors.New("type must be a valid MessageType")
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
	data := []byte{hdr.Version.Byte(), hdr.HopLimit, byte(hdr.PayloadLength >> 8), byte(hdr.PayloadLength), byte(hdr.Type)}

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

// Deserialize greedily populates hdr's fields from the given byte array.
// Clobbers existing data.
//
// Returns when rd is empty or we have read FixedHeaderLen bytes, whichever is first.
// Does NOT validate fields. Does not drain rd.
// Swallows EOF errors and short reads.
func (hdr *Header) Deserialize(rd io.Reader) (err error) {
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

	{ // Version
		b, done, err := readByte()
		if done || err != nil {
			return err
		}
		hdr.Version = VersionFromByte(b)
	}
	{ // Hop Limit
		b, done, err := readByte()
		if done || err != nil {
			return err
		}
		hdr.HopLimit = b
	}
	{ // Payload Length
		MSB, done, err := readByte()
		if done || err != nil {
			return err
		}
		LSB, done, err := readByte()
		if done || err != nil {
			return err
		}
		hdr.PayloadLength = uint16(MSB)<<8 | uint16(LSB)
	}
	{ // Message Type
		b, done, err := readByte()
		if done || err != nil {
			return err
		}
		hdr.Type = mt.MessageType(b)
	}

	return nil
}

// Validate tests each field in header, returning a list of issues.
func (hdr *Header) Validate() (errors []error) {
	if hdr.Version.Major > 15 {
		errors = append(errors, ErrInvalidVersionMajor)
	}
	if hdr.Version.Minor > 15 {
		errors = append(errors, ErrInvalidVersionMinor)
	}
	if hdr.PayloadLength > (math.MaxUint16 - uint16(FixedHeaderLen)) {
		errors = append(errors, ErrInvalidPayloadLength)
	}
	if hdr.Type == mt.UNKNOWN || hdr.Type.String() == "UNKNOWN" {
		errors = append(errors, ErrUnknownMessageType)
	}

	return errors
}

// Zerolog attaches header's fields to the given log event.
// The given event is returned so it can be chained.
func (hdr *Header) Zerolog(ev *zerolog.Event) *zerolog.Event {
	ev.Str("version", fmt.Sprintf("%d.%d", hdr.Version.Major, hdr.Version.Minor)).
		Uint8("hop limit", hdr.HopLimit).
		Uint16("payload length", hdr.PayloadLength).
		Str("type", hdr.Type.String())
	return ev
}

//#region MessageType
