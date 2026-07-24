package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockBlackboxExporter simulates a blackbox exporter for e2e tests.
// Toggle success/failure to simulate connectivity establishment.
type MockBlackboxExporter struct {
	server  *httptest.Server
	mu      sync.RWMutex
	success bool
}

func NewMockBlackboxExporter(success bool) *MockBlackboxExporter {
	m := &MockBlackboxExporter{success: success}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		s := m.success
		m.mu.RUnlock()

		val := "0"
		if s {
			val = "1"
		}
		_, _ = fmt.Fprintf(w, "# HELP probe_success Displays whether or not the probe was a success\n# TYPE probe_success gauge\nprobe_success %s\n", val)
	}))
	return m
}

func (m *MockBlackboxExporter) SetSuccess(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.success = success
}

func (m *MockBlackboxExporter) Addr() string {
	return m.server.Listener.Addr().String()
}

func (m *MockBlackboxExporter) Close() {
	m.server.Close()
}
