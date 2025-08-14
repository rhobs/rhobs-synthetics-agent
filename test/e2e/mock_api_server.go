package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

// MockAPIServer provides a mock implementation of the RHOBS Synthetics API
type MockAPIServer struct {
	server *httptest.Server
	mu     sync.RWMutex
	probes map[string]*api.Probe
	URL    string
}

// NewMockAPIServer creates a new mock API server
func NewMockAPIServer() *MockAPIServer {
	mock := &MockAPIServer{
		probes: make(map[string]*api.Probe),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/probes", mock.handleProbes)
	mux.HandleFunc("/probes/", mock.handleProbeByID)

	mock.server = httptest.NewServer(mux)
	mock.URL = mock.server.URL

	// Add some default test probes
	mock.AddProbe(&api.Probe{
		ID:        "test-probe-1",
		StaticURL: "https://httpbin.org/status/200",
		Labels: map[string]string{
			"env":     "test",
			"private": "false",
			"rhobs-synthetics/status": "pending",
		},
		Status: "pending",
	})

	mock.AddProbe(&api.Probe{
		ID:        "test-probe-2", 
		StaticURL: "https://httpbin.org/get",
		Labels: map[string]string{
			"env":     "test",
			"private": "false",
			"rhobs-synthetics/status": "pending",
		},
		Status: "pending",
	})

	return mock
}

// Close shuts down the mock server
func (m *MockAPIServer) Close() {
	m.server.Close()
}

// AddProbe adds a probe to the mock server
func (m *MockAPIServer) AddProbe(probe *api.Probe) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probes[probe.ID] = probe
}

// GetProbe retrieves a probe by ID
func (m *MockAPIServer) GetProbe(id string) *api.Probe {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.probes[id]
}

// UpdateProbeStatus updates the status of a probe
func (m *MockAPIServer) UpdateProbeStatus(id, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if probe, exists := m.probes[id]; exists {
		probe.Status = status
	}
}

// GetProbeCount returns the number of probes
func (m *MockAPIServer) GetProbeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.probes)
}

// handleProbes handles GET /probes requests
func (m *MockAPIServer) handleProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Filter probes based on label selector if provided
	labelSelector := r.URL.Query().Get("label_selector")
	var filteredProbes []*api.Probe

	for _, probe := range m.probes {
		if matchesLabelSelector(probe, labelSelector) {
			filteredProbes = append(filteredProbes, probe)
		}
	}

	response := api.ProbeListResponse{
		Probes: make([]api.Probe, len(filteredProbes)),
	}

	for i, probe := range filteredProbes {
		response.Probes[i] = *probe
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleProbeByID handles GET/PATCH /probes/{id} requests
func (m *MockAPIServer) handleProbeByID(w http.ResponseWriter, r *http.Request) {
	// Extract probe ID from URL path
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/probes/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Probe ID required", http.StatusBadRequest)
		return
	}
	probeID := parts[0]

	switch r.Method {
	case http.MethodGet:
		m.handleGetProbe(w, probeID)
	case http.MethodPatch:
		m.handleUpdateProbe(w, r, probeID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetProbe handles GET /probes/{id}
func (m *MockAPIServer) handleGetProbe(w http.ResponseWriter, probeID string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	probe, exists := m.probes[probeID]
	if !exists {
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(probe); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleUpdateProbe handles PATCH /probes/{id}
func (m *MockAPIServer) handleUpdateProbe(w http.ResponseWriter, r *http.Request, probeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	probe, exists := m.probes[probeID]
	if !exists {
		http.Error(w, "Probe not found", http.StatusNotFound)
		return
	}

	var statusUpdate api.ProbeStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&statusUpdate); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update the probe status
	probe.Status = statusUpdate.Status

	w.WriteHeader(http.StatusOK)
}

// matchesLabelSelector checks if a probe matches the given label selector
func matchesLabelSelector(probe *api.Probe, selector string) bool {
	if selector == "" {
		return true
	}

	// Simple label selector implementation
	// Format: "key1=value1,key2=value2"
	pairs := strings.Split(selector, ",")
	for _, pair := range pairs {
		parts := strings.Split(strings.TrimSpace(pair), "=")
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		expectedValue := strings.TrimSpace(parts[1])

		if actualValue, exists := probe.Labels[key]; !exists || actualValue != expectedValue {
			return false
		}
	}

	return true
}