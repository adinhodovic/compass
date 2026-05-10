COMPASS_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS += -X "main.version=$(COMPASS_VERSION)"
LDFLAGS += -X "main.commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)"
LDFLAGS += -X "main.buildTime=$(shell date -u "+%Y-%m-%d %H:%M:%S")"

GO := GO111MODULE=on CGO_ENABLED=0 go

.PHONY: build
build: assets
	$(GO) build -ldflags '$(LDFLAGS)' -o compass ./cmd/compass

.PHONY: build-all
build-all: assets
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o compass-linux-amd64 ./cmd/compass
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags '$(LDFLAGS)' -o compass-linux-arm64 ./cmd/compass

.PHONY: assets
assets: node_modules/.installed
	npm run build

node_modules/.installed: package.json package-lock.json
	npm ci
	@touch $@

.PHONY: test
test:
	$(GO) test ./... -v -covermode=count -coverprofile=coverage.txt

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: fmt
fmt:
	golangci-lint fmt ./...

.PHONY: test-e2e
test-e2e:
	npm run test:e2e

DC := docker compose -f deploy/dev/docker-compose.yml

.PHONY: dev
dev: dev-up assets
	@command -v air >/dev/null 2>&1 || { \
		echo "air not found on PATH; install with: go install github.com/air-verse/air@latest"; \
		exit 1; \
	}
	@HEADSCALE_API_KEY="$$(cat deploy/dev/.headscale-key/apikey.txt)" air

.PHONY: dev-up
dev-up:
	$(DC) up -d --wait

.PHONY: dev-down
dev-down:
	$(DC) down

# QoL wrappers around `docker compose` so contributors don't have to
# remember the full -f path. SERVICE is optional for dev-logs and
# dev-restart (empty = act on the whole stack); required for dev-shell.
.PHONY: dev-logs
dev-logs:
	$(DC) logs -f --tail=200 $(SERVICE)

.PHONY: dev-shell
dev-shell:
	@test -n "$(SERVICE)" || { echo "usage: make dev-shell SERVICE=<name>"; exit 1; }
	$(DC) exec $(SERVICE) sh

.PHONY: dev-restart
dev-restart:
	$(DC) restart $(SERVICE)

DOCS_PORT ?= 8001

.PHONY: docs
docs:
	uvx zensical serve -a localhost:$(DOCS_PORT)

.PHONY: docs-build
docs-build:
	uvx zensical build

.PHONY: clean
clean:
	rm -rf compass compass-linux-amd64 compass-linux-arm64 \
	       coverage.txt site test-results playwright-report \
	       internal/server/static node_modules/.installed
