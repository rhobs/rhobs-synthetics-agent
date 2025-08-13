# RHOBS Synthetics Agent

A synthetic monitoring agent for the Red Hat Observability Service (RHOBS) ecosystem that retrieves synthetic probe configurations from the RHOBS Probes API and reconciles them locally by creating `probe.monitoring.rhobs` Custom Resources for automated instantiation of synthetic monitoring probes.

## Overview

The RHOBS Synthetics Agent provides:
- **API Integration**: Polls the RHOBS Probes API to retrieve probe configurations
- **URL Validation**: Validates target URLs before creating monitoring resources  
- **Custom Resource Management**: Creates `probe.monitoring.rhobs` CRs in Kubernetes
- **Status Tracking**: Updates probe status (created/failed) via API calls
- **Label-based Filtering**: Uses configurable label selectors to target specific probes

## Quick Start

### Building the Application

```bash
# Build the binary
make build

# Build and run immediately  
make run

# Build with specific configuration
./rhobs-synthetics-agent start --config example-config.yaml
```

### Running Tests

```bash
# Run all tests with coverage
make test

# Generate detailed coverage report (creates coverage.html)
make coverage

# Run tests with race detection
make test-race
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

## Core Functionality

### Probe Reconciliation Workflow

1. **Poll API**: Retrieves probe configurations from `/api/metrics/v1/{tenant}/probes`
2. **Filter Probes**: Uses label selectors (e.g., `private=false,rhobs-synthetics/status=pending`)
3. **Validate URLs**: Checks if target URLs are ready for monitoring
4. **Create Resources**: Generates `probe.monitoring.rhobs` Custom Resources
5. **Update Status**: Reports success/failure back to the API via PATCH calls

### Label Selector Support

The agent uses configurable label selectors to filter probes:
```bash
# Example: Only process non-private, pending probes
LABEL_SELECTOR="private=false,rhobs-synthetics/status=pending"
```

### URL Validation

Before creating monitoring resources, the agent validates target URLs to prevent false positive alerts:
- Checks URL format and scheme (HTTP/HTTPS only)
- Performs HEAD requests to verify accessibility
- Handles server errors appropriately (5xx = validation failure, 4xx = acceptable)

## Configuration

### Full Configuration Example

```yaml
# config.yaml
log_level: debug
log_format: json
polling_interval: 30s
graceful_timeout: 60s

# API Configuration
api_base_url: "https://observatorium-api.example.com"
api_tenant: "my-tenant"
label_selector: "private=false,rhobs-synthetics/status=pending"

# Kubernetes Configuration  
namespace: "monitoring"
```

### Environment Variables

```bash
# Core settings
export LOG_LEVEL=debug
export LOG_FORMAT=json
export POLLING_INTERVAL=30s
export GRACEFUL_TIMEOUT=60s

# API configuration
export API_BASE_URL="https://observatorium-api.example.com"
export API_TENANT="my-tenant"
export LABEL_SELECTOR="private=false,rhobs-synthetics/status=pending"

# Kubernetes settings
export NAMESPACE="monitoring"

./rhobs-synthetics-agent start
```

### Command Line Flags

```bash
./rhobs-synthetics-agent start \
  --config /path/to/config.yaml \
  --log-level debug \
  --interval 30s \
  --graceful-timeout 60s
```

## Architecture

### Components

- **API Client** (`internal/api`): Handles communication with RHOBS Probes API
- **Probe Manager** (`internal/k8s`): Manages Custom Resource creation and URL validation
- **Worker** (`internal/agent`): Orchestrates the reconciliation process
- **Configuration** (`internal/agent`): Handles all configuration sources

### Data Flow

```
RHOBS Probes API → Agent → URL Validation → Custom Resource Creation → Status Update
```

### Custom Resource Format

Generated `probe.monitoring.rhobs` resources include:

```yaml
apiVersion: monitoring.rhobs/v1
kind: Probe
metadata:
  name: probe-{id}
  namespace: monitoring
  labels:
    rhobs.monitoring/probe-id: "{id}"
    rhobs.monitoring/cluster-id: "{cluster_id}"
    rhobs.monitoring/management-cluster: "{management_cluster_id}"
    rhobs.monitoring/managed-by: "rhobs-synthetics-agent"
spec:
  interval: "30s"
  module: "http_2xx" 
  prober_url: "http://blackbox-exporter:9115"
  targets:
    staticConfig:
      static:
        - "{target_url}"
      labels:
        apiserver_url: "{target_url}"
        cluster_id: "{cluster_id}"
        management_cluster_id: "{management_cluster_id}"
        private: "{private_flag}"
```

## Development

### Setup Dependencies

```bash
# Download Go modules
make go-mod-download

# Clean up dependencies
make go-mod-tidy
```

### Development Cycle

```bash
# Make your changes, then run quality checks
make lint-fix
make test
make build

# Run with development settings
./rhobs-synthetics-agent start --log-level debug --interval 10s
```

### Testing

```bash
# Run all tests
make test

# Run with coverage report
make coverage

# Run specific package tests
go test -v ./internal/api -run TestClient

# Run integration tests
go test -v ./internal/agent -run TestWorker_FullIntegration

# Test with race detection
make test-race
```

### Test Coverage

Current test coverage:
- **Internal/Agent**: 67.3% (core functionality, worker processes)
- **Internal/API**: 90.9% (API client, error handling)
- **Internal/K8s**: 93.1% (resource creation, validation)

See `TEST_COVERAGE_SUMMARY.md` for detailed coverage information.

## API Reference

### RHOBS Probes API Endpoints

- `GET /api/metrics/v1/{tenant}/probes?label_selector={selectors}` - Retrieve probes
- `PATCH /api/metrics/v1/{tenant}/probes/{id}` - Update probe status

### Probe Data Model

```json
{
  "id": "probe-123",
  "static_url": "https://api.example.com/health",
  "labels": {
    "cluster_id": "cluster-456",
    "management_cluster_id": "mgmt-789",
    "private": "false"
  },
  "status": "pending"
}
```

## Deployment

### Docker Build

```bash
# Build Docker image
make docker-build

# Push to registry
make docker-push
```

### Kubernetes Deployment

The agent is designed to run as a Kubernetes Deployment with:
- Service account with permissions to create Custom Resources
- ConfigMap or Secret for API credentials
- Appropriate RBAC for probe.monitoring.rhobs resources

## Troubleshooting

### Common Issues

1. **API Connection Failures**
   - Verify `API_BASE_URL` and network connectivity
   - Check API credentials and tenant configuration

2. **URL Validation Failures**
   - Review target URLs in probe configurations
   - Check firewall/network policies for outbound requests

3. **Custom Resource Creation Issues**
   - Verify Kubernetes permissions and RBAC
   - Check if CRD is installed: `kubectl get crd probe.monitoring.rhobs`

### Debug Mode

```bash
# Enable debug logging
./rhobs-synthetics-agent start --log-level debug

# Use shorter polling interval for testing
./rhobs-synthetics-agent start --interval 10s --log-level debug
```

## Contributing

### Code Style

- Follow Go conventions and best practices
- Run `make lint-fix` before submitting changes
- Ensure all tests pass: `make test`
- Add tests for new functionality

### Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make changes with appropriate tests
4. Run quality checks: `make lint test`
5. Submit pull request with clear description

## License

This project follows the same license as the RHOBS ecosystem.

