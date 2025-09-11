package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"

	promv1 "github.com/rhobs/obo-prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
)

type ProberManager interface {
	// GetProber retrieves the given Prober
	GetProber(ctx context.Context, name string) (p Prober, found bool, err error)
	// CreateProber creates a Prober with the given name
	CreateProber(ctx context.Context, name string) (p Prober, err error)
	// DeleteProber removes a Prober with the given name
	DeleteProber(ctx context.Context, name string) (err error)
	// PrometheusExists checks if the Prometheus instance exists for this manager
	PrometheusExists(ctx context.Context) (found bool, err error)
	// CreatePrometheus creates a Prometheus instance for synthetic monitoring
	CreatePrometheus(ctx context.Context) (err error)
	// DeletePrometheus removes the Prometheus instance
	DeletePrometheus(ctx context.Context) (err error)
}

const (
	// BlackBoxProberManagerResourceType defines the Kubernetes resource-type used to run
	// Blackbox exporter probers
	BlackBoxProberManagerResourceType = "deployment"
	// BlackBoxProberManagerProberLabelKey defines the label-key used to identify which Prober
	// Kubernetes objects belong to
	BlackBoxProberManagerProberLabelKey = "prober.synthetics-agent.rhobs"
	// DefaultBlackBoxExporterImage defines the container image used if none is given to the BlackBoxProberManager at creation-time
	DefaultBlackBoxExporterImage = "quay.io/prometheus/blackbox-exporter:latest"
	// DefaultBlackBoxProberManagerNamespace defines the namespace used if none is provided when creating a new BlackBoxProberManager.
	DefaultBlackBoxProberManagerNamespace = "default"
	// BlackBoxProberPrefix defines the prefix used to generate the name of a Prober
	BlackBoxProberPrefix = "synthetics-blackbox-prober"
	// PrometheusResourceName defines the name of the Prometheus instance for synthetic monitoring
	PrometheusResourceName = "synthetics-agent"
	// PrometheusAPIVersion defines the API version for Prometheus CRD
	PrometheusAPIVersion = "monitoring.rhobs/v1"
	// PrometheusKind defines the Kind for Prometheus resources
	PrometheusKind = "Prometheus"
	// SyntheticsAgentManagedByValue defines the value for rhobs.monitoring/managed-by labels
	SyntheticsAgentManagedByValue = "rhobs-synthetics-agent"
)

type BlackBoxProberManager struct {
	// kubeClient allows the BlackBoxProberManager to interact with a Kubernetes cluster
	kubeClient *kubeclient.Client
	// namespace denotes the namespace the ProberManager is operating under
	namespace string
	// cfg defines the BlackBoxProberManager's configuration. All BlackBoxProbers managed by
	// this BlackBoxProberManager will be subject to this configuration
	cfg BlackboxDeploymentConfig
	// remoteWriteURL defines the Thanos remote write endpoint URL for Prometheus configuration
	remoteWriteURL string
	// remoteWriteTenant defines the Thanos tenant identifier for remote write requests
	remoteWriteTenant string
	// prometheusResources defines the CPU and memory resource configuration for Prometheus
	prometheusResources PrometheusResourceConfig
	// managedByOperator defines the value for app.kubernetes.io/managed-by label on Prometheus resources
	managedByOperator string
}

// PrometheusResourceConfig holds the resource configuration for Prometheus pods
type PrometheusResourceConfig struct {
	CPURequests    string
	CPULimits      string
	MemoryRequests string
	MemoryLimits   string
}

// BlackBoxProberManagerConfig holds the configuration for creating a BlackBoxProberManager
type BlackBoxProberManagerConfig struct {
	Namespace           string
	KubeconfigPath      string
	Deployment          BlackboxDeploymentConfig
	RemoteWriteURL      string
	RemoteWriteTenant   string
	PrometheusResources PrometheusResourceConfig
	ManagedByOperator   string
}

// NewBlackBoxProberManager creates a new BlackBoxProberManager with the provided configuration
func NewBlackBoxProberManager(config BlackBoxProberManagerConfig) (*BlackBoxProberManager, error) {
	client, err := kubeclient.NewClient(kubeclient.Config{KubeconfigPath: config.KubeconfigPath})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	namespace := config.Namespace
	if namespace == "" {
		namespace = DefaultBlackBoxProberManagerNamespace
	}

	remoteWriteURL := config.RemoteWriteURL
	if remoteWriteURL == "" {
		remoteWriteURL = fmt.Sprintf("http://thanos-receive-router-rhobs.%s.svc.cluster.local:19291/api/v1/receive", namespace)
	}

	managedByOperator := config.ManagedByOperator
	if managedByOperator == "" {
		managedByOperator = "observability-operator"
	}

	manager := &BlackBoxProberManager{
		kubeClient:          client,
		namespace:           namespace,
		cfg:                 config.Deployment,
		remoteWriteURL:      remoteWriteURL,
		remoteWriteTenant:   config.RemoteWriteTenant,
		prometheusResources: config.PrometheusResources,
		managedByOperator:   managedByOperator,
	}
	return manager, nil
}

