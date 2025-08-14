# End-to-End Tests

This directory contains end-to-end tests for the RHOBS Synthetics Agent that test the complete integration between the agent and the RHOBS Synthetics API.

## Overview

The e2e tests verify:
- Agent can successfully communicate with the RHOBS Synthetics API
- Probe fetching and filtering works correctly with label selectors
- Probe status updates are handled properly
- Agent gracefully handles API unavailability
- Different configuration scenarios work as expected

## Test Structure

### Files

- `e2e_test.go` - End-to-end test suite using mock API server
- `e2e_real_api_test.go` - End-to-end test suite using real RHOBS Synthetics API
- `mock_api_server.go` - Mock implementation of the RHOBS Synthetics API
- `api_manager.go` - Manager for starting/stopping real API server during tests
- `README.md` - This documentation

### Test Cases

#### Mock API Tests (`e2e_test.go`)

1. **TestAgent_E2E_WithAPI** - Main e2e test using mock API:
   - Starts a mock API server with test probes
   - Configures and runs the agent against the mock API
   - Verifies probe fetching, status updates, and label filtering
   - Tests graceful shutdown

2. **TestAgent_E2E_ErrorHandling** - Tests error scenarios:
   - Agent behavior when API is unavailable
   - Graceful error handling and recovery

3. **TestAgent_E2E_ConfigurationVariations** - Tests different configurations:
   - No label selector (gets all probes)
   - Restrictive label selectors
   - Various logging and polling configurations

#### Real API Tests (`e2e_real_api_test.go`)

1. **TestAgent_E2E_WithRealAPI** - Main e2e test using real RHOBS Synthetics API:
   - Builds and starts the actual RHOBS Synthetics API server
   - Seeds the API with test data
   - Runs the agent against the real API
   - Verifies all integration functionality

2. **TestAgent_E2E_RealAPI_ErrorHandling** - Tests error scenarios with real API:
   - Agent behavior when real API is unavailable
   - Graceful error handling and recovery

3. **TestAgent_E2E_RealAPI_ConfigurationVariations** - Tests different configurations with real API:
   - Various label selector scenarios
   - Different API configurations

## Running the Tests

### Prerequisites

- Go 1.24.1 or later
- All dependencies installed (`go mod download`)
- For real API tests: RHOBS Synthetics API source code (configurable via `RHOBS_SYNTHETICS_API_PATH` environment variable - **must be an absolute path**)

### Running E2E Tests

#### Mock API Tests (Faster)

```bash
# Run e2e tests with mock API server
make test-e2e

# Run mock API tests with verbose output
go test -v ./test/e2e -run "TestAgent_E2E_WithAPI"

# Run specific mock test
go test -v ./test/e2e -run TestAgent_E2E_WithAPI/VerifyProbesFetched
```

#### Real API Tests (More Comprehensive)

**Note:** These tests require `RHOBS_SYNTHETICS_API_PATH` to be set to an absolute path pointing to the RHOBS Synthetics API source code.

```bash
# Set the API path (must be absolute)
export RHOBS_SYNTHETICS_API_PATH="/absolute/path/to/rhobs-synthetics-api"

# Run e2e tests with real RHOBS Synthetics API
make test-e2e-real

# Run real API tests with verbose output
go test -v ./test/e2e -run "TestAgent_E2E_.*RealAPI"

# Run specific real API test
go test -v ./test/e2e -run TestAgent_E2E_WithRealAPI
```

#### All Tests

**Note:** Running all tests requires `RHOBS_SYNTHETICS_API_PATH` to be set to an absolute path.

```bash
# Set the API path (must be absolute)
export RHOBS_SYNTHETICS_API_PATH="/absolute/path/to/rhobs-synthetics-api"

# Run all e2e tests (mock + real API)
make test-e2e-all

# Run with extended timeout
go test -v ./test/e2e -timeout 90s
```

### Running All Tests

```bash
# Run all tests (unit + integration + e2e mock)
make test

# Run with race detection
make test-race

# Generate coverage report
make coverage
```

