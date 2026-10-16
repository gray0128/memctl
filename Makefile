GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test clean
all: build

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/memctl ./cmd/memctl

test:
	$(GO) test ./...

clean:
	rm -rf bin dist coverage
