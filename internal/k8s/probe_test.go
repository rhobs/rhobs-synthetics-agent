package k8s

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestProbeManager_ValidateURL(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("test-namespace", "", "")

	// Test valid URL
	err := pm.ValidateURL(server.URL)
	if err != nil {
		t.Errorf("ValidateURL() failed for valid URL: %v", err)
	}

	// Test invalid URL
	err = pm.ValidateURL("invalid-url")
	if err == nil {
		t.Error("ValidateURL() should fail for invalid URL")
	}

	// Test unsupported scheme
	err = pm.ValidateURL("ftp://example.com")
	if err == nil {
		t.Error("ValidateURL() should fail for unsupported scheme")
	}
}

func TestProbeManager_CreateProbeResource(t *testing.T) {
	// Create a test server that returns 200 OK for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring", "", "")
	// Set API group for testing - test with monitoring.rhobs which should be preferred
	pm.SetProbeAPIGroup("monitoring.rhobs")

	probe := api.Probe{
		ID:        "test-probe-1",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster-id":            "cluster-123",
			"cluster_id":            "cluster-123",
			"management_cluster_id": "mgmt-cluster-456",
			"private":               "false",
			"last-reconciled":       "2026-07-15T00:00:00Z", // must be excluded from target labels
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Verify the Custom Resource structure
	if cr.APIVersion != "monitoring.rhobs/v1" {
		t.Errorf("Expected APIVersion 'monitoring.rhobs/v1', got %s", cr.APIVersion)
	}

	if cr.Kind != "Probe" {
		t.Errorf("Expected Kind 'Probe', got %s", cr.Kind)
	}

	// Name should be deterministic based on cluster-id label (not API probe ID)
	expectedName := "probe-cluster-123"
	if cr.Name != expectedName {
		t.Errorf("Expected name '%s', got %s", expectedName, cr.Name)
	}

	if cr.Namespace != "monitoring" {
		t.Errorf("Expected namespace 'monitoring', got %s", cr.Namespace)
	}

	// Verify the spec
	if cr.Spec.Interval != "30s" {
		t.Errorf("Expected interval '30s', got %s", cr.Spec.Interval)
	}

	if cr.Spec.Module != "http_2xx" {
		t.Errorf("Expected module 'http_2xx', got %s", cr.Spec.Module)
	}

	if len(cr.Spec.Targets.StaticConfig.Targets) != 1 {
		t.Errorf("Expected 1 target, got %d", len(cr.Spec.Targets.StaticConfig.Targets))
	}

	if cr.Spec.Targets.StaticConfig.Targets[0] != server.URL {
		t.Errorf("Expected target URL '%s', got %s", server.URL, cr.Spec.Targets.StaticConfig.Targets[0])
	}

	// Verify labels
	if cr.Spec.Targets.StaticConfig.Labels["cluster_id"] != "cluster-123" {
		t.Errorf("Expected cluster_id 'cluster-123', got %s", cr.Spec.Targets.StaticConfig.Labels["cluster_id"])
	}

	// probe_url must be set so probe_success metrics match alert rule selector
	// probe_success{probe_url=~".*api.*"} used by api-ErrorBudgetBurn (ROSAENG-60340)
	if cr.Spec.Targets.StaticConfig.Labels["probe_url"] != server.URL {
		t.Errorf("Expected probe_url '%s', got %s", server.URL, cr.Spec.Targets.StaticConfig.Labels["probe_url"])
	}

	// last-reconciled must NOT be in target labels — it changes every reconcile cycle,
	// creating a new Prometheus time series that breaks burn rate window continuity
	if _, exists := cr.Spec.Targets.StaticConfig.Labels["last-reconciled"]; exists {
		t.Error("last-reconciled must not be in target labels (it creates time series churn)")
	}
}

func TestProbeManager_CreateProbeResource_InvalidURL(t *testing.T) {
	pm := NewProbeManager("monitoring", "", "")

	probe := api.Probe{
		ID:        "test-probe-invalid",
		StaticURL: "invalid-url-format",
		Labels: map[string]string{
			"cluster_id": "cluster-123",
			"private":    "false",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	_, err := pm.CreateProbeResource(probe, probeConfig)
	if err == nil {
		t.Error("CreateProbeResource() should fail for invalid URL")
	}
}

func TestProbeManager_isRunningInK8sCluster(t *testing.T) {
	// Test the kubeclient function directly since ProbeManager no longer has this method
	result := kubeclient.IsRunningInK8sCluster()

	// In test environment, should return false since we don't have K8s service account
	if result {
		t.Log("Running in actual Kubernetes environment - this is expected if tests are run in a K8s pod")
	} else {
		t.Log("Not running in Kubernetes environment - this is expected for local testing")
	}
}

func TestProbeManager_APIVersionFallback(t *testing.T) {
	// Create a test server that returns 200 OK for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring", "", "")
	// Force empty probeAPIGroup to test fallback behavior
	pm.SetProbeAPIGroup("")

	probe := api.Probe{
		ID:        "test-probe-fallback",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id": "cluster-123",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Should fallback to monitoring.coreos.com when probeAPIGroup is empty
	if cr.APIVersion != "monitoring.coreos.com/v1" {
		t.Errorf("Expected fallback APIVersion 'monitoring.coreos.com/v1', got %s", cr.APIVersion)
	}
}

func TestProbeManager_initializeK8sClients(t *testing.T) {
	pm := NewProbeManager("test-namespace", "", "")

	// After initialization, check state
	if pm.isK8sCluster() {
		// If we're actually in a K8s cluster
		if pm.kubeClient == nil {
			t.Error("kubeClient should not be nil when in K8s cluster")
		}
		if pm.dynamicClient == nil {
			t.Error("dynamicClient should not be nil when in K8s cluster")
		}
	} else {
		// If we're not in a K8s cluster (normal test case)
		if pm.dynamicClient != nil {
			t.Error("dynamicClient should be nil when not in K8s cluster")
		}
	}
}

func TestProbeManager_CreateProbeK8sResource_NotInCluster(t *testing.T) {
	// Create a test server for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("test-namespace", "", "")

	probe := api.Probe{
		ID:        "test-probe-k8s",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id": "test-cluster",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	// This should fail either because we're not in a K8s cluster OR due to permissions
	err := pm.CreateProbeK8sResource(probe, probeConfig)
	if err == nil {
		t.Error("CreateProbeK8sResource() should fail when not in K8s cluster or lacking permissions")
	}

	// Accept either error scenario:
	// 1. "not running in a Kubernetes cluster" (when running outside K8s)
	// 2. Permission denied (when running in K8s CI without proper RBAC)
	expectedErrors := []string{
		"not running in a Kubernetes cluster",
		"failed to create Probe resource in Kubernetes:",
	}

	errorMatched := false
	for _, expectedError := range expectedErrors {
		if err.Error() == expectedError ||
			(expectedError == "failed to create Probe resource in Kubernetes:" &&
				strings.Contains(err.Error(), expectedError)) {
			errorMatched = true
			break
		}
	}

	if !errorMatched {
		t.Errorf("Expected error containing one of %v, got '%s'", expectedErrors, err.Error())
	}
}


func TestProbeManager_checkProbeCRDs_NoClient(t *testing.T) {
	pm := &ProbeManager{
		namespace:     "test",
		httpClient:    &http.Client{},
		kubeClient:    nil,
		probeAPIGroup: "",
	}

	pm.checkProbeCRDs()

	if pm.probeAPIGroup != "" {
		t.Error("probeAPIGroup should be empty when kubeClient is nil")
	}
}

// newTestProbeManager creates a ProbeManager with a fake dynamic client for unit testing.
func newTestProbeManager(namespace, apiGroup string, dynClient dynamic.Interface) *ProbeManager {
	return &ProbeManager{
		namespace:     namespace,
		probeAPIGroup: apiGroup,
		dynamicClient: dynClient,
		httpClient:    &http.Client{},
	}
}

// newFakeDynamicClient returns a fake dynamic client seeded with the given objects.
// The scheme is kept minimal; objects are stored as unstructured.
func newFakeDynamicClient(objs ...runtime.Object) *fake.FakeDynamicClient {
	s := runtime.NewScheme()
	// Register the Probe list type so the fake client can handle lists.
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "monitoring.rhobs", Version: "v1", Kind: "ProbeList"},
		&unstructured.UnstructuredList{},
	)
	return fake.NewSimpleDynamicClient(s, objs...)
}

// makeProbeUnstructured builds an *unstructured.Unstructured representing a Probe CR.
func makeProbeUnstructured(name, namespace, apiGroup, interval string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiGroup + "/v1",
			"kind":       "Probe",
			"metadata": map[string]interface{}{
				"name":            name,
				"namespace":       namespace,
				"resourceVersion": "1",
			},
			"spec": map[string]interface{}{
				"interval": interval,
				"module":   "http_2xx",
				"prober": map[string]interface{}{
					"url":  "synthetics-blackbox-prober-default-service:9115",
					"path": "/probe",
				},
				"targets": map[string]interface{}{
					"staticConfig": map[string]interface{}{
						"static": []interface{}{"https://example.com"},
						"labels": map[string]interface{}{},
					},
				},
			},
		},
	}
}

func TestProbeManager_updateProbeK8sResource(t *testing.T) {
	const (
		ns       = "test-ns"
		apiGroup = "monitoring.rhobs"
		name     = "probe-test"
	)
	gvr := schema.GroupVersionResource{Group: apiGroup, Version: "v1", Resource: "probes"}
	ctx := context.Background()

	t.Run("identical spec returns nil without calling Update", func(t *testing.T) {
		existing := makeProbeUnstructured(name, ns, apiGroup, "30s")
		desired := makeProbeUnstructured(name, ns, apiGroup, "30s")

		dynClient := newFakeDynamicClient(existing)
		pm := newTestProbeManager(ns, apiGroup, dynClient)

		if err := pm.updateProbeK8sResource(ctx, gvr, desired); err != nil {
			t.Fatalf("expected nil, got: %v", err)
		}

		// No Update action should have been sent to the fake client.
		for _, action := range dynClient.Actions() {
			if action.GetVerb() == "update" {
				t.Error("Update was called despite identical spec")
			}
		}
	})

	t.Run("changed spec returns nil and calls Update", func(t *testing.T) {
		existing := makeProbeUnstructured(name, ns, apiGroup, "30s")
		desired := makeProbeUnstructured(name, ns, apiGroup, "60s") // different interval

		dynClient := newFakeDynamicClient(existing)
		pm := newTestProbeManager(ns, apiGroup, dynClient)

		if err := pm.updateProbeK8sResource(ctx, gvr, desired); err != nil {
			t.Fatalf("expected nil, got: %v", err)
		}

		updateSeen := false
		for _, action := range dynClient.Actions() {
			if action.GetVerb() == "update" {
				updateSeen = true
				ua, ok := action.(clienttesting.UpdateAction)
				if !ok {
					t.Fatal("expected UpdateAction")
				}
				updated := ua.GetObject().(*unstructured.Unstructured)
				if updated.GetResourceVersion() != "1" {
					t.Errorf("resourceVersion must be preserved from existing CR, got %q", updated.GetResourceVersion())
				}
			}
		}
		if !updateSeen {
			t.Error("expected Update to be called for changed spec")
		}
	})

	t.Run("resource not found returns error", func(t *testing.T) {
		desired := makeProbeUnstructured(name, ns, apiGroup, "30s")

		dynClient := newFakeDynamicClient() // empty — no existing CR
		pm := newTestProbeManager(ns, apiGroup, dynClient)

		err := pm.updateProbeK8sResource(ctx, gvr, desired)
		if err == nil {
			t.Fatal("expected error for missing resource, got nil")
		}
		if !strings.Contains(err.Error(), "failed to get existing Probe resource") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestProbeManager_CheckConnectivity(t *testing.T) {
	t.Run("returns true when blackbox exporter reports probe_success 1", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("# HELP probe_success Displays whether or not the probe was a success\n# TYPE probe_success gauge\nprobe_success 1\n"))
		}))
		defer server.Close()

		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: strings.TrimPrefix(server.URL, "http://"),
			Module:    "http_2xx",
		}

		if !pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected true for probe_success 1")
		}
	})

	t.Run("returns false when blackbox exporter reports probe_success 0", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("# HELP probe_success Displays whether or not the probe was a success\n# TYPE probe_success gauge\nprobe_success 0\n"))
		}))
		defer server.Close()

		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: strings.TrimPrefix(server.URL, "http://"),
			Module:    "http_2xx",
		}

		if pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected false for probe_success 0")
		}
	})

	t.Run("returns false when blackbox exporter is unreachable", func(t *testing.T) {
		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: "127.0.0.1:1",
			Module:    "http_2xx",
		}

		if pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected false when prober is unreachable")
		}
	})

	t.Run("returns true when no prober URL configured", func(t *testing.T) {
		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{Module: "http_2xx"}

		if !pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected true when prober URL is empty (skip pre-flight)")
		}
	})

	t.Run("returns false when response has no probe_success line", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("# some other metrics\nprobe_duration_seconds 0.5\n"))
		}))
		defer server.Close()

		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: strings.TrimPrefix(server.URL, "http://"),
			Module:    "http_2xx",
		}

		if pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected false when probe_success metric is missing")
		}
	})

	t.Run("returns false on non-2xx response even with probe_success 1", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("probe_success 1\n"))
		}))
		defer server.Close()

		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: strings.TrimPrefix(server.URL, "http://"),
			Module:    "http_2xx",
		}

		if pm.CheckConnectivity(context.Background(), "https://api.example.com/livez", config) {
			t.Error("expected false when blackbox exporter returns non-2xx status")
		}
	})

	t.Run("passes correct target and module query params", func(t *testing.T) {
		var capturedTarget, capturedModule string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedTarget = r.URL.Query().Get("target")
			capturedModule = r.URL.Query().Get("module")
			_, _ = w.Write([]byte("probe_success 1\n"))
		}))
		defer server.Close()

		pm := newTestProbeManager("test-ns", "monitoring.rhobs", nil)
		config := BlackboxProbingConfig{
			ProberURL: strings.TrimPrefix(server.URL, "http://"),
			Module:    "http_2xx",
		}

		pm.CheckConnectivity(context.Background(), "https://api.test.com:6443/livez", config)

		if capturedTarget != "https://api.test.com:6443/livez" {
			t.Errorf("expected target 'https://api.test.com:6443/livez', got %q", capturedTarget)
		}
		if capturedModule != "http_2xx" {
			t.Errorf("expected module 'http_2xx', got %q", capturedModule)
		}
	})
}

// Compile-time assertion: monitoringv1 is imported for convertFromUnstructured coverage.
var _ = monitoringv1.Probe{}
var _ = metav1.GetOptions{}
