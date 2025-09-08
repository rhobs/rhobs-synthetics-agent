package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-agent/internal/k8s"
	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
	"github.com/rhobs/rhobs-synthetics-agent/internal/metrics"
)

const defaultProbeNamespace = "default"

type Worker struct {
	config            *Config
	apiClients        []*api.Client
	probeManager      *k8s.ProbeManager
	proberManager     k8s.ProberManager
	readinessCallback func(bool)
}

func NewWorker(cfg *Config) (*Worker, error) {
	var apiClients []*api.Client

	namespace := defaultProbeNamespace
	kubeConfigPath := ""
	blackboxDeploymentCfg := k8s.BlackboxDeploymentConfig{}
	if cfg != nil {
		// Create API clients for each configured URL
		apiURLs := cfg.GetAPIURLs()
		for _, apiURL := range apiURLs {
			if apiURL != "" {
				client := api.NewClient(apiURL, cfg.JWTToken)
				apiClients = append(apiClients, client)
			}
		}

		if cfg.Namespace != "" {
			namespace = cfg.Namespace
		}
		if cfg.KubeConfig != "" {
			kubeConfigPath = cfg.KubeConfig
		}

		// Blackbox exporter configs
		if len(cfg.Blackbox.Deployment.Args) != 0 {
			blackboxDeploymentCfg.Args = cfg.Blackbox.Deployment.Args
		}
		if len(cfg.Blackbox.Deployment.Cmd) != 0 {
			blackboxDeploymentCfg.Cmd = cfg.Blackbox.Deployment.Cmd
		}
		if cfg.Blackbox.Deployment.Image != "" {
			blackboxDeploymentCfg.Image = cfg.Blackbox.Deployment.Image
		}
		if cfg.Blackbox.Deployment.Labels != nil {
			blackboxDeploymentCfg.Labels = cfg.Blackbox.Deployment.Labels
		}
	}

	probeManager := k8s.NewProbeManager(namespace, kubeConfigPath)
	var proberManager k8s.ProberManager
	var err error
	if kubeConfigPath != "" {
		proberManager, err = k8s.NewBlackBoxProberManager(namespace, kubeConfigPath, blackboxDeploymentCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create prober manager: %w", err)
		}
	}

	w := &Worker{
		config:            cfg,
		apiClients:        apiClients,
		probeManager:      probeManager,
		proberManager:     proberManager,
		readinessCallback: func(bool) {}, // no-op by default
	}
	return w, nil
}

// SetReadinessCallback sets the callback function to signal readiness state changes
func (w *Worker) SetReadinessCallback(callback func(bool)) {
	w.readinessCallback = callback
}

func (w *Worker) Start(ctx context.Context, taskWG *sync.WaitGroup, shutdownChan chan struct{}) error {
	logger.Info("RHOBS Synthetic Agent worker thread started")

	if len(w.apiClients) == 0 {
		logger.Info("Warning: No API URLs configured. Agent will run in standalone mode without probe processing.")
		logger.Info("To enable probe processing, configure api_urls in your config file or set API_URLS environment variable with complete URLs (e.g., https://api.example.com/api/metrics/v1/tenant/probes).")
		// Signal ready for standalone mode
		w.readinessCallback(true)
	} else {
		logger.Infof("Configured %d API endpoint(s) for probe processing", len(w.apiClients))
	}

	ticker := time.NewTicker(w.config.PollingInterval)
	defer ticker.Stop()

	// Initial run
	if err := w.processProbes(ctx, taskWG, shutdownChan); err != nil {
		logger.Errorf("initial work failed: %v\n", err)
		w.readinessCallback(false)
	} else if len(w.apiClients) > 0 {
		// Signal ready after successful initial API communication
		w.readinessCallback(true)
	}

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopping due to context cancellation")
			return ctx.Err()
		case <-shutdownChan:
			logger.Info("worker stopping due to shutdown signal")
			return nil
		case <-ticker.C:
			// Check if shutdown is in progress before starting new tasks
			select {
			case <-shutdownChan:
				logger.Info("shutdown in progress, skipping new probe processing")
				return nil
			default:
			}

			if err := w.processProbes(ctx, taskWG, shutdownChan); err != nil {
				logger.Errorf("work iteration failed: %v\n", err)
				// Continue running even if one iteration fails
			}
			if err := w.processProbers(ctx, shutdownChan); err != nil {
				logger.Errorf("failed to manage prober operands: %v\n", err)
			}
		}
	}
}

