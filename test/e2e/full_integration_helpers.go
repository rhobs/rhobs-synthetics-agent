package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	awsvpceapi "github.com/openshift/aws-vpce-operator/api/v1alpha2"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// testWriter forwards log output to t.Log and captures logs for validation
type testWriter struct {
	t        *testing.T
	logs     []string
	logMutex sync.Mutex
}

func (tw *testWriter) Write(p []byte) (n int, err error) {
	logLine := string(p)
	tw.t.Log(logLine)

	tw.logMutex.Lock()
	tw.logs = append(tw.logs, logLine)
	tw.logMutex.Unlock()

	return len(p), nil
}

func (tw *testWriter) ContainsLog(substring string) bool {
	tw.logMutex.Lock()
	defer tw.logMutex.Unlock()

	for _, log := range tw.logs {
		if strings.Contains(log, substring) {
			return true
		}
	}
	return false
}

func (tw *testWriter) GetLogs() []string {
	tw.logMutex.Lock()
	defer tw.logMutex.Unlock()

	logs := make([]string, len(tw.logs))
	copy(logs, tw.logs)
	return logs
}

// createProbeViaAPI creates a probe by calling the API directly (simulates RMO behavior)
func createProbeViaAPI(baseURL, clusterID, probeURL string, isPrivate bool) (string, error) {
	createReq := map[string]interface{}{
		"static_url": probeURL,
		"labels": map[string]string{
			"cluster_id":    clusterID,
			"private":       fmt.Sprintf("%t", isPrivate),
			"source":        "route-monitor-operator",
			"resource_type": "hostedcontrolplane",
			"probe_type":    "blackbox",
		},
	}

	reqBody, err := json.Marshal(createReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", baseURL+"/probes", bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var probe api.Probe
	if err := json.NewDecoder(resp.Body).Decode(&probe); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return probe.ID, nil
}

// getProbeByID fetches a single probe by ID from the API
func getProbeByID(baseURL, probeID string) (*api.Probe, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/probes/" + probeID)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("probe not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var probe api.Probe
	if err := json.NewDecoder(resp.Body).Decode(&probe); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &probe, nil
}

// listProbes fetches all probes matching the label selector from the API
func listProbes(baseURL, labelSelector string) ([]api.Probe, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := baseURL + "/probes"
	if labelSelector != "" {
		url += "?label_selector=" + labelSelector
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var response api.ProbeListResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Probes, nil
}

// deleteProbeViaAPI deletes a probe by calling the API directly
func deleteProbeViaAPI(baseURL, probeID string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("DELETE", baseURL+"/probes/"+probeID, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// setupRMODependencies creates the Kubernetes resources that RMO expects to exist
func setupRMODependencies(t *testing.T, k8sClient client.Client, ctx context.Context, dynatraceURL string) {
	// Create kube-apiserver service
	apiServerService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-apiserver",
			Namespace: "clusters",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 6443}},
		},
	}
	if err := k8sClient.Create(ctx, apiServerService); err != nil {
		t.Fatalf("Failed to create kube-apiserver service: %v", err)
	}

	// Create VpcEndpoint resource
	vpcEndpoint := &awsvpceapi.VpcEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "private-hcp",
			Namespace: "clusters",
			Labels: map[string]string{
				"hypershift.openshift.io/cluster": "hcp",
			},
		},
		Status: awsvpceapi.VpcEndpointStatus{
			Status: "available",
		},
	}
	if err := k8sClient.Create(ctx, vpcEndpoint); err != nil {
		t.Fatalf("Failed to create VpcEndpoint: %v", err)
	}

	// Create Dynatrace secret
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-route-monitor-operator",
		},
	}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	dynatraceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dynatrace-token",
			Namespace: "openshift-route-monitor-operator",
		},
		Data: map[string][]byte{
			"apiToken": []byte("mock-token"),
			"apiUrl":   []byte(dynatraceURL),
		},
	}
	if err := k8sClient.Create(ctx, dynatraceSecret); err != nil {
		t.Fatalf("Failed to create Dynatrace secret: %v", err)
	}
}

// startRMOAPIProxy starts a reverse proxy that translates RMO's API path format to the actual API format
func startRMOAPIProxy(targetAPIURL string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalPath := r.URL.Path
		translatedPath := originalPath

		if strings.HasPrefix(originalPath, "/api/metrics/v1/") {
			parts := strings.Split(strings.TrimPrefix(originalPath, "/api/metrics/v1/"), "/")
			if len(parts) > 1 {
				translatedPath = "/" + strings.Join(parts[1:], "/")
			}
		}

		proxyURL := targetAPIURL + translatedPath
		if r.URL.RawQuery != "" {
			proxyURL += "?" + r.URL.RawQuery
		}

		var bodyBytes []byte
		if r.Body != nil {
			bodyBytes, _ = io.ReadAll(r.Body)
			_ = r.Body.Close()
		}

		proxyReq, err := http.NewRequest(r.Method, proxyURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create proxy request: %v", err), http.StatusInternalServerError)
			return
		}

		for key, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to execute proxy request: %v", err), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
}

// startMockProbeTargetServer starts a mock server that simulates a healthy cluster API endpoint
func startMockProbeTargetServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/livez" || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// startMockDynatraceServer starts a mock Dynatrace API server for testing
func startMockDynatraceServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/synthetic/monitors/":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"monitors":[]}`))

		case r.Method == "POST" && r.URL.Path == "/v1/synthetic/monitors":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"entityId":"SYNTHETIC_TEST-1234567890","name":"mock-monitor"}`))

		case r.Method == "DELETE":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == "GET" && r.URL.Path == "/v1/synthetic/locations":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"locations":[
				{"name":"N. Virginia","entityId":"SYNTHETIC_LOCATION-PUBLIC-123","type":"PUBLIC","status":"ENABLED"},
				{"name":"backplanei03xyz","entityId":"SYNTHETIC_LOCATION-PRIVATE-456","type":"PRIVATE","status":"ENABLED"},
				{"name":"Oregon","entityId":"SYNTHETIC_LOCATION-PUBLIC-789","type":"PUBLIC","status":"ENABLED"}
			]}`))

		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	}))
}
