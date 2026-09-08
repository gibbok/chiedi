SHELL := /bin/bash
.DEFAULT_GOAL := help

BINARY := bin/chiedi

.PHONY: help build clean deps test test-unit test-race race test-vet vet test-repeat test-e2e test-all demo verify verify-full

help: ## Show the available developer commands
	@printf '%s\n' \
	  'chiedi developer commands' \
	  '' \
	  '  make demo         Build and run a readable local walkthrough' \
	  '  make test-all     Run every automated verification layer' \
	  '  make test-e2e     Run the real-binary functional acceptance test' \
	  '  make test-unit    Run all Go tests once' \
	  '  make test-race    Run all tests with the race detector' \
	  '  make test-repeat  Repeat incremental and retrieval tests three times' \
	  '  make test-vet     Run Go static analysis' \
	  '  make deps         Verify downloaded module checksums' \
	  '  make build        Build bin/chiedi' \
	  '  make clean        Remove local build output' \
	  '' \
	  'See docs/testing.md for coverage and expected results.'

build: ## Build the chiedi executable
	go build -o $(BINARY) ./cmd/chiedi

clean: ## Remove local build output
	rm -rf bin

deps: ## Verify module downloads against go.sum
	go mod verify

test: test-unit ## Backward-compatible alias for test-unit

test-unit: ## Run all tests once
	go test ./...

test-race: ## Run all tests with the Go race detector
	go test -race ./...

race: test-race ## Backward-compatible alias for test-race

test-vet: ## Run Go static analysis
	go vet ./...

vet: test-vet ## Backward-compatible alias for test-vet

test-repeat: ## Repeat the state-sensitive packages to expose flakes
	go test -count=3 ./internal/indexer ./internal/retrieval

test-e2e: ## Run the real compiled binary through the acceptance scenario
	CHIEDI_E2E=1 go test -v -count=1 ./internal/e2e

test-all: ## Run dependency, unit, race, vet, build, repeat, and E2E checks
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
