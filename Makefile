# Makefile for building the rhobs-synthetics-agent binary

# Replace 'your-quay-namespace' with your actual Quay.io namespace.
IMAGE_URL ?= quay.io/app-sre/rhobs/rhobs-synthetics-agent
# Image tag, defaults to 'latest'
TAG ?= latest
# Namespace where the resources will be deployed
NAMESPACE ?= rhobs
# Konflux manifests directory
KONFLUX_DIR := konflux
# The name of the binary to be built
BINARY_NAME=rhobs-synthetics-agent
# The main package of the application
MAIN_PACKAGE=./cmd/agent/main.go
# podman vs. docker
CONTAINER_ENGINE ?= podman
TESTOPTS ?= -cover

.PHONY: all build clean run help lint lint-fix lint-ci go-mod-tidy go-mod-download test coverage docker-build docker-push

all: build

# Build the Go binary
build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "$(BINARY_NAME) built successfully."

# Golangci-lint setup similar to API project
GOLANGCI_LINT_VERSION ?= v2.0.2
GOLANGCI_LINT_BIN := $(shell go env GOPATH)/bin/golangci-lint

lint: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) run ./...

lint-ci: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) run ./... --output.text.path=stdout --timeout=5m

lint-fix: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) run --fix ./...

$(GOLANGCI_LINT_BIN):
	@echo "Checking for golangci-lint..."
	@if [ ! -f "$@" ]; then \
		echo "golangci-lint not found. Installing $(GOLANGCI_LINT_VERSION)..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(dir $@) $(GOLANGCI_LINT_VERSION); \
	else \
		echo "golangci-lint already installed."; \
	fi

test: go-mod-download
	go test $(TESTOPTS) ./...

.PHONY: coverage
coverage:
	@echo "Running tests with coverage report..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@go tool cover -func=coverage.out

go-mod-tidy:
	go mod tidy

go-mod-download:
	go mod download

# Build the Docker image
docker-build:
	@echo "Building Docker image for linux/amd64: $(IMAGE_URL):$(TAG)"
	$(CONTAINER_ENGINE) build --platform linux/amd64 -t $(IMAGE_URL):$(TAG) .

# Push the Docker image to the registry
docker-push:
	@echo "Pushing Docker image: $(IMAGE_URL):$(TAG)"
	$(CONTAINER_ENGINE) push $(IMAGE_URL):$(TAG)

# Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME) start

# Clean up build artifacts
clean:
	@echo "Cleaning up..."
	@go clean
	@rm -f $(BINARY_NAME)
	@rm -f coverage.out coverage.html
	@$(CONTAINER_ENGINE) rmi -f $(IMAGE_URL):$(TAG) 2>/dev/null || true
	@echo "Cleanup complete."

# Display help information
help:
	@echo "Available targets:"
	@echo "  build         - Build the Go binary"
	@echo "  test          - Run tests with coverage"
	@echo "  coverage      - Generate detailed coverage report"
	@echo "  lint          - Run golangci-lint"
	@echo "  lint-fix      - Run golangci-lint with --fix"
	@echo "  lint-ci       - Run golangci-lint for CI"
	@echo "  go-mod-tidy   - Run go mod tidy"
	@echo "  go-mod-download - Run go mod download"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-push   - Push Docker image to registry"
	@echo "  run           - Build and run the application"
	@echo "  clean         - Clean up build artifacts"
	@echo "  help          - Show this help message"

