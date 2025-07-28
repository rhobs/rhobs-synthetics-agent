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

.PHONY: all build clean run help lint lint-ci tidy docker-build docker-push

all: build

# Build the Go binary
build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "$(BINARY_NAME) built successfully."

lint:
	$(GOBIN)/golangci-lint run ./...

lint-ci:
	$(GOBIN)/golangci-lint run ./... --output.text.path=stdout --timeout=5m

test:
	go test -cover ./...

tidy:
	go mod tidy

# Build the Docker image
docker-build:
	@echo "Building Docker image for linux/amd64: $(IMAGE_URL):$(TAG)"
	$(CONTAINER_ENGINE) build --platform linux/amd64 -t $(IMAGE_URL):$(TAG) .

# Push the Docker image to the registry
docker-push:
	@echo "Pushing Docker image: $(IMAGE_URL):$(TAG)"
	$(CONTAINER_ENGINE) push $(IMAGE_URL):$(TAG)

# Clean up build artifacts
clean:
	@echo "Cleaning up..."
	@go clean
	@rm -f $(BINARY_NAME)
	@echo "Cleanup complete."

