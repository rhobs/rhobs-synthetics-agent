package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rhobs/rhobs-synthetics-agent/internal/version"
)

func TestMetricsRegistration(t *testing.T) {
	// Test that metrics can be registered without error
	// Since our metrics are already registered globally in init(),
	// we'll test creating new ones to ensure the structure is valid
	
	testRegistry := prometheus.NewRegistry()
	
	testProbeListFetchDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "test_probe_list_fetch_duration_seconds",
			Help: "Test duration metric",
		},
		[]string{"api_endpoint", "status"},
	)
	
	testProbeListFetchTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "test_probe_list_fetch_total",
			Help: "Test counter metric",
		},
		[]string{"api_endpoint", "status"},
	)
	
	// This should not panic
	testRegistry.MustRegister(testProbeListFetchDuration, testProbeListFetchTotal)

	// Record some values to make the metrics appear
	testProbeListFetchTotal.WithLabelValues("test", "success").Inc()
	testProbeListFetchDuration.WithLabelValues("test", "success").Observe(0.1)

	// Gather metrics to verify registration
	metricFamilies, err := testRegistry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	if len(metricFamilies) != 2 {
		t.Errorf("Expected 2 metric families, got %d", len(metricFamilies))
	}
}

func TestRecordProbeListFetch(t *testing.T) {
	testCases := []struct {
		name        string
		apiEndpoint string
		duration    time.Duration
		success     bool
		expectedStatus string
	}{
		{
			name:        "Successful fetch",
			apiEndpoint: "endpoint_1",
			duration:    100 * time.Millisecond,
			success:     true,
			expectedStatus: "success",
		},
		{
			name:        "Failed fetch",
			apiEndpoint: "endpoint_2",
			duration:    500 * time.Millisecond,
			success:     false,
			expectedStatus: "error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Record the metric
			RecordProbeListFetch(tc.apiEndpoint, tc.duration, tc.success)

			// Verify counter was incremented
			counter := probeListFetchTotal.WithLabelValues(tc.apiEndpoint, tc.expectedStatus)
			value := testutil.ToFloat64(counter)
			if value < 1 {
				t.Errorf("Expected counter to be incremented, got %f", value)
			}

			// For histograms, we can't directly test the Observer interface with testutil.ToFloat64
			// Instead, we'll verify that the histogram metric exists by checking if the function doesn't panic
			// The actual values will be tested in the handler integration test
		})
	}
}

func TestSetProbeResourcesManaged(t *testing.T) {
	testCases := []struct {
		namespace string
		state     string
		count     float64
	}{
		{"default", "active", 5},
		{"test-namespace", "pending", 3},
		{"production", "failed", 1},
	}

	for _, tc := range testCases {
		t.Run(tc.namespace+"_"+tc.state, func(t *testing.T) {
			SetProbeResourcesManaged(tc.namespace, tc.state, tc.count)

			gauge := probeResourcesManaged.WithLabelValues(tc.namespace, tc.state)
			value := testutil.ToFloat64(gauge)
			if value != tc.count {
				t.Errorf("Expected gauge value %f, got %f", tc.count, value)
			}
		})
	}
}

func TestRecordProbeResourceOperation(t *testing.T) {
	testCases := []struct {
		name      string
		operation string
		success   bool
		expectedStatus string
	}{
		{"Successful create", "create", true, "success"},
		{"Failed create", "create", false, "error"},
		{"Successful update", "update", true, "success"},
		{"Failed delete", "delete", false, "error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RecordProbeResourceOperation(tc.operation, tc.success)

			counter := probeResourceOperations.WithLabelValues(tc.operation, tc.expectedStatus)
			value := testutil.ToFloat64(counter)
			if value < 1 {
				t.Errorf("Expected counter to be incremented, got %f", value)
			}
		})
	}
}

func TestRecordReconciliation(t *testing.T) {
	testCases := []struct {
		name           string
		duration       time.Duration
		success        bool
		expectedStatus string
	}{
		{"Successful reconciliation", 250 * time.Millisecond, true, "success"},
		{"Failed reconciliation", 1 * time.Second, false, "error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Record the metric
			RecordReconciliation(tc.duration, tc.success)

			// For histograms, we can't use testutil.ToFloat64 directly
			// The histogram values will be verified in the handler integration test

			// Check counter was incremented
			counter := reconciliationTotal.WithLabelValues(tc.expectedStatus)
			counterValue := testutil.ToFloat64(counter)
			if counterValue < 1 {
				t.Errorf("Expected counter to be incremented, got %f", counterValue)
			}

			// Check timestamp was updated (should be recent, within 5 seconds)
			timestamp := testutil.ToFloat64(lastReconciliationTime)
			now := float64(time.Now().Unix())
			if timestamp == 0 || timestamp > now+5 || timestamp < now-5 {
				t.Errorf("Expected recent timestamp within 5 seconds of %f, got %f", now, timestamp)
			}
		})
	}
}

