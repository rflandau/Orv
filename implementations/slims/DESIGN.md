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