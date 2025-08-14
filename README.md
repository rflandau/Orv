![orv logo](img/logo.png)

# Decentralized, Hierarchical, Self-Organizing, Service Discovery Trees

Orv is an algorithm for building self-organizing, decentralized service discovery networks. The general idea is to allow machines to assemble themselves into resilient, fault-tolerant networks so they can request services from one another (and find new providers when an existing one disappears). 

Orv does not actually interact with services, it just finds other nodes that purport to provide the service (by direct string-match). Services can be any form of resource, from DNS, NAT, tunnel endpoints to files available for download to sensor values like temperature or barometer.

Nodes join the network as either a leaf or a *vault keeper* (the latter routes messages and supports child nodes, the former does neither) and both offer and request services to/from the tree (referred to as the *vault*). If the service is found, the tree returns the address serving it.

Here is one example of a vault:

![a diagram of an Orv vault](img/Orv.drawio.svg)

Orv is highly flexible with the above example being just one of a myriad of implementation paradigms.

## Authorship

Designed by Shrivyas (shrivyas@andrew.cmu.edu) & R Landau (rlandau@andrew.cmu.edu/rflandau@pm.me). We originated, designed, and prototyped Orv in two, very long weeks for Professor Patrick Tague's Distributed Systems course at Carnegie Mellon University as part of our masters program. The original version is tagged [1.0.1 @ commit 74c4d4f5c94b14d803c39590982b778e57ae7a96](https://github.com/rflandau/Orv/releases/tag/v1.0.1), if you are interested in the form we turned in for the course.

R (the guy writing this README) is the current maintainer.

# Terminology

*Leaf* (better name pending): A single node that can request or provide service, but cannot support children, route messages, or otherwise contribute to the Vault.

*Vault Keeper*: The counterpart to a leaf, a vault keeper is any node that can request or provide services, route messages, and support the growth of the tree by enabling children to join. This could be a Raft group or a similar, replicated collection of machines. It could be a single server. It could be a whole data center. As long as it can service Orv requests atomically, it can be a vk.

*Vault*: A vault is any, complete instance of the algorithm. A single vault keeper with any number of leaves (included 0) is a vault. A tree with 4 layers and hundreds of leaves is a vault. Any tree that supports Orv semantics is a vault.

*Sub-Vault*: Any vault that is a child to another vault. When two vaults join and one ascends to root vault keeper, the other becomes a sub-vault. The sub-vault moniker can be used recursively down a branch.

# Implementations

If you just want to run Orv yourself (or play with it using pre-constructed libraries), there are currently two implementations: Proof and Slims. Each implementation contains a README describing the actual implementation and design trade-offs, a prototype client, a prototype Vault Keeper, and a library that can be imported by other Go applications.

> [!NOTE]
> The implementations follow different design paradigms and are NOT cross compatible.

## Proof

[Proof](implementations/proof) is the first prototype. It is implemented as a REST API and serves as a proof of concept more than anything else.

## Slims

[Slims](implementations/slims) is the second prototype. It is implemented as a layer 5/application layer protocol. This version explores different design decisions and the library contains tools to interface directly with its L5 headers.

# Core Design Goals

Orv is first-and-foremost an algo.