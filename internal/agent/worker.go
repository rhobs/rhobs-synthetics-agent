package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-agent/internal/k8s"
)

type Worker struct {
	config       *Config
	apiClients   []*api.Client
	probeManager *k8s.ProbeManager
}

func NewWorker(cfg *Config) *Worker {
	var apiClients []*api.Client
	var probeManager *k8s.ProbeManager

	if cfg != nil {
		// Create API clients for each configured URL
		apiURLs := cfg.GetAPIBaseURLs()
		for _, baseURL := range apiURLs {
			if baseURL != "" {
				client := api.NewClient(baseURL, cfg.APITenant, cfg.APIEndpoint, cfg.JWTToken)
				apiClients = append(apiClients, client)
			}
		}

		namespace := cfg.Namespace
		if namespace == "" {
			namespace = "default"
		}
		probeManager = k8s.NewProbeManager(namespace, cfg.KubeConfig)
	} else {
		probeManager = k8s.NewProbeManager("default", "")
	}

	return &Worker{
		config:       cfg,
		apiClients:   apiClients,
		probeManager: probeManager,
	}
}

func (w *Worker) Start(ctx context.Context, resourceMgr *ResourceManager, taskWG *sync.WaitGroup, shutdownChan chan struct{}) error {
	log.Println("RHOBS Synthetic Agent worker thread started")

	if len(w.apiClients) == 0 {
		log.Println("Warning: No API URLs configured. Agent will run in standalone mode without probe processing.")
		log.Println("To enable probe processing, configure api_base_urls in your config file or set API_BASE_URLS environment variable.")
	} else {
		log.Printf("Configured %d API endpoint(s) for probe processing", len(w.apiClients))
	}

	ticker := time.NewTicker(w.config.PollingInterval)
	defer ticker.Stop()

	// Initial run
	if err := w.processProbes(ctx, resourceMgr, taskWG, shutdownChan); err != nil {
		log.Printf("initial work failed: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("worker stopping due to context cancellation")
			return ctx.Err()
		case <-shutdownChan:
			log.Println("worker stopping due to shutdown signal")
			return nil
		case <-ticker.C:
			// Check if shutdown is in progress before starting new tasks
			select {
			case <-shutdownChan:
				log.Println("shutdown in progress, skipping new probe processing")
				return nil
			default:
			}

			if err := w.processProbes(ctx, resourceMgr, taskWG, shutdownChan); err != nil {
				log.Printf("work iteration failed: %v\n", err)
				// Continue running even if one iteration fails
			}
		}
	}
}

func (w *Worker) processProbes(ctx context.Context, resourceMgr *ResourceManager, taskWG *sync.WaitGroup, shutdownChan chan struct{}) error {
	// Check if shutdown is in progress before starting new tasks
	select {
	case <-shutdownChan:
		log.Println("shutdown in progress, skipping probe processing")
		return nil
	default:
	}

	log.Println("Starting probe reconciliation cycle")

	// Add task to wait group for graceful shutdown tracking
	taskWG.Add(1)
	defer taskWG.Done()

	if len(w.apiClients) == 0 {
		log.Println("No API URLs configured, continuing to run in standalone mode (no probe processing)")
		return nil
	}

	// Fetch probe configurations from the API
	probes, err := w.fetchProbeList(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch probe list: %w", err)
	}

	if len(probes) == 0 {
		log.Println("No pending probes found")
		return nil
	}

	log.Printf("Found %d probes to process", len(probes))

	// Process each probe
	for _, probe := range probes {
		select {
		case <-shutdownChan:
			log.Println("shutdown in progress, stopping probe processing")
			return nil
		default:
		}

		if err := w.processProbe(ctx, probe); err != nil {
			log.Printf("Failed to process probe %s: %v", probe.ID, err)
			// Update probe status to failed
			w.updateProbeStatus(probe.ID, "failed")
		} else {
			log.Printf("Successfully processed probe %s", probe.ID)
			// Update probe status to active
			w.updateProbeStatus(probe.ID, "active")
		}
	}

	return nil
}

