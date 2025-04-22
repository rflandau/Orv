# Orv: the Decentralized, Hierarchical (Height-Aware), Self-Organizing, Service Discovery Tree

(*OR*ganized *V*aults)

Orv provides a mechanism for ...

## Other names

DSOD (Decentralized, Self-Organizing Discovery)

SOSD (pronounced "sauced") (Self-Organizing Service Discovery)

# Terminology

*Leaf* (better name pending): A single node that can request or provide service, but cannot support children, route messages, or otherwise contribute to the Vault.

*Vault Keeper*: The counterpart to a leaf, a vault keeper is a single node that can request or provide services, route messages, and support the growth of the tree by enabling children to join.

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

### Dragon's Hoard (Tree-Seeding)

**Not Implemented**

As height adjustments only happen when root-root joins occur, small trees can rapidly accrue a lot of leaves. This increases the possibility of localized, cascading failure for overloaded vks.

If you know that your tree will grow quickly (at least initially), you can start it "with a hoard".
Rather than starting a vault by creating a vk with height 0, start the node with an arbitrary height, thus allowing the vk to subsume other vks without vying for root control.

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