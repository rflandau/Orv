We may toss this file entirely as we are splitting by implementation.

# 1.1

- Updated versioning scheme to major.minor
- Outlined suggested packet specification. See [PACKETS.md](PACKETS.md).
- STATUS requests now optionally take a hop limit. If omitted, it will be treated as a hop limit of 0 or 1 (stopping at the first receiving VK).
    - This enables STATUS to be used to poll the whole branch.
- Added optional *reason* fields to the payload of FAULT and DENY packets 

# 1.0.1

Initial release