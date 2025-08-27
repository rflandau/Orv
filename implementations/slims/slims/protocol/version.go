package protocol

import (
	"errors"
	"slices"
)

var (
	// major -> minors
	supportedVersions = map[uint8][]uint8{
		1: {2},
	}
	// the highest version contained within the list of all supported versions
	HighestSupported Version = Version{1, 2}
)

// IsVersionSupported returns if the given version is known to be supported by the current implementation.
func IsVersionSupported(v Version) bool {
	minors := supportedVersions[v.Major]
	return slices.Contains(minors, v.Minor)
}

// VersionsSupported lists all Orv versions currently supported (which is just 1.2 atm).
// Returns a mapping from major version to the minor versions it supports.
// Ex: 1 -> [1, 2] implies that we support versions 1.1 and 1.2.
func VersionsSupported() map[uint8][]uint8 {
	return supportedVersions
}

// VersionsSupportedAsBytes returns an array of packed bytes, one bye for each supported <major>.<minor>.
// Versions are sorted from highest to lowest.
// Each byte can be unpacked via proto.VersionFromByte().
func VersionsSupportedAsBytes() []byte {
	var versionsAsBytes []byte
	for major, minors := range supportedVersions {
		// corral minor versions of current major
		var v = make([]byte, len(minors))
		for i := range minors {
			version := Version{major, minors[i]}
			v[i] = version.Byte()
		}
		versionsAsBytes = append(versionsAsBytes, v...)
	}

	slices.SortStableFunc(versionsAsBytes, func(a byte, b byte) int {
		if a < b {
			return 1
		} else if a > b {
			return -1
		}
		return 0
	})
	return versionsAsBytes
}

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
func (v Version) Byte() byte {
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
