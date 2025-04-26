package orv

type PacketType = string

// connection initialization
const (
	// Sent by a node not part of the vault to introduce itself.
	// VKs respond to HELLOs with HELLO_ACK or not at all.
	// Requester nodes typically follow up with a JOIN or MERGE, but do not have to.
	//
	// All interactions must start with a HELLO.
	PT_HELLO PacketType = "HELLO"
	// Sent by VKs in response to a node's HELLO in order to relay basic information to the requester node.
	PT_HELLO_ACK PacketType = "HELLO_ACK"
)

// special commands that do not necessarily need to follow a HELLO
const (
	PT_STATUS PacketType = "STATUS"
)

// new node joining as leaf or VK
const (
	// Sent by a node not part of the vault to request to join under the receiver VK.
	PT_JOIN PacketType = "JOIN"
	// Sent by VKs in response to a node's JOIN request to accept the request.
	//
	// Once received by a node, that node can safely mark the VK as its parent.
	PT_JOIN_ACCEPT PacketType = "JOIN_ACCEPT"
	// Sent by VKs in response to a node's JOIN request to deny the request.
	//
	// Once received by a node, that node must resend a HELLO if it wishes to continue interacting with this VK.
	PT_JOIN_DENY PacketType = "JOIN_DENY"
)

// service registration
const (
	// Sent by a child node already part of a vault to tell its parent about a new service.
	PT_REGISTER PacketType = "REGISTER"
	// Sent by a parent VK to confirm registration of the service offered by the child.
	PT_REGISTER_ACCEPT PacketType = "REGISTER_ACCEPT"
)

// root-root merging
const (
	// Sent by a node to indicate that the VK should become one of its children.
	// Only used in root-root interactions.
	//
	// Must be followed up by a MERGE_ACCEPT to confirm.
	PT_MERGE PacketType = "MERGE"
	// Sent by a VK to accept a node's request to merge.
	// Only used in root-root interactions.
	//
	// Once received by the requester node, that node can safely consider itself to be the new root.
	// The requester node must then update its height and send an INCR to its pre-existing children.
	PT_MERGE_ACCEPT PacketType = "MERGE_ACCEPT"
	PT_INCR         PacketType = "INCR"
)