func TestSetAgentInfo(t *testing.T) {
	testCases := []struct {
		version   string
		namespace string
	}{
		{version.Version, "default"},
		{"v2.1.0", "test-namespace"},
		{"v1.5.2", "production"},
	}

	for _, tc := range testCases {
		t.Run(tc.version+"_"+tc.namespace, func(t *testing.T) {
			SetAgentInfo(tc.version, tc.namespace)

			gauge := agentInfo.WithLabelValues(tc.version, tc.namespace)
			value := testutil.ToFloat64(gauge)
			if value != 1 {
				t.Errorf("Expected agent info value to be 1, got %f", value)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	handler := Handler()
	if handler == nil {
		t.Fatal("Handler() returned nil")
	}

	// Create a test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make a request to the metrics endpoint
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected Content-Type to contain 'text/plain', got %s", contentType)
	}

	// Read and check response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	content := string(body)

	// Check for basic Prometheus format
	if !strings.Contains(content, "# HELP") {
		t.Error("Response should contain HELP comments")
	}

	if !strings.Contains(content, "# TYPE") {
		t.Error("Response should contain TYPE comments")
	}

	// Check for our custom metrics (they should be registered)
	expectedMetrics := []string{
		"rhobs_synthetics_agent_info",
		"rhobs_synthetics_agent_reconciliation_duration_seconds",
		"rhobs_synthetics_agent_reconciliation_total",
		"rhobs_synthetics_agent_last_reconciliation_timestamp_seconds",
	}

	for _, metric := range expectedMetrics {
		if !strings.Contains(content, metric) {
			t.Errorf("Expected metric %s in response", metric)
		}
	}
}

func TestMetricLabels(t *testing.T) {
	// Test that metrics have the expected labels
	
	// Record some test data
	RecordProbeListFetch("test-endpoint", 100*time.Millisecond, true)
	SetProbeResourcesManaged("test-ns", "active", 3)
	RecordProbeResourceOperation("create", true)
	RecordReconciliation(200*time.Millisecond, true)
	SetAgentInfo(version.Version, "test-ns")

	// Create test server to get metrics output
	handler := Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	content := string(body)

	// Test label patterns
	testCases := []struct {
		name    string
		pattern string
	}{
		{
			"API endpoint labels",
			`rhobs_synthetics_agent_probe_list_fetch_total\{api_endpoint="test-endpoint",status="success"\}`,
		},
		{
			"Probe resources labels",
			`rhobs_synthetics_agent_probe_resources_managed\{namespace="test-ns",state="active"\}`,
		},
		{
			"Resource operation labels",
			`rhobs_synthetics_agent_probe_resource_operations_total\{operation="create",status="success"\}`,
		},
		{
			"Reconciliation labels",
			`rhobs_synthetics_agent_reconciliation_total\{status="success"\}`,
		},
		{
			"Agent info labels",
			`rhobs_synthetics_agent_info\{namespace="test-ns",version="` + version.Version + `"\}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matched, err := regexp.MatchString(tc.pattern, content)
			if err != nil {
				t.Fatalf("Invalid regex pattern: %v", err)
			}
			if !matched {
				t.Errorf("Pattern %s not found in metrics output", tc.pattern)
			}
		})
	}
}

func TestMetricTypes(t *testing.T) {
	// Create test server to get metrics output
	handler := Handler()
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("Failed to close response body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	content := string(body)

	// Test metric type declarations
	expectedTypes := map[string]string{
		"rhobs_synthetics_agent_probe_list_fetch_duration_seconds": "histogram",
		"rhobs_synthetics_agent_probe_list_fetch_total":            "counter",
		"rhobs_synthetics_agent_probe_resources_managed":          "gauge",
		"rhobs_synthetics_agent_probe_resource_operations_total":  "counter",
		"rhobs_synthetics_agent_reconciliation_duration_seconds":  "histogram",
		"rhobs_synthetics_agent_reconciliation_total":             "counter",
		"rhobs_synthetics_agent_last_reconciliation_timestamp_seconds": "gauge",
		"rhobs_synthetics_agent_info": "gauge",
	}

	for metric, expectedType := range expectedTypes {
		typePattern := `# TYPE ` + metric + ` ` + expectedType
		if !strings.Contains(content, typePattern) {
			t.Errorf("Expected type declaration '%s' not found", typePattern)
		}
	}
}