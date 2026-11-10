# KeenProxy project context
- KeenProxy provides selective routing on Keenetic and Netcraze routers with Entware. It combines Go, embedded sing-box, and a Vue panel. See `mem:platform/core` for installer coverage and experimental OpenWrt support.
- Start with README.md and AGENTS.md. Use the versioned docs and source as authority for current behavior. Check the working tree before editing.
- Configuration, traffic interception, DNS, and failover: `mem:backend/core`. API, Vue, and demo contracts: `mem:frontend/core`. Router constraints and test boundaries: `mem:platform/core`.
- Preset merging, DNS routes, filters, and YAML: `mem:features/presets_dns_filters`. Panel behavior and interaction rules: `mem:features/ui-redesign` and `mem:product/design-direction`.
- Build dependencies and commands: `mem:tech_stack` and `mem:suggested_commands`. Code conventions and verification: `mem:conventions` and `mem:task_completion`.
- Product decisions and historical implementation context: `mem:product/decisions` and `mem:history/development`. Release boundaries: `mem:product/publication-preparation` and `mem:history/initial-public-snapshot`.
- Guidelines for maintaining this public memory graph: `mem:memory_maintenance`.
