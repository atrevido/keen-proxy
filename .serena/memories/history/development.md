# Development milestones
- The initial public source combined the embedded networking engine, web panel, YAML configuration, routing, DNS, diagnostics, and preset management.
- Later changes added GitHub Actions for CI and Release Please, a bilingual MkDocs site, and a standalone browser demo using the same Vue source.
- Client hostname discovery uses DHCP and local reverse DNS. It keeps manual names authoritative and does not query the router on API requests.
- Use Git history to identify the code present in a checkout. Use CI results for the specific revision when assessing verification.
