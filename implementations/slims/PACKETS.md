Packet specification for Orv [Slims](implementations/slims) Version 1.0

Orv Slims packets are application (L5) layer and the current implementation uses UDP for the transport layer.

Addresses are assumed to be IPv4/v6 in this implementation (and thus typically represented as netip.AddrPort{}s) for ease-of-use. However, Slims can you any layer 3.

# Header [2 or 10 Bytes]

The [protocol package](orv/protocol/protocol.go) contain structs and functions for interacting with an Orv header; you shouldn't need to manipulate the bits yourself if your client is written in Go.

The Orv header is typically 10 bytes, with the bulk of that space taken up by the ID field.
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

ID is the unique identifier of the sender as a uint64, used to correlate a packet to its origin for handling (such as from a known child, a pending join, etc). It may only be omitted when sending client requests, as client requests may originate from outside the vault. If it is omitted, S must be set.

### NOTE

An ID of zero is valid. However, this means that omitting an ID (and allowing it to default to 0) without specifying that the packet is shorthand will cause the receiver to act as if the sending node's ID is 0 (not as if the sending node's ID is unset).

# Message Types (Packets)

## FAULT

**Type Number:** 1

Sent by a vk as a negative response to a request. Indicates that the request was denied or otherwise failed. This may be because of server error, a bad request, or just that the vk is unable to process the request at this moment.

FAULT may be sent in response to any number of request packets.

### Payload

1. *message type* (**uint8**): type number of the original message (the message that caused the FAULT).
    - example (in response to a HELLO): 2
2. *errno* (**uint16**): error number indicated the specific error that occurred. Errnos are divided into categories, like HTTP, CoAP, etc. General Codes can be sent in response to any message type, while type-specific codes (like JOIN Codes) can only be sent in response to that message type.

    - General Codes (0XX):
    
        000: UNKNOWN_TYPE: sent when an unknown message type number is declared.
    
        001: BODY_NOT_ACCEPTED: payload was included in a packet that does not have one.
        
        002: BODY_REQUIRED: payload was not included with a message type that requires one.

        003: SHORTHAND_NOT_ACCEPTED: message indicated shorthand, but shorthand is not supported for the given message type.

        004: VERSION_NOT_SUPPORTED: given version is not supported by the receiver vk.

        005: MALFORMED_BODY: failed to unpack the body of the message.

        006: MALFORMED_ADDRESS: given address does not fit the expected address syntax.
            - This error is subject to implementation choices and may not be sent (if addresses are not validated).

    - JOIN Codes (4XX):

        400: HELLO_REQUIRED: sent when a JOIN is received but there is no outstanding HELLO for the given ID.

        401: BAD_HEIGHT: the height of the requesting child was not equal to VK's height-1.

        402: ID_IN_USE: the given ID is already in use by a child of a different kind
            - if a VK sent the JOIN request, then its ID is already in use here by a leaf and vice versa.

3. *additional_info* (**string**):  (OPTIONAL) extra, human-readable information to include

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

1. *height* (**uint16**): the height of the node answering the greeting
    - example: 3

## JOIN

**Type Number:** 4

Sent by a node not part of the vault to request to join under the receiver VK. Sender must have already introduced itself via a recent HELLO.

Repeated or duplicate joins for a node already registered as a child of the VK are idempotent, but will proc JOIN_ACCEPTs (as the vk assumes the child did not receive the original acceptance).

### Payload

1. *isVK* (**bool**): is this node a VaultKeeper or a leaf?
2. *vkAddr* (**any**): address of the listening VK service that can receive INCRs
    - required if isVK.
3. *height* (**uint16**): height of the vk attempting to join the vault
    - required if isVK. 

## JOIN_ACCEPT

**Type Number:** 5

Sent by VKs in response to a node's JOIN request to accept the request.

Once received by a node, that node can safely mark the VK as its parent.

### Payload

1. *height* (**uint16**): the height of the requester's new parent

## REGISTER

**Type Number:** 6

Sent by a child node already part of a vault to tell its parent VK about a new service. This REGISTER echoes up the tree until it has reached root.

Echoing responsibility falls to each parent VK to pass the message recursively.

If an existing service is registered to the same child node, the new information will supplant the existing information (ex: address and stale time).

Valid REGISTERs from child vaultkeepers refresh the child's prune time on their parent.

### Payload

1. *service* (**string**):  name of the service to be registered
    - example: "SSH" 
2. *address* (**any**): address the service is bound to. Only populated from leaf to parent
    - example: "172.1.1.54:22" 
    - example: "20"
    - example: "rorylandau.com"
3. *stale* (**Go time**): after how much time without a heartbeat is this service eligible for pruning. Services may be pruned lazily and thus may survive longer than their stale time. Actual implementation is left up to the VK. Services are only guaranteed to *not* be pruned while within their stale time.
    - example:"1m5s45ms"

## REGISTER_ACCEPT

**Type Number:** 7

Sent by a parent VK to confirm registration of the service offered by the child.

### Payload

