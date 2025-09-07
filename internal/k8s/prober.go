package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"

	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
)

type ProberManager interface {
	// GetProber retrieves the given Prober
	GetProber(ctx context.Context, name string) (p Prober, found bool, err error)
	// CreateProber creates a Prober with the given name
	CreateProber(ctx context.Context, name string) (p Prober, err error)
	// DeleteProber removes a Prober with the given name
	DeleteProber(ctx context.Context, name string) (err error)
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
)

type BlackBoxProberManager struct {
	// kubeClient allows the BlackBoxProberManager to interact with a Kubernetes cluster
	kubeClient *kubeclient.Client
	// namespace denotes the namespace the ProberManager is operating under
	namespace string
	// cfg defines the BlackBoxProberManager's configuration. All BlackBoxProbers managed by
	// this BlackBoxProberManager will be subject to this configuration
	cfg BlackboxDeploymentConfig
}

//func NewBlackBoxProberManager(namespace string, kubeconfigPath string, opts... BlackBoxProberOption) (*BlackBoxProberManager, error) {
func NewBlackBoxProberManager(namespace string, kubeconfigPath string, cfg BlackboxDeploymentConfig) (*BlackBoxProberManager, error) {
	client, err := kubeclient.NewClient(kubeclient.Config{KubeconfigPath: kubeconfigPath})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	if namespace == "" {
		namespace = DefaultBlackBoxProberManagerNamespace
	}

	manager := &BlackBoxProberManager{
		kubeClient: client,
		namespace:  namespace,
		cfg:        cfg,
	}
	return manager, nil
}

// deploymentClient is a helper function to interact with appsv1.Deployment objects in the namespace specified
// in the BlackBoxProberManager's config
func (m *BlackBoxProberManager) deploymentClient() dynamic.ResourceInterface {
	return m.kubeClient.DynamicClient().Resource(appsv1.SchemeGroupVersion.WithResource("deployments")).Namespace(m.namespace)
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
	return result, nil
}

func (m *BlackBoxProberManager) DeleteProber(ctx context.Context, name string) error {
	return nil
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

// proberDeploymentName generates the standardized name for a BlackBoxProber's deployment
func (m *BlackBoxProberManager) proberDeploymentName(name string) string {
	return fmt.Sprintf("synthetics-agent-%s", name)
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
type Prober interface{
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
