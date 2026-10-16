GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all ui build test clean
all: build

# The frontend is embedded by web/embed.go, so it must be built before any Go
# build, test or vet that compiles internal/server. dist/ is not committed.
ui:
	cd web && npm ci && npm run build

test: ui
	cd web && npm test
	$(GO) test ./...

build: ui
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/memctl ./cmd/memctl

clean:
	rm -rf bin dist coverage web/dist
