# End-to-End Tests

This directory contains end-to-end tests for the RHOBS Synthetics Agent that test the complete integration between the agent and the RHOBS Synthetics API.

## Overview

The e2e tests verify:
- Agent can successfully communicate with the RHOBS Synthetics API
- Probe fetching and filtering works correctly with label selectors
- Probe status updates are handled properly
- Agent gracefully handles API unavailability
- Different configuration scenarios work as expected

### Full-Stack Integration Test

The **TestFullStackIntegration** test verifies the complete workflow of all three components working together:
1. Route Monitor Operator (RMO) - By default this pulls the RMO code from github to create probes from HostedControlPlane CustomResources. This can be overwritten to use a local repo.
2. RHOBS Synthetics API - API server stores and serves probe configurations
3. RHOBS Synthetics Agent - Fetches and executes probes

This test demonstrates the complete end-to-end flow from HostedControlPlane CR creation through RMO reconciliation to probe execution and cleanup.

## Test Structure

### Files

- `e2e_test.go` - End-to-end test suite using mock API server
- `e2e_real_api_test.go` - End-to-end test suite using real RHOBS Synthetics API
- `full_integration_test.go` - Full-stack integration test (RMO + API + Agent)
- `mock_api_server.go` - Mock implementation of the RHOBS Synthetics API
- `api_manager.go` - Manager for starting/stopping real API server during tests
- `run_full_integration.sh` - Helper script to run full-stack integration test
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

#### Full-Stack Integration Test (`full_integration_test.go`)

**TestFullStackIntegration** - Complete integration test using **ACTUAL RMO code**:

This test executes the entire lifecycle of monitoring a HostedControlPlane cluster using real RMO controller logic:

1. **RMO Creates Probe from HCP CR** (`RMO_Creates_Probe_From_HCP_CR` sub-test):
   - **Uses actual RMO HostedControlPlaneReconciler code** (not simulation)
   - Creates a HostedControlPlane CustomResource in a fake Kubernetes cluster
   - Sets up required K8s resources (Services, VpcEndpoints, Secrets)
   - Mocks Dynatrace API to allow RMO to complete its reconciliation
   - Triggers RMO's actual reconciliation logic which:
     - Validates VPC endpoint readiness
     - Deploys internal monitoring objects
     - Creates Dynatrace HTTP monitor
     - **Attempts to create RHOBS probe via synthetics-api**
   - Verifies RMO reached the `ensureRHOBSProbe` function
   - Labels include: cluster_id, private, source, resource_type, probe_type

2. **Agent Fetches and Executes Probe** (`Agent_Fetches_And_Executes_Probe` sub-test):
   - Starts the synthetics-agent configured to poll the API
   - Agent fetches probes matching label selector (private=false)
   - Verifies agent successfully retrieved the test probe
   - Tests graceful shutdown of the agent

3. **API Receives Probe Results** (`API_Receives_Probe_Results` sub-test):
   - Verifies the probe exists in the API
   - Checks that probe has a valid status (pending, active, failed, terminating)
   - Confirms API is properly storing probe state

4. **RMO Deletes Probe** (`RMO_Deletes_Probe` sub-test):
   - Simulates HostedControlPlane deletion
   - Deletes the probe via API (DELETE /probes/{id})
   - Verifies probe is marked as terminating or deleted

**Key Features:**
- **Uses actual RMO controller code** (imported as Go dependency)
- Uses real RHOBS Synthetics API server (built and started automatically)
- Mocks Dynatrace API and Kubernetes cluster (no external dependencies)
- Tests complete probe lifecycle from HCP CR creation to deletion
- Verifies all three components (RMO → API → Agent) can communicate correctly
- Automatic cleanup of resources after test completion

## Running the Tests

### Prerequisites

- Go 1.24.1 or later
- All dependencies installed (`go mod download`)
- **That's it!** RMO and API are automatically pulled from Go modules (just like any other dependency)

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

**Note:** The API is automatically found in the Go module cache. No manual setup required!

```bash
# Just run the tests - the API will be found automatically
make test-e2e-real

# Run real API tests with verbose output
go test -v ./test/e2e -run "TestAgent_E2E_.*RealAPI"

# Run specific real API test
go test -v ./test/e2e -run TestAgent_E2E_WithRealAPI
```

#### All Tests

