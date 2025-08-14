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
	apiClient    *api.Client
	probeManager *k8s.ProbeManager
}

func NewWorker(cfg *Config) *Worker {
	var apiClient *api.Client
	var probeManager *k8s.ProbeManager

	if cfg != nil {
		if cfg.APIBaseURL != "" {
			apiClient = api.NewClient(cfg.APIBaseURL, cfg.APITenant, cfg.APIEndpoint, cfg.JWTToken)
		}
		probeManager = k8s.NewProbeManager(cfg.Namespace)
	} else {
		probeManager = k8s.NewProbeManager("default")
	}

	return &Worker{
		config:       cfg,
		apiClient:    apiClient,
		probeManager: probeManager,
	}
}

func (w *Worker) Start(ctx context.Context, resourceMgr *ResourceManager, taskWG *sync.WaitGroup, shutdownChan chan struct{}) error {
	log.Println("RHOBS Synthetic Agent worker thread started")

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

	if w.apiClient == nil {
		log.Println("API client not configured, skipping probe processing")
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
			if updateErr := w.apiClient.UpdateProbeStatus(probe.ID, "failed"); updateErr != nil {
				log.Printf("Failed to update probe %s status to failed: %v", probe.ID, updateErr)
			}
		} else {
			log.Printf("Successfully processed probe %s", probe.ID)
			// Update probe status to active
			if updateErr := w.apiClient.UpdateProbeStatus(probe.ID, "active"); updateErr != nil {
				log.Printf("Failed to update probe %s status to active: %v", probe.ID, updateErr)
			}
		}
	}

	return nil
}

// fetchProbeList retrieves probe configurations from the RHOBS Probes API
func (w *Worker) fetchProbeList(ctx context.Context) ([]api.Probe, error) {
	if w.apiClient == nil {
		return nil, fmt.Errorf("API client not configured")
	}

	labelSelector := ""
	if w.config != nil {
		labelSelector = w.config.LabelSelector
	}

	log.Printf("Fetching probe list from API with label selector: %s", labelSelector)

	probes, err := w.apiClient.GetProbes(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to get probes from API: %w", err)
	}

	log.Printf("Successfully fetched %d probes from API", len(probes))
	return probes, nil
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
