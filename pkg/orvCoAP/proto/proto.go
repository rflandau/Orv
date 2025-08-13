// Package proto contains tools for interacting with the L5 orv header.
// Includes structs that can be composed into a fixed header; you should never have to interact with the raw bits or endian-ness of the header.
package proto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// FixedHeaderLen is the length (in bytes) of the Orv fixed header.
const FixedHeaderLen uint = 2

type Header struct {
	Version  Version
	HopLimit uint8
}

// ErrInvalidHopLimit means that the given hop limit was out of bounds.
var ErrInvalidHopLimit = errors.New("HopLimit must be 1 <= x <= 255 ")

// SerializeTo consumes the data required to compose an Orv header and returns a network-order byte string.
// Also validates that header has all required fields.
func (hdr *Header) SerializeTo() ([]byte, error) {
	if hdr.HopLimit < 1 {
		return nil, ErrInvalidHopLimit
	}

	buf := make([]byte, FixedHeaderLen)
	if count, err := binary.Encode(buf, binary.BigEndian, []byte{hdr.Version.Byte(), hdr.HopLimit}); err != nil {
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

//#region FieldOrder

type FieldOrder = uint

const (
	VersionMajorOrder FieldOrder = iota
	VersionMinorOrder
	HopLimitFieldOrder
)

//#endregion FieldOrder

type FieldToByte uint

const (
	VersionMajorByte FieldToByte = 0
	VersionMinorByte FieldToByte = 0
	HopLimitByte     FieldToByte = 1
)
