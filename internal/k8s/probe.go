package k8s

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
	appsv1 "k8s.io/api/apps/v1"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ProbeManager handles the creation and management of probe Custom Resources
type ProbeManager struct {
	namespace     string
	httpClient    *http.Client
	kubeClient    *kubeclient.Client
	probeAPIGroup string // "monitoring.rhobs" or "monitoring.coreos.com" or ""
}

// NewProbeManager creates a new probe manager
func NewProbeManager(namespace, kubeconfigPath string) *ProbeManager {
	pm := &ProbeManager{
		namespace: namespace,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}

	// Initialize Kubernetes clients and check cluster capabilities
	pm.initializeK8sClients(kubeconfigPath)
	return pm
}

// ValidateURL checks if a URL has valid format and scheme
// Note: We only validate format and scheme, not connectivity, as URLs may be
// temporarily unreachable during deployment or due to transient network issues.
// The blackbox exporter will handle the actual connectivity monitoring.
func (pm *ProbeManager) ValidateURL(targetURL string) error {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must have a host")
	}

	return nil
}

// initializeK8sClients sets up Kubernetes clients and checks cluster capabilities
func (pm *ProbeManager) initializeK8sClients(kubeconfigPath string) {
	// Check if running in a Kubernetes cluster
	if !kubeclient.IsRunningInK8sCluster() {
		return
	}

	// Create Kubernetes client
	cfg := kubeclient.Config{
		KubeconfigPath: kubeconfigPath,
	}

	client, err := kubeclient.NewClient(cfg)
	if err != nil {
		logger.Errorf("Failed to create Kubernetes client: %v", err)
		return
	}

	pm.kubeClient = client

	// Check if Probe CRD exists
	pm.checkProbeCRDs()
}

func (pm *ProbeManager) isK8sCluster() bool {
	return pm.kubeClient != nil
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
					logger.Infof("Using monitoring.rhobs/v1 for Probe resources")
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
					logger.Infof("Using monitoring.coreos.com/v1 for Probe resources")
					return
				}
			}
		}
	}

	logger.Errorf("No compatible Probe CRDs found in cluster")
}

// SetProbeAPIGroup sets the API group for testing purposes
func (pm *ProbeManager) SetProbeAPIGroup(apiGroup string) {
	pm.probeAPIGroup = apiGroup
}

// CreateProbeK8sResource creates and applies a Probe Custom Resource to Kubernetes
func (pm *ProbeManager) CreateProbeK8sResource(probe api.Probe, config BlackboxProbingConfig) error {
	// Check if we can create Kubernetes resources
	if !pm.isK8sCluster() {
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

	unstructuredCR, err := convertToUnstructured(probeResource)
	if err != nil {
		return fmt.Errorf("failed to convert probe resource to unstructured data: %w", err)
	}

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

// DeleteProbeK8sResource deletes a Probe Custom Resource from Kubernetes
func (pm *ProbeManager) DeleteProbeK8sResource(probe api.Probe) error {
	// Check if we can interact with Kubernetes resources
	if !pm.isK8sCluster() {
		return fmt.Errorf("not running in a Kubernetes cluster")
	}

	if pm.probeAPIGroup == "" {
		return fmt.Errorf("no compatible Probe CRDs found in cluster")
	}

	if pm.kubeClient == nil {
		return fmt.Errorf("kubernetes client not available")
	}

	// Define the GVR for Probe resources
	probeGVR := schema.GroupVersionResource{
		Group:    pm.probeAPIGroup,
		Version:  "v1",
		Resource: "probes",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Construct the probe name using the same pattern as in CreateProbeK8sResource
	probeName := fmt.Sprintf("probe-%s", probe.ID)

	logger.Debugf("Attempting to delete Probe resource: name=%s, namespace=%s, apiGroup=%s",
		probeName, pm.namespace, pm.probeAPIGroup)

	// Delete the resource from Kubernetes
	err := pm.kubeClient.DynamicClient().Resource(probeGVR).Namespace(pm.namespace).Delete(
		ctx,
		probeName,
		metav1.DeleteOptions{},
	)

	if err != nil {
		// Check if the error is "not found" - this is considered successful deletion (idempotent)
		if kerr.IsNotFound(err) {
			logger.Infof("Probe resource %s not found (already deleted), treating as successful", probeName)
			return nil
		}
		return fmt.Errorf("failed to delete Probe resource %s from Kubernetes: %w", probeName, err)
	}

	logger.Infof("Successfully deleted Probe resource %s from Kubernetes", probeName)
	return nil
}

// CreateProbeResource creates a Probe Custom Resource (compatible with both monitoring.coreos.com/v1 and monitoring.rhobs/v1)
func (pm *ProbeManager) CreateProbeResource(probe api.Probe, config BlackboxProbingConfig) (*monitoringv1.Probe, error) {
	if err := pm.ValidateURL(probe.StaticURL); err != nil {
		return nil, fmt.Errorf("URL validation failed for probe %s: %w", probe.ID, err)
	}

	// Create metadata labels starting with required labels
	metadataLabels := map[string]string{
		"rhobs.monitoring/probe-id":   probe.ID,
		"rhobs.monitoring/managed-by": SyntheticsAgentManagedByValue,
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
	// Use detected API group, fallback to monitoring.coreos.com for backwards compatibility
	apiGroup := pm.probeAPIGroup
	if apiGroup == "" {
		apiGroup = "monitoring.coreos.com"
	}
	apiVersion := fmt.Sprintf("%s/v1", apiGroup)
	probeResource := &monitoringv1.Probe{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiVersion,
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

func convertToUnstructured(object interface{}) (*unstructured.Unstructured, error) {
	// If it's already a map[string]interface{}, use it directly
	if objMap, ok := object.(map[string]interface{}); ok {
		return &unstructured.Unstructured{
			Object: objMap,
		}, nil
	}

	// Otherwise, convert struct to map
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return &unstructured.Unstructured{}, err
	}
	unstruct := &unstructured.Unstructured{
		Object: obj,
	}
	return unstruct, nil
}

func convertUnstructuredToDeployment(unstruct *unstructured.Unstructured) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	if unstruct == nil {
		return deployment, fmt.Errorf("provided unstructured object is nil")
	}
	if unstruct.Object == nil {
		return deployment, fmt.Errorf("provided unstructured object has .Object=nil")
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.Object, deployment)
	return deployment, err
}
