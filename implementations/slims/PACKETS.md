Suggested packet specification for Orv Version 1.1.

Implemented by the [CoAP prototype](pkg/orvCoAP).

Types and formats make the assumptions inherent in the README. If you are implementing a modified version of Orv (such as with in-house routing, no root omnipotence, or sequence numbers), this spec sheet may not fit your needs.

# A Note on Nomenclature

Response packets have a variety of suffixes (ACK, Resp, Accept, Deny, Fault). While an argument could be made for making these uniform, I used varying verbiage to indicate varying expectations.

- ACKs are just acknowledgements. They indicate receipt and little else. They are sometimes paired with FAULTs, which indicate that the original message was received (hence the sister ACK) but was faulty for one reason or another.

- ACCEPT and DENY are paired, indicating an outcome from the logic of the receiver (the VK, generally).

- RESP is only used for requests and contain either the answer or an error.

# Packets

## HELLO

Sent by a node not part of the vault to introduce itself.
VKs respond to HELLOs with HELLO_ACK.

Requester nodes typically follow up with a JOIN or MERGE, but do not have to.

### Payload

1. *id*: unique identifier of the sending node
    - example: 123

## HELLO_ACK

Sent by VKs in response to a node's HELLO to acknowledge receipt and relay basic information to the requester node.

### Payload

1. *id*: unique identifier of the VK answering the hello
    - example: 456
2. *height*: the height of the node answering the greeting
    - example: 3
3. *version*: version of Orv the VK is running
    - example: 0b0010 0001 (2.1)

## JOIN

Sent by a node not part of the vault to request to join under the receiver VK. Sender must have already introduced itself via a recent HELLO.

Repeated or duplicate joins for a node already registered as a child of the VK are thrown away.

### Payload

1. *id*: unique identifier of the sending node
    - example: 123
2. *isVK*: is this node a VaultKeeper or a leaf?
3. *vkAddr*: address of the listening VK service that can receive INCRs
    - required if isVK. 
4. *height*: height of the vk attempting to join the vault
    - required if isVK. 

## JOIN_ACCEPT

Sent by VKs in response to a node's JOIN request to accept the request.

Once received by a node, that node can safely mark the VK as its parent.

### Payload

1. *id*: unique identifier for the VK
    - example: 456
2. *height*: the height of the requester's new parent

## JOIN_DENY

Used by VKs to deny a node's JOIN request Common reasons include mismatched height and max child limit reached.

Once received by a node, that node must resend a HELLO if it wishes to continue interacting with this VK.

### Payload

1. *reason*: (OPTIONAL) reason for rejecting this JOIN request.

## REGISTER

Sent by a child node already part of a vault to tell its parent VK about a new service. This REGISTER echoes up the tree until it has reached root.

Echoing responsibility falls to each parent VK to pass the message recursively.

If an existing service is registered to the same child node, the new information will supplant the existing information (ex: address and stale time).

### Payload

1. *id*: unique identifier of the sending node
    - example: 123
2. *service*:  name of the service to be registered
    - example: "SSH" 
3. *address* address the service is bound to. Only populated from leaf to parent
    - example: "172.1.1.54:22" 
4. *stale*: after how much time without a heartbeat is this service eligible for pruning. Services may be pruned lazily and thus may survive longer than their stale time. Actual implementation is left up to the VK. Services are only guaranteed to *not* be pruned while within their stale time.
    - example:"1m5s45ms"

## REGISTER_ACCEPT

Sent by a parent VK to confirm registration of the service offered by the child.

### Payload

1. *id*: unique identifier of the sending node
    - example: 123
2. *service*: name of the service that was registered.
    - example: "SSH"

## REGISTER_DENY

Sent by a parent VK to deny registration of the service the child attempted to register.

Common reasons include the sender not being a child of the receiver and invalid data in the request.

### Payload

1. *reason*: (OPTIONAL) reason for rejecting this REGISTER request.
    - example: "bad stale time"

## MERGE
## MERGE_ACCEPT
## INCREMENT
## INCREMENT_ACK

## SERVICE_HEARTBEAT
## SERVICE_HEARTBEAT_ACK
## SERVICE_HEARTBEAT_FAULT

## VK_HEARTBEAT

Sent by child VKs to alert their parent that they are still alive.

### Payload

1. *id*: unique identifier of the cVK
    - example: 456

## VK_HEARTBEAT_ACK

Sent by a parent VK to confirm receipt of a child's VK_HEARTBEAT.

### Payload

1. *id*: unique identifier of the parent vk that refreshed the child (as triggered by prior VK_HEARTBEAT)

## VK_HEARTBEAT_FAULT

Sent by a parent VK to alert the cVK that their heartbeat was not processed.

### Payload

1. *reason*: (OPTIONAL) reason for rejecting this VK_HEARTBEAT request.
    - example: "not my child"

# Service Requests

Service requests are requests that can be made by any client, whether or not they are part of the tree or even previously known.

## STATUS

Used by clients and tests to fetch information about the current state of the receiver VK. STATUS can recur up the tree up to *hop limit* times or until it hits root, whichever is sooner. If hop limit is 0 or 1, requests will be halted at the first VK.

STATUS does not have a payload.

## STATUS_RESP

Returns the status of the current node.
All fields (other than id) are optional and may be omitted at the VK's discretion.

### Payload

1. *id*: unique identifier of the VK
    - example 456
2. *height*: (OPTIONAL) height of the queried VK
    - example: 8
3. *children*: (OPTIONAL) children of this VK and their services. Represents a point-in-time snapshot. No representations are guaranteed and format is left up to the discretion of the VK implementation
4. *parentID*: (OPTIONAL) unique identifier for the VK's parent. 0 if VK is root.
5. *parentAddress*: (OPTIONAL) address and port of the VK parent's process
6. *pruneTimes*: (OPTIONAL) this VK's timings for considering associated data to be stale
    - *pendingHello*
	- *servicelessChild*
	- *cVK*

## LIST

Sent by a client to learn what services are available. Lists targeting higher-hop nodes should return a superset of services from lower nodes (assuming your Orv implementation has root omnipotence and/or does not rely on downward traversal outside of INCREMENTs).

Use hop limit to enforce locality. A hop limit of 0 or 1 means the request will only query the client's immediate contact. Like STATUS, hop count is limited by vault height.

### Payload

1. *hop limit*: (OPTIONAL) number of hops to walk up the tree. 0, 1, and omitted all cause the request to be halted at the first VK. 

## LIST_RESPONSE