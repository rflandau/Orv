Packet specification for Orv [Slims](implementations/slims) Version 1.0

Orv Slims packets are application (L5) packets and the current implementation uses CoAP for the transport layer.

# Header [2 or 12 Bytes]

The [protocol package](orv/protocol/protocol.go) contain structs and functions for interacting with an Orv header; you shouldn't need to manipulate the bits yourself if your client is written in Go.

The Orv header is typically 12 bytes, with the bulk of that space taken up by the ID field.
When sending a client request, ID may be dropped to bring the header down to a mere 2 bytes.

0               1               2               3
0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7 0 1 2 3 4 5 6 7

+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
| Major | Minor | S |   Type    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
                              ID                               
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

## Version [1 Byte]

Version is a composite byte: the most significant nibble is the major version, the least significant nibble is the minor version.

## S(horthand) [1 bit]

Shorthand whether or not ID has been omitted. If S is set, then the 2B form of the header was sent (ID was omitted).

## Type [7 bits]

Type declares the message type of this packet as an unsigned integer that maps to the types defined below.
Undefined type numbers are reserved for future use.

## ID [0 or 10 Bytes]

ID is the unique identifier of the sender, used to correlate a packet to its origin for handling (such as from a known child, a pending join, etc). It may only be omitted when sending client requests, as client requests may originate from outside the vault. If it is omitted, S must be set.

# Message Types (Packets)

## FAULT

**Type Number:** 1

Sent by a vk as a negative response to a request. Indicates that the request was denied or otherwise failed. This may be because of server error, a bad request, or just that the vk is unable to process the request at this moment.

FAULT may be sent in response to any number of request packets.

### Payload

1. *packet type*: the type of the original packet (in uint form).
    - example (in response to a HELLO): 2 
1. *reason*: (OPTIONAL) reason for rejecting this JOIN request.
    - example (for REGISTER): "bad stale time"
    - example (for VK_HEARTBEAT): "not my child"
## HELLO

**Type Number:** 2

Sent by a node not part of the vault to introduce itself.
VKs respond to HELLOs with HELLO_ACK.

The Version field in the header has special meaning for HELLOs: the node sets Header version to the highest version they support/ wish to use. 

Requester nodes typically follow up with a JOIN or MERGE, but do not have to.

HELLO has no payload.

## HELLO_ACK

**Type Number:** 3

Sent by VKs in response to a node's HELLO to acknowledge receipt and relay basic information to the requester node.

The Version field in the header has special meaning for HELLO_ACKs:

1. If the vk supports the requested version, HELLO_ACK will echo that version.
2. If the version is higher than the VK supports, the VK will send its highest supported version.
3. If the version is lower than the VK supports, the VK will send its lowest supported version.
4. If the version is between two versions the VK supports, it will send the next lower version that it supports.

The vk sets the response's Header version to the version given by the requestor node if the VK supports that version.

If a client wants to see all versions a vk supports, try the [STATUS](#status) packet.

### Payload

1. *height*: the height of the node answering the greeting
    - example: 3

## JOIN

**Type Number:** 4

Sent by a node not part of the vault to request to join under the receiver VK. Sender must have already introduced itself via a recent HELLO.

Repeated or duplicate joins for a node already registered as a child of the VK are thrown away.

### Payload

1. *isVK*: is this node a VaultKeeper or a leaf?
2. *vkAddr*: address of the listening VK service that can receive INCRs
    - required if isVK. 
3. *height*: height of the vk attempting to join the vault
    - required if isVK. 

## JOIN_ACCEPT

**Type Number:** 5

Sent by VKs in response to a node's JOIN request to accept the request.

Once received by a node, that node can safely mark the VK as its parent.

### Payload

1. *height*: the height of the requester's new parent

## REGISTER

**Type Number:** 6

Sent by a child node already part of a vault to tell its parent VK about a new service. This REGISTER echoes up the tree until it has reached root.

Echoing responsibility falls to each parent VK to pass the message recursively.

If an existing service is registered to the same child node, the new information will supplant the existing information (ex: address and stale time).

### Payload

1. *service*:  name of the service to be registered
    - example: "SSH" 
2. *address* address the service is bound to. Only populated from leaf to parent
    - example: "172.1.1.54:22" 
3. *stale*: after how much time without a heartbeat is this service eligible for pruning. Services may be pruned lazily and thus may survive longer than their stale time. Actual implementation is left up to the VK. Services are only guaranteed to *not* be pruned while within their stale time.
    - example:"1m5s45ms"

## REGISTER_ACCEPT

**Type Number:** 7

Sent by a parent VK to confirm registration of the service offered by the child.

### Payload

1. *service*: name of the service that was registered.
    - example: "SSH"
    

## MERGE

**Type Number:** 8

## MERGE_ACCEPT

**Type Number:** 9

## INCREMENT

**Type Number:** 10

## INCREMENT_ACK

**Type Number:** 11


## SERVICE_HEARTBEAT

**Type Number:** 12

## SERVICE_HEARTBEAT_ACK

**Type Number:** 13

## VK_HEARTBEAT

**Type Number:** 14

Sent by child VKs to alert their parent that they are still alive.

VK_HEARTBEAT has no payload.

## VK_HEARTBEAT_ACK

**Type Number:** 15

Sent by a parent VK to confirm receipt of a child's VK_HEARTBEAT.

VK_HEARTBEAT_ACK has no payload.

# Service Requests

Service requests are requests that can be made by any client, whether or not they are part of the tree or even previously known.

The ID field of the header is not used in client requests and can be zeroed.

## STATUS

**Type Number:** 16

Used by clients and tests to fetch information about the current state of the receiver VK. STATUS can recur up the tree up to *hop limit* times or until it hits root, whichever is sooner. If hop limit is 0 or 1, requests will be halted at the first VK.

STATUS does not have a payload.

## STATUS_RESP

**Type Number:** 17

Returns the status of the current node.
All fields (other than id) are optional and may be omitted at the VK's discretion.

### Payload

1. *height*: (OPTIONAL) height of the queried VK
    - example: 8
2. *children*: (OPTIONAL) children of this VK and their services. Represents a point-in-time snapshot. No representations are guaranteed and format is left up to the discretion of the VK implementation
3. *parentID*: (OPTIONAL) unique identifier for the VK's parent. 0 if VK is root.
4. *parentAddress*: (OPTIONAL) address and port of the VK parent's process
5. *pruneTimes*: (OPTIONAL) this VK's timings for considering associated data to be stale
    - *pendingHello*
	- *servicelessChild*
	- *cVK*

## LIST

**Type Number:** 18

Sent by a client to learn what services are available. Lists targeting higher-hop nodes should return a superset of services from lower nodes (assuming your Orv implementation has root omnipotence and/or does not rely on downward traversal outside of INCREMENTs).

Use hop limit to enforce locality. A hop limit of 0 or 1 means the request will only query the client's immediate contact. Like STATUS, hop count is limited by vault height.

### Payload

1. *hop limit*: (OPTIONAL) number of hops to walk up the tree. 0, 1, and omitted all cause the request to be halted at the first VK. 

## LIST_RESPONSE

**Type Number:** 19

## GET

**Type Number:** 20

## GET_RESPONSE

**Type Number:** 21