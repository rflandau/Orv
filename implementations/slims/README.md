Slims is a an Orv implementation using a custom layer 5 protocol.

# Implementation Design

## Payload Serialization

Payloads, within a CoAP header, are composed of the Orv header (with custom serialization via Header.Serialize() and Header.Deserialize()) and a body serialized as a protobuffer.

Protocol Buffers are smaller than JSON, but implementations with stringent memory constraints could shrink payloads further by hand-packing the bits into a declared schema (like we have done for the Orv headers).

## Version Negotiation

Version negotiation between nodes is OpenFlow-ish. OpenFlow packets *each* contain a header. Orv packets only include version on initial handshake; it is assumed that communications after that point will use the agreed-upon version.

Versions are negotiated implicitly. A client requests the version it would like to use. If the target VK supports that version, its HELLO_ACK will echo the version. If it does not, the target VK's HELLO_ACK will include its highest supported version if the client's version was higher or its lowest supported version if it is lower. If the client is requesting an unsupported intermediary version, the VK will respond with the closest version it has in either direction. TODO: finalize the intermediary version handling.

*TODO:* expand on this after full implementation.
As we are only going to support 1 version, this is more of a what-if discussion.

## Changes from Proof

On top of the other design difference described in this file, it is worth pointing out some notable changes between the vision implemented by Proof and the vision implemented by Slims.

### FAULT Packet

_DENY and _FAULT packets no longer exist, replaced instead by a single FAULT packet that is sent in response to any request packet. This change draws inspiration on the use of ICMP packets to indicate failure when another L4 packet was used in the request.

#### FAULTs Do Not Reset Interaction

In the original vision, a JOIN_DENY packet would remove the requestor from the HELLO table of the vk. This was a side effect of the time-constraints Proof was developed under; nodes were removed from the HELLO table whenever a JOIN was received, no matter the outcome. There is no good reason to maintain this behavior and removing it should decrease the total number of messages required in exchanges that do not take solely the happy path.

#### FAULTs have opaque errors

For better or for worse, FAULT packets do not provide an easy mechanism for an automated system to determine if the fault is due to a bad request, internal server error, or something all together different.

A future version should probably follow the HTTP and CoAP path of using errno-like constants (403, 404, 500, etc) to categorize errors.