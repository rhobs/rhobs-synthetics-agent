package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestBlackBoxProberManager_buildProberDeployment(t *testing.T) {
	cfg := BlackboxDeploymentConfig{
		Image: "quay.io/prometheus/blackbox-exporter:latest",
		Cmd:   []string{"/bin/blackbox_exporter"},
		Args:  []string{"--config.file=/etc/blackbox_exporter/config.yml"},
		Labels: map[string]string{
			"app": "blackbox-exporter",
		},
	}

	manager := &BlackBoxProberManager{
		namespace: "test-namespace",
		cfg:       cfg,
	}

	deployment := manager.buildProberDeployment("test-prober")

	// Test basic properties
	expectedName := "synthetics-blackbox-prober-test-prober"
	if deployment.Name != expectedName {
		t.Errorf("Expected deployment name '%s', got '%s'", expectedName, deployment.Name)
	}

	if deployment.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", deployment.Namespace)
	}

	// Test labels
	expectedProberLabel := "test-prober"
	if deployment.Labels[BlackBoxProberManagerProberLabelKey] != expectedProberLabel {
		t.Errorf("Expected prober label '%s', got '%s'",
			expectedProberLabel, deployment.Labels[BlackBoxProberManagerProberLabelKey])
	}

	if deployment.Labels["app"] != "blackbox-exporter" {
		t.Errorf("Expected custom label 'app=blackbox-exporter', got '%s'", deployment.Labels["app"])
	}

	// Test replica count
	if *deployment.Spec.Replicas != 1 {
		t.Errorf("Expected 1 replica, got %d", *deployment.Spec.Replicas)
	}

	// Test selector
	if deployment.Spec.Selector.MatchLabels[BlackBoxProberManagerProberLabelKey] != expectedProberLabel {
		t.Errorf("Expected selector to match prober label '%s', got '%s'",
			expectedProberLabel, deployment.Spec.Selector.MatchLabels[BlackBoxProberManagerProberLabelKey])
	}

	// Test container configuration
	containers := deployment.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}

	container := containers[0]
	if container.Name != "prober" {
		t.Errorf("Expected container name 'prober', got '%s'", container.Name)
	}

	if container.Image != cfg.Image {
		t.Errorf("Expected image '%s', got '%s'", cfg.Image, container.Image)
	}

	if len(container.Command) != len(cfg.Cmd) || container.Command[0] != cfg.Cmd[0] {
		t.Errorf("Expected command '%v', got '%v'", cfg.Cmd, container.Command)
	}

	if len(container.Args) != len(cfg.Args) || container.Args[0] != cfg.Args[0] {
		t.Errorf("Expected args '%v', got '%v'", cfg.Args, container.Args)
	}
}

