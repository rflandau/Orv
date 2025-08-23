// Package orv is the parent package of the Orv implementation.
// It contains child packages vk (vault keeper implementation), proto (L5 Orv header handling), and client (static subroutines for interacting with a vault).
// Child packages are mostly self-contained, the Orv parent package provides the few shared utilities.
package orv

import (
	"github.com/plgd-dev/go-coap/v3/message"
)

// VK or Leaf's unique identifier
type NodeID = uint64

// StatusResponse is the information sent in response to a STATUS packet.
type StatusResponse struct {
	// ID of the VK responding to the Status
	ID     NodeID `json:"id,omitempty"`
	Height uint16 `json:"height,omitempty"`
	// packed bytes; MSN is major, LSN is minor
	VersionsSupported []byte `json:"versions_supported,omitempty"`
	// TODO
}

// ResponseMediaType returns the media type Orv uses by default.
// Getter function as we cannot const a custom type (even if it is a primitive).
func ResponseMediaType() message.MediaType { return message.AppOctets }
