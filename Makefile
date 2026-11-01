# CirrusUX. Go uses only the standard library; the UI is built first
# because the gateway embeds it.

GO      ?= go
LDFLAGS := -s -w
BIN     := bin

# Development only: a fixed token for the local gateway and agent.
DEV_TOKEN := dev-only-token-not-a-secret-0123456789

.PHONY: all web build test check dev-gateway dev-agent clean

all: build

web:
	cd web && npm ci && npm run build

build: web
	mkdir -p $(BIN)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-gateway ./cmd/gateway
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-agent ./cmd/agent
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-helper ./cmd/helper
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/cirrusux-dockerfilter ./cmd/dockerfilter

test:
	$(GO) vet ./...
	$(GO) test ./...
	cd web && npm run check

# Local run without cloud64 or Tailscale: the gateway in dev mode and an
# agent pushing the recorded demo snapshot. Open http://127.0.0.1:8064.
dev-gateway:
	CIRRUS_DEV=1 CIRRUS_DATA_DIR=.dev-data CIRRUS_AGENT_TOKEN=$(DEV_TOKEN) $(GO) run ./cmd/gateway

dev-agent:
	CIRRUS_AGENT_TOKEN=$(DEV_TOKEN) $(GO) run ./cmd/agent -gateway http://127.0.0.1:8064/agent/snapshot -demo deploy/dev/demo-snapshot.json -interval 1m

clean:
	rm -rf $(BIN) .dev-data web/dist
