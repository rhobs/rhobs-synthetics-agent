# RHOBS Synthetics Agent

A synthetic monitoring agent for the Red Hat Observability Service (RHOBS) ecosystem that polls for probe configurations and executes synthetic monitoring tasks.

## Quick Start

### Building the Application

```bash
# Build the binary
make build

# Build and run immediately
make run
```

### Running Tests

```bash
# Run all tests with coverage
make test

# Generate detailed coverage report (creates coverage.html)
make coverage
```

### Code Quality

```bash
# Run linter (auto-installs golangci-lint if needed)
make lint

# Auto-fix linting issues
make lint-fix

# Run linter for CI environments
make lint-ci
```

## Development Workflow

### 1. Setup Dependencies
```bash
# Download Go modules
make go-mod-download

# Clean up dependencies
make go-mod-tidy
```

### 2. Development Cycle
```bash
# Make your changes, then run quality checks
make lint-fix
make test
make build
```

### 3. Testing Your Changes
```bash
# Run the agent with custom configuration
./rhobs-synthetics-agent start --log-level debug --interval 10s

# Or use make to build and run
make run
```

## Configuration

The agent can be configured via:

### Command Line Flags
```bash
./rhobs-synthetics-agent start \
  --config /path/to/config.yaml \
  --log-level debug \
  --interval 30s \
  --graceful-timeout 60s
```

### Environment Variables
```bash
export LOG_LEVEL=debug
export LOG_FORMAT=json
export POLLING_INTERVAL=30s
export GRACEFUL_TIMEOUT=60s
./rhobs-synthetics-agent start
```

### Configuration File
```yaml
# config.yaml
log_level: debug
log_format: json
polling_interval: 30s
graceful_timeout: 60s
```

## Development

### Running Tests
```bash
# Run all tests
make test

# Run specific test
go test -v ./internal/agent -run TestAgent_Run

# Run tests with race detection
go test -race ./...

# Generate coverage report
make coverage
open coverage.html
```

