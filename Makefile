.PHONY: build test test-race fmt fmt-check vet tidy tidy-check check clean install help

BINARY := $(CURDIR)/bin/mailbox
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X mailbox/src/internal/cli.Version=$(VERSION) -X mailbox/src/internal/cli.Commit=$(COMMIT) -X mailbox/src/internal/cli.Date=$(DATE)

help:
	@echo "mailbox CLI"
	@echo ""
	@echo "Usage:"
	@echo "  make build      Build the CLI into bin/"
	@echo "  make test       Run tests"
	@echo "  make test-race  Run tests with the race detector"
	@echo "  make fmt        Format Go source files"
	@echo "  make fmt-check  Check formatting (CI gate)"
	@echo "  make vet        Run go vet"
	@echo "  make tidy-check Verify go.mod/go.sum tidiness"
	@echo "  make check      fmt-check + vet + test-race + tidy-check"
	@echo "  make clean      Remove build artifacts"
	@echo "  make install    Build and install into ~/.local/bin/mailbox"

# Build CLI
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./src/cmd/mailbox

# Run tests
test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -s -w .

fmt-check:
	@test -z "$$(gofmt -l src)" || (echo "Files not formatted:"; gofmt -l src; exit 1)

vet:
	go vet ./...

tidy:
	go mod tidy

tidy-check:
	@set -eu; \
	trap 'mv -f go.mod.bak go.mod; mv -f go.sum.bak go.sum' EXIT; \
	cp go.mod go.mod.bak; \
	cp go.sum go.sum.bak; \
	go mod tidy; \
	if ! diff -q go.mod go.mod.bak >/dev/null || ! diff -q go.sum go.sum.bak >/dev/null; then \
		echo "go.mod or go.sum is not tidy — run 'make tidy'"; \
		exit 1; \
	fi

check: fmt-check vet test-race tidy-check

clean:
	rm -rf bin/

install: build
	install -D $(BINARY) ~/.local/bin/mailbox
