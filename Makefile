BINARY := orchard
PKG := ./...

# Version baked into the binary; derived from git tags, "dev" outside a checkout.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

# Install location. Defaults to ~/.local/bin (already on PATH for most setups).
# Override for a different dir, e.g. `make install PREFIX=/usr/local/bin`.
PREFIX ?= $(HOME)/.local/bin

.DEFAULT_GOAL := build

## build: compile the orchard binary into ./orchard
.PHONY: build
build:
	go build $(LDFLAGS) -o $(BINARY) .

## install: build with the version baked in and install into $(PREFIX) (default ~/.local/bin)
.PHONY: install
install:
	@mkdir -p $(PREFIX)
	go build $(LDFLAGS) -o $(PREFIX)/$(BINARY) .
	@echo "installed $(PREFIX)/$(BINARY) ($(VERSION))"

## run: build and start the TUI
.PHONY: run
run:
	go run .

## test: run the full test suite
.PHONY: test
test:
	go test $(PKG)

## test-race: run tests with the race detector
.PHONY: test-race
test-race:
	go test -race $(PKG)

## cover: run tests and open an HTML coverage report
.PHONY: cover
cover:
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

## fmt: format all Go code
.PHONY: fmt
fmt:
	gofmt -w .

## vet: run go vet
.PHONY: vet
vet:
	go vet $(PKG)

## lint: verify formatting and run go vet (CI gate, no changes made)
.PHONY: lint
lint:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }
	go vet $(PKG)

## tidy: sync go.mod / go.sum
.PHONY: tidy
tidy:
	go mod tidy

## check: everything CI runs - lint + race tests + build (run before committing)
.PHONY: check
check: lint test-race build

## clean: remove build and coverage artifacts
.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | awk -F': ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
