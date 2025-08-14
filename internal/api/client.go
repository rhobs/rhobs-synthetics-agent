package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Probe represents a synthetic monitoring probe configuration
type Probe struct {
	ID        string            `json:"id"`
	StaticURL string            `json:"static_url"`
	Labels    map[string]string `json:"labels"`
	Status    string            `json:"status,omitempty"`
}

// ProbeListResponse represents the response from the probes list endpoint
type ProbeListResponse struct {
	Probes []Probe `json:"probes"`
}

// ProbeStatusUpdate represents the payload for updating probe status
type ProbeStatusUpdate struct {
	Status string `json:"status"`
}

// Client handles communication with the RHOBS Probes API
type Client struct {
	BaseURL     string
	HTTPClient  *http.Client
	Tenant      string
	APIEndpoint string
	JWTToken    string
}

// NewClient creates a new API client
func NewClient(baseURL, tenant, apiEndpoint, jwtToken string) *Client {
	return &Client{
		BaseURL:     baseURL,
		Tenant:      tenant,
		APIEndpoint: apiEndpoint,
		JWTToken:    jwtToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetProbes retrieves probes from the API with optional label selectors
func (c *Client) GetProbes(labelSelector string) ([]Probe, error) {
	var endpoint string
	if c.APIEndpoint == "/probes" {
		// Direct API access without tenant
		endpoint = "/probes"
	} else {
		// Observatorium-style API with tenant
		endpoint = fmt.Sprintf("%s/%s/probes", c.APIEndpoint, c.Tenant)
	}
	
	reqURL, err := url.Parse(c.BaseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if labelSelector != "" {
		q := reqURL.Query()
		q.Set("label_selector", labelSelector)
		reqURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequest("GET", reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	
	// Add JWT token if configured
	if c.JWTToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.JWTToken))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var probeResponse ProbeListResponse
	if err := json.NewDecoder(resp.Body).Decode(&probeResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return probeResponse.Probes, nil
}

// UpdateProbeStatus updates the status of a probe
func (c *Client) UpdateProbeStatus(probeID, status string) error {
	var endpoint string
	if c.APIEndpoint == "/probes" {
		// Direct API access without tenant
		endpoint = fmt.Sprintf("/probes/%s", probeID)
	} else {
		// Observatorium-style API with tenant
		endpoint = fmt.Sprintf("%s/%s/probes/%s", c.APIEndpoint, c.Tenant, probeID)
	}
	
	reqURL := c.BaseURL + endpoint

	statusUpdate := ProbeStatusUpdate{Status: status}
	payload, err := json.Marshal(statusUpdate)
	if err != nil {
		return fmt.Errorf("failed to marshal status update: %w", err)
	}

	req, err := http.NewRequest("PATCH", reqURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	
	// Add JWT token if configured
	if c.JWTToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.JWTToken))
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
