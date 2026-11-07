# Commands from the repository root
- make test — language checks, installer tests, and tagged Go tests for internal and cmd.
- make vet — go vet with the same tags, ./...
- make web — npm ci and npm run build (vue-tsc + Vite).
- make build — web + bin/keenproxy with ui.
- make ui-demo — build and run UI on :8086 with examples/demo.yaml, without network changes. Demo configuration is in memory; token: bin/demo/token.
- cd web && npm run dev — HMR, /api proxy on :8086; KEENPROXY_URL selects the backend. npm run typecheck checks TS/Vue separately.
- go run -tags with_wireguard,with_gvisor ./cmd/keenproxy -dry-run -config examples/config.yaml -arp /dev/null — print artifacts without installing firewall rules. Output includes configuration: never publish it with real keys.
- make build-router / make build-router-lite — dist Linux arm64 + mipsle softfloat, CGO=0; both require a web build.
- make integration — Docker --privileged, Linux network namespaces; installs packages inside a container and requires network access and Docker. Run for changes to routing, firewall, or DNS behavior.
- CLI flags/default paths: cmd/keenproxy/main.go; tools/probe and separate Go modules: tools/README.md.

- npm --prefix web run build:demo builds the static browser demo. See the documentation workflow for the Pages build.
