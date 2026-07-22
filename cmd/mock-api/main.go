package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

type mockAPI struct {
	mu     sync.RWMutex
	probes map[string]*api.Probe
}

func newMockAPI() *mockAPI {
	m := &mockAPI{probes: make(map[string]*api.Probe)}
	m.probes["e2e-probe-1"] = &api.Probe{
		ID:        "e2e-probe-1",
		StaticURL: "https://httpbin.org/status/200",
		Labels:    map[string]string{"env": "e2e-test", "private": "false", "_id": "e2e-cluster-1"},
		Status:    "pending",
	}
	m.probes["e2e-probe-2"] = &api.Probe{
		ID:        "e2e-probe-2",
		StaticURL: "https://httpbin.org/get",
		Labels:    map[string]string{"env": "e2e-test", "private": "false", "_id": "e2e-cluster-2"},
		Status:    "pending",
	}
	return m
}

func (m *mockAPI) handleProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	selector := r.URL.Query().Get("label_selector")
	var filtered []api.Probe
	for _, p := range m.probes {
		if matchSelector(p, selector) {
			filtered = append(filtered, *p)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(api.ProbeListResponse{Probes: filtered}); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

func (m *mockAPI) handleProbeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/probes/")
	if id == "" {
		http.Error(w, "probe ID required", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	probe, ok := m.probes[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(probe); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	case http.MethodPatch:
		var update api.ProbeStatusUpdate
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		probe.Status = update.Status
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func matchSelector(p *api.Probe, selector string) bool {
	if selector == "" {
		return true
	}
	for _, pair := range strings.Split(selector, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) != 2 {
			continue
		}
		if v, ok := p.Labels[parts[0]]; !ok || v != parts[1] {
			return false
		}
	}
	return true
}

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	m := newMockAPI()
	mux := http.NewServeMux()
	mux.HandleFunc("/probes", m.handleProbes)
	mux.HandleFunc("/probes/", m.handleProbeByID)
	writeString := func(w http.ResponseWriter, s string) {
		_, _ = w.Write([]byte(s))
	}
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		writeString(w, "promhttp_metric_handler_requests_total 1\n")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeString(w, "ok\n")
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeString(w, "ok\n")
	})

	log.Printf("mock synthetics-api listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
