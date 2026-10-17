GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all ui build test install clean release
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

install: build
	mkdir -p "$(HOME)/.agent-memory/bin"
	install -m 755 bin/memctl "$(HOME)/.agent-memory/bin/memctl"

# Release candidates for darwin arm64/amd64, linux amd64 and windows amd64,
# with SHA-256 checksums, third-party license notices and release info. The
# test prerequisite keeps cross-compilation from being mistaken for a runtime
# acceptance run; see docs/04-ui-and-cli-specification.md section 6.
release: test
	VERSION="$(VERSION)" scripts/release.sh

clean:
	rm -rf bin dist coverage web/dist
