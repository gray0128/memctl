GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all ui build test test-shard install clean release release-dist dsh-plugin-test dsh-plugin-pack pi-plugin-test pi-plugin-pack
all: build

# The frontend is embedded by web/embed.go, so it must be built before any Go
# build, test or vet that compiles internal/server. dist/ is not committed.
ui:
	cd web && npm ci && npm run build

# Full local gate: every package, every integration test, in one run. CI splits
# the same suite across shards (scripts/test-shard.sh) for wall-clock time; both
# paths must cover the same tests.
test: ui
	cd web && npm test
	node --test plugins/dsh/test/*.test.js
	node --test plugins/pi/test/*.test.js
	$(GO) test ./...

dsh-plugin-test:
	node --test plugins/dsh/test/*.test.js

dsh-plugin-pack: dsh-plugin-test
	mkdir -p dist
	cd plugins/dsh && npm pack --pack-destination ../../dist

pi-plugin-test:
	node --test plugins/pi/test/*.test.js

pi-plugin-pack: pi-plugin-test
	mkdir -p dist
	cd plugins/pi && npm pack --pack-destination ../../dist

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

# Builds the same candidates as `release` without running the test suite. CI
# uses it in a job that depends on a separate verify job, so the tests still
# gate every artifact. Do not use it for a local release.
release-dist: ui
	VERSION="$(VERSION)" scripts/release.sh

# Runs every unit package plus one quarter of the integration suite. CI only;
# `make test` stays the full local gate that covers the same tests in one run.
SHARD ?= 1
test-shard: ui
	scripts/test-shard.sh $(SHARD) 4

clean:
	rm -rf bin dist coverage web/dist
