# Keenetic and Netcraze constraints
- The Keenetic and Netcraze Entware bootstrap supports ARM64 and little-endian MIPS soft float. The Makefile builds additional Linux binaries, including ARMv5 and big-endian MIPS, for experimental manual OpenWrt installation. See `tools/bootstrap.sh` for installer architecture detection and `docs/en/getting-started.md` for platform requirements. Stock BusyBox commands differ from full Linux utilities; use the project's netlink implementation for policy routing.
- TCP interception uses REDIRECT. UDP TPROXY requires kernel facilities, policy routing, and compatible iptables modules. Check platform prerequisites and avoid overlapping interception services.
- Router firmware can rebuild firewall chains. The watchdog repairs missing rules and jumps. Check tools/install.sh and the deployment guides for startup and recovery behavior.
- Host unit tests, disposable Linux namespace tests, and router tests establish different properties. Namespace tests validate packet routing and convergence. Real firmware, startup, and installed-client compatibility require hardware verification.
- Linux log descriptors use /proc/self/fd for compatibility with systems without /dev/fd. Entware TLS clients need a usable CA bundle.
- Hardware testing and deployment constraints: `mem:platform/hardware-verification` and `mem:platform/deployment-safety`.