## Test Infrastructure

### Mock API Server

The `MockAPIServer` provides a lightweight test implementation of the RHOBS Synthetics API with the following features:

#### Endpoints

- `GET /probes` - List probes with optional label selector filtering
- `GET /probes/{id}` - Get specific probe by ID
- `PATCH /probes/{id}` - Update probe status

#### Default Test Data

The mock server comes pre-loaded with test probes:

```go
// Probe 1
{
    ID:        "test-probe-1",
    StaticURL: "https://example.com/health",
    Labels: {
        "env":     "test",
        "private": "false",
        "rhobs-synthetics/status": "pending",
    },
    Status: "pending",
}

// Probe 2
{
    ID:        "test-probe-2", 
    StaticURL: "https://api.example.com/status",
    Labels: {
        "env":     "test",
        "private": "false", 
        "rhobs-synthetics/status": "pending",
    },
    Status: "pending",
}
```

#### Label Selector Support

Both mock and real API servers support label selectors in the format: `key1=value1,key2=value2`

Examples:
- `env=test` - Get probes with env=test
- `env=test,private=false` - Get probes with both labels
- `nonexistent=value` - Returns no probes

### Real API Manager

The `RealAPIManager` provides automated management of the actual RHOBS Synthetics API server for comprehensive testing:

#### Features

- **Automatic Build**: Builds the RHOBS Synthetics API binary from source
- **Port Management**: Automatically finds available ports to avoid conflicts
- **Lifecycle Management**: Handles startup, readiness checks, and graceful shutdown
- **Test Data Seeding**: Automatically creates test probes for consistent testing
- **Cleanup**: Removes temporary data and processes after tests

#### API Server Configuration

The real API server is started with:
- **Database Engine**: `local` (in-memory storage for testing)
- **Data Directory**: Temporary directory (cleaned up after tests)
- **Port**: Automatically selected available port
- **Log Level**: `debug` for comprehensive test output
- **Graceful Timeout**: `5s` for quick test cycles

#### Requirements

- RHOBS Synthetics API source code must be available (path configurable via `RHOBS_SYNTHETICS_API_PATH` environment variable - **must be an absolute path**)
- Go build environment configured
- No conflicting processes on ports 8080-8099

## Test Configuration

The e2e tests use the following agent configuration:

```go
&agent.Config{
    LogLevel:        "debug",
    LogFormat:       "text",
    PollingInterval: 2 * time.Second,
    GracefulTimeout: 5 * time.Second,
    APIBaseURL:      mockAPI.URL,        // Points to mock server
    APIEndpoint:     "/probes",
    LabelSelector:   "env=test,private=false",
}
```

## Debugging Tests

### Verbose Output

Run tests with `-v` flag to see detailed output:

```bash
go test -v ./test/e2e
```

### Test Logs

The agent runs with debug logging enabled during tests. Check test output for agent logs.

### Mock Server Inspection

You can add debugging to the mock server by modifying `mock_api_server.go` to log requests:

```go
func (m *MockAPIServer) handleProbes(w http.ResponseWriter, r *http.Request) {
    log.Printf("Mock API received request: %s %s", r.Method, r.URL.String())
    // ... rest of handler
}
```

## Integration with CI/CD

The e2e tests are designed to run in CI/CD environments:

- Tests use a short timeout (30s by default)
- No external dependencies required
- Deterministic test data
- Proper cleanup and resource management

Add to your CI pipeline:

```yaml
- name: Run E2E Tests
  run: make test-e2e
```

## Adding New Tests

To add new e2e test scenarios:

1. Add test functions to `e2e_test.go`
2. Use the existing `MockAPIServer` or extend it as needed
3. Follow the pattern of creating agent config, running briefly, and verifying behavior
4. Ensure proper cleanup and timeout handling

Example:

```go
func TestAgent_E2E_NewScenario(t *testing.T) {
    mockAPI := NewMockAPIServer()
    defer mockAPI.Close()

    cfg := &agent.Config{
        // your test config
    }

    // Test implementation
}
```