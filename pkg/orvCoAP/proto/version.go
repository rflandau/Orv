package proto

import "errors"

// Version represents the Orv version number this node speaks.
type Version struct {
	// Orv version minor number.
	// Only 4 least-significant bits are used.
	Major uint8
	// Orv version minor number.
	// Only 4 least-significant bits are used.
	Minor uint8
}

// Byte returns Orv version as a single byte.
// The most-significant nibble is major; the rest are minor.
// Ex: 0b 0010 0001 equates to version 2.1
//
// Data beyond the LS nibble of each field is dropped.
func (v Version) Byte() uint8 {
	return v.Major<<4 | v.Minor&0b00001111
}

// NewVersion returns a version struct after ensuring that major and minor can each fit into a nibble.
func NewVersion(major, minor uint8) (Version, error) {
	if major > 15 {
		return Version{}, errors.New("major must be able to fit into a nibble (4 bits, <= 0xF)")
	} else if minor > 15 {
		return Version{}, errors.New("minor must be able to fit into a nibble (4 bits, <= 0xF)")
	}

	return Version{major, minor}, nil
}

// VersionFromByte splits the byte into two nibbles, setting them to major and minor respectively.
func VersionFromByte(b byte) Version {
	return Version{b >> 4, b & 0b00001111}
}
