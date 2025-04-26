package orv

// Representations of each packet type used by Orv along with their functions and related packets.
// As the prototype is implemented as an API, these packet types are somewhat secondary to the go structs.
// For consistency's sake, every request and response issued by the API includes the corresponding packet type as a http header.
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

const (
	// Sent by a child node to refresh the lifetimes of all services named in the HB.
	PT_HEARTBEAT PacketType = "HEARTBEAT"
)

// special commands that do not necessarily need to follow a HELLO
const (
	PT_STATUS          PacketType = "STATUS"
	PT_STATUS_RESPONSE PacketType = "STATUS_RESPONSE"
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
	// Initially proc'd by a new service at a leaf or VK, the REGISTER echoes up the tree until it has reached root.
	// Echoing responsibility falls to each parent VK to pass the message iteratively.
	PT_REGISTER PacketType = "REGISTER"
	// Sent by a parent VK to confirm registration of the service offered by the child.
	PT_REGISTER_ACCEPT PacketType = "REGISTER_ACCEPT"
)

// service requests
const (
	// Sent by a child node to learn what services are available.
	// Use hop count to enforce locality. A hop count of 1 means the request will only query the node's immediate parent. Hop count is limited by vault height.
	// While a LIST with a hop count of 0 is technically an error, hop counts of 0 and 1 are treated the same.
	PT_LIST PacketType = "LIST"
	// Sent by a VK when it receives a LIST request to acknowledge it while the VK propagates it up the vault.
	// Only sent if the hop count is greater than 1 when it was received (because otherwise it will be decremented to 0 and answered by LIST_RESPONSE.
	PT_LIST_ACK PacketType = "LIST_ACK"
	// Sent by a VK when it receives a LIST request and the hop count decrements to 0 OR it is the root of the vault.
	PT_LIST_RESPONSE PacketType = "LIST_RESPONSE"
	// TODO
	PT_GET PacketType = "GET"
	// TODO
	PT_GET_RESPONSE PacketType = "GET_RESPONSE"
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