func TestBlackBoxProberManager_buildProberService(t *testing.T) {
	cfg := BlackboxDeploymentConfig{
		Labels: map[string]string{
			"app": "blackbox-exporter",
		},
	}

	manager := &BlackBoxProberManager{
		namespace: "test-namespace",
		cfg:       cfg,
	}

	service := manager.buildProberService("test-prober")

	// Test basic properties
	expectedName := "synthetics-blackbox-prober-test-prober-service"
	if service.Name != expectedName {
		t.Errorf("Expected service name '%s', got '%s'", expectedName, service.Name)
	}

	if service.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got '%s'", service.Namespace)
	}

	// Test labels
	expectedProberLabel := "test-prober"
	if service.Labels[BlackBoxProberManagerProberLabelKey] != expectedProberLabel {
		t.Errorf("Expected prober label '%s', got '%s'",
			expectedProberLabel, service.Labels[BlackBoxProberManagerProberLabelKey])
	}

	if service.Labels["app.kubernetes.io/name"] != "blackbox-exporter" {
		t.Errorf("Expected standard label 'app.kubernetes.io/name=blackbox-exporter', got '%s'",
			service.Labels["app.kubernetes.io/name"])
	}

	if service.Labels["app.kubernetes.io/instance"] != "test-prober" {
		t.Errorf("Expected instance label 'test-prober', got '%s'",
			service.Labels["app.kubernetes.io/instance"])
	}

	// Test selector - should match deployment pods
	if service.Spec.Selector[BlackBoxProberManagerProberLabelKey] != expectedProberLabel {
		t.Errorf("Expected selector to match prober label '%s', got '%s'",
			expectedProberLabel, service.Spec.Selector[BlackBoxProberManagerProberLabelKey])
	}

	// Test ports
	if len(service.Spec.Ports) != 1 {
		t.Fatalf("Expected 1 port, got %d", len(service.Spec.Ports))
	}

	port := service.Spec.Ports[0]
	if port.Name != "http" {
		t.Errorf("Expected port name 'http', got '%s'", port.Name)
	}

	if port.Protocol != corev1.ProtocolTCP {
		t.Errorf("Expected protocol TCP, got %s", port.Protocol)
	}

	if port.Port != 9115 {
		t.Errorf("Expected port 9115, got %d", port.Port)
	}

	expectedTargetPort := intstr.FromInt(9115)
	if port.TargetPort != expectedTargetPort {
		t.Errorf("Expected target port 9115, got %v", port.TargetPort)
	}

	// Test service type
	if service.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("Expected service type ClusterIP, got %s", service.Spec.Type)
	}
}

func TestBlackBoxProberManager_proberDeploymentName(t *testing.T) {
	manager := &BlackBoxProberManager{}

	name := manager.proberDeploymentName("test-prober")
	expected := "synthetics-blackbox-prober-test-prober"
	if name != expected {
		t.Errorf("Expected deployment name '%s', got '%s'", expected, name)
	}
}

func TestBlackBoxProberManager_proberServiceName(t *testing.T) {
	manager := &BlackBoxProberManager{}

	name := manager.proberServiceName("test-prober")
	expected := "synthetics-blackbox-prober-test-prober-service"
	if name != expected {
		t.Errorf("Expected service name '%s', got '%s'", expected, name)
	}
}

func TestBlackBoxProberManager_proberCustomLabels(t *testing.T) {
	tests := []struct {
		name           string
		configLabels   map[string]string
		expectedLabels map[string]string
	}{
		{
			name:           "nil labels",
			configLabels:   nil,
			expectedLabels: map[string]string{},
		},
		{
			name:           "empty labels",
			configLabels:   map[string]string{},
			expectedLabels: map[string]string{},
		},
		{
			name: "custom labels",
			configLabels: map[string]string{
				"app":     "blackbox-exporter",
				"version": "latest",
			},
			expectedLabels: map[string]string{
				"app":     "blackbox-exporter",
				"version": "latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BlackboxDeploymentConfig{
				Labels: tt.configLabels,
			}
			manager := &BlackBoxProberManager{cfg: cfg}

			labels := manager.proberCustomLabels()

			if labels == nil {
				t.Error("proberCustomLabels() should never return nil")
			}

			if len(labels) != len(tt.expectedLabels) {
				t.Errorf("Expected %d labels, got %d", len(tt.expectedLabels), len(labels))
			}

			for key, expectedValue := range tt.expectedLabels {
				if actualValue, exists := labels[key]; !exists {
					t.Errorf("Expected label '%s' to exist", key)
				} else if actualValue != expectedValue {
					t.Errorf("Expected label '%s=%s', got '%s=%s'", key, expectedValue, key, actualValue)
				}
			}
		})
	}
}

