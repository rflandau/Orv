Slims is a an Orv implementation using a custom layer 5 protocol.

# Implementation Design

## Payload Serialization

Payloads, within a CoAP header, are composed of the Orv header (with custom serialization via Header.Serialize() and Header.Deserialize()) and a body serialized as a protobuffer.

Protocol Buffers are smaller than JSON, but implementations with stringent memory constraints could shrink payloads further by hand-packing the bits into a declared schema (like we have done for the Orv headers).