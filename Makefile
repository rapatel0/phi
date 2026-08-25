# mise.toml is the source of truth for the toolchain and every task.
# This Makefile forwards to it so `make <target>` keeps working.
#
#   mise install     install the pinned toolchain
#   mise tasks       list every task
#   mise run check   the full gate
#
# Add or change a task in mise.toml, not here. A target only needs a line in
# this file when it should also be reachable as `make <target>`.

.DEFAULT_GOAL := build

.PHONY: all help build install run clean test fmt fmt-check lint
.PHONY: deadcode deadcode-update tidy check

all: build

build:
	@mise run build

install:
	@mise run install

run:
	@mise run run

clean:
	@mise run clean

test:
	@mise run test

fmt:
	@mise run fmt

fmt-check:
	@mise run fmt-check

lint:
	@mise run lint

deadcode:
	@mise run deadcode

deadcode-update:
	@mise run deadcode-update

tidy:
	@mise run tidy

check:
	@mise run check

help:
	@echo "Usage: make <target>   (forwards to 'mise run <target>')"
	@echo
	@echo "  build           build ./alpha (CGO off, stripped)"
	@echo "  install         build and install into GOBIN"
	@echo "  run             build and start the TUI"
	@echo "  clean           remove the binary and Go cache artifacts"
	@echo "  test            run all Go tests"
	@echo "  fmt             apply gofumpt / goimports / golines"
	@echo "  fmt-check       fail if formatting would change files"
	@echo "  lint            run golangci-lint"
	@echo "  deadcode        report unreachable functions, fail on new ones"
	@echo "  deadcode-update rewrite the dead-code baseline"
	@echo "  tidy            tidy go.mod / go.sum"
	@echo "  check           fmt-check + lint + deadcode + test"
	@echo
	@echo "'mise tasks' lists everything, including tasks with no make target."
