BINARY   ?= alpha
MAIN_SRC  = ./cmd

GOBIN    ?= $(shell go env GOBIN)
GOPATH   ?= $(shell go env GOPATH)
ifeq ($(GOBIN),)
GOBIN     = $(GOPATH)/bin
endif

GO       ?= go
GOFLAGS  ?= -ldflags="-s -w"
CGO      ?= 0

.PHONY: all build install run clean test fmt fmt-check lint deadcode check help

all: build

build:
	CGO_ENABLED=$(CGO) $(GO) build $(GOFLAGS) -o $(BINARY) $(MAIN_SRC)

install: build
	@mkdir -p $(GOBIN)
	mv $(BINARY) $(GOBIN)/$(BINARY)
	@echo "installed $(BINARY) -> $(GOBIN)/$(BINARY)"

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	$(GO) clean

test:
	$(GO) test ./...

# Apply gofumpt / goimports / golines via .golangci.yml formatters.
fmt:
	golangci-lint fmt ./...

# Fail if formatting would change files (used by CI).
fmt-check:
	golangci-lint fmt --diff ./...

lint:
	golangci-lint run ./...

# Whole-program reachability from main. Catches exported functions that
# nothing calls, which `unused` cannot see. Fails only on new findings.
deadcode:
	./scripts/deadcode.sh

check: fmt-check lint deadcode test

help:
	@echo "Usage:"
	@echo "  make          - build binary ($(BINARY))"
	@echo "  make install  - build & install to \$$GOBIN ($(GOBIN))"
	@echo "  make run      - build & run"
	@echo "  make clean    - remove binary & cache"
	@echo "  make test     - run all tests"
	@echo "  make fmt      - format Go sources (gofumpt/goimports/golines)"
	@echo "  make fmt-check - check formatting without writing (CI)"
	@echo "  make lint     - run golangci-lint"
	@echo "  make deadcode - report unreachable functions (fails on new ones)"
	@echo "  make check    - fmt-check + lint + deadcode + test"
