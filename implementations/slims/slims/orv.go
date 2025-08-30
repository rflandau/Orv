// Package slims is the parent package of the Orv Slims implementation.
// It contains child packages vk (vault keeper implementation), proto (L5 Orv header handling), and client (static subroutines for interacting with a vault).
// Child packages are mostly self-contained, the Orv parent package provides the few shared utilities.
package slims

// NodeID is the VK or Leaf's unique identifier
type NodeID = uint64

// MaxPacketSize specifies the buffer size used to hold UDP payloads.
// UDP can theoretically support payloads nearing 65535 bytes, but 1500B is a common MTU and Slims assumes that each Orv message fits into a single packet.
// Thankfully for our allocator, Orv packets should easily fit in a thousand bytes, if not much less.
// The specific maximum is set arbitrarily and can be changed arbitrarily (at the cost of requiring more heap memory for packet processing and a greater MTU for transmission).
const MaxPacketSize uint16 = 1024
