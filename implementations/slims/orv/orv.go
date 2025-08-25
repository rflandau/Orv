// Package orv is the parent package of the Orv implementation.
// It contains child packages vk (vault keeper implementation), proto (L5 Orv header handling), and client (static subroutines for interacting with a vault).
// Child packages are mostly self-contained, the Orv parent package provides the few shared utilities.
package orv

import (
	"github.com/plgd-dev/go-coap/v3/message"
)

// VK or Leaf's unique identifier
type NodeID = uint64

// ResponseMediaType returns the media type Orv uses by default.
// Getter function as we cannot const a custom type (even if it is a primitive).
func ResponseMediaType() message.MediaType { return message.AppOctets }