// deploymentClient is a helper function to interact with appsv1.Deployment objects in the namespace specified
// in the BlackBoxProberManager's config
func (m *BlackBoxProberManager) deploymentClient() dynamic.ResourceInterface {
	return m.kubeClient.DynamicClient().Resource(appsv1.SchemeGroupVersion.WithResource("deployments")).Namespace(m.namespace)
}

// serviceClient is a helper function to interact with corev1.Service objects in the namespace specified
// in the BlackBoxProberManager's config
func (m *BlackBoxProberManager) serviceClient() dynamic.ResourceInterface {
	serviceGVR := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "services",
	}
	return m.kubeClient.DynamicClient().Resource(serviceGVR).Namespace(m.namespace)
}

// prometheusClient is a helper function to interact with monitoring.rhobs/v1 Prometheus objects
func (m *BlackBoxProberManager) prometheusClient() dynamic.ResourceInterface {
	prometheusGVR := schema.GroupVersionResource{
		Group:    "monitoring.rhobs",
		Version:  "v1",
		Resource: "prometheuses",
	}
	return m.kubeClient.DynamicClient().Resource(prometheusGVR).Namespace(m.namespace)
}

func (m *BlackBoxProberManager) GetProber(ctx context.Context, name string) (p Prober, found bool, err error) {
	deploymentName := m.proberDeploymentName(name)
	unstruct, err := m.deploymentClient().Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		if kerr.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to GET prober deployment %q: %w", fmt.Sprintf("%s/%s", m.namespace, deploymentName), err)
	}

	deployment, err := convertUnstructuredToDeployment(unstruct)
	if err != nil {
		return nil, false, fmt.Errorf("failed to convert unstructured object to deployment object: %w", err)
	}

	p = &BlackBoxProber{
		deployment: *deployment,
	}
	return p, true, nil
}

func (m *BlackBoxProberManager) CreateProber(ctx context.Context, name string) (Prober, error) {
	// Create deployment
	deployment := m.buildProberDeployment(name)
	unstructuredProber, err := convertToUnstructured(&deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to convert deployment to unstructured object: %w", err)
	}

	unstructuredResult, err := m.deploymentClient().Create(ctx, unstructuredProber, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to CREATE prober deployment %q: %w", fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name), err)
	}

	result, err := convertUnstructuredToDeployment(unstructuredResult)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured object to deployment object: %w", err)
	}

	// Create service
	service := m.buildProberService(name)
	unstructuredService, err := convertToUnstructured(&service)
	if err != nil {
		return nil, fmt.Errorf("failed to convert service to unstructured object: %w", err)
	}

	_, err = m.serviceClient().Create(ctx, unstructuredService, metav1.CreateOptions{})
	if err != nil {
		// If service creation fails, we should still return the deployment result
		// but log the error since the service is not critical for basic functionality
		return result, fmt.Errorf("prober deployment created but service creation failed %q: %w", fmt.Sprintf("%s/%s", service.Namespace, service.Name), err)
	}

	return result, nil
}

func (m *BlackBoxProberManager) DeleteProber(ctx context.Context, name string) error {
	return nil
}

func (m *BlackBoxProberManager) PrometheusExists(ctx context.Context) (found bool, err error) {
	// Add timeout for the operation
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err = m.prometheusClient().Get(ctx, PrometheusResourceName, metav1.GetOptions{})
	if err != nil {
		if kerr.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to GET prometheus %q: %w",
			fmt.Sprintf("%s/%s", m.namespace, PrometheusResourceName), err)
	}
	return true, nil
}

func (m *BlackBoxProberManager) CreatePrometheus(ctx context.Context) error {
	// Add timeout for the operation
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	prometheus := m.buildPrometheusResource()
	unstructuredPrometheus, err := convertToUnstructured(prometheus)
	if err != nil {
		return fmt.Errorf("failed to convert prometheus to unstructured object: %w", err)
	}

	_, err = m.prometheusClient().Create(ctx, unstructuredPrometheus, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to CREATE prometheus %q: %w",
			fmt.Sprintf("%s/%s", m.namespace, PrometheusResourceName), err)
	}

	return nil
}

