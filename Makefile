.PHONY: build test race vet verify verify-full

build:
	go build -o bin/docdex ./cmd/docdex

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

verify: test race vet build

verify-full: verify
	go test -count=3 ./internal/indexer ./internal/retrieval
	DOCDEX_E2E=1 go test -count=1 ./internal/e2e