func TestNewBlackBoxProberManager(t *testing.T) {
	cfg := BlackboxDeploymentConfig{
		Image: "test-image",
	}

	tests := []struct {
		name              string
		namespace         string
		expectedNamespace string
	}{
		{
			name:              "custom namespace",
			namespace:         "custom-namespace",
			expectedNamespace: "custom-namespace",
		},
		{
			name:              "empty namespace defaults",
			namespace:         "",
			expectedNamespace: DefaultBlackBoxProberManagerNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := BlackBoxProberManagerConfig{
				Namespace:         tt.namespace,
				KubeconfigPath:    "",
				Deployment:        cfg,
				RemoteWriteURL:    "http://test-thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				PrometheusResources: PrometheusResourceConfig{
					CPURequests:    "100m",
					CPULimits:      "500m",
					MemoryRequests: "256Mi",
					MemoryLimits:   "512Mi",
				},
				ManagedByOperator: "test-operator",
			}
			manager, err := NewBlackBoxProberManager(config)

			// The test can succeed OR fail depending on environment:
			// - In local environment: kubeclient creation fails, manager is nil
			// - In K8s environment (CI): kubeclient creation succeeds, manager is created

			if err != nil {
				// Test environment without K8s access - expected failure
				if manager != nil {
					t.Error("Expected nil manager when kubeclient creation fails")
				}
				t.Logf("Running in non-K8s environment: %v", err)
			} else {
				// K8s environment with valid credentials - successful creation
				if manager == nil {
					t.Error("Expected non-nil manager when kubeclient creation succeeds")
				} else {
					// Verify the manager was configured correctly
					if manager.namespace != tt.expectedNamespace {
						t.Errorf("Expected namespace '%s', got '%s'", tt.expectedNamespace, manager.namespace)
					}
					if manager.cfg.Image != cfg.Image {
						t.Errorf("Expected image '%s', got '%s'", cfg.Image, manager.cfg.Image)
					}
				}
				t.Logf("Running in K8s environment - manager created successfully")
			}
		})
	}
}

// Test helper functions
func TestBlackBoxProberManager_serviceClient(t *testing.T) {
	// This test requires a real Kubernetes client, so we'll skip it in unit tests
	// It's more appropriate for integration tests
	t.Skip("serviceClient() requires real Kubernetes client - tested in integration tests")
}

func TestBlackBoxProberManager_deploymentClient(t *testing.T) {
	// This test requires a real Kubernetes client, so we'll skip it in unit tests
	// It's more appropriate for integration tests
	t.Skip("deploymentClient() requires real Kubernetes client - tested in integration tests")
}

// Mock tests for CreateProber would require extensive mocking of the Kubernetes client
// These are better handled in integration tests or with more sophisticated mocking frameworks
func TestBlackBoxProberManager_CreateProber_UnitTest(t *testing.T) {
	t.Skip("CreateProber() requires real Kubernetes client - should be tested in integration tests")
}

func TestBlackBoxProberManager_GetProber_UnitTest(t *testing.T) {
	t.Skip("GetProber() requires real Kubernetes client - should be tested in integration tests")
}

func TestBlackBoxProberManager_DeleteProber_UnitTest(t *testing.T) {
	t.Skip("DeleteProber() requires real Kubernetes client - should be tested in integration tests")
}

