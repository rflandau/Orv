// Package slims is the parent package of the Orv Slims implementation.
// It contains child packages vk (vault keeper implementation), proto (L5 Orv header handling), and client (static subroutines for interacting with a vault).
// Child packages are mostly self-contained, the Orv parent package provides the few shared utilities.
package slims

import (
	"github.com/plgd-dev/go-coap/v3/message"
)

// NodeID is the VK or Leaf's unique identifier
type NodeID = uint64

// ResponseMediaType returns the media type Orv uses by default.
// Getter function as we cannot const a custom type (even if it is a primitive).
func ResponseMediaType() message.MediaType { return message.AppOctets }

// MaxPacketSize specifies the buffer size used to hold UDP payloads.
// UDP can theoretically support payloads nearing 65535 bytes.
// Thankfully for our allocator, Orv packets should easily fit in a thousand bytes.
// The specific maximum is set arbitrarily and can be changed arbitrarily (at the cost of requiring more heap memory for packet processing).
const MaxPacketSize uint16 = 2024