func (w *Worker) processProbes(ctx context.Context, taskWG *sync.WaitGroup, shutdownChan chan struct{}) error {
	// Check if shutdown is in progress before starting new tasks
	select {
	case <-shutdownChan:
		logger.Info("shutdown in progress, skipping probe processing")
		return nil
	default:
	}

	logger.Info("Starting probe reconciliation cycle")
	reconciliationStart := time.Now()
	var reconciliationErr error

	// Add task to wait group for graceful shutdown tracking
	taskWG.Add(1)
	defer func() {
		taskWG.Done()
		// Record reconciliation metrics
		duration := time.Since(reconciliationStart)
		success := reconciliationErr == nil
		metrics.RecordReconciliation(duration, success)
	}()

	if len(w.apiClients) == 0 {
		logger.Info("No API URLs configured, continuing to run in standalone mode (no probe processing)")
		return nil
	}

	if err := w.createProbes(ctx, shutdownChan); err != nil {
		logger.Infof("error processing probes with status=pending: %v", err)
	} else {
		logger.Infof("successfully processed probes with status=pending")
	}

	if err := w.deleteProbe(ctx, shutdownChan); err != nil {
		logger.Infof("error processing probes with status=terminating: %v", err)
	} else {
		logger.Infof("successfully processed probes with status=terminating")
	}

	return nil
}

// fetchProbeList retrieves probe configurations from all configured RHOBS Probes APIs
func (w *Worker) fetchProbeList(ctx context.Context, selector string) ([]api.Probe, error) {
	if len(w.apiClients) == 0 {
		return []api.Probe{}, nil
	}

	labelSelector := selector
	if w.config != nil {
		labelSelector = w.config.LabelSelector
	}

	logger.Infof("Fetching probe list from %d API endpoints with label selector: %s", len(w.apiClients), labelSelector)

	var allProbes []api.Probe
	var errors []error

	// Fetch probes from all API endpoints
	for i, apiClient := range w.apiClients {
		logger.Infof("Fetching probes from API endpoint %d/%d", i+1, len(w.apiClients))

		fetchStart := time.Now()
		probes, err := apiClient.GetProbes(labelSelector)
		fetchDuration := time.Since(fetchStart)

		apiEndpoint := fmt.Sprintf("endpoint_%d", i+1)
		if err != nil {
			logger.Infof("Failed to fetch probes from API endpoint %d: %v", i+1, err)
			errors = append(errors, fmt.Errorf("API endpoint %d: %w", i+1, err))
			metrics.RecordProbeListFetch(apiEndpoint, fetchDuration, false)
			continue
		}

		logger.Infof("Successfully fetched %d probes from API endpoint %d", len(probes), i+1)
		metrics.RecordProbeListFetch(apiEndpoint, fetchDuration, true)
		allProbes = append(allProbes, probes...)
	}

	// If all API endpoints failed, return an error
	if len(errors) == len(w.apiClients) {
		return nil, fmt.Errorf("failed to fetch probes from all %d API endpoints: %v", len(w.apiClients), errors)
	}

	// Remove duplicate probes by ID
	uniqueProbes := w.deduplicateProbes(allProbes)

	logger.Infof("Successfully fetched %d total probes (%d unique) from %d API endpoints", len(allProbes), len(uniqueProbes), len(w.apiClients))
	return uniqueProbes, nil
}

// deduplicateProbes removes duplicate probes by URL, keeping the first occurrence
func (w *Worker) deduplicateProbes(probes []api.Probe) []api.Probe {
	seen := make(map[string]bool)
	var unique []api.Probe

	for _, probe := range probes {
		if !seen[probe.StaticURL] {
			seen[probe.StaticURL] = true
			unique = append(unique, probe)
		}
	}

	return unique
}

// updateProbeStatus updates the probe status on all API clients that might have this probe
func (w *Worker) updateProbeStatus(probeID, status string) {
	var errors []error
	successCount := 0

	for i, apiClient := range w.apiClients {
		if err := apiClient.UpdateProbeStatus(probeID, status); err != nil {
			logger.Infof("Failed to update probe %s status on API endpoint %d: %v", probeID, i+1, err)
			errors = append(errors, err)
		} else {
			logger.Infof("Successfully updated probe %s status to %s on API endpoint %d", probeID, status, i+1)
			successCount++
		}
	}

	if successCount == 0 {
		logger.Infof("Failed to update probe %s status on all %d API endpoints", probeID, len(w.apiClients))
	} else if len(errors) > 0 {
		logger.Infof("Updated probe %s status on %d/%d API endpoints", probeID, successCount, len(w.apiClients))
	}
}

