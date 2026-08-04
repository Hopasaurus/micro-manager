# Makefile -- build and install the micro-manager Go CLI (mm) and, optionally,
# the GUI service (mm-ui). See README.md for the project itself.
#
# The Go implementation in implementations/golang is the canonical one (see
# AGENTS.md's "Current state"); this Makefile is a thin wrapper over its build
# so `make install` works from the repository root without having to remember
# the exact invocation. That invocation is not obvious: implementations/golang's
# own AGENTS.md warns that `go build -o mm ./cmd/mm` does not fail -- `mm` is
# also the library package directory, so Go silently writes the binary INTO it
# as mm/mm instead of refusing. Building to an explicit bin/ output, as every
# recipe below does, is what avoids that trap.
#
# Usage:
#   make               same as `make help`
#   make build         build ./implementations/golang/bin/mm
#   make build-ui       build ./implementations/golang/bin/mm-ui
#   make build-all      build both
#   make install         build mm, then install it to $(BINDIR)
#   make install-ui       build mm-ui, then install it to $(BINDIR)
#   make install-all      both of the above
#   make uninstall        remove mm from $(BINDIR)
#   make uninstall-ui      remove mm-ui from $(BINDIR)
#   make uninstall-all     both of the above
#   make test          go test ./... in the Go implementation
#   make vet           go vet ./... in the Go implementation
#   make check         ./check.sh --all at the repository root (the reference
#                      validator; see project/SKILL.md)
#   make clean         remove build output (implementations/golang/bin)
#   make help          list these targets
#
# PREFIX/DESTDIR override where `install` puts the binaries. PREFIX defaults to
# $(HOME)/.local -- a per-user path, no sudo required, and one most shells and
# distributions already put on PATH (~/.local/bin). For a system-wide install
# instead:
#
#   sudo make install PREFIX=/usr/local
#
# (PREFIX must be given explicitly under sudo: $(HOME) would otherwise resolve
# to root's home, which is not what a system-wide install means.)

GO       ?= go
INSTALL  ?= install
GO_DIR   := implementations/golang

PREFIX   ?= $(HOME)/.local
DESTDIR  ?=
BINDIR   := $(DESTDIR)$(PREFIX)/bin

.PHONY: all help build build-ui build-all \
        install install-ui install-all \
        uninstall uninstall-ui uninstall-all \
        test vet check clean

all: help

## help: list available targets
help:
	@echo "Targets:"
	@echo "  build           build implementations/golang/bin/mm"
	@echo "  build-ui        build implementations/golang/bin/mm-ui"
	@echo "  build-all       build both"
	@echo "  install         build mm and install it to \$$(BINDIR) [$(BINDIR)]"
	@echo "  install-ui      build mm-ui and install it to \$$(BINDIR)"
	@echo "  install-all     install both"
	@echo "  uninstall       remove mm from \$$(BINDIR)"
	@echo "  uninstall-ui    remove mm-ui from \$$(BINDIR)"
	@echo "  uninstall-all   remove both"
	@echo "  test            go test ./... in $(GO_DIR)"
	@echo "  vet             go vet ./... in $(GO_DIR)"
	@echo "  check           ./check.sh --all (the reference validator)"
	@echo "  clean           remove $(GO_DIR)/bin"
	@echo
	@echo "Override where install puts binaries with PREFIX (default $(HOME)/.local):"
	@echo "  make install PREFIX=/usr/local   # then likely: sudo make install PREFIX=/usr/local"

## build: build the mm CLI to implementations/golang/bin/mm
build:
	cd $(GO_DIR) && $(GO) build -o bin/mm ./cmd/mm

## build-ui: build the GUI service to implementations/golang/bin/mm-ui
build-ui:
	cd $(GO_DIR) && $(GO) build -o bin/mm-ui ./cmd/mm-ui

## build-all: build both mm and mm-ui
build-all: build build-ui

## install: build mm and install it to $(BINDIR)
install: build
	$(INSTALL) -d "$(BINDIR)"
	$(INSTALL) -m 0755 "$(GO_DIR)/bin/mm" "$(BINDIR)/mm"
	@echo "installed mm to $(BINDIR)/mm"
	@case ":$$PATH:" in \
		*":$(BINDIR):"*) ;; \
		*) echo "warning: $(BINDIR) is not on PATH -- add it, e.g. export PATH=\"$(BINDIR):\$$PATH\"" >&2 ;; \
	esac

## install-ui: build mm-ui and install it to $(BINDIR)
install-ui: build-ui
	$(INSTALL) -d "$(BINDIR)"
	$(INSTALL) -m 0755 "$(GO_DIR)/bin/mm-ui" "$(BINDIR)/mm-ui"
	@echo "installed mm-ui to $(BINDIR)/mm-ui"

## install-all: install both mm and mm-ui
install-all: install install-ui

## uninstall: remove mm from $(BINDIR)
uninstall:
	rm -f "$(BINDIR)/mm"

## uninstall-ui: remove mm-ui from $(BINDIR)
uninstall-ui:
	rm -f "$(BINDIR)/mm-ui"

## uninstall-all: remove both mm and mm-ui
uninstall-all: uninstall uninstall-ui

## test: go test ./... in the Go implementation
test:
	cd $(GO_DIR) && $(GO) test ./...

## vet: go vet ./... in the Go implementation
vet:
	cd $(GO_DIR) && $(GO) vet ./...

## check: the reference validator, over every micro-manager directory found
check:
	./check.sh --all

## clean: remove build output
clean:
	rm -rf $(GO_DIR)/bin
