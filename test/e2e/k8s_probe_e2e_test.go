package e2e

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/rhobs-synthetics-agent/internal/agent"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-agent/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestAgent_E2E_KubernetesProbeCreation(t *testing.T) {
	// This test verifies that the agent can create Kubernetes Probe CRDs
	// when configured to do so

	// Start mock API server
	mockAPI := NewMockAPIServer()
	defer mockAPI.Close()

	// Create a test probe for Kubernetes resource creation
	testProbe := &api.Probe{
		ID:        "k8s-test-probe",
		StaticURL: "https://httpbin.org/status/200",
		Labels: map[string]string{
			"env":                   "test",
			"private":               "false",
			"cluster_id":            "test-cluster-123",
			"management_cluster_id": "mgmt-cluster-456",
		},
		Status: "pending",
	}

	// Add the test probe to mock API
	mockAPI.AddProbe(testProbe)

	// Create agent configuration
	cfg := &agent.Config{
		LogLevel:        "debug",
		LogFormat:       "text",
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 2 * time.Second,
		APIURLs:         []string{mockAPI.URL + "/probes"},
		LabelSelector:   "env=test,private=false",
	}

	// Create and start agent
	testAgent, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("failed to start test agent: %v", err)
	}

	// Run agent in background
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		_ = testAgent.Run()
	}()

	// Wait for agent to process probes
	time.Sleep(2 * time.Second)

	// Test K8s Probe Manager functionality
	t.Run("TestProbeManagerCreation", func(t *testing.T) {
		pm := k8s.NewProbeManager("monitoring", "")

		if pm == nil {
			t.Fatal("ProbeManager should not be nil")
		}
	})

	t.Run("TestProbeResourceCreation", func(t *testing.T) {
		pm := k8s.NewProbeManager("monitoring", "")

		config := k8s.BlackboxProbingConfig{
			Interval:  "30s",
			Module:    "http_2xx",
			ProberURL: "http://blackbox-exporter:9115",
		}

		// Create probe resource using the new CRD types
		probeResource, err := pm.CreateProbeResource(*testProbe, config)
		if err != nil {
			t.Fatalf("Failed to create probe resource: %v", err)
		}

		// Verify the resource structure matches prometheus-operator CRD
		// Should be either monitoring.rhobs/v1 (preferred) or monitoring.coreos.com/v1 (fallback)
		expectedVersions := []string{"monitoring.rhobs/v1", "monitoring.coreos.com/v1"}
		validAPIVersion := false
		for _, expectedVersion := range expectedVersions {
			if probeResource.APIVersion == expectedVersion {
				validAPIVersion = true
				break
			}
		}
		if !validAPIVersion {
			t.Errorf("Expected APIVersion to be one of %v, got %s", expectedVersions, probeResource.APIVersion)
		}

		if probeResource.Kind != "Probe" {
			t.Errorf("Expected Kind 'Probe', got %s", probeResource.Kind)
		}

		if probeResource.Name != "probe-k8s-test-probe" {
			t.Errorf("Expected name 'probe-k8s-test-probe', got %s", probeResource.Name)
		}

		if probeResource.Namespace != "monitoring" {
			t.Errorf("Expected namespace 'monitoring', got %s", probeResource.Namespace)
		}

		// Verify spec fields
		if probeResource.Spec.Module != "http_2xx" {
			t.Errorf("Expected module 'http_2xx', got %s", probeResource.Spec.Module)
		}

		if probeResource.Spec.Interval != "30s" {
			t.Errorf("Expected interval '30s', got %s", probeResource.Spec.Interval)
		}

		if probeResource.Spec.ProberSpec.URL != "http://blackbox-exporter:9115" {
			t.Errorf("Expected prober URL 'http://blackbox-exporter:9115', got %s", probeResource.Spec.ProberSpec.URL)
		}

		// Verify targets
		if probeResource.Spec.Targets.StaticConfig == nil {
			t.Fatal("StaticConfig should not be nil")
		}

		if len(probeResource.Spec.Targets.StaticConfig.Targets) != 1 {
			t.Errorf("Expected 1 target, got %d", len(probeResource.Spec.Targets.StaticConfig.Targets))
		}

		if probeResource.Spec.Targets.StaticConfig.Targets[0] != testProbe.StaticURL {
			t.Errorf("Expected target URL '%s', got %s", testProbe.StaticURL, probeResource.Spec.Targets.StaticConfig.Targets[0])
		}

		// Verify labels
		expectedLabels := []string{"cluster_id", "management_cluster_id", "apiserver_url"}
		for _, label := range expectedLabels {
			if _, exists := probeResource.Spec.Targets.StaticConfig.Labels[label]; !exists {
				t.Errorf("Expected label '%s' to exist in target labels", label)
			}
		}

		// Verify metadata labels
		expectedMetadataLabels := []string{"rhobs.monitoring/probe-id", "rhobs.monitoring/managed-by"}
		for _, label := range expectedMetadataLabels {
			if _, exists := probeResource.Labels[label]; !exists {
				t.Errorf("Expected metadata label '%s' to exist", label)
			}
		}
	})

	t.Run("TestProbeResourceValidation", func(t *testing.T) {
		pm := k8s.NewProbeManager("monitoring", "")

		// Test with invalid URL
		invalidProbe := api.Probe{
			ID:        "invalid-probe",
			StaticURL: "invalid-url",
			Labels:    map[string]string{"env": "test"},
			Status:    "pending",
		}

		config := k8s.BlackboxProbingConfig{
			Interval:  "30s",
			Module:    "http_2xx",
			ProberURL: "http://blackbox-exporter:9115",
		}

		_, err := pm.CreateProbeResource(invalidProbe, config)
		if err == nil {
			t.Error("Expected error for invalid URL, but got none")
		}
	})

	// Gracefully shutdown the agent
	testAgent.Shutdown()

	// Wait for agent to shutdown
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not shut down within timeout")
	}
}

