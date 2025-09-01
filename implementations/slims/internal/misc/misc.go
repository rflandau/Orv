package misc

import (
	"math"
	"math/rand/v2"
)

func RandomPort() uint16 {
	return uint16(rand.Uint32N(math.MaxUint16))
}
