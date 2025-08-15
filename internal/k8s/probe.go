package k8s

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ProbeConfig holds configuration for creating probe resources
type ProbeConfig struct {
	Interval  string `json:"interval"`
	Module    string `json:"module"`
	ProberURL string `json:"prober_url"`
}

// ProbeManager handles the creation and management of probe Custom Resources
type ProbeManager struct {
	namespace      string
	kubeconfigPath string
	httpClient     *http.Client
	kubeClient     *kubeclient.Client
	isK8sCluster   bool
	probeAPIGroup  string // "monitoring.rhobs" or "monitoring.coreos.com" or ""
}

// NewProbeManager creates a new probe manager
func NewProbeManager(namespace, kubeconfigPath string) *ProbeManager {
	pm := &ProbeManager{
		namespace:      namespace,
		kubeconfigPath: kubeconfigPath,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Initialize Kubernetes clients and check cluster capabilities
	pm.initializeK8sClients()
	return pm
}

// ValidateURL checks if a URL is ready to be monitored
func (pm *ProbeManager) ValidateURL(targetURL string) error {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := pm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("URL validation failed with server error: %d", resp.StatusCode)
	}

	return nil
}

// initializeK8sClients sets up Kubernetes clients and checks cluster capabilities
func (pm *ProbeManager) initializeK8sClients() {
	// Check if running in a Kubernetes cluster
	if !kubeclient.IsRunningInK8sCluster() {
		pm.isK8sCluster = false
		return
	}

	// Create Kubernetes client
	cfg := kubeclient.Config{
		KubeconfigPath: pm.kubeconfigPath,
	}

	client, err := kubeclient.NewClient(cfg)
	if err != nil {
		log.Printf("Failed to create Kubernetes client: %v", err)
		pm.isK8sCluster = false
		return
	}

	pm.kubeClient = client
	pm.isK8sCluster = true

	// Check if Probe CRD exists
	pm.checkProbeCRDs()
}

// checkProbeCRDs checks if Probe CRDs exist in the cluster
func (pm *ProbeManager) checkProbeCRDs() {
	if pm.kubeClient == nil {
		return
	}

	// Check if the CRDs exist
	crdClient := pm.kubeClient.Clientset().Discovery()
	_, apiLists, err := crdClient.ServerGroupsAndResources()
	if err != nil {
		return
	}

	// Prefer monitoring.rhobs, fallback to monitoring.coreos.com
	for _, apiList := range apiLists {
		if apiList.GroupVersion == "monitoring.rhobs/v1" {
			for _, resource := range apiList.APIResources {
				if resource.Kind == "Probe" {
					pm.probeAPIGroup = "monitoring.rhobs"
					log.Printf("Using monitoring.rhobs/v1 for Probe resources")
					return
				}
			}
		}
	}

	for _, apiList := range apiLists {
		if apiList.GroupVersion == "monitoring.coreos.com/v1" {
			for _, resource := range apiList.APIResources {
				if resource.Kind == "Probe" {
					pm.probeAPIGroup = "monitoring.coreos.com"
					log.Printf("Using monitoring.coreos.com/v1 for Probe resources")
					return
				}
			}
		}
	}

	log.Printf("No compatible Probe CRDs found in cluster")
}

// CreateProbeK8sResource creates and applies a Probe Custom Resource to Kubernetes
func (pm *ProbeManager) CreateProbeK8sResource(probe api.Probe, config ProbeConfig) error {
	// Check if we can create Kubernetes resources
	if !pm.isK8sCluster {
		return fmt.Errorf("not running in a Kubernetes cluster")
	}

	if pm.probeAPIGroup == "" {
		return fmt.Errorf("no compatible Probe CRDs found in cluster")
	}

	if pm.kubeClient == nil {
		return fmt.Errorf("kubernetes client not available")
	}

	// Create the probe Custom Resource definition
	probeResource, err := pm.CreateProbeResource(probe, config)
	if err != nil {
		return fmt.Errorf("failed to create probe resource definition: %w", err)
	}

	// Convert to unstructured for dynamic client
	unstructuredCR := &unstructured.Unstructured{}
	unstructuredCR.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": pm.probeAPIGroup + "/v1",
		"kind":       "Probe",
		"metadata":   probeResource.ObjectMeta,
		"spec": map[string]interface{}{
			"prober": map[string]interface{}{
				"url": probeResource.Spec.ProberSpec.URL,
			},
			"module":   probeResource.Spec.Module,
			"interval": probeResource.Spec.Interval,
			"targets": map[string]interface{}{
				"staticConfig": map[string]interface{}{
					"static": probeResource.Spec.Targets.StaticConfig.Targets,
					"labels": probeResource.Spec.Targets.StaticConfig.Labels,
				},
			},
		},
	})

	// Define the GVR for Probe resources
	probeGVR := schema.GroupVersionResource{
		Group:    pm.probeAPIGroup,
		Version:  "v1",
		Resource: "probes",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create the resource in Kubernetes
	_, err = pm.kubeClient.DynamicClient().Resource(probeGVR).Namespace(pm.namespace).Create(
		ctx,
		unstructuredCR,
		metav1.CreateOptions{},
	)

	if err != nil {
		return fmt.Errorf("failed to create Probe resource in Kubernetes: %w", err)
	}

	return nil
}

// CreateProbeResource creates a Probe Custom Resource (compatible with both monitoring.coreos.com/v1 and monitoring.rhobs/v1)
func (pm *ProbeManager) CreateProbeResource(probe api.Probe, config ProbeConfig) (*monitoringv1.Probe, error) {
	if err := pm.ValidateURL(probe.StaticURL); err != nil {
		return nil, fmt.Errorf("URL validation failed for probe %s: %w", probe.ID, err)
	}

	// Create metadata labels starting with required labels
	metadataLabels := map[string]string{
		"rhobs.monitoring/probe-id":   probe.ID,
		"rhobs.monitoring/managed-by": "rhobs-synthetics-agent",
	}

	// Create target labels starting with basic probe information
	targetLabels := map[string]string{
		"apiserver_url": probe.StaticURL,
	}

	// Add all probe labels to both metadata and target labels generically
	for key, value := range probe.Labels {
		// Add to metadata with rhobs.monitoring prefix for known cluster fields
		if key == "cluster_id" || key == "management_cluster_id" {
			metadataKey := fmt.Sprintf("rhobs.monitoring/%s", key)
			metadataLabels[metadataKey] = value
		}

		// Add all labels to target labels as-is
		targetLabels[key] = value
	}

	// Create the Probe Custom Resource using the actual CRD types
	probeResource := &monitoringv1.Probe{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "Probe",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("probe-%s", probe.ID),
			Namespace: pm.namespace,
			Labels:    metadataLabels,
		},
		Spec: monitoringv1.ProbeSpec{
			Module:   config.Module,
			Interval: config.Interval,
			ProberSpec: monitoringv1.ProberSpec{
				URL: config.ProberURL,
			},
			Targets: monitoringv1.ProbeTargets{
				StaticConfig: &monitoringv1.ProbeTargetStaticConfig{
					Targets: []string{probe.StaticURL},
					Labels:  targetLabels,
				},
			},
		},
	}

	return probeResource, nil
}
