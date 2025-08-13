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
package proto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
)

// FixedHeaderLen is the length (in bytes) of the Orv fixed header.
const FixedHeaderLen uint = 5

// A Header represents a deconstructed Orv packet header.
// Use SerializeTo()/SerializeFrom().
type Header struct {
	Version  Version
	HopLimit uint8
	// length of the payload.
	// Must be <= (65535-FixedHeaderLen)
	PayloadLength uint16
	Type          MessageType
}

var (
	// ErrInvalidHopLimit means that the given hop limit was out of bounds.
	ErrInvalidHopLimit      = errors.New("HopLimit must be 1 <= x <= 255")
	ErrInvalidPayloadLength = errors.New("PayloadLength must be 0 <= x <= " + strconv.FormatUint(uint64(65535-FixedHeaderLen), 10) + " (uint16MAX - length of the fixed header)")
)

// SerializeTo consumes the data required to compose an Orv header and returns a network-order byte string.
// Also validates that the header has all required fields.
func (hdr *Header) SerializeTo() ([]byte, error) {
	if hdr.HopLimit < 1 {
		return nil, ErrInvalidHopLimit
	}

	buf := make([]byte, FixedHeaderLen)
	if count, err := binary.Encode(buf, binary.BigEndian,
		[]byte{hdr.Version.Byte(), hdr.HopLimit, byte(hdr.PayloadLength >> 8), byte(hdr.PayloadLength), hdr.Type}); err != nil {
		return nil, err
	} else if count != int(FixedHeaderLen) {
		var moreOrFewer = "fewer"
		if count > int(FixedHeaderLen) {
			moreOrFewer = "more"
		}
		return nil, fmt.Errorf("encoded %s bytes (%d) than expected (%d)", moreOrFewer, count, FixedHeaderLen)
	}
	return buf, nil
}

// SerializeFrom attempts to populate the header's fields from the given byte array.
// SerializeFrom does not drain rd.
// Clobbers any existing data.
func (hdr *Header) SerializeFrom(rd *bytes.Reader) (err error) {
	{ // Version
		b, err := rd.ReadByte()
		if err != nil {
			return err
		}
		hdr.Version = VersionFromByte(b)
	}

	// Hop Limit
	if hdr.HopLimit, err = rd.ReadByte(); err != nil {
		return err
	}

	return nil
}

//#region MessageType

// MessageType enumerates the message types and the integers used to represent them in the Type field.
type MessageType = uint8

const (
	Hello MessageType = iota
	HelloAck

	Join
	JoinAccept
	JoinDeny

	Register
	RegisterAccept
	RegisterDeny

	Merge
	MergeAccept
	Increment
	IncrementACK

	ServiceHeartbeat
	ServiceHeartbeatAck
	ServiceHeartbeatFault

	VKHeartbeat
	VKHeartbeatAck
	VKHeartbeatFault
)

//#endregion MessageType