func (m *BlackBoxProberManager) DeletePrometheus(ctx context.Context) error {
	// Add timeout for cleanup operation
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	err := m.prometheusClient().Delete(ctx, PrometheusResourceName, metav1.DeleteOptions{})
	if err != nil && !kerr.IsNotFound(err) {
		return fmt.Errorf("failed to DELETE prometheus %q: %w",
			fmt.Sprintf("%s/%s", m.namespace, PrometheusResourceName), err)
	}
	return nil
}

// buildPrometheusResource creates a Prometheus resource for synthetic monitoring
// based on the configuration from obs-prometheus-rhobs.yaml
func (m *BlackBoxProberManager) buildPrometheusResource() *promv1.Prometheus {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": m.managedByOperator,
	}

	return &promv1.Prometheus{
		TypeMeta: metav1.TypeMeta{
			APIVersion: PrometheusAPIVersion,
			Kind:       PrometheusKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PrometheusResourceName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: promv1.PrometheusSpec{
			CommonPrometheusFields: promv1.CommonPrometheusFields{
				ProbeNamespaceSelector: &metav1.LabelSelector{},
				ProbeSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"rhobs.monitoring/managed-by": SyntheticsAgentManagedByValue,
					},
				},
				RemoteWrite: []promv1.RemoteWriteSpec{
					{
						URL: m.remoteWriteURL,
						Headers: map[string]string{
							"THANOS-TENANT": m.remoteWriteTenant,
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    parseResourceQuantity(m.prometheusResources.CPURequests),
						corev1.ResourceMemory: parseResourceQuantity(m.prometheusResources.MemoryRequests),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    parseResourceQuantity(m.prometheusResources.CPULimits),
						corev1.ResourceMemory: parseResourceQuantity(m.prometheusResources.MemoryLimits),
					},
				},
			},
		},
	}
}

// parseResourceQuantity safely parses a resource quantity string
func parseResourceQuantity(resourceStr string) resource.Quantity {
	quantity, err := resource.ParseQuantity(resourceStr)
	if err != nil {
		// Return zero quantity if parsing fails (should not happen with validated config)
		return resource.Quantity{}
	}
	return quantity
}

func (m *BlackBoxProberManager) buildProberDeployment(proberName string) appsv1.Deployment {
	replicas := int32(1)
	labels := m.proberCustomLabels()
	labels[BlackBoxProberManagerProberLabelKey] = proberName

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.proberDeploymentName(proberName),
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{BlackBoxProberManagerProberLabelKey: proberName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "prober",
							Image:   m.cfg.Image,
							Command: m.cfg.Cmd,
							Args:    m.cfg.Args,
						},
					},
				},
			},
		},
	}
	return deployment
}

// buildProberService creates a Service for the BlackBoxProber deployment
func (m *BlackBoxProberManager) buildProberService(proberName string) corev1.Service {
	// Create labels that match the deployment selector
	selectorLabels := map[string]string{BlackBoxProberManagerProberLabelKey: proberName}

	// Create metadata labels
	labels := m.proberCustomLabels()
	labels[BlackBoxProberManagerProberLabelKey] = proberName
	labels["app.kubernetes.io/name"] = "blackbox-exporter"
	labels["app.kubernetes.io/instance"] = proberName

	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.proberServiceName(proberName),
			Namespace: m.namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       9115,
					TargetPort: intstr.FromInt(9115),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	return service
}

// proberDeploymentName generates the standardized name for a BlackBoxProber's deployment
func (m *BlackBoxProberManager) proberDeploymentName(name string) string {
	return fmt.Sprintf("%s-%s", BlackBoxProberPrefix, name)
}

// proberServiceName generates the standardized name for a BlackBoxProber's service
func (m *BlackBoxProberManager) proberServiceName(name string) string {
	return fmt.Sprintf("%s-%s-service", BlackBoxProberPrefix, name)
}

// proberCustomLabels retrieves the custom deployment labels specified by the BlackBoxProberManager's config.
// If none are defined, a non-nil empty map is returned
func (m *BlackBoxProberManager) proberCustomLabels() map[string]string {
	labels := m.cfg.Labels
	if labels == nil {
		labels = map[string]string{}
	}

	return labels
}

// Prober implementations define the operands which measure the availability of endpoints
type Prober interface {
	String() string
}

// BlackBoxProber defines a Prober which measures endpoint availability via BlackBox Exporter
// deployments
type BlackBoxProber struct {
	deployment appsv1.Deployment
}

// String prints a uniquely-identifying string for the BlackBoxProber
func (p *BlackBoxProber) String() string {
	return fmt.Sprintf("apps/v1/deployment/%s/%s", p.deployment.Namespace, p.deployment.Name)
}
