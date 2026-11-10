# Dependencies and builds
- go.mod: github.com/ordo-systems/keen-proxy, Go 1.27.3, embedded sing-box 1.14.4; sing-box APIs are version-sensitive. Project license: GPL-3.0-only.
- DNS: miekg/dns; YAML: yaml.v3; Linux policy routing: vishvananda/netlink; MMDB: maxminddb-golang/v2. tools/bench and tools/nlprobe have separate go.mod files.
- Main Makefile TAGS: with_quic, with_utls, with_wireguard, with_gvisor. ui requires web/dist; without ui, web/embed_stub.go permits Go tests without a frontend build.
- keenproxy_lite selects internal/engine/registry_lite.go instead of the full registry; size and protocol support depend on build. Lite retains uTLS/WireGuard/gvisor, omits QUIC.
- UI: Vue 3, strict TypeScript, Vite 8, Naive UI, vue-router, vue-i18n, @lucide/vue; npm and web/package-lock.json. Vite requires Node >=20.19 or >=22.12; Makefile searches nvm for Node v22.
