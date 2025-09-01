// Package misc is a placeholder package for internal utilities that are shared across packages, but do not have any shared characteristics.
package misc

import (
	"math"
	"math/rand/v2"
)

// RandomPort returns a random port number from 1024 - 65535
func RandomPort() uint16 {
	return uint16(1024 + rand.Uint32N((math.MaxUint16 - 1024)))
}
