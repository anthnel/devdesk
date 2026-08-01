.PHONY: help build run clean install dev test test-race cover fmt vet tidy deps

# Variables
BINARY_NAME=dk
VERSION?=dev
BUILD_DIR=bin

help: ## Display this help message
	@echo "DevDesk - Makefile commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

run: build ## Build and run the application
	@./$(BUILD_DIR)/$(BINARY_NAME)

dev: ## Run directly with go run (faster for development)
	@go run .

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean
	@echo "Clean complete"

install: ## Install the binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install .
	@echo "$(BINARY_NAME) installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

test-race: ## Run tests with the race detector (requires a C toolchain: gcc/clang)
	@echo "Running tests with race detector..."
	@CGO_ENABLED=1 go test -race ./...

cover: ## Run tests and report coverage per package
	@go test -cover ./...

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Code formatted"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

tidy: ## Tidy dependencies
	@echo "Tidying dependencies..."
	@go mod tidy
	@echo "Dependencies tidied"

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@echo "Dependencies downloaded"

.DEFAULT_GOAL := help