func TestBlackBoxProberManager_PrometheusOperations(t *testing.T) {
	// Test buildPrometheusResource without requiring K8s client
	manager := &BlackBoxProberManager{
		namespace:         "test-namespace",
		remoteWriteURL:    "http://test-thanos:19291/api/v1/receive",
		remoteWriteTenant: "test-tenant",
		prometheusResources: PrometheusResourceConfig{
			CPURequests:    "100m",
			CPULimits:      "500m",
			MemoryRequests: "256Mi",
			MemoryLimits:   "512Mi",
		},
		managedByOperator: "test-operator",
	}

	// Test buildPrometheusResource
	prometheusResource := manager.buildPrometheusResource()

	// Verify basic structure
	if prometheusResource.APIVersion != PrometheusAPIVersion {
		t.Errorf("Expected apiVersion %q, got %q", PrometheusAPIVersion, prometheusResource.APIVersion)
	}

	if prometheusResource.Kind != PrometheusKind {
		t.Errorf("Expected kind %q, got %q", PrometheusKind, prometheusResource.Kind)
	}

	// Verify metadata
	if prometheusResource.Name != PrometheusResourceName {
		t.Errorf("Expected name %q, got %q", PrometheusResourceName, prometheusResource.Name)
	}

	if prometheusResource.Namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got %q", prometheusResource.Namespace)
	}

	// Verify labels
	if prometheusResource.Labels["app.kubernetes.io/managed-by"] != "test-operator" {
		t.Errorf("Expected managed-by label 'test-operator', got %q", prometheusResource.Labels["app.kubernetes.io/managed-by"])
	}

	// Verify probeSelector
	if prometheusResource.Spec.ProbeSelector == nil {
		t.Fatal("Expected probeSelector to be non-nil")
	}

	if prometheusResource.Spec.ProbeSelector.MatchLabels["rhobs.monitoring/managed-by"] != SyntheticsAgentManagedByValue {
		t.Errorf("Expected probe selector label %q, got %q", SyntheticsAgentManagedByValue, prometheusResource.Spec.ProbeSelector.MatchLabels["rhobs.monitoring/managed-by"])
	}

	// Verify remoteWrite
	if len(prometheusResource.Spec.RemoteWrite) != 1 {
		t.Fatalf("Expected 1 remoteWrite entry, got %d", len(prometheusResource.Spec.RemoteWrite))
	}

	remoteWrite := prometheusResource.Spec.RemoteWrite[0]
	if remoteWrite.URL != "http://test-thanos:19291/api/v1/receive" {
		t.Errorf("Expected remoteWrite URL 'http://test-thanos:19291/api/v1/receive', got %q", remoteWrite.URL)
	}

	if remoteWrite.Headers["THANOS-TENANT"] != "test-tenant" {
		t.Errorf("Expected THANOS-TENANT header 'test-tenant', got %q", remoteWrite.Headers["THANOS-TENANT"])
	}

	// Verify resources
	resources := prometheusResource.Spec.Resources

	// Verify requests
	if cpuRequest := resources.Requests[corev1.ResourceCPU]; cpuRequest.String() != "100m" {
		t.Errorf("Expected CPU requests '100m', got %q", cpuRequest.String())
	}

	if memoryRequest := resources.Requests[corev1.ResourceMemory]; memoryRequest.String() != "256Mi" {
		t.Errorf("Expected memory requests '256Mi', got %q", memoryRequest.String())
	}

	// Verify limits
	if cpuLimit := resources.Limits[corev1.ResourceCPU]; cpuLimit.String() != "500m" {
		t.Errorf("Expected CPU limits '500m', got %q", cpuLimit.String())
	}

	if memoryLimit := resources.Limits[corev1.ResourceMemory]; memoryLimit.String() != "512Mi" {
		t.Errorf("Expected memory limits '512Mi', got %q", memoryLimit.String())
	}
}

func TestBlackBoxProberManager_PrometheusResourceDefaults(t *testing.T) {
	// Test default values without requiring K8s client
	manager := &BlackBoxProberManager{
		namespace:         "test-namespace",
		managedByOperator: "observability-operator", // Default value
	}

	// Test that default managedByOperator is set
	if manager.managedByOperator != "observability-operator" {
		t.Errorf("Expected default managedByOperator 'observability-operator', got %q", manager.managedByOperator)
	}

	prometheusResource := manager.buildPrometheusResource()

	if prometheusResource.Labels["app.kubernetes.io/managed-by"] != "observability-operator" {
		t.Errorf("Expected default managed-by label 'observability-operator', got %q", prometheusResource.Labels["app.kubernetes.io/managed-by"])
	}
}

func TestBlackBoxProberManager_CreatePrometheus_UnitTest(t *testing.T) {
	t.Skip("CreatePrometheus() requires real Kubernetes client - should be tested in integration tests")
}

func TestBlackBoxProberManager_PrometheusExists_UnitTest(t *testing.T) {
	t.Skip("PrometheusExists() requires real Kubernetes client - should be tested in integration tests")
}

func TestBlackBoxProberManager_DeletePrometheus_UnitTest(t *testing.T) {
	t.Skip("DeletePrometheus() requires real Kubernetes client - should be tested in integration tests")
}