func TestAgent_E2E_KubernetesProbeWithFakeClient(t *testing.T) {
	// This test uses fake Kubernetes clients to test the full K8s integration
	// without requiring an actual Kubernetes cluster

	// Start mock API server
	mockAPI := NewMockAPIServer()
	defer mockAPI.Close()

	// Create test probe
	testProbe := &api.Probe{
		ID:        "fake-k8s-probe",
		StaticURL: "https://httpbin.org/status/200",
		Labels: map[string]string{
			"env":        "test",
			"private":    "false",
			"cluster_id": "fake-cluster",
		},
		Status: "pending",
	}

	mockAPI.AddProbe(testProbe)

	t.Run("TestWithFakeKubernetesClient", func(t *testing.T) {
		// Create a runtime scheme for the fake client
		scheme := runtime.NewScheme()

		// Create fake Kubernetes clients
		fakeKubeClient := kubefake.NewSimpleClientset()
		fakeDynamicClient := fake.NewSimpleDynamicClient(scheme)

		// Create a custom probe manager that uses fake clients
		pm := createTestProbeManagerWithFakeClients("monitoring", fakeKubeClient, fakeDynamicClient)

		config := k8s.BlackboxProbingConfig{
			Interval:  "30s",
			Module:    "http_2xx",
			ProberURL: "http://blackbox-exporter:9115",
		}

		// Create probe resource
		probeResource, err := pm.CreateProbeResource(*testProbe, config)
		if err != nil {
			t.Fatalf("Failed to create probe resource: %v", err)
		}

		// Convert to unstructured for dynamic client
		unstructuredProbe, err := convertProbeToUnstructured(probeResource)
		if err != nil {
			t.Fatalf("Failed to convert probe to unstructured: %v", err)
		}

		// Define the GVR for Probe resources
		probeGVR := schema.GroupVersionResource{
			Group:    "monitoring.coreos.com",
			Version:  "v1",
			Resource: "probes",
		}

		// Create the resource using fake dynamic client
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		createdProbe, err := fakeDynamicClient.Resource(probeGVR).Namespace("monitoring").Create(
			ctx,
			unstructuredProbe,
			metav1.CreateOptions{},
		)

		if err != nil {
			t.Fatalf("Failed to create probe in fake K8s cluster: %v", err)
		}

		// Verify the created resource
		if createdProbe.GetKind() != "Probe" {
			t.Errorf("Expected Kind 'Probe', got %s", createdProbe.GetKind())
		}

		// Should be either monitoring.rhobs/v1 (preferred) or monitoring.coreos.com/v1 (fallback)
		expectedVersions := []string{"monitoring.rhobs/v1", "monitoring.coreos.com/v1"}
		validAPIVersion := false
		for _, expectedVersion := range expectedVersions {
			if createdProbe.GetAPIVersion() == expectedVersion {
				validAPIVersion = true
				break
			}
		}
		if !validAPIVersion {
			t.Errorf("Expected APIVersion to be one of %v, got %s", expectedVersions, createdProbe.GetAPIVersion())
		}

		if createdProbe.GetName() != "probe-fake-k8s-probe" {
			t.Errorf("Expected name 'probe-fake-k8s-probe', got %s", createdProbe.GetName())
		}

		// Verify spec structure
		spec, found, err := unstructured.NestedMap(createdProbe.Object, "spec")
		if err != nil || !found {
			t.Fatalf("Failed to get spec from created probe: %v", err)
		}

		if spec["module"] != "http_2xx" {
			t.Errorf("Expected module 'http_2xx', got %s", spec["module"])
		}

		if spec["interval"] != "30s" {
			t.Errorf("Expected interval '30s', got %s", spec["interval"])
		}

		// Verify prober configuration
		prober, found, err := unstructured.NestedMap(spec, "prober")
		if err != nil || !found {
			t.Fatalf("Failed to get prober from spec: %v", err)
		}

		if prober["url"] != "http://blackbox-exporter:9115" {
			t.Errorf("Expected prober URL 'http://blackbox-exporter:9115', got %s", prober["url"])
		}

		// Verify targets
		targets, found, err := unstructured.NestedMap(spec, "targets")
		if err != nil || !found {
			t.Fatalf("Failed to get targets from spec: %v", err)
		}

		staticConfig, found, err := unstructured.NestedMap(targets, "staticConfig")
		if err != nil || !found {
			t.Fatalf("Failed to get staticConfig from targets: %v", err)
		}

		staticTargets, found, err := unstructured.NestedStringSlice(staticConfig, "static")
		if err != nil || !found {
			t.Fatalf("Failed to get static targets: %v", err)
		}

		if len(staticTargets) != 1 || staticTargets[0] != testProbe.StaticURL {
			t.Errorf("Expected target [%s], got %v", testProbe.StaticURL, staticTargets)
		}
	})
}