// fetchProbeList retrieves probe configurations from all configured RHOBS Probes APIs
func (w *Worker) fetchProbeList(ctx context.Context) ([]api.Probe, error) {
	if len(w.apiClients) == 0 {
		return []api.Probe{}, nil
	}

	labelSelector := ""
	if w.config != nil {
		labelSelector = w.config.LabelSelector
	}

	log.Printf("Fetching probe list from %d API endpoints with label selector: %s", len(w.apiClients), labelSelector)

	var allProbes []api.Probe
	var errors []error

	// Fetch probes from all API endpoints
	for i, apiClient := range w.apiClients {
		log.Printf("Fetching probes from API endpoint %d/%d", i+1, len(w.apiClients))

		probes, err := apiClient.GetProbes(labelSelector)
		if err != nil {
			log.Printf("Failed to fetch probes from API endpoint %d: %v", i+1, err)
			errors = append(errors, fmt.Errorf("API endpoint %d: %w", i+1, err))
			continue
		}

		log.Printf("Successfully fetched %d probes from API endpoint %d", len(probes), i+1)
		allProbes = append(allProbes, probes...)
	}

	// If all API endpoints failed, return an error
	if len(errors) == len(w.apiClients) {
		return nil, fmt.Errorf("failed to fetch probes from all %d API endpoints: %v", len(w.apiClients), errors)
	}

	// Remove duplicate probes by ID
	uniqueProbes := w.deduplicateProbes(allProbes)

	log.Printf("Successfully fetched %d total probes (%d unique) from %d API endpoints", len(allProbes), len(uniqueProbes), len(w.apiClients))
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
			log.Printf("Failed to update probe %s status on API endpoint %d: %v", probeID, i+1, err)
			errors = append(errors, err)
		} else {
			log.Printf("Successfully updated probe %s status to %s on API endpoint %d", probeID, status, i+1)
			successCount++
		}
	}

	if successCount == 0 {
		log.Printf("Failed to update probe %s status on all %d API endpoints", probeID, len(w.apiClients))
	} else if len(errors) > 0 {
		log.Printf("Updated probe %s status on %d/%d API endpoints", probeID, successCount, len(w.apiClients))
	}
}

// processProbe creates a Custom Resource for a single probe
func (w *Worker) processProbe(ctx context.Context, probe api.Probe) error {
	log.Printf("Processing probe %s with target URL: %s", probe.ID, probe.StaticURL)

	// Create probe configuration from the agent config
	probeConfig := k8s.ProbeConfig{
		Interval:  w.config.Blackbox.Interval,
		Module:    w.config.Blackbox.Module,
		ProberURL: w.config.Blackbox.ProberURL,
	}

	// Try to create the probe Custom Resource in Kubernetes
	err := w.probeManager.CreateProbeK8sResource(probe, probeConfig)
	if err != nil {
		// If K8s creation fails, fall back to logging the resource definition
		log.Printf("Failed to create Kubernetes resource (falling back to logging): %v", err)

		cr, crErr := w.probeManager.CreateProbeResource(probe, probeConfig)
		if crErr != nil {
			return fmt.Errorf("failed to create probe resource definition: %w", crErr)
		}

		crJSON, jsonErr := json.MarshalIndent(cr, "", "  ")
		if jsonErr != nil {
			log.Printf("Failed to marshal CR to JSON: %v", jsonErr)
		} else {
			log.Printf("Would create probe Custom Resource:\n%s", string(crJSON))
		}

		log.Printf("Probe %s processed (logged only - not running in compatible K8s cluster)", probe.ID)
	} else {
		log.Printf("Successfully created monitoring.coreos.com/v1 Probe resource for probe %s", probe.ID)
	}
	return nil
}
