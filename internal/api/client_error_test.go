package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetProbes_ErrorCases(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
	}{
		{
			name: "API returns 404",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("Not found"))
			},
			expectError: true,
		},
		{
			name: "API returns 500",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Internal server error"))
			},
			expectError: true,
		},
		{
			name: "API returns invalid JSON",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectError: true,
		},
		{
			name: "API returns empty response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("{}"))
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := NewClient(server.URL+"/api/metrics/v1/test-tenant/probes", "")
			_, err := client.GetProbes("test=true")

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestClient_UpdateProbeStatus_ErrorCases(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse func(w http.ResponseWriter, r *http.Request)
		expectError    bool
	}{
		{
			name: "API returns 404",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("Probe not found"))
			},
			expectError: true,
		},
		{
			name: "API returns 500",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Internal server error"))
			},
			expectError: true,
		},
		{
			name: "API returns 204 No Content",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
			defer server.Close()

			client := NewClient(server.URL+"/api/metrics/v1/test-tenant/probes", "")
			err := client.UpdateProbeStatus("probe-1", "created")

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestClient_InvalidURL(t *testing.T) {
	client := NewClient("invalid-url/api/metrics/v1/test-tenant/probes", "")
	
	_, err := client.GetProbes("test=true")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}

	err = client.UpdateProbeStatus("probe-1", "created")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("https://example.com/api/metrics/v1/test-tenant/probes", "")
	
	if client.BaseURL != "https://example.com/api/metrics/v1/test-tenant/probes" {
		t.Errorf("Expected BaseURL 'https://example.com/api/metrics/v1/test-tenant/probes', got %s", client.BaseURL)
	}
	
	if client.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
}