1. *service* (**string**): name of the service that was registered.
    - example: "SSH"

## MERGE

**Type Number:** 8

Sent by a vk to indicate that the target vk should become one of its children (like a reverse JOIN). Only used in root-root interactions. Must follow a HELLO_ACK. Should not be considered confirmed until a MERGE_ACCEPT is received by the requestor.

### Payload

1. *height* (**uint16**): the current height of the requestor node
2. *vkAddr* (**any**): address the requestor vk will be listening for heartbeats on

## MERGE_ACCEPT

**Type Number:** 9

Sent by a vk to accept a vk's request to merge. Only used in root-root interactions.

Once received, the original vk (the vk that originally sent the MERGE) can safely consider itself to be the root of the newly merged vault, with the MERGE_ACCEPT sender a child vk. The requestor node must then update its height and send an INCREMENT to each child vk that was not part of the prior merge.

MERGE_ACCEPT has no payload.

## INCREMENT

**Type Number:** 10

Sent by the new root of a freshly merged vault to inform all existing children of their new height (which should be a simple +1).
Children who receive an INCREMENT must echo it to *their* child vks.
Do **not** send INCREMENT down the newly merged branch; in other words, do not send one to the vk that just sent a MERGE_ACCEPT as its height is already correct.

If a vk receives an INCREMENT with an unexpected height from its parent, handling is implementation-dependent. However, the child must take steps to remedy the solution. It may leave the vault, it may request a STATUS from its parent (to verify height), or it may do something else; it may *not* remain in the vault with a height inconsistent to that of there rest of the vault.

### Payload

1. *newHeight* (**uint16**): the new height the receiver should adjust themselves to. It should be their current height + 1.

## INCREMENT_ACK

**Type Number:** 11

**Optionally** sent by the a child vk after it has adjusted its height so the parent knows it does not need to resend the INCREMENT. Counts as a VK_HEARTBEAT.

### Payload

1. *newHeight* (**uint16**): the height the child vk just set itself and its branch to.

## SERVICE_HEARTBEAT

**Type Number:** 12

Sent by a leaf to refresh the lifetimes of all services named therein.

### Payload

1. *services* (**string**): names of the services to refresh
    - array
    - example: ["serviceX", "serviceY"]

## SERVICE_HEARTBEAT_ACK

**Type Number:** 13

Sent by a parent vk to acknowledge receipt of a SERVICE_HEARTBEAT and enumerate the services that were refreshed.

If no services were refreshed, a FAULT is sent instead.

### Payload

1. *refreshed* (**array of strings**): names of the services that were refreshed
    - array
    - example: ["serviceX", "serviceY"]
2. *unknown* (**array of strings**): names of services that a refresh was requested for, but could not be identified
    - array

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

## STATUS_RESPONSE

**Type Number:** 17

Returns the status of the current node.
All fields (other than id) are optional and may be omitted at the VK's discretion.

### Payload

1. *height* (**uint16**): (OPTIONAL) height of the queried VK
    - example: 8
2. *children*(**any**): (OPTIONAL) children of this VK and their services. Represents a point-in-time snapshot. No representations are guaranteed and format is left up to the discretion of the VK implementation
3. *parentID*(**uint64**): (OPTIONAL) unique identifier for the VK's parent. 0 if VK is root.
4. *parentAddress* (**any**): (OPTIONAL) address and port of the VK parent's process
5. *pruneTimes*(**struct of Go times**): (OPTIONAL) this VK's timings for considering associated data to be stale
    - *pendingHello*
	- *servicelessChild*
	- *cVK*

## LIST

**Type Number:** 18

Sent by a client to learn what services are available. Lists targeting higher-hop nodes should return a superset of services from lower nodes (assuming your Orv implementation has root omnipotence and/or does not rely on downward traversal outside of INCREMENTs).

Use hop limit to enforce locality. A hop limit of 0 or 1 means the request will only query the client's immediate contact. Like STATUS, hop count is limited by vault height.

### Payload

1. *token* (**any**): a requestor-generated token used to identify this request. To consider a response valid, it must echo back this token.
2. *hop limit* (**uint16**): (OPTIONAL) number of hops to walk up the tree. 0, 1, and omitted all cause the request to be halted at the first VK. 

## LIST_RESPONSE

**Type Number:** 19

### Payload

1. *services* (**array of strings**): list of services known to the responding vk

## GET

**Type Number:** 20

### Payload

1. *service* (**string**): the name of the desired service
2. *hop limit* (**uint16**): (OPTIONAL) number of hops to walk up the tree if closer vks do not know of the requested service. 0, 1, and omitted all cause the request to be halted at the first VK. 
3. *token* (**any**): token is a locally generated identifier that can be used to positively identify the response to this request. The response should only be considered valid if it has this token.

## GET_RESPONSE

**Type Number:** 21

### Payload

1. *hostID* (**uint16**): ID of the node that is responding to this request.
2. *service* (**string**): name of the requested service.
3. *addr* (**any**): the address the requested service can be accessed at.
4. *token* (**any**): echo of the original request's token