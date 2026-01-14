package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
)

const (
	defaultHTTPTimeout = 30 * time.Second
	tokenExpiryBuffer  = 30 * time.Second
)

// OIDCConfig holds OIDC authentication configuration
type OIDCConfig struct {
	ClientID     string
	ClientSecret string
	IssuerURL    string
}

// IsConfigured returns true if OIDC credentials are configured
func (c *OIDCConfig) IsConfigured() bool {
	return c != nil && c.ClientID != "" && c.ClientSecret != "" && c.IssuerURL != ""
}

// tokenResponse represents an OIDC token response
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// TokenProvider manages OIDC token acquisition and caching
type TokenProvider struct {
	config     *OIDCConfig
	httpClient *http.Client

	// Token management
	tokenMutex  sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// NewTokenProvider creates a new OIDC token provider
func NewTokenProvider(config *OIDCConfig) *TokenProvider {
	if config == nil || !config.IsConfigured() {
		return nil
	}

	return &TokenProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
	}
}

// GetAccessToken retrieves a valid access token, refreshing if necessary
func (p *TokenProvider) GetAccessToken(ctx context.Context) (string, error) {
	if p == nil || p.config == nil {
		return "", nil // No OIDC config, no token needed
	}

	p.tokenMutex.RLock()
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry.Add(-tokenExpiryBuffer)) {
		token := p.accessToken
		p.tokenMutex.RUnlock()
		return token, nil
	}
	p.tokenMutex.RUnlock()

	// Need to refresh token
	return p.refreshAccessToken(ctx)
}

// refreshAccessToken obtains a new access token using client credentials flow
func (p *TokenProvider) refreshAccessToken(ctx context.Context) (string, error) {
	p.tokenMutex.Lock()
	defer p.tokenMutex.Unlock()

	// Double-check that we still need to refresh
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry.Add(-tokenExpiryBuffer)) {
		return p.accessToken, nil
	}

	// Handle both direct token endpoint URLs and issuer URLs that need /token appended
	tokenURL := p.config.IssuerURL
	if !strings.HasSuffix(tokenURL, "/token") {
		tokenURL = strings.TrimSuffix(tokenURL, "/") + "/token"
	}

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)
	data.Set("scope", "profile")

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	logger.Debugf("Requesting OIDC access token from %s", tokenURL)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request access token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	p.accessToken = tokenResp.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	logger.Debugf("Successfully obtained OIDC access token, expires in %d seconds", tokenResp.ExpiresIn)

	return p.accessToken, nil
}
