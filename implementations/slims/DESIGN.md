This file expounds upon the implementation design described in [PACKETS](PACKETS.md) and [README](README.md), diving deeper into various decisions.
Only the truly masochistic (or turbo nerds among us) will want this level of detail in the design process.

# L5 Headers

Slims is defined by the design and implementation of its custom packet headers. The goal was to keep these headers thin, but encapsulate as much data as possible so payloads can also remain thin.

Memory reads were also considered, but little weight was given to it considering 32b, 64b, and even 16b devices could all be used in a vault. In the final design, ID will be split over 2 reads in a 64b system and 3 reads in a 32b system. Not ideal, but I figure better we keep the header size as small as possible, so no padding.

### Field: Version

Version is composed of two nibbles. While this greatly restricts the representable range, I figure that it is unlikely that Orv will go through 16 major versions without a substantial header redesign (thus making the restriction negligible).

### Field: Packet Type

Taking a cue from so many other protocols (for example, CoAP's request/response code), Orv uses enumerated packet types. A single byte gives plenty of range in case packet types grow.

### PayloadLength

Originally a 2B field, I quickly realized this was redundant, given we fit the payload into a single datagram (minus header length) and UDP already has a payload length

## Discussion

The headers went through several designs. 1B Version and 1B Message Type were omnipresent, but there was also a 2B Payload Length and 1B Hop Limit field at other points. ID was originally omitted from the header, instead being serialized into the payload.

Hop Limit was eliminated early, as it really only applies to a few packets; most defaulted to setting it to 1 (which tipped me off that it should be dropped).

Payload Length is redundant, given UDP already contains the size of the datagram.

ID is in a strange position because it is the largest chunk of ....

After the fields had settled to the ones in the final version, I wanted to enable client requests to drop the ID field entirely (as clients are not assumed to be part of the vault and whether they are or not is irrelevant in this implementation). This would shrink client request headers to a mere 2B. However, this would also required the introduction of 

# L4 Protocol

Orv Slims is built on UDP. This decision was two-pronged:

1) Orv expects to run on constrained systems so UDP is a more natural choice than TCP.

2) I needed practice working with UDP rather than TCP.

A production-ready implementation should consider a hybridized layer 4, where packets that do not require acks are sent via UDP and those that do are sent with something that guarantees delivery. Possibly QUIC?

## Building on UDP

Building on UDP and working with the raw buffers is kind of a pain (though infinitely less than it is in C). Slims is built on a trick that makes this much easier: tiny payloads. By packing everything into a single packet (Slims assumes an MTU of just 1KB), we can rely on UDP's integrity (via checksums) and Orv's idempotence so that we do not have to deal with out of order or incomplete payloads.

This trick will not work in every scenario, but Orv's design coincidentally results in very small payloads, allowing it to fit within a single packet even on relatively small MTUs. The only message type that could be an issue is STATUS, as its payload is technically unbounded.

### Resending Packets

The Slims prototype does *not* do much in the way of automatic retries (such as in the case of a lost request/response). A production-ready version should automate retrying a request if a response is not received within the context deadline, up to a specified limit.

These were omitted from the current version of Slims due to time-constraints; they are an obvious addition and thus do little to explore the finer points of Orv (as was one of the goals in building Slims).

# Compressing Payloads Further

TODO rewrite this section now that it is in DESIGN.

Protocol Buffers and Flatbuffers are smaller than JSON, but implementations with stringent memory constraints could shrink payloads further by hand-packing the bits into a declared schema (like we have done for the Orv headers).

# FAULT Packet

_DENY and _FAULT packets (used in Proof to replace negatively to their paired response (ex: JOIN_ACCEPT and JOIN_DENY)) no longer exist, replaced instead by a single FAULT packet that is sent in response to any request packet. This change draws inspiration on the use of ICMP packets to indicate failure when another L4 packet was used in the request.

## FAULTs Do Not Reset Interaction

In the original vision, a JOIN_DENY packet would remove the requestor from the HELLO table of the vk. This was a side effect of the time-constraints Proof was developed under; nodes were removed from the HELLO table whenever a JOIN was received, no matter the outcome. There is no good reason to maintain this behavior and removing it should decrease the total number of messages required in exchanges that do not take solely the happy path.

## FAULTs have opaque errors

For better or for worse, FAULT packets do not provide an easy mechanism for an automated system to determine if the fault is due to a bad request, internal server error, or something all together different.

A future version should probably follow the HTTP and CoAP path of using errno-like constants (403, 404, 500, etc) to categorize errors.