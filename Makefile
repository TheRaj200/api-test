BINARY ?= api-test
PKG    := ./cmd/api

.PHONY: run build test test-integration race vet fmt check

run: ## Run the API locally with go run
	go run $(PKG)

build: ## Build a stripped static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/$(BINARY) $(PKG)

test: ## Unit tests (no database required)
	go test ./...

test-integration: ## Unit + Postgres tests (needs TEST_DATABASE_URL)
	go test -count=1 ./...

race: ## Unit tests with the race detector
	go test -race ./...

vet: ## Static analysis
	go vet ./...

fmt: ## Format Go sources
	gofmt -l -w cmd internal

check: ## Verify formatting, analyse and test, same as CI
	@test -z "$(shell gofmt -l cmd internal)" || (echo "Go files need formatting; run make fmt" && exit 1)
	go vet ./...
	go test ./...
