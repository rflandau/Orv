# Slims Variant

Slims is an Orv variant using a custom layer 5 protocol. This Go implementation was created as part of an independent study during the last semester of my masters at Carnegie Mellon University.

# Message Types

Message types are similar to those detailed in the interaction model in the [main README](../../README.md) and are detailed in [PACKETS.md](PACKETS.md).

# Payload Serialization

Orv payloads are serialized as protocol buffers (version 3) and encapsulated in the header detailed above.

# Version Negotiation

Version negotiation between nodes is OpenFlow-ish. Versions are negotiated implicitly. A client requests the version it would like to use. The vk will respond according to the following rules:

1. If the vk supports the requested version, HELLO_ACK will echo that version.
2. If the version is higher than the VK supports, the VK will send its highest supported version.
3. If the version is lower than the VK supports, the VK will send its lowest supported version.
4. If the version is between two versions the VK supports, it will send the next lower version that it supports.

Of course, this is all theoretical as the implementation only supports one version at the moment.

# Client Requests

Slims uses the hand-off method for client requests, making handling requests reasonably efficient at a vault-level. Do, however, note the [caveat](#client-response-handling) below.

# Caveats

## Unreliable UDP and Retries

Slims does not currently perform retries or even await ACKs in many cases to save on development/research time.

See [DESIGN.md](implementations/slims/DESIGN.md) for more information.

## Client Response Handling

The functions in the `client` package, namely `Get` and `List`, spawn a new server on the given laddr in order to receive the response from the final hop. This address is included in the requested. This means, however, that NAT could mangle the addressing and cause responses to not be received.

For a NAT-safe Orv implementation, consider the request mechanism used in [proof](implementations/proof). The Proof variant sends the request to a node that forwards it up the vault for us, like Slims' mechanism. Unlike Slims, however, Proof then rubber-bands the request back down the vault so the original target is also the answerer (thereby not requiring a response address to be included in the original request payload).

## Lack of Graceful Shutdown

Unlike how the logs read, vault keepers do not actually shutdown gracefully. The context is immediately cancelled, so all inflight operations will be thrown away.

Adding a timed waitgroup to vault keepers to ensure that all in-flights actually complete would easily facilitate true graceful shutdown.

## Gossip Hops Limited to 1

As it says in the title, gossip is implemented, but only supports a single hop. In other words, information learned via gossip will not propagate beyond the paired vks.

## VKs cannot directly register services

TODO: enable services to be registered directly at a VK, with or without heartbeats.

# Contributing

Orv Slims is a pretty run-of-the-mill Go project, just make sure you utilize [staticcheck](staticcheck.dev).

Also you'll probably want to read [DESIGN](DESIGN.md), you poor bastard.

## Road to 1.0

- [] MERGE, MERGE_ACCEPT, INCREMENT
- [] Rivering