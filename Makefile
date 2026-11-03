# CirrusUX. Go uses only the standard library; the UI is built first
# because the gateway embeds it.

GO      ?= go
BIN     := bin
# VERSION is the single source of the version number (see `make bump`).
VERSION := $(shell cat VERSION)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
STAMP   := -X cirrusux/internal/version.Version=$(VERSION) -X cirrusux/internal/version.Commit=$(COMMIT)
LDFLAGS := -s -w $(STAMP)

# Development only: a fixed token for the local gateway and agent.
DEV_TOKEN := dev-only-token-not-a-secret-0123456789

.PHONY: all web web-stub build test test-host dev-gateway dev-agent store-site bump clean

all: build

web:
	cd web && npm ci && npm run build

# go vet/test/run need web/dist, which the gateway embeds. Without a UI build
# (a fresh clone) they get a placeholder page; `make web` replaces it.
web-stub:
	@test -f web/dist/index.html || { mkdir -p web/dist && printf '<!doctype html><title>CirrusUX</title><p>UI not built: run <code>make web</code>.</p>\n' > web/dist/index.html; }

build: web
	mkdir -p $(BIN)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-gateway ./cmd/gateway
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-agent ./cmd/agent
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-helper ./cmd/helper
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-dockerfilter ./cmd/dockerfilter
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-idle ./cmd/idle
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusstore ./cmd/cirrusstore

test: web-stub
	scripts/check-file-length.sh
	test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }
	$(GO) vet ./...
	$(GO) test ./...
	cd web && npm run check && npm test

# The host setup scripts: shellcheck, then each module in a private user
# namespace (never as real root). The firewall test needs nft.
test-host:
	deploy/host/tests/run.sh

# Local run without a server or Tailscale: the gateway in dev mode and an
# agent pushing the recorded demo snapshot. Open http://127.0.0.1:8064
# (run `make web` first for the real UI).
# SETUP=single runs the single setup instead.
SETUP ?= split

dev-gateway: web-stub
	CIRRUS_DEV=1 CIRRUS_DATA_DIR=.dev-data CIRRUS_AGENT_TOKEN=$(DEV_TOKEN) CIRRUS_SETUP=$(SETUP) \
	CIRRUS_SERVER_NAME=homeserver $(GO) run -ldflags '$(STAMP)' ./cmd/gateway

dev-agent:
	CIRRUS_AGENT_TOKEN=$(DEV_TOKEN) $(GO) run -ldflags '$(STAMP)' ./cmd/agent -gateway http://127.0.0.1:8064/agent/snapshot -demo deploy/dev/demo-snapshot.json -interval 1m

# The official CirrusStore catalogue from the built-in apps, signed with the
# offline key: make store-site KEY=/media/offline/cirrusstore.key [OUT=site]
OUT ?= site
store-site:
	@test -n "$(KEY)" || { echo "usage: make store-site KEY=cirrusstore.key [OUT=site]"; exit 2; }
	$(GO) run ./cmd/cirrusstore sign -key $(KEY) -dir $(OUT) internal/catalogue/apps/*.json
	$(GO) run ./cmd/cirrusstore index -key $(KEY) -dir $(OUT)

# make bump V=0.2.0 — writes the version everywhere; doesn't commit or tag.
bump:
	@test -n "$(V)" || { echo "usage: make bump V=x.y.z"; exit 2; }
	scripts/bump-version.sh $(V)

clean:
	rm -rf $(BIN) .dev-data web/dist
