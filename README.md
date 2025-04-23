Team: Network-bois

Shrivyas | shrivyas@andrew.cmu.edu

R Landau | rlandau@andrew.cmu.edu <-- the guy writing this README

# Orv: the Decentralized, Hierarchical (Height-Aware), Self-Organizing, Service Discovery Tree

(*OR*ganized *V*aults)

Orv is an algorithm for building self-organizing, decentralized service discovery networks. Nodes join the network as either a leaf or a *vault keeper* (the latter routes messages and supports child nodes, the former does not) and both offer and request services to/from the tree (referred to as the *vault*). If the service is found, the tree returns the address serving it.

Orv does not actually interact with services, it just finds other nodes that purport to provide the service. Services can be any form of resource, from DNS, NAT, tunnel endpoints to files available for download to sensor values like temperature or barometer.

## The Name 

We couldn't land on a name and I needed something to call it. Orv is the lowest layer of the world in the Pathfinder TTRPG, a sprawling network of self-sufficient biomes ("vaults") interconnected by a labyrinth of tunnel.

If we come up with something better, outstanding. If not, life goes on.

### Other names

DSOD (Decentralized, Self-Organizing Discovery)

SOSD (pronounced "sauced") (Self-Organizing Service Discovery)

# Terminology

*Leaf* (better name pending): A single node that can request or provide service, but cannot support children, route messages, or otherwise contribute to the Vault.

*Vault Keeper*: The counterpart to a leaf, a vault keeper is any node that can request or provide services, route messages, and support the growth of the tree by enabling children to join. This can also be a Raft group or similar, replicated collection of machines. As long as it can service Orv requests, it can be a vk.  

*Vault*: A vault is any, complete instance of the algorithm. A single vault keeper with any number of leaves (included 0) is a vault. A tree with 4 layers and hundreds of leaves is a vault. Any tree that supports Orv semantics is a vault.

# Core Design Goals

## IoT Support
The single largest design influence was the desire to support IoT networks effectively. This provides strong boundaries to design within and led to the bubble-up paradigm early.

A multi-level vault will naturally begin to resemble a distributed cloud architecture (mist < fog < cloud), with more data, responsibility, and power being found at the top.

## Bubble-Up Paradigm

Building off the desired support for IoT, a natural "bubble-up" paradigm emerged. Heartbeats are necessarily leaf-driven. Registrations walk leaf -> vk -> vk parent -> ... -> vk root. Requests are localized until they cannot be serviced at which point they "bubble-up" the tree until a vault manager knows where to locate a specific service (or we hit the root and thus know the service does not exist).

# Core Assumptions

- Nodes are cooperative
    - Like Raft, we are assuming that all peers are cooperative. This causes some cognitive dissonance with it being decentralized, but life goes on.
- Discovery is extrinsic 
    - While we have mechanisms for handling joins, we do not have a mechanism for node discovery, but assume one is available. In a full implementation, this would likely be served by locally broadcasting HELLO.
- Low-powered leaves
    - As we want to support IoT networks, we must assume that the leaves are low-powered and therefore should have minimal requirements. They cannot be assumed to be always listening, always accessible, or even terribly reliable.
- Powered vault keepers
    - To support ultra-low-power leaves, we shift the assumption of power to their parents.
    - This is closely related to the mist < fog < cloud architecture and follows from power requirements rising with a node's height in the tree.
- Built on an existing layer 3
    - IP for the prototype, but MPLS or any other kind of Layer 3 protocol would work fine.
    - This requirement is for the corollary assumption that responses can be independently routed to the requester (and do not necessarily walk the tree on response).

# Distributed Concepts

- Staleness
- Gossip-based knowledge
- Heartbeats
- Decentralized, dynamic cooperation

# The Protocol

### Dragon's Hoard (Tree-Seeding)

**Not Implemented**

As height adjustments only happen when root-root joins occur, small trees can rapidly accrue a lot of leaves. This increases the possibility of localized, cascading failure for overloaded vks.

If you know that your tree will grow quickly (at least initially), you can start it "with a hoard".
Rather than starting a vault by creating a vk with height 0, start the node with an arbitrary height, thus allowing the vk to subsume other vks without vying for root control.

# Other Design Decisions

## Layer 5 vs Layer 4 (vs Layer 3?!?)

The prototype is designed as an application layer protocol because it is easier for us to develop in the short time span. However, the protocol would make a lot of sense to be implemented at layer 4 on top of some kind of reliable UDP (or CoAP, just something less expensive than TCP). You could probably even construct this to operate at Layer 3, but then the assumption that the there exists a way to get the response to the requester directly falls apart and would have to be accounted for.

## Depth-less Hierarchy and Cycles

The original design allowed for trees of arbitrary height and width, completely self-organizing naturally as machines joined. However, because our routing is only next-hop, this would make cycle detection *really* hard and/or expensive.

Echoing...

## Depth versus Height

A key trade-off is whether we measure a node's depth (its distance from the root) or we measure a node's height (its distance from the lowest vk in the vault).

### Asking to increase the height on vk join

.

### Lazy Depth/Height Knowledge

Our current design broadcasts height changes down the branch with the root that took over as total root. This is some degree of antithetical to our bubble-up philosophy.

Another approach would be to force vks to request up the tree when a vk wants to join it. This would allow the root to approve new height changes and allow vk's lazily learn about their actual height.

## A note on security

One of our core assumptions is cooperation. This, of course, is not in anyway realistic

... after discovery, key exchange. The vault could be used to pass around public keys, providing a second source of possible "truth" against MitM attacks. These can be self-signed for fully decentralized or rely on a PKI if Orv is used internally or by the controlling interest.


# The Prototype 

(some description of what the prototype is)

## I forgot what I was going to put here



## Description of project topic, goals, and tasks

### Distributed concepts

- Staleness and gossip-based knowledge
- heartbeats

### Goals

- bubble-up paradigm
    - messages originate from the leaves of the tree and bubble up as necessary
    - heartbeats are child-oriented, allowing children to set their own schedule
- flexible staleness and heartbeats (related to the bubble-up paradigm)

...

## Dependencies to run this code

...

## Description of tests and how to run them

1. Test for...

```
make test
```

# Special Thanks

- Professors Patrick Tague and Pedro Bustamante, for all of your assistance, advice, support, and just general pleasantness to be around
- My cats: Bee (the pretty tortie) and Coconut (the idiot stuck under a drawer), the rubber duck stand-ins
![the babies](img/idiot_under_a_drawer.jpeg)