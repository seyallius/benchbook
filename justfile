# justfile - the developer shortcuts for benchbook itself.
#
# `just --list` shows this list. Every recipe is a thin wrapper around a command
# that you could type yourself; nothing here is required to build the tool.

set shell := ["bash", "-euo", "pipefail", "-c"]

# the Go toolchain to use; override with `just GO=/usr/local/go/bin/go test`
GO := "go"

# build the binary into ./bin
demo:
    {{GO}} build -o bin/benchbook ./cmd/benchbook

default: demo

# run the whole test suite
test:
    {{GO}} test ./...

# run the whole test suite with the race detector
race:
    {{GO}} test -race ./...

# gofmt + vet, the two checks CI runs first
vet:
    @files="$$(gofmt -l . | grep -v '^vendor/' || true)"; \
    if [ -n "$$files" ]; then echo "gofmt needed on:"; echo "$$files"; exit 1; fi
    {{GO}} vet ./...

# staticcheck, when it is installed
lint:
    staticcheck ./...

# the CLI integration suite: a temporary project, a fake runner, no toolchain
bench-check:
    {{GO}} test -v ./internal/cli/

# refresh the golden Markdown pages after an intentional rendering change
golden:
    UPDATE_GOLDEN=1 {{GO}} test ./internal/render/

# re-record the golden files and review the diff before committing
golden-review: golden
    git --no-pager diff --stat internal/render/testdata/golden

# verify the browser comparer against the shared Go/SciPy vectors
compare-js:
    node scripts/test-compare.mjs

# download, verify and install the pinned front-end assets (needs network)
vendor-fetch:
    bash scripts/vendor.sh

# verify the vendored assets without touching the network
vendor:
    bash scripts/vendor.sh --verify

# everything CI runs, in the order it runs it
ci: vet test compare-js vendor
