# Default sing-box features: QUIC, browser TLS fingerprints, WireGuard and gVisor.
# Additional protocol tags increase the binary size.
TAGS ?= with_quic,with_utls,with_wireguard,with_gvisor
LDFLAGS := -s -w -buildid=

# Prefer an installed nvm Node 22 toolchain; Vite needs Node 20.19+ or 22.12+.
NODE_BIN ?= $(shell ls -d $(HOME)/.nvm/versions/node/v22.*/bin 2>/dev/null | tail -1)
NPM := PATH="$(NODE_BIN):$$PATH" npm

.PHONY: test vet web build ui-demo build-router build-router-lite clean integration test-install check-language

check-language:
	python3 -B -m unittest discover -s test/language -v
	python3 tools/check-language.py

test-install:
	python3 -B -m unittest discover -s test/install -v

test: check-language test-install
	go test -tags $(TAGS) ./internal/... ./cmd/...

vet:
	go vet -tags $(TAGS) ./...

# Embed the production web/dist assets when building with the ui tag.
web:
	cd web && $(NPM) ci --no-audit --no-fund && $(NPM) run build

build: web
	go build -tags $(TAGS),ui -o bin/keenproxy ./cmd/keenproxy

# Run the local UI demo with synthetic traffic and in-memory configuration.
ui-demo: build
	mkdir -p bin/demo && ./bin/keenproxy -ui-demo -config examples/demo.yaml

# Cross-compile router binaries for ARM64 and MIPS little-endian.
build-router: web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags $(TAGS),ui -trimpath -ldflags "$(LDFLAGS)" -o dist/keenproxy-linux-arm64 ./cmd/keenproxy
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -tags $(TAGS),ui -trimpath -ldflags "$(LDFLAGS)" -o dist/keenproxy-linux-mipsle ./cmd/keenproxy

# Use the reduced protocol registry for memory-constrained routers.
# See internal/engine/registry_lite.go for supported protocols.
LITE_TAGS := keenproxy_lite,with_utls,with_wireguard,with_gvisor,ui
build-router-lite: web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags $(LITE_TAGS) -trimpath -ldflags "$(LDFLAGS)" -o dist/keenproxy-lite-linux-arm64 ./cmd/keenproxy
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -tags $(LITE_TAGS) -trimpath -ldflags "$(LDFLAGS)" -o dist/keenproxy-lite-linux-mipsle ./cmd/keenproxy

clean:
	rm -rf bin dist web/dist

# Exercise real interception inside disposable Docker network namespaces.
integration:
	CGO_ENABLED=0 GOOS=linux go build -tags $(TAGS) -o dist/keenproxy-test ./cmd/keenproxy
	CGO_ENABLED=0 GOOS=linux go build -tags $(TAGS) -o dist/sbserver-test ./test/netns/sbserver
	CGO_ENABLED=0 GOOS=linux go build -o dist/mkgeo-test ./test/netns/mkgeo
	docker run --rm --privileged -v $(CURDIR)/dist/keenproxy-test:/work/keenproxy:ro \
		-v $(CURDIR)/dist/sbserver-test:/work/sbserver:ro \
		-v $(CURDIR)/dist/mkgeo-test:/work/mkgeo:ro \
		-v $(CURDIR)/test/netns/run.sh:/work/run.sh:ro debian:bookworm-slim sh -c '\
		apt-get -qq update >/dev/null && apt-get -qq install -y --no-install-recommends \
		iptables iproute2 dnsmasq-base curl dnsutils python3 >/dev/null 2>&1 && sh /work/run.sh'
