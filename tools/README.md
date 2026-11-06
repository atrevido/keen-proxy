# Router capability tools

These auxiliary tools help establish which features a router kernel and firmware support. Test each target architecture rather than assuming a successful build means the router can run it.

| Tool | Purpose |
| --- | --- |
| `probe/probe.sh` | Kernel modules, iptables/ipset, TUN, IPv6, policy routing, occupied ports and an optional temporary LAN alias |
| `bench/` | AEAD encryption throughput as an upper bound for userspace tunnel performance |
| `nlprobe/` | Temporary netlink objects: timed ipsets, marked routes, local routes and TUN |

`bench` and `nlprobe` are separate Go modules. Their ignored `dist/` directories hold cross-compiled binaries. Internal hardware reports belong in ignored local reports, not public issue attachments without review.

## Run a capability probe

Copy the script and a binary built for your router to a temporary Entware directory. Substitute your own router hostname, architecture and SSH port:

```sh
scp -P 222 tools/probe/probe.sh root@router:/opt/tmp/
ssh -p 222 root@router
```

Read-only inspection is the default:

```sh
/opt/bin/sh /opt/tmp/probe.sh
```

`--active` changes temporary kernel/network state: it creates and removes probe-owned ipsets, unattached iptables chains, a policy route, and a TUN device. It may temporarily add a LAN alias for 30 seconds. Loaded kernel modules remain loaded. Review the script and retain a recovery path before enabling active checks:

```sh
/opt/bin/sh /opt/tmp/probe.sh --active
```

The script attempts to redact credentials, MAC addresses and public IPv4 addresses. This is best-effort masking: review its output manually before sharing anything. Do not publish full production configuration or unreviewed router logs.

## Build a benchmark

```sh
cd tools/bench
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '-s -w' -o dist/bench-linux-arm64 .
```

For MIPS little-endian use `GOARCH=mipsle GOMIPS=softfloat`. Choose the architecture matching your router. Build the netlink probe from `tools/nlprobe` in the same way.

On the router, compare all available workers with a single worker:

```sh
/opt/tmp/bench-linux-arm64
/opt/tmp/bench-linux-arm64 -p 1
```

AEAD throughput excludes packet copying, syscalls, firewall processing and network overhead; it is not a VPN speed guarantee. The netlink probe uses reserved temporary names and attempts cleanup after partial failure, but it still performs privileged kernel operations.

## Historical recovery scripts

`router-test/guard.sh` and `router-test/rollback.sh` are local-trial recovery helpers, not a general installer or service manager. They assume a particular deployment directory and previous service layout. Inspect and adapt them before use; do not run them against an unrelated router.
