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