// createProbe creates a Custom Resource for a single probe
func (w *Worker) createProbes(ctx context.Context, shutdownChan chan struct{}) error {
	// Fetch probe configurations from the API
	probes, err := w.fetchProbeList(ctx, "pending")
	if err != nil {
		return fmt.Errorf("failed to fetch probe list: %w", err)
	}

	if len(probes) == 0 {
		logger.Info("No pending probes found")
		return nil
	}

	logger.Infof("Found %d probes to process", len(probes))

	// Process each probe
	for _, probe := range probes {
		select {
		case <-shutdownChan:
			logger.Info("shutdown in progress, stopping probe processing")
			return nil
		default:
		}

		// Try to create the probe Custom Resource in Kubernetes
		err = w.probeManager.CreateProbeK8sResource(probe, w.config.Blackbox.Probing)
		logger.Infof("Processing probe %s with target URL: %s", probe.ID, probe.StaticURL)
		if err != nil {
			// If K8s creation fails, fall back to logging the resource definition
			logger.Infof("Failed to create Kubernetes resource (falling back to logging): %v", err)

			cr, crErr := w.probeManager.CreateProbeResource(probe, w.config.Blackbox.Probing)
			if crErr != nil {
				w.updateProbeStatus(probe.ID, "failed")
				return fmt.Errorf("failed to create probe resource definition: %w", crErr)
			}

			crJSON, jsonErr := json.MarshalIndent(cr, "", "  ")
			if jsonErr != nil {
				logger.Infof("Failed to marshal CR to JSON: %v", jsonErr)
			} else {
				logger.Infof("Would create probe Custom Resource:\n%s", string(crJSON))
			}

			logger.Infof("Probe %s processed (logged only - not running in compatible K8s cluster)", probe.ID)
			w.updateProbeStatus(probe.ID, "active")
			metrics.RecordProbeResourceOperation("create", true)
		} else {
			logger.Infof("Successfully created monitoring.coreos.com/v1 Probe resource for probe %s", probe.ID)
			w.updateProbeStatus(probe.ID, "active")
			metrics.RecordProbeResourceOperation("create", true)
		}
	}
	return nil
}

func (w *Worker) processProbers(ctx context.Context, shutdownChan chan struct{}) error {
	if w.proberManager == nil {
		return nil
	}

	shards := w.getProberShards()
	if len(shards) == 0 {
		logger.Warn("no probers to manage")
	}
	for _, shard := range shards {
		logger.Infof("reconciling prober %q", shard)
		err := w.manageProber(ctx, shard)
		if err != nil {
			// log error, but continue to reconcile other tenants
			logger.Errorf("failed to reconcile prober %q: %v", shard, err)
		}
	}
	return nil
}

func (w *Worker) getProberShards() []string {
	return []string{"default"}
}

func (w *Worker) manageProber(ctx context.Context, name string) error {
	prober, found, err := w.proberManager.GetProber(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to retrieve prober %q: %w", name, err)
	}
	if !found {
		logger.Infof("prober %q not found; creating new prober", name)
		prober, err = w.proberManager.CreateProber(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to create prober %q: %w", name, err)
		}
		logger.Infof("new prober %q for %q created successfully", prober, name)
	}
	logger.Debugf("prober: %#v", prober)
	return nil
}

func (w *Worker) deleteProbe(ctx context.Context, shutdownChan chan struct{}) error {
	labelSelector, err := w.setStatusSelector(ctx, "terminating")
	if err != nil {
		return fmt.Errorf("failed to fetch probe list: %w", err)
	}
	logger.Infof("Fetching probe list from %d API endpoints with label selector: %s", len(w.apiClients), labelSelector)
	probes, err := w.fetchProbeList(ctx, labelSelector)
	if err != nil {
		return fmt.Errorf("failed to fetch probe list: %w", err)
	}
	logger.Infof("Found %d probes waiting to be deleted", len(probes))
	for _, probe := range probes {
		logger.Infof("Deleting probe %s with target URL: %s", probe.ID, probe.StaticURL)
		select {
		case <-shutdownChan:
			logger.Info("shutdown in progress, reattempting probe deletion")
			return nil
		default:
		}
		err := w.probeManager.DeleteProbeK8sResource(probe)
		if err != nil {
			return fmt.Errorf("failed to delete CR for probe %s: %w", probe.ID, err)
		}
		err = w.apiClients[0].DeleteProbe(probe.ID)
		if err != nil {
			return fmt.Errorf("failed to delete probe %s configuration from API database: %w", probe.ID, err)
		}
	}
	return nil
}

func (w *Worker) setStatusSelector(ctx context.Context, statusSelector string) (string, error) {
	label_selector := w.config.LabelSelector
	switch statusSelector {
	case "terminating":
		label_selector = fmt.Sprintf("%s,rhobs-synthetics/status=terminating", w.config.LabelSelector)
	case "pending":
		label_selector = fmt.Sprintf("%s,rhobs-synthetics/status=pending", w.config.LabelSelector)
	case "failed":
		label_selector = fmt.Sprintf("%s,rhobs-synthetics/status=failed", w.config.LabelSelector)
	case "active":
		label_selector = fmt.Sprintf("%s,rhobs-synthetics/status=active", w.config.LabelSelector)
	case "deleted":
		label_selector = fmt.Sprintf("%s,rhobs-synthetics/status=deleted", w.config.LabelSelector)
	default:
	}
	return label_selector, nil

}
