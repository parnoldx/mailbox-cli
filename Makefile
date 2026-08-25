.PHONY: build test fmt fmt-check vet tidy tidy-check check clean install help

BINARY := $(CURDIR)/bin/mailbox

help:
	@echo "mailbox CLI"
	@echo ""
	@echo "Usage:"
	@echo "  make build      Build the CLI into bin/"
	@echo "  make test       Run tests"
	@echo "  make fmt        Format Go source files"
	@echo "  make fmt-check  Check formatting (CI gate)"
	@echo "  make vet        Run go vet"
	@echo "  make tidy-check Verify go.mod/go.sum tidiness"
	@echo "  make check      fmt-check + vet + test + tidy-check"
	@echo "  make clean      Remove build artifacts"
	@echo "  make install    Build and install into ~/.local/bin/mailbox"

# Build CLI
build:
	@mkdir -p bin
	go build -o $(BINARY) ./src/cmd/mailbox

# Run tests
test:
	go test ./src/internal/...

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

check: fmt-check vet test tidy-check

clean:
	rm -rf bin/

install: build
	install -D $(BINARY) ~/.local/bin/mailbox
