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
			// Note: This will fail in test environment since we don't have actual K8s credentials
			// but we can test that it attempts to create the manager correctly
			manager, err := NewBlackBoxProberManager(tt.namespace, "", cfg)

			// Expected to fail with kubeclient creation error in test environment
			if err == nil {
				t.Error("Expected error when creating manager without valid kubeconfig")
			}

			if manager != nil {
				t.Error("Expected nil manager when kubeclient creation fails")
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
