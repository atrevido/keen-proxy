# Routing and DNS

## Policy building blocks

| Item | Purpose |
| --- | --- |
| Connection | A named tunnel/proxy profile that can be selected as a target |
| Group | A domain/address list or country selector with a target |
| Direct | Connect without a configured tunnel |
| Block | Reject matching traffic |
| Network | Default policy for a subnet |
| Client | Per-device policy identified by MAC address |

Country **Except** selectors describe traffic outside the listed countries. Explicit domain lists and country rules express different levels of policy; inspect the routing journal to confirm which rule actually won for a request.

## Fallback chains

A connection can point to another connection or Direct as its immediate fallback. The interface displays the resulting chain. Edit the selected profile's immediate fallback; downstream inherited edges belong to their own profiles. Avoid cycles. Choosing Direct as a fallback intentionally allows a direct path when the primary connection fails.

Periodic health checks measure HTTP requests, not ICMP ping. They are separate from the manual HTTPS and transfer checks in Diagnostics.

## DNS

DNS settings choose upstream resolvers and whether requests travel directly or through a connection. Bootstrap addresses help resolve encrypted resolver hostnames. Failure policy controls whether a failed selected path may use direct DNS. Filter lists and exceptions change DNS answers, not every possible form of application traffic.

Use your router's actual network test procedure to evaluate UDP/TCP interception and encrypted-DNS handling. Applications can use their own resolvers. The UI and demo do not establish universal DNS-leak protection.

See the [user guide](user-guide.md) for screenshots and [Troubleshooting](troubleshooting.md) for interpreting evidence.
