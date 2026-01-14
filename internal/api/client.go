package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/auth"
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
	BaseURL       string
	HTTPClient    *http.Client
	JWTToken      string
	TokenProvider *auth.TokenProvider
}

// NewClient creates a new API client with a full URL and optional static JWT token
func NewClient(apiURL, jwtToken string) *Client {
	return &Client{
		BaseURL:  apiURL,
		JWTToken: jwtToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithOIDC creates a new API client with OIDC token provider
func NewClientWithOIDC(apiURL string, tokenProvider *auth.TokenProvider) *Client {
	return &Client{
		BaseURL:       apiURL,
		TokenProvider: tokenProvider,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// addAuthHeaders adds authentication to the request
// Priority: OIDC token provider > static JWT token
func (c *Client) addAuthHeaders(ctx context.Context, req *http.Request) error {
	// Try OIDC token provider first
	if c.TokenProvider != nil {
		token, err := c.TokenProvider.GetAccessToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get OIDC access token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
			return nil
		}
	}

	// Fall back to static JWT token
	if c.JWTToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.JWTToken))
	}

	return nil
}

// GetProbes retrieves probes from the API with optional label selectors
func (c *Client) GetProbes(labelSelector string) ([]Probe, error) {
	return c.GetProbesWithContext(context.Background(), labelSelector)
}

// GetProbesWithContext retrieves probes from the API with context support
func (c *Client) GetProbesWithContext(ctx context.Context, labelSelector string) ([]Probe, error) {
	reqURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if labelSelector != "" {
		q := reqURL.Query()
		q.Set("label_selector", labelSelector)
		reqURL.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers
	if err := c.addAuthHeaders(ctx, req); err != nil {
		return nil, fmt.Errorf("failed to add auth headers: %w", err)
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
	return c.UpdateProbeStatusWithContext(context.Background(), probeID, status)
}

// UpdateProbeStatusWithContext updates the status of a probe with context support
func (c *Client) UpdateProbeStatusWithContext(ctx context.Context, probeID, status string) error {
	reqURL := fmt.Sprintf("%s/%s", c.BaseURL, probeID)

	statusUpdate := ProbeStatusUpdate{Status: status}
	payload, err := json.Marshal(statusUpdate)
	if err != nil {
		return fmt.Errorf("failed to marshal status update: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers
	if err := c.addAuthHeaders(ctx, req); err != nil {
		return fmt.Errorf("failed to add auth headers: %w", err)
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

// DeleteProbe marks a probe for deletion by setting status to "deleted"
func (c *Client) DeleteProbe(probeID string) error {
	return c.DeleteProbeWithContext(context.Background(), probeID)
}

// DeleteProbeWithContext marks a probe for deletion with context support
func (c *Client) DeleteProbeWithContext(ctx context.Context, probeID string) error {
	reqURL := fmt.Sprintf("%s/%s", c.BaseURL, probeID)

	// Create request body with status "deleted"
	requestBody := map[string]string{
		"status": "deleted",
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", reqURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers
	if err := c.addAuthHeaders(ctx, req); err != nil {
		return fmt.Errorf("failed to add auth headers: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
