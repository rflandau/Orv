This file expounds upon the implementation design described in [PACKETS](PACKETS.md) and [README](README.md), diving deeper into various decisions.
Only the truly masochistic (or turbo nerds among us) will want this level of detail in the design process.

# L5 Headers

Slims is defined by the design and implementation of its custom packet headers. The goal was to keep these headers thin, but encapsulate as much data as possible so payloads can also remain thin. See [PACKETS.md](implementations/slims/PACKETS.md) for a full breakdown of the header and how to send messages.

Memory reads were also considered, but little weight was given to it considering 32b, 64b, and even 16b devices could all be used in a vault. In the final design, ID will be split over 2 reads in a 64b system and 3 reads in a 32b system. Not ideal, but I figure better we keep the header size as small as possible, so no padding.

### Field: Version

Version is composed of two nibbles. While this greatly restricts the representable range, I figure that it is unlikely that Orv will go through 16 major versions without a substantial header redesign (thus making the restriction negligible).

### Field: Packet Type

Taking a cue from so many other protocols (for example, CoAP's request/response code), Orv uses enumerated packet types. A single byte gives plenty of range in case packet types grow.

## Discussion

The headers went through several designs. 1B Version and 1B Message Type were omnipresent, but there was also a 2B Payload Length and 1B Hop Limit field at other points. ID was originally omitted from the header, instead being serialized into the payload.

Hop Limit was eliminated early, as it really only applies to a few packets; most defaulted to setting it to 1 (which tipped me off that it should be dropped).

Payload Length is redundant, given UDP already contains the size of the datagram.

After the fields had settled to the ones in the final version, I wanted to enable client requests to drop the ID field entirely (as clients are not assumed to be part of the vault and whether they are or not is irrelevant in this implementation). This would shrink client request headers to a mere 2B. However, this also required the introduction of the shorthand bit to tell decoders to read only 2 bytes.

# L4 Protocol

Orv Slims is built on UDP. This decision was two-pronged:

1) Orv expects to run on constrained systems so UDP is a more natural choice than TCP.

2) I needed practice working with UDP rather than TCP.

A production-ready implementation should consider a hybridized layer 4, where packets that do not require ACKs are sent via UDP and those that do are sent with something that guarantees delivery. Possibly QUIC?

## Building on UDP

Building on UDP and working with the raw buffers is kind of a pain (though infinitely less than it is in C). Slims is built on a trick that makes this much easier: tiny payloads. By packing everything into a single packet (Slims assumes an MTU of just 1KB), we can rely on UDP's integrity (via checksums) and Orv's idempotence so that we do not have to deal with out of order or incomplete payloads.

This trick will not work in every scenario, but Orv's design coincidentally results in very small payloads, allowing it to fit within a single packet even on relatively small MTUs.

### Resending Packets

The Slims prototype does *not* do much in the way of automatic retries (such as in the case of a lost request/response). A production-ready version should automate retrying a request if a response is not received within the context deadline, up to a specified limit.

These were omitted from the current version of Slims due to time-constraints; they are an obvious addition and thus do little to explore the finer points of Orv (as was one of the goals in building Slims).

## Multi-Packet Messages

Slims aims to fit each message into a single UDP packet; this approach means it does not need to deal with out-of-order packets or incomplete messages or stall for/await additional fragments. Either a message arrives and can be handled properly or it goes missing and does not need to be handled at all.

For most message types, this works great. The Orv header is quite small at 2 or 10 bytes and most payloads are relatively static in size (discounting unbounded string fields in the payload, which are trivial to trim down).
Even messages that do not have upper limits on payload size typically have easy ways to trim them down to fit an MTU. For instance:

- REGISTER: Slims does not allow registering services in batches. Even if it did, however, a batched register could be broken into as many as 1 message per service to ensure each message fits a single packet. 
- FAULT: extra info and/or the string representation of errno can be dropped.

STATUS messages, however, require a little more care. If left alone, they will quickly become too large for a single packet. If allowed to trim themselves, they may trim out required/desired information. To solve this, we have two approaches: packet streams and targeted requests.

The simpler of the two is targeted requests: allow the requestor to request which fields it wants, enabling the handler to trim out other fields to shrink the packet. This only goes so far, however, as long-running trees may have exceptionally large single fields (such as information on children). Here is where the second approach shines: splitting a message over multiple packets and identifying them with START and END markers.

[TODO packet streams are WIP and NYI]

# Compressing Payloads Further

Protocol buffers already compress pretty far, but use-cases with stringent memory requirements may prefer flatbuffers or, in the most extreme case, hand-packed bits with a custom schema (like the header).

# FAULT Packet

_DENY and _FAULT packets (used in Proof to respond negatively to their paired success message (ex: JOIN_ACCEPT and JOIN_DENY)) no longer exist, replaced instead by a single FAULT packet that is sent in response to any request packet. This change draws inspiration from the use of ICMP packets to indicate failure when another L4 packet was used in the request.

## FAULTs Do Not Reset Interaction

In the original vision, a JOIN_DENY packet would remove the requestor from the HELLO table of the vk. This was a side effect of the time-constraints Proof was developed under; nodes were removed from the HELLO table whenever a JOIN was received, no matter the outcome. There is no good reason to maintain this behavior and removing it decreases the total number of messages required in exchanges that do not take solely the happy path.

## FAULTs have error numbers

Once again following in ICMP's footsteps (and that of so many other network protocols), Faults have an error number tied to them to ease automated error checking and response.

### Design Process

Errno was a relatively late addition and required a fair amount of refactoring. Originally, FAULTs were intended to be opaque, carrying only a "reason" string. This, however, made unit testing less precise and I figured that if I would get use out of error numbers, so could others.

# Bifurcated Sending and Receiving in VKs

This was not a conscious choice and I didn't really realize it until implementing MERGEs.

It would likely make more sense to:

1) collapse all communications for a given VK to a single port, like how most processes operate.

or 

2) bind a second port at start up to use for unprompted writes rather than creating a new client each time.

