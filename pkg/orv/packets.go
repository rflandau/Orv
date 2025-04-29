package orv

/*
Details the packet types Orv uses and provides enumerated constants for them.
*/

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

// heartbeats
const (
	// Sent by a leaf to refresh the lifetimes of all services named in the HB.
	PT_SERVICE_HEARTBEAT       PacketType = "SERVICE_HEARTBEAT"
	PT_SERVICE_HEARTBEAT_ACK   PacketType = "SERVICE_HEARTBEAT_ACK"
	PT_SERVICE_HEARTBEAT_FAULT PacketType = "SERVICE_HEARTBEAT_FAULT"
	// Sent by a child VK to refresh the time until it is considered dead.
	PT_VK_HEARTBEAT       PacketType = "VK_HEARTBEAT"
	PT_VK_HEARTBEAT_ACK   PacketType = "VK_HEARTBEAT_ACK"
	PT_VK_HEARTBEAT_FAULT PacketType = "VK_HEARTBEAT_FAULT"
)

// new node joining as leaf or VK
const (
	// Sent by a node not part of the vault to request to join under the receiver VK.
	// Repeated or duplicate joins for a node already registered as a child of the VK are thrown away.
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
	// Echoing responsibility falls to each parent VK to pass the message recursively.
	// If an existing service is registered to the same child node, the new information will supplant the existing information (ex: address and stale time).
	PT_REGISTER PacketType = "REGISTER"
	// Sent by a parent VK to confirm registration of the service offered by the child.
	PT_REGISTER_ACCEPT PacketType = "REGISTER_ACCEPT"
	PT_REGISTER_DENY   PacketType = "REGISTER_DENY"
)

// service requests
const (
	// Sent by a client to learn about the receiver VK. Does not echo up the vault.
	PT_STATUS          PacketType = "STATUS"
	PT_STATUS_RESPONSE PacketType = "STATUS_RESPONSE"
	// Sent by a client to learn what services are available.
	// Use hop count to enforce locality. A hop count of 1 means the request will only query the client immediate contact. Hop count is limited by vault height.
	// While a LIST with a hop count of 0 is technically an error, hop counts of 0 and 1 are treated the same.
	PT_LIST          PacketType = "LIST"
	PT_LIST_RESPONSE PacketType = "LIST_RESPONSE"
	// Sent by a client to fetch the address of a node providing the named service.
	// Use hop count to enforce locality. A hop count of 1 means the request will only query the client immediate contact. Hop count is limited by vault height.
	// While a GET with a hop count of 0 is technically an error, hop counts of 0 and 1 are treated the same.
	PT_GET          PacketType = "GET"
	PT_GET_RESPONSE PacketType = "GET_RESPONSE"
	// Sent by a VK to a client when the client's GET request is poorly formatted.
	// NOTE(_): not sent when GET does not find a service; that is still considered a good response.
	PT_GET_FAULT PacketType = "GET_FAULT"
)

// root-root merging
const (
	// Sent by a node to indicate that the VK should become one of its children.
	// Only used in root-root interactions.
	// Must follow a HELLO_ACK.
	//
	// Must be followed up by a MERGE_ACCEPT to confirm.
	PT_MERGE PacketType = "MERGE"
	// Sent by a VK to accept a node's request to merge.
	// Only used in root-root interactions.
	//
	// Once received by the requester node, that node can safely consider itself to be the new root.
	// The requester node must then update its height and send an INCREMENT to its pre-existing children.
	PT_MERGE_ACCEPT PacketType = "MERGE_ACCEPT"
	// Sent by a VK when it MERGEs with another VK in order to notify the pre-existing child VKs that their heights have increased by 1.
	// Children who receive an INCREMENT must echo it to their child VKs.
	// Do NOT send an INCREMENT down the new branch that was just attached because of the merge.
	PT_INCREMENT PacketType = "INCREMENT"
	// Optional response from a child VK who receives an ACK to confirm to their parent that their height has been updated.
	//
	// Should be used to validate that there are no gaps in the height (in other words, every child VK's height is exactly equal to its parent's height-1).
	PT_INCREMENT_ACK PacketType = "INCREMENT_ACK"
)
