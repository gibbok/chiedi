SHELL := /bin/bash
.DEFAULT_GOAL := help

BINARY := bin/chiedi
export CGO_ENABLED := 1
export CHIEDI_ASSETS := $(abspath bin/assets)

.PHONY: setup install package
setup: ## Fetch pinned model, tokenizer and native libraries (build-time only)
	python3 scripts/setup.py

install: build ## Install executable and assets under ~/.local (override PREFIX)
	bash scripts/install.sh

package: build ## Make a relocatable release archive for the current platform
	mkdir -p dist
	cp scripts/install.sh bin/install.sh
	printf '%s\n' 'Run: bash install.sh "$$(pwd)"' > bin/INSTALL.txt
	tar -czf dist/chiedi-$$(go env GOOS)-$$(go env GOARCH).tar.gz -C bin chiedi assets install.sh INSTALL.txt


.PHONY: help build clean deps test test-unit test-race race test-vet vet test-repeat test-e2e test-all demo verify verify-full

help: ## Show the available developer commands
	@printf '%s\n' \
	  'chiedi developer commands' \
	  '' \
	  '  make benchmark    Measure personal-use search performance (opt-in)' \
	  '  make demo         Build and run a readable local walkthrough' \
	  '  make test-all     Run every automated verification layer' \
	  '  make test-e2e     Run the real-binary functional acceptance test' \
	  '  make test-unit    Run all Go tests once' \
	  '  make test-race    Run all tests with the race detector' \
	  '  make test-repeat  Repeat incremental and retrieval tests three times' \
	  '  make test-vet     Run Go static analysis' \
	  '  make deps         Verify downloaded module checksums' \
	  '  make build        Build bin/chiedi with bundled offline E5 assets' \
	  '  make install      Install everything under ~/.local' \
	  '  make package      Create a platform release archive' \
	  '  make clean        Remove local build output' \
	  '' \
	  'See docs/testing.md for coverage and expected results.'

build: setup ## Build the chiedi executable
	go build -o $(BINARY) ./cmd/chiedi

clean: ## Remove local build output
	rm -rf bin

deps: ## Verify module downloads against go.sum
	go mod verify

test: test-unit ## Backward-compatible alias for test-unit

test-unit: setup ## Run all tests once
	go test -p 1 ./...

test-race: setup ## Run all tests with the Go race detector
	go test -p 1 -race ./...

race: test-race ## Backward-compatible alias for test-race

test-vet: setup ## Run Go static analysis
	go vet ./...

vet: test-vet ## Backward-compatible alias for test-vet

test-repeat: setup ## Repeat the state-sensitive packages to expose flakes
	go test -p 1 -count=3 ./internal/indexer ./internal/retrieval

test-e2e: setup ## Run the real compiled binary through the acceptance scenario
	CHIEDI_E2E=1 go test -p 1 -v -count=1 ./internal/e2e

test-install: ## Check installation upgrades and path handling
	python3 scripts/install_test.py

.PHONY: test-install

test-all: ## Run dependency, unit, race, vet, build, repeat, and E2E checks
	$(MAKE) test-install
	$(MAKE) deps
	$(MAKE) test-unit
	$(MAKE) test-race
	$(MAKE) test-vet
	$(MAKE) build
	$(MAKE) test-repeat
	$(MAKE) test-e2e
	$(MAKE) demo

demo: build ## Exercise the CLI and MCP using an isolated temporary corpus
	./scripts/demo.sh ./$(BINARY)

verify: test-all ## Backward-compatible complete verification alias

verify-full: test-all ## Backward-compatible complete verification alias

BENCHMARK_SIZES ?= 100,500,2000
BENCHMARK_REPEATS ?= 30

.PHONY: benchmark
benchmark: setup ## Explicitly run the personal-use search benchmark (never part of verify)
	go test -p 1 -tags benchmark -count=1 ./benchmarks/search
	go run -tags benchmark ./benchmarks/search -sizes '$(BENCHMARK_SIZES)' -repeats '$(BENCHMARK_REPEATS)'
