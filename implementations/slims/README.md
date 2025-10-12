Slims is a an Orv variant using a custom layer 5 protocol. This Go implementation was created as part of an independent study during the last semester of my masters at Carnegie Mellon University.

# Message Types

Message types are similar to those detailed in the interaction model in the [main README](../../README.md) and are detailed in [PACKETS.md](PACKETS.md).

# Payload Serialization

Orv payloads are serialized as protocol buffers (version 3) and encapsulated in the header detailed above.

# Version Negotiation

Version negotiation between nodes is OpenFlow-ish. ~~OpenFlow packets *each* contain version in the header. Orv packets only include version on initial handshake; it is assumed that communications after that point will use the agreed-upon version.~~

Versions are negotiated implicitly. A client requests the version it would like to use. The vk will respond according to the following rules:

1. If the vk supports the requested version, HELLO_ACK will echo that version.
2. If the version is higher than the VK supports, the VK will send its highest supported version.
3. If the version is lower than the VK supports, the VK will send its lowest supported version.
4. If the version is between two versions the VK supports, it will send the next lower version that it supports.

Of course, this is all theoretical as the implementation only supports one version at the moment.

# Caveats

## Client Response Handling

The functions in the `client` package, namely `Get` and `List`, spawn a new server on the given laddr in order to receive the response from the final hop. This address is included in the requested. This means, however, that NAT could mangle the addressing and cause responses to not be received.

# Contributing

Orv Slims is a pretty run-of-the-mill Go project, just make sure you utilize [staticcheck](staticcheck.dev).

Also you'll probably want to read [DESIGN](DESIGN.md), you poor bastard.

