# mise.toml is the source of truth for the toolchain and every task.
# This Makefile forwards to it so `make <target>` keeps working.
#
#   mise install     install the pinned toolchain
#   mise tasks       list every task
#   mise run check   the full gate
#
# Add or change a task in mise.toml, not here. A target only needs a line in
# this file when it should also be reachable as `make <target>`.
MISE?=mise

.PHONY: all help build install run clean test fmt fmt-check lint
.PHONY: deadcode deadcode-update tidy check

all: build

build:
	@$(MISE) run build

install:
	@$(MISE) run install

run:
	@$(MISE) run run

clean:
	@$(MISE) run clean

test:
	@$(MISE) run test

fmt:
	@$(MISE) run fmt

fmt-check:
	@$(MISE) run fmt-check

lint:
	@$(MISE) run lint

deadcode:
	@$(MISE) run deadcode

deadcode-update:
	@$(MISE) run deadcode-update

tidy:
	@$(MISE) run tidy

check:
	@$(MISE) run check

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
