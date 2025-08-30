// Package version provides mechanisms for tracking which versions are supported by a node.
// Sets represent all available versions while Version represents a single instance of major+minor that can be compared against a set to determine support.
package version

import (
	"errors"
	"slices"
)

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

// New returns a version struct after ensuring that major and minor can each fit into a nibble.
func New(major, minor uint8) (Version, error) {
	if major > 15 {
		return Version{}, errors.New("major must be able to fit into a nibble (4 bits, <= 0xF)")
	} else if minor > 15 {
		return Version{}, errors.New("minor must be able to fit into a nibble (4 bits, <= 0xF)")
	}

	return Version{major, minor}, nil
}

// FromByte splits the byte into two nibbles, setting them to major and minor respectively.
func FromByte(b byte) Version {
	return Version{b >> 4, b & 0b00001111}
}

// A Set is a collection of versions.
// Typically denotes the versions supported by something (like the versions of Orv a VK can support).
// Sets are not thread-safe.
type Set struct {
	all     map[uint8][]uint8 // major -> minors
	highest Version
}

// NewSet catalogs the given versions, making them rapidly accessible from outside the set.
func NewSet(vs ...Version) Set {
	s := Set{all: make(map[uint8][]uint8)}
	for _, v := range vs {
		// if this is a new major, alloc space for it
		if _, found := s.all[v.Major]; !found {
			s.all[v.Major] = []byte{v.Minor}
		} else {
			s.all[v.Major] = append(s.all[v.Major], v.Minor)
		}
		// if this is a new highest, save it
		if (v.Major > s.highest.Major) || (v.Major == s.highest.Major && v.Minor > s.highest.Minor) {
			s.highest = v
		}
	}
	return s
}

// Supports returns if the given version is supported by this Set.
func (vs Set) Supports(v Version) bool {
	if minors, found := vs.all[v.Major]; found {
		return slices.Contains(minors, v.Minor)
	}
	return false
}

// HighestSupported returns the highest version supported by this Set.
func (vs Set) HighestSupported() Version {
	return vs.highest
}

// AsBytes returns all versions supported by this set with each represented as a single byte.
func (vs Set) AsBytes() []byte {
	var versionsAsBytes []byte
	for major, minors := range vs.all {
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
