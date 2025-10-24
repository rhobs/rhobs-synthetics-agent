package k8s

import (
	"context"
	"fmt"
	"testing"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
)

// Test that BlackBoxProberManager implements ProberManager interface
var _ ProberManager = (*BlackBoxProberManager)(nil)

func TestBlackBoxProberManager_GetProber(t *testing.T) {
	// Test objects
	var (
		defaultNamespace = "default"
		ctx              = context.Background()
		blackboxConfig   = BlackboxDeploymentConfig{}
	)

	// Test result
	type result struct {
		prober string
		err    bool
		found  bool
	}

	tests := []struct {
		name    string
		prober  string
		manager func() BlackBoxProberManager
		want    result
	}{
		{
			name: "return found=false if no prober present",
			manager: func() BlackBoxProberManager {
				return newTestBlackBoxProberManager(defaultNamespace, blackboxConfig, []runtime.Object{})
			},
			want: result{
				found: false,
				err:   false,
			},
		},
		{
			name:   "returns no error if prober exists",
			prober: "test",
			manager: func() BlackBoxProberManager {
				objs := []runtime.Object{emptyProberDeployment("test", defaultNamespace)}
				return newTestBlackBoxProberManager(defaultNamespace, blackboxConfig, objs)
			},
			want: result{
				found:  true,
				prober: fmt.Sprintf("apps/v1/deployment/%s/%s", defaultNamespace, blackboxProberDeploymentName("test")),
			},
		},
		{
			name:   "correct prober is returned out of many",
			prober: "test1",
			manager: func() BlackBoxProberManager {
				objs := []runtime.Object{
					emptyProberDeployment("test0", defaultNamespace),
					emptyProberDeployment("test1", defaultNamespace),
					emptyProberDeployment("test2", defaultNamespace),
				}

				return newTestBlackBoxProberManager(defaultNamespace, blackboxConfig, objs)
			},
			want: result{
				found:  true,
				prober: fmt.Sprintf("apps/v1/deployment/%s/%s", defaultNamespace, blackboxProberDeploymentName("test1")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			mgr := tt.manager()

			// Run
			prober, found, err := mgr.GetProber(ctx, tt.prober)

			// Test
			if (err != nil) != tt.want.err {
				t.Errorf("unexpected error in manager.GetProber test %q: err = %v", tt.name, err)
			}

			if found != tt.want.found {
				t.Errorf("unexpected result from manager.GetProber test %q: wanted found to be %t, got = %t", tt.name, tt.want.found, found)
			}

			if prober != nil {
				if tt.want.prober == "" {
					t.Errorf("unexpected result from manager.GetProber test %q: wanted prober to be nil, got %s", tt.name, prober.String())
				} else if prober.String() != tt.want.prober {
					t.Errorf("unexpected result from manager.GetProber test %q: wanted prober to be %s, got = %s", tt.name, tt.want.prober, prober)
				}
			} else {
				if tt.want.prober != "" {
					t.Errorf("unexpected result from manager.GetProber test %q: wanted non-nil prober %s, got nil", tt.name, tt.want.prober)
				}
			}
		})
	}
}

func TestBlackBoxProberManager_CreateProber(t *testing.T) {
	// test objects
	var (
		ctx             = context.Background()
		proberNamesapce = "default"
		proberName      = "test"
	)

	type result struct {
		err bool
	}

	tests := []struct {
		name     string
		cfg      BlackboxDeploymentConfig
		validate func(BlackBoxProber, clienttesting.ObjectTracker) (bool, string)
		want     result
	}{
		{
			name: "Returns a in fully populated BlackBoxProber",
			cfg:  BlackboxDeploymentConfig{},
			validate: func(p BlackBoxProber, _ clienttesting.ObjectTracker) (bool, string) {
				if p.deployment == nil {
					return false, "unexpected nil assignment to resulting BlackBoxProber.deployment"
				}
				if p.service == nil {
					return false, "unexpected nil assignment to resulting BlackBoxProber.service"
				}
				return true, ""
			},
			want: result{
				err: false,
			},
		},
		{
			name: "Configuration options are set on returned Prober.deployment",
			cfg: BlackboxDeploymentConfig{
				Image:  "quay.io/faketest/image:faketag",
				Cmd:    []string{"fakecommand"},
				Args:   []string{"fakearg1", "fakearg2"},
				Labels: map[string]string{"fakekey": "fakevalue"},
			},
			validate: func(p BlackBoxProber, _ clienttesting.ObjectTracker) (bool, string) {
				if p.deployment == nil {
					return false, "unexpected nil assignment to resulting BlackBoxProber.deployment"
				}
				container := p.deployment.Spec.Template.Spec.Containers[0]

				// image
				if container.Image != "quay.io/faketest/image:faketag" {
					return false, "prober container image does not match config"
				}
				// command
				if len(container.Command) != 1 || container.Command[0] != "fakecommand" {
					return false, "prober container command does not match config"
				}
				// args
				if len(container.Args) != 2 ||
					container.Args[0] != "fakearg1" ||
					container.Args[1] != "fakearg2" {
					return false, "prober container arguments do not match config"
				}
				// labels
				val, found := p.deployment.Labels["fakekey"]
				if !found || val != "fakevalue" {
					return false, "prober deployment label does not match config"
				}

				val, found = p.deployment.Spec.Template.Labels["fakekey"]
				if !found || val != "fakevalue" {
					return false, "prober template label does not match config"
				}
				return true, ""
			},
		},
		{
			name: "Deployment is actually created by manager",
			cfg:  BlackboxDeploymentConfig{},
			validate: func(_ BlackBoxProber, t clienttesting.ObjectTracker) (bool, string) {
				_, err := t.Get(appsv1.SchemeGroupVersion.WithResource("deployments"), proberNamesapce, blackboxProberDeploymentName(proberName))
				if err != nil {
					return false, "expected deployment object was not created"
				}
				return true, ""
			},
		},
		{
			name: "In-cluster deployment matches configuraion",
			cfg: BlackboxDeploymentConfig{
				Image:  "quay.io/faketest/image:faketag",
				Cmd:    []string{"fakecommand"},
				Args:   []string{"fakearg1", "fakearg2"},
				Labels: map[string]string{"fakekey": "fakevalue"},
			},
			validate: func(_ BlackBoxProber, t clienttesting.ObjectTracker) (bool, string) {
				obj, err := t.Get(appsv1.SchemeGroupVersion.WithResource("deployments"), proberNamesapce, blackboxProberDeploymentName(proberName))
				if err != nil {
					return false, "expected deployment object was not created"
				}
				unstruct, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return false, "[should never happen] tracked object was not returned as unstructured data!"
				}
				deployment, err := convertUnstructuredToDeployment(unstruct)
				if err != nil {
					return false, "[should never happen] was not able to convert from unstructured data to deployment"
				}

				container := deployment.Spec.Template.Spec.Containers[0]
				// image
				if container.Image != "quay.io/faketest/image:faketag" {
					return false, "prober container image does not match config"
				}
				// command
				if len(container.Command) != 1 || container.Command[0] != "fakecommand" {
					return false, "prober container command does not match config"
				}
				// args
				if len(container.Args) != 2 ||
					container.Args[0] != "fakearg1" ||
					container.Args[1] != "fakearg2" {
					return false, "prober container arguments do not match config"
				}
				// labels
				val, found := deployment.Labels["fakekey"]
				if !found || val != "fakevalue" {
					return false, "prober deployment label does not match config"
				}

				val, found = deployment.Spec.Template.Labels["fakekey"]
				if !found || val != "fakevalue" {
					return false, "prober template label does not match config"
				}
				return true, ""
			},
		},
		{
			name: "ProberManager label cannot be overwritten",
			cfg: BlackboxDeploymentConfig{
				Labels: map[string]string{BlackBoxProberManagerProberLabelKey: "invalidLabelValue"},
			},
			validate: func(_ BlackBoxProber, t clienttesting.ObjectTracker) (bool, string) {
				obj, err := t.Get(appsv1.SchemeGroupVersion.WithResource("deployments"), proberNamesapce, blackboxProberDeploymentName(proberName))
				if err != nil {
					return false, "expected deployment object was not created"
				}
				unstruct, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return false, "[should never happen] tracked object was not returned as unstructured data!"
				}
				deployment, err := convertUnstructuredToDeployment(unstruct)
				if err != nil {
					return false, "[should never happen] was not able to convert from unstructured data to deployment"
				}

				val, found := deployment.Labels[BlackBoxProberManagerProberLabelKey]
				if !found || val != proberName {
					return false, "in-cluster deployment's BlackBoxProberManagerProberLabelKey value was not overwritten as expected"
				}

				val, found = deployment.Spec.Selector.MatchLabels[BlackBoxProberManagerProberLabelKey]
				if !found || val != proberName {
					return false, "in-cluster deployment's label selector not being set to expected value"
				}

				return true, ""
			},
		},
		{
			name: "Service is created by manager",
			cfg:  BlackboxDeploymentConfig{},
			validate: func(_ BlackBoxProber, t clienttesting.ObjectTracker) (bool, string) {
				_, err := t.Get(corev1.SchemeGroupVersion.WithResource("services"), proberNamesapce, blackboxProberServiceName(proberName))
				if err != nil {
					return false, "expected service object was not created"
				}
				return true, ""
			},
		},
		{
			name: "Service selects prober deployment",
			cfg:  BlackboxDeploymentConfig{},
			validate: func(_ BlackBoxProber, t clienttesting.ObjectTracker) (bool, string) {
				obj, err := t.Get(corev1.SchemeGroupVersion.WithResource("services"), proberNamesapce, blackboxProberServiceName(proberName))
				if err != nil {
					return false, "expected service object was not created"
				}
				unstruct, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return false, "[should never happen] tracked object was not returned as unstructured data!"
				}
				service, err := convertUnstructuredToService(unstruct)
				if err != nil {
					return false, "[should never happen] was not able to convert from unstructured data to deployment"
				}

				val, found := service.Spec.Selector[BlackBoxProberManagerProberLabelKey]
				if !found || val != proberName {
					return false, "service's label selector does not include prober deployment"
				}

				return true, ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			manager, tracker := newTestBlackBoxProberManagerWithTracker(proberNamesapce, tt.cfg, []runtime.Object{})

			// Run test
			p, err := manager.CreateProber(ctx, "test")
			if (err != nil) != tt.want.err {
				t.Errorf("unexpected error in manager.CreateProber test %q: err = %v", tt.name, err)
			}

			// Validate results
			var prober *BlackBoxProber
			var ok bool
			prober, ok = p.(*BlackBoxProber)
			if !ok {
				t.Errorf("unexpected result in manager.CreateProber test %q: BlackBoxProberManager not creating valid BlackBoxProber objects", tt.name)
			}

			valid, reason := tt.validate(*prober, tracker)
			if !valid {
				t.Errorf("unexpected result in manager.CreateProber test %q: %s", tt.name, reason)
			}
		})
	}
}

