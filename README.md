Proof of concept of network-capture testing on travis-ci for [gopacket](https://github.com/google/gopacket)
=====================================================================

**DEPRECATED** since travis_ci allows loading kernel modules. New code will be based on tuntap and run directly in the image.

This is a small poc for testing gopacket pf_ring capture on travis-ci using qemu.

**DO NOT USE THIS CODE**. It is coded very poorly (I just threw code at the problem, until it worked).
If this is accepted by the project, it will be rewritten into something way nicer and better and shinier.

Discussion at https://github.com/google/gopacket/issues/561