```bash
# Run all e2e tests (mock + real API)
# No environment setup needed - everything is automatic!
make test-e2e-all

# Run with extended timeout
go test -v ./test/e2e -timeout 90s
```

#### Full-Stack Integration Test

**No setup required!** Both RMO and API are automatically pulled from Go modules.

```bash
# Just run it - everything is automatic!
make test-full-e2e

# Or run directly with go test
go test -v ./test/e2e -run TestFullStackIntegration -timeout 5m
```

**What happens during the test:**
1. **Automatic dependency resolution:** API is found in Go module cache (downloaded via `go mod download`)
2. Mock Dynatrace server starts (for RMO integration)
3. API binary is built from source (from the module cache)
4. API server starts on an available port (8080-8099)
5. **Test uses ACTUAL RMO code:**
   - Creates HostedControlPlane CR in fake K8s cluster
   - Runs real RMO HostedControlPlaneReconciler
   - RMO attempts to create probe via synthetics-api
6. Synthetics-agent starts and fetches the probe from the API
7. Test verifies all components communicated correctly (RMO → API → Agent)
8. Resources are cleaned up automatically

**Requirements:**
- Go 1.24.1 or later
- Port 8080-8099 available (test will find an open port)
- ~5-10 seconds test execution time
- **That's it!** No manual cloning or environment variables needed

**Using Local Repositories (Optional):**

For local development with your own changes:

```go
// Option 1 (RMO): Add replace directive to go.mod
replace github.com/openshift/route-monitor-operator => /path/to/route-monitor-operator

// Option 2 (API): Use environment variable (quickest)
export RHOBS_SYNTHETICS_API_PATH=/path/to/rhobs-synthetics-api

// Option 2 (API): Or use replace directive in go.mod
replace github.com/rhobs/rhobs-synthetics-api => /path/to/rhobs-synthetics-api
```

Then run: `go mod tidy`

**Remember to remove replace directives before committing!**

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
    APIURLs:         []string{mockAPI.URL + "/probes"}, // Points to mock server
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
    logger.Infof("Mock API received request: %s %s", r.Method, r.URL.String())
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

## Full-Stack Integration Test Workflow

Here's a complete example of running the full-stack integration test from start to finish:

```bash
# 1. Clone all three repositories (if you haven't already)
cd ~/code
git clone https://github.com/your-org/route-monitor-operator.git
git clone https://github.com/your-org/rhobs-synthetics-api.git
git clone https://github.com/your-org/rhobs-synthetics-agent.git

# 2. Set up the environment variable
export RHOBS_SYNTHETICS_API_PATH="$HOME/code/rhobs-synthetics-api"

# 3. Navigate to the agent directory
cd rhobs-synthetics-agent

# 4. Run the full-stack integration test
make test-full-e2e

# Or use the helper script for better output
./test/e2e/run_full_integration.sh
```

**What Happens Behind the Scenes:**

```
┌─────────────────────────────────────────────────┐
│ 1. Test starts, builds API from source         │
│    Location: $RHOBS_SYNTHETICS_API_PATH         │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 2. API server starts on http://localhost:808X  │
│    (finds available port automatically)         │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 3. Test simulates RMO creating probe            │
│    POST /probes                                 │
│    {                                            │
│      "static_url": "https://api.cluster.../livez"│
│      "labels": {                                │
│        "cluster_id": "test-hcp-cluster-123",   │
│        "private": "false",                      │
│        "source": "route-monitor-operator"       │
│      }                                          │
│    }                                            │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 4. Agent starts and polls API                   │
│    GET /probes?label_selector=private=false     │
│    Fetches probe with cluster metadata          │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 5. Test verifies probe in API                   │
│    GET /probes/{probe_id}                       │
│    Checks status: pending/active/failed         │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 6. Test simulates RMO deleting probe            │
│    DELETE /probes/{probe_id}                    │
│    Probe marked as terminating/deleted          │
└─────────────────┬───────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────┐
│ 7. Cleanup: API server stopped, data deleted    │
│    Test completes successfully ✓                │
└─────────────────────────────────────────────────┘
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

To add new full-stack integration scenarios:

1. Add test functions to `full_integration_test.go`
2. Use the `RealAPIManager` for API server lifecycle
3. Simulate RMO behavior using the helper functions (`createProbeViaAPI`, etc.)
4. Verify all three components interact correctly