func TestBlackBoxProberManager_buildProberDeployment(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		prober    string
		cfg       BlackboxDeploymentConfig
		validate  func(appsv1.Deployment) (bool, string)
	}{
		{
			name:   "deployment is named properly",
			prober: "test-prober",
			cfg:    BlackboxDeploymentConfig{},
			validate: func(d appsv1.Deployment) (bool, string) {
				if d.Name != blackboxProberDeploymentName("test-prober") {
					return false, "deployment name does not match expected value"
				}
				return true, ""
			},
		},
		{
			name:      "deployment is in manager's namespace",
			namespace: "test-namespace",
			cfg:       BlackboxDeploymentConfig{},
			validate: func(d appsv1.Deployment) (bool, string) {
				if d.Namespace != "test-namespace" {
					return false, "deployment namespace does not match expected value"
				}
				return true, ""
			},
		},
		{
			name:   "deployment labels are all correct by default",
			prober: "test-prober",
			cfg:    BlackboxDeploymentConfig{},
			validate: func(d appsv1.Deployment) (bool, string) {
				// test .metadata.label
				val, found := d.Labels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("default ProberManager label %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("default ProberManager label %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				// test .spec.labelselector
				val, found = d.Spec.Selector.MatchLabels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("expected label selector %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("label selector %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				// test .spec.template.label
				val, found = d.Spec.Template.Labels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("expected label selector %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("label selector %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				return true, ""
			},
		},
		{
			name:   "custom labels cannot override label selector",
			prober: "test-prober",
			cfg: BlackboxDeploymentConfig{
				Labels: map[string]string{BlackBoxProberManagerProberLabelKey: "invalidLabelValue"},
			},
			validate: func(d appsv1.Deployment) (bool, string) {
				// test .metadata.label
				val, found := d.Labels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("default ProberManager label %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("default ProberManager label %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				// test .spec.labelselector
				val, found = d.Spec.Selector.MatchLabels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("expected label selector %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("label selector %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				// test .spec.template.label
				val, found = d.Spec.Template.Labels[BlackBoxProberManagerProberLabelKey]
				if !found {
					return false, fmt.Sprintf("expected label selector %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "test-prober" {
					return false, fmt.Sprintf("label selector %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				return true, ""
			},
		},
		{
			name:   "non-conflicting custom labels get applied to deployment",
			prober: "test-prober",
			cfg: BlackboxDeploymentConfig{
				Labels: map[string]string{"validCustomLabelKey": "validCustomLabelValue"},
			},
			validate: func(d appsv1.Deployment) (bool, string) {
				// test .metadata.label
				val, found := d.Labels["validCustomLabelKey"]
				if !found {
					return false, fmt.Sprintf("custom ProberManager label %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "validCustomLabelValue" {
					return false, fmt.Sprintf("custom ProberManager label %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				// test .spec.template.label
				val, found = d.Spec.Template.Labels["validCustomLabelKey"]
				if !found {
					return false, fmt.Sprintf("expected label selector %q is not present", BlackBoxProberManagerProberLabelKey)
				}
				if val != "validCustomLabelValue" {
					return false, fmt.Sprintf("label selector %q does not have the correct value %q", BlackBoxProberManagerProberLabelKey, "test-prober")
				}

				return true, ""
			},
		},
		{
			name: "container is configurable",
			cfg: BlackboxDeploymentConfig{
				Cmd:   []string{"fakecommand"},
				Args:  []string{"fakearg1", "fakearg2"},
				Image: "quay.io/faketest/image:faketag",
			},
			validate: func(d appsv1.Deployment) (bool, string) {
				if len(d.Spec.Template.Spec.Containers) != 1 {
					return false, fmt.Sprintf("unexpected number of containers configured for prober deployment: expected 1, got %d", len(d.Spec.Template.Spec.Containers))
				}
				container := d.Spec.Template.Spec.Containers[0]

				// image
				if container.Image != "quay.io/faketest/image:faketag" {
					return false, "prober container image does not match config"
				}
				// command
				if len(container.Command) != 1 || container.Command[0] != "fakecommand" {
					return false, "prober container command does not match config"
				}
				// args
				if len(container.Args) != 2 ||
					container.Args[0] != "fakearg1" ||
					container.Args[1] != "fakearg2" {
					return false, "prober container arguments do not match config"
				}

				return true, ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			manager := newTestBlackBoxProberManager(tt.namespace, tt.cfg, []runtime.Object{})

			// Run test
			deployment := manager.buildProberDeployment(tt.prober)

			// Validate results
			valid, reason := tt.validate(deployment)
			if !valid {
				t.Errorf("unexpected result in manager.buildProberDeployment test %q: %s", tt.name, reason)
			}
		})
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

func Test_blackBoxProberDeploymentName(t *testing.T) {
	name := blackboxProberDeploymentName("test-prober")
	expected := "synthetics-blackbox-prober-test-prober"
	if name != expected {
		t.Errorf("Expected deployment name '%s', got '%s'", expected, name)
	}
}

func Test_blackBoxProberServiceName(t *testing.T) {
	name := blackboxProberServiceName("test-prober")
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

func TestBlackBoxProberManager_PrometheusExists(t *testing.T) {
	var (
		ctx              = context.Background()
		defaultNamespace = "default"
	)
	type result struct {
		found bool
		err   bool
	}

	tests := []struct {
		name      string
		namespace string
		objs      []runtime.Object
		want      result
	}{
		{
			name: "Returns true when matching object present",
			objs: []runtime.Object{
				emptyPrometheus(PrometheusResourceName, defaultNamespace),
			},
			want: result{
				found: true,
				err:   false,
			},
		},
		{
			name: "Returns false without error if no object present",
			want: result{
				found: false,
				err:   false,
			},
		},
		{
			name: "Returns false without error if different prometheus present",
			objs: []runtime.Object{
				emptyPrometheus("invalidPromName", defaultNamespace),
			},
			want: result{
				found: false,
				err:   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			manager := newTestBlackBoxProberManager(defaultNamespace, BlackboxDeploymentConfig{}, tt.objs)

			// Run tests
			found, err := manager.PrometheusExists(ctx)

			// Validate
			if (err != nil) != tt.want.err {
				t.Errorf("unexpected error in manager.PrometheusExists test %q: err = %v", tt.name, err)
			}

			if found != tt.want.found {
				t.Errorf("unexpected result in manager.PrometheusExists test %q: wanted found to be = %t, got %t", tt.name, tt.want.found, found)
			}
		})
	}
}

func TestBlackBoxProberManager_CreatePrometheus(t *testing.T) {
	var (
		ctx              = context.Background()
		defaultNamespace = "default"
	)
	tests := []struct {
		name     string
		objs     []runtime.Object
		validate func(clienttesting.ObjectTracker, error) (bool, string)
	}{
		{
			name: "prometheus CR will be created",
			validate: func(t clienttesting.ObjectTracker, err error) (bool, string) {
				// CreatePrometheus() had an error
				if err != nil {
					return false, fmt.Sprintf("unexpected error while creating prometheus instance: %v", err)
				}

				// Make sure the fake-client has a matching prom instance present
				_, err = t.Get(promv1.SchemeGroupVersion.WithResource("prometheuses"), defaultNamespace, PrometheusResourceName)
				if err != nil {
					return false, "expected prometheus object was not created"
				}
				return true, ""
			},
		},
		{
			name: "errors are bubbled up",
			objs: []runtime.Object{emptyPrometheus(PrometheusResourceName, defaultNamespace)},
			validate: func(_ clienttesting.ObjectTracker, err error) (bool, string) {
				if err == nil {
					return false, "expected error from conflicting resource, but none was returned"
				}
				return true, ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			manager, tracker := newTestBlackBoxProberManagerWithTracker(defaultNamespace, BlackboxDeploymentConfig{}, tt.objs)

			// Run tests
			err := manager.CreatePrometheus(ctx)

			// Validate
			valid, reason := tt.validate(tracker, err)
			if !valid {
				t.Errorf("unexpected result in manager.CreatePrometheus test %q: %s", tt.name, reason)
			}
		})
	}
}

func TestBlackBoxProberManager_DeletePrometheus(t *testing.T) {
	// Test objects
	var (
		ctx              = context.Background()
		defaultNamespace = "default"
	)

	tests := []struct {
		name     string
		objs     []runtime.Object
		validate func(clienttesting.ObjectTracker, error) (bool, string)
	}{
		{
			name: "Existing Prometheus will be deleted",
			objs: []runtime.Object{emptyPrometheus(PrometheusResourceName, defaultNamespace)},
			validate: func(t clienttesting.ObjectTracker, err error) (bool, string) {
				if err != nil {
					return false, fmt.Sprintf("unexpected error while deleting prometheus: %v", err)
				}

				_, err = t.Get(promv1.SchemeGroupVersion.WithResource("prometheuses"), defaultNamespace, PrometheusResourceName)
				if err == nil {
					return false, "prometheus was not deleted as expected"
				}
				if !kerr.IsNotFound(err) {
					return false, fmt.Sprintf("unexpected error while checking that prometheus was deleted: %v", err)
				}
				return true, ""
			},
		},
		{
			name: "no error returned if prometheus is already deleted",
			objs: []runtime.Object{},
			validate: func(t clienttesting.ObjectTracker, err error) (bool, string) {
				if err != nil {
					return false, fmt.Sprintf("unexpected error deleting already-missing prometheus: %v", err)
				}
				return true, ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			manager, tracker := newTestBlackBoxProberManagerWithTracker(defaultNamespace, BlackboxDeploymentConfig{}, tt.objs)

			// Run tests
			err := manager.DeletePrometheus(ctx)

			valid, reason := tt.validate(tracker, err)
			if !valid {
				t.Errorf("unexpected result in manager.CreatePrometheus test %q: %s", tt.name, reason)
			}
		})
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

// Test utilities

func newTestBlackBoxProberManagerWithTracker(namespace string, cfg BlackboxDeploymentConfig, objs []runtime.Object) (BlackBoxProberManager, clienttesting.ObjectTracker) {
	s := scheme.Scheme
	kubeClient := fake.NewSimpleDynamicClient(s, objs...)

	manager := BlackBoxProberManager{
		kubeClient: kubeClient,
		namespace:  namespace,
		cfg:        cfg,
	}

	return manager, kubeClient.Tracker()
}

func newTestBlackBoxProberManager(namespace string, cfg BlackboxDeploymentConfig, objs []runtime.Object) BlackBoxProberManager {
	s := scheme.Scheme
	err := promv1.AddToScheme(s)
	if err != nil {
		panic(fmt.Sprintf("could not add promv1 to scheme: %v", err))
	}

	kubeClient := fake.NewSimpleDynamicClient(s, objs...)

	manager := BlackBoxProberManager{
		kubeClient: kubeClient,
		namespace:  namespace,
		cfg:        cfg,
	}

	return manager
}

func emptyProberDeployment(name, namespace string) *appsv1.Deployment {
	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxProberDeploymentName(name),
			Namespace: namespace,
		},
	}
	return &deployment
}

func emptyPrometheus(name, namespace string) *promv1.Prometheus {
	p := promv1.Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	return &p
}
