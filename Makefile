# Payment Reconciliation Engine — developer tasks.
# Override GO to point at a specific toolchain, e.g. `make test GO=/path/to/go`.
GO ?= go
BIN := bin

.PHONY: all build test cover lint fmt vet run-server run-example docker clean tidy

all: fmt vet test build

build: ## Build the reconcile CLI and the server into ./bin
	$(GO) build -o $(BIN)/reconcile ./cmd/reconcile
	$(GO) build -o $(BIN)/server ./cmd/server

test: ## Run the full test suite with the race detector
	$(GO) test ./... -race

cover: ## Run tests and open an HTML coverage report
	$(GO) test ./... -coverprofile=coverage.out
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "coverage report written to coverage.html"

lint: vet ## Alias for vet (add golangci-lint here if desired)

fmt: ## Format all Go source
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

run-server: ## Run the HTTP server on :8080
	$(GO) run ./cmd/server --addr :8080

run-example: ## Reconcile the committed sample files and print a text report
	$(GO) run ./cmd/reconcile --psp testdata/psp_settlement.csv --ledger testdata/internal_ledger.csv --format text

docker: ## Build the server container image
	docker build -t payment-reconciliation-engine:latest .

clean: ## Remove build and coverage artifacts
	rm -rf $(BIN) coverage.out coverage.html
