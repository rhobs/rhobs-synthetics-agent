package agent

import (
	"context"
	"log"
	"sync"
	"time"
)

type Worker struct {
	config *Config
}

func NewWorker(cfg *Config) *Worker {

	return &Worker{
		config: cfg,
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

	log.Println("Checking for probes to create")

	// Add task to wait group for graceful shutdown tracking
	taskWG.Add(1)
	defer taskWG.Done()

	// TODO: Add core logic here:
	// 1. Call API to check for probes to create using api client
	w.fetchProbeList(ctx, resourceMgr)

	// 2. Process the probe creation
	w.executeProbes(ctx, resourceMgr)

	return nil
}

// fetchProbeList simulates fetching probe lists from API
func (w *Worker) fetchProbeList(ctx context.Context, resourceMgr *ResourceManager) {
	log.Printf("Starting probe list fetch...")

	// Simulate creating network connection for API call
	// In real implementation, this would be an actual HTTP connection
	// For demo purposes, we'll simulate it
	log.Printf("Establishing connection to API...")

	// Simulate API call - allow to complete naturally
	time.Sleep(1 * time.Second)
	log.Printf("Successfully fetched probe list from API")
}

// executeProbes simulates processing the fetched probes
func (w *Worker) executeProbes(ctx context.Context, resourceMgr *ResourceManager) {
	log.Printf("Starting probe processing...")

	// Simulate opening temporary files for probe data
	// In real implementation, these would be actual file handles
	log.Printf("Creating temporary files for probe data...")

	// Simulate probe processing - allow to complete naturally
	time.Sleep(500 * time.Millisecond)
	log.Printf("Processed probes successfully")
}
