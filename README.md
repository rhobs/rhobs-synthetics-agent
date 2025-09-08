# RHOBS Synthetics Agent

A synthetic monitoring agent for the Red Hat Observability Service (RHOBS) ecosystem that retrieves synthetic probe configurations from the RHOBS Probes API and reconciles them locally by creating `probe.monitoring.rhobs` Custom Resources for automated instantiation of synthetic monitoring probes.

## Overview

The RHOBS Synthetics Agent provides:
- **Multi-API Integration**: Polls multiple RHOBS Probes APIs simultaneously to retrieve probe configurations
- **URL Validation**: Validates target URLs before creating monitoring resources
- **Custom Resource Management**: Creates Probe CRs in Kubernetes (auto-detects `monitoring.rhobs/v1` or `monitoring.coreos.com/v1`)
- **Status Tracking**: Updates probe status (active/failed) via API calls across all configured APIs
- **Label-based Filtering**: Uses configurable label selectors to target specific probes
- **Probe Deduplication**: Automatically removes duplicate probes when fetching from multiple APIs

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

1. **Poll APIs**: Retrieves probe configurations from multiple `/api/metrics/v1/{tenant}/probes` endpoints
2. **Deduplicate**: Removes duplicate probes by URL when multiple APIs return probes monitoring the same endpoint
3. **Filter Probes**: Uses label selectors (e.g., `private=false,rhobs-synthetics/status=pending`)
4. **Validate URLs**: Checks if target URLs are ready for monitoring
5. **Create Resources**: Generates Probe Custom Resources (auto-detects API group)
6. **Update Status**: Reports success/failure back to all relevant APIs via PATCH calls

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
api_base_urls:
  - "https://observatorium-api-1.example.com"
  - "https://observatorium-api-2.example.com"
  - "https://observatorium-api-3.example.com"

api_tenant: "my-rhobs-tenant"
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

# API configuration - comma-separated list of complete URLs
export API_URLS="https://api1.example.com/api/metrics/v1/my-rhobs-tenant/probes,https://api2.example.com/api/metrics/v1/my-rhobs-tenant/probes,https://api3.example.com/api/metrics/v1/my-rhobs-tenant/probes"
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
  --graceful-timeout 60s \
  --api-urls "https://api1.example.com/api/metrics/v1/my-tenant/probes,https://api2.example.com/api/metrics/v1/my-tenant/probes"
```

## Architecture

### Components

- **API Client** (`internal/api`): Handles communication with RHOBS Probes API
- **Probe Manager** (`internal/k8s`): Manages Custom Resource creation and URL validation
- **Worker** (`internal/agent`): Orchestrates the reconciliation process
- **Configuration** (`internal/agent`): Handles all configuration sources

### Data Flow

```
Multiple APIs → Agent → Deduplication → URL Validation → Custom Resource Creation → Status Update
```

### Custom Resource Format

Generated Probe resources include (example shows `monitoring.rhobs/v1`, but `monitoring.coreos.com/v1` is also supported):

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
  prober_url: "http://synthetics-blackbox-prober-default:9115"
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
- Appropriate RBAC for Probe resources (monitoring.rhobs/v1 and/or monitoring.coreos.com/v1)

## Troubleshooting

### Common Issues

1. **API Connection Failures**
   - Verify `API_URLS` and network connectivity
   - Check API credentials and ensure URLs include the correct tenant

2. **URL Validation Failures**
   - Review target URLs in probe configurations
   - Check firewall/network policies for outbound requests

3. **Custom Resource Creation Issues**
   - Verify Kubernetes permissions and RBAC
   - Check if CRDs are installed: `kubectl get crd probes.monitoring.rhobs probes.monitoring.coreos.com`

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