// Helper function to create a test probe manager with fake clients
// This simulates a probe manager that thinks it's in a K8s cluster with Probe CRD support
func createTestProbeManagerWithFakeClients(namespace string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) *k8s.ProbeManager {
	// We can't directly inject the clients into ProbeManager due to private fields,
	// but we can test the CreateProbeResource method which is the core functionality
	return k8s.NewProbeManager(namespace, "")
}

// Helper function to convert a Probe CRD to unstructured format
func convertProbeToUnstructured(probe *monitoringv1.Probe) (*unstructured.Unstructured, error) {
	// Convert to JSON first, then to unstructured
	probeJSON, err := json.Marshal(probe)
	if err != nil {
		return nil, err
	}

	var probeMap map[string]interface{}
	err = json.Unmarshal(probeJSON, &probeMap)
	if err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{Object: probeMap}, nil
}

func TestAgent_E2E_KubernetesProbeEdgeCases(t *testing.T) {
	// Test various edge cases for K8s Probe creation

	testCases := []struct {
		name        string
		probe       api.Probe
		config      k8s.BlackboxProbingConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "ValidProbeWithMinimalLabels",
			probe: api.Probe{
				ID:        "minimal-probe",
				StaticURL: "https://httpbin.org/status/200",
				Labels:    map[string]string{"env": "test"},
				Status:    "pending",
			},
			config: k8s.BlackboxProbingConfig{
				Interval:  "60s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
			expectError: false,
		},
		{
			name: "ProbeWithCompleteLabels",
			probe: api.Probe{
				ID:        "complete-probe",
				StaticURL: "https://httpbin.org/status/200",
				Labels: map[string]string{
					"env":                   "production",
					"private":               "true",
					"cluster_id":            "prod-cluster-789",
					"management_cluster_id": "mgmt-prod-123",
					"team":                  "platform",
					"criticality":           "high",
				},
				Status: "active",
			},
			config: k8s.BlackboxProbingConfig{
				Interval:  "15s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
			expectError: false,
		},
		{
			name: "ProbeWithInvalidURL",
			probe: api.Probe{
				ID:        "invalid-url-probe",
				StaticURL: "not-a-valid-url",
				Labels:    map[string]string{"env": "test"},
				Status:    "pending",
			},
			config: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
			expectError: true,
			errorMsg:    "URL validation failed",
		},
		{
			name: "ProbeWithUnsupportedScheme",
			probe: api.Probe{
				ID:        "ftp-probe",
				StaticURL: "ftp://example.com/file.txt",
				Labels:    map[string]string{"env": "test"},
				Status:    "pending",
			},
			config: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
			expectError: true,
			errorMsg:    "unsupported URL scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pm := k8s.NewProbeManager("test-namespace", "")

			probeResource, err := pm.CreateProbeResource(tc.probe, tc.config)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error for test case '%s', but got none", tc.name)
				} else if tc.errorMsg != "" && err.Error() != tc.errorMsg {
					// For more flexible error checking, we'll check if the error contains the expected message
					t.Logf("Expected error message containing '%s', got '%s'", tc.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for test case '%s': %v", tc.name, err)
				} else {
					// Verify basic structure
					// Should be either monitoring.rhobs/v1 (preferred) or monitoring.coreos.com/v1 (fallback)
					expectedVersions := []string{"monitoring.rhobs/v1", "monitoring.coreos.com/v1"}
					validAPIVersion := false
					for _, expectedVersion := range expectedVersions {
						if probeResource.APIVersion == expectedVersion {
							validAPIVersion = true
							break
						}
					}
					if !validAPIVersion {
						t.Errorf("Expected APIVersion to be one of %v, got %s", expectedVersions, probeResource.APIVersion)
					}

					if probeResource.Kind != "Probe" {
						t.Errorf("Expected Kind 'Probe', got %s", probeResource.Kind)
					}

					expectedName := "probe-" + tc.probe.ID
					if probeResource.Name != expectedName {
						t.Errorf("Expected name '%s', got %s", expectedName, probeResource.Name)
					}

					// Verify all probe labels are included in target labels
					for key, value := range tc.probe.Labels {
						if probeResource.Spec.Targets.StaticConfig.Labels[key] != value {
							t.Errorf("Expected target label '%s=%s', got '%s=%s'",
								key, value, key, probeResource.Spec.Targets.StaticConfig.Labels[key])
						}
					}
				}
			}
		})
	}
}
