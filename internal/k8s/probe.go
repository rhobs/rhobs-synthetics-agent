package k8s

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

// ProbeSpec defines the desired state of a synthetic probe
type ProbeSpec struct {
	Interval   string            `json:"interval"`
	Module     string            `json:"module"`
	ProberURL  string            `json:"prober_url"`
	Targets    ProbeTargets      `json:"targets"`
	Labels     map[string]string `json:"labels"`
}

// ProbeTargets defines the target configuration for the probe
type ProbeTargets struct {
	StaticConfig StaticConfig `json:"staticConfig"`
}

// StaticConfig defines static target configuration
type StaticConfig struct {
	Static []string          `json:"static"`
	Labels map[string]string `json:"labels"`
}

// ProbeConfig holds configuration for creating probe resources
type ProbeConfig struct {
	Interval  string `json:"interval"`
	Module    string `json:"module"`
	ProberURL string `json:"prober_url"`
}

// ProbeCustomResource represents a probe.monitoring.rhobs Custom Resource
type ProbeCustomResource struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   map[string]interface{} `json:"metadata"`
	Spec       ProbeSpec              `json:"spec"`
}

// ProbeManager handles the creation and management of probe Custom Resources
type ProbeManager struct {
	namespace  string
	httpClient *http.Client
}

// NewProbeManager creates a new probe manager
func NewProbeManager(namespace string) *ProbeManager {
	return &ProbeManager{
		namespace: namespace,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
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

// CreateProbeResource creates a probe.monitoring.rhobs Custom Resource
func (pm *ProbeManager) CreateProbeResource(probe api.Probe, config ProbeConfig) (*ProbeCustomResource, error) {
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

	// Create the Custom Resource
	cr := &ProbeCustomResource{
		APIVersion: "monitoring.rhobs/v1",
		Kind:       "Probe",
		Metadata: map[string]interface{}{
			"name":      fmt.Sprintf("probe-%s", probe.ID),
			"namespace": pm.namespace,
			"labels":    metadataLabels,
		},
		Spec: ProbeSpec{
			Interval:  config.Interval,
			Module:    config.Module,
			ProberURL: config.ProberURL,
			Targets: ProbeTargets{
				StaticConfig: StaticConfig{
					Static: []string{probe.StaticURL},
					Labels: targetLabels,
				},
			},
			Labels: probe.Labels,
		},
	}

	return cr, nil
}