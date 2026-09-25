package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

type haRoundTripper func(*http.Request) (*http.Response, error)

func (f haRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func haResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func haWorker(t *testing.T, transport haRoundTripper) *Worker {
	t.Helper()
	w, err := NewWorker(&Config{
		APIURLs:         []string{"http://synthetics-api.rhobs-int.svc:8080/probes"},
		PollingInterval: 20 * time.Millisecond,
		LeaderElect:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.apiClients[0].HTTPClient.Transport = transport
	return w
}

func TestWorkerYieldsAfterConsecutiveAPIFetchFailures(t *testing.T) {
	var fetches atomic.Int32
	w := haWorker(t, func(req *http.Request) (*http.Response, error) {
		fetches.Add(1)
		return nil, &net.DNSError{Err: "i/o timeout", Name: req.URL.Hostname(), IsTimeout: true}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := w.Start(ctx, &sync.WaitGroup{}, make(chan struct{}))
	if !errors.Is(err, errAllProbeListsFailed) {
		t.Fatalf("worker should yield after persistent DNS failure, got %v", err)
	}
	if got := fetches.Load(); got != 3*consecutiveFetchFailuresToYield {
		t.Fatalf("got %d fetches, want %d over three fully failed cycles", got, 3*consecutiveFetchFailuresToYield)
	}
}

func TestWorkerResetsFetchFailureCountAfterSuccess(t *testing.T) {
	var fetches atomic.Int32
	w := haWorker(t, func(req *http.Request) (*http.Response, error) {
		attempt := fetches.Add(1)
		if attempt <= 6 || attempt > 9 {
			return nil, &net.DNSError{Err: "i/o timeout", Name: req.URL.Hostname(), IsTimeout: true}
		}
		return haResponse(req, `{"probes":[]}`), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := w.Start(ctx, &sync.WaitGroup{}, make(chan struct{}))
	if !errors.Is(err, errAllProbeListsFailed) {
		t.Fatalf("worker should eventually yield after a new run of failures, got %v", err)
	}
	// Two failed cycles, one successful cycle, then three more failed cycles.
	if got := fetches.Load(); got != 18 {
		t.Fatalf("fetch failures were not reset by a successful cycle: got %d attempts", got)
	}
}

func TestLeaderCallbacksStopElectionWhenAPIStaysUnavailable(t *testing.T) {
	a, err := New(&Config{PollingInterval: 20 * time.Millisecond, LeaderElect: true})
	if err != nil {
		t.Fatal(err)
	}
	a.worker.SetReadinessCallback(func(bool) {})
	a.worker.apiClients = []*api.Client{api.NewClient("http://synthetics-api.rhobs-int.svc:8080/probes", "")}
	a.worker.apiClients[0].HTTPClient.Transport = haRoundTripper(func(req *http.Request) (*http.Response, error) {
		return nil, &net.DNSError{Err: "i/o timeout", Name: req.URL.Hostname(), IsTimeout: true}
	})
	stopped := make(chan struct{})
	workerDone := make(chan error, 1)
	callbacks := a.leaderCallbacks("broken-pod", func() { close(stopped) }, workerDone)
	go callbacks.OnStartedLeading(context.Background())
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("leader did not leave election after persistent API failure")
	}
	if err := <-workerDone; !errors.Is(err, errAllProbeListsFailed) {
		t.Fatalf("worker exited for the wrong reason: %v", err)
	}
}

func TestLeaderElectionDrainsMutationBeforeReturning(t *testing.T) {
	a, err := New(&Config{PollingInterval: time.Second, LeaderElect: true})
	if err != nil {
		t.Fatal(err)
	}
	a.worker.SetReadinessCallback(func(bool) {})
	patchStarted := make(chan struct{}, 1)
	patchFinished := make(chan struct{}, 1)
	client := api.NewClient("http://synthetics-api.rhobs-int.svc:8080/probes", "")
	client.HTTPClient.Transport = haRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch {
			patchStarted <- struct{}{}
			<-req.Context().Done()
			patchFinished <- struct{}{}
			return nil, req.Context().Err()
		}
		if strings.Contains(req.URL.Query().Get("label_selector"), "terminating") {
			return haResponse(req, `{"probes":[{"id":"review-probe","static_url":"https://example.test"}]}`), nil
		}
		return haResponse(req, `{"probes":[]}`), nil
	})
	a.worker.apiClients = []*api.Client{client}
	kube := kubefake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.runLeaderElection(ctx, kube, "old-pod", "default", nil) }()
	select {
	case <-patchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("leader never began the API mutation")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not drain its worker")
	}
	select {
	case <-patchFinished:
	default:
		t.Fatal("leader election returned before its in-flight API mutation stopped")
	}
	lease, err := kube.CoordinationV1().Leases("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != "old-pod" {
		t.Fatal("the Lease was released while the old worker could still be running")
	}
}

func TestLeaderElectionDrainsMutationAfterLeaseRenewalFails(t *testing.T) {
	a, err := New(&Config{PollingInterval: time.Second, LeaderElect: true})
	if err != nil {
		t.Fatal(err)
	}
	a.worker.SetReadinessCallback(func(bool) {})
	patchStarted := make(chan struct{}, 1)
	patchFinished := make(chan struct{}, 1)
	client := api.NewClient("http://synthetics-api.rhobs-int.svc:8080/probes", "")
	client.HTTPClient.Transport = haRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch {
			patchStarted <- struct{}{}
			<-req.Context().Done()
			patchFinished <- struct{}{}
			return nil, req.Context().Err()
		}
		if strings.Contains(req.URL.Query().Get("label_selector"), "terminating") {
			return haResponse(req, `{"probes":[{"id":"review-probe","static_url":"https://example.test"}]}`), nil
		}
		return haResponse(req, `{"probes":[]}`), nil
	})
	a.worker.apiClients = []*api.Client{client}
	kube := kubefake.NewSimpleClientset()
	var failRenew atomic.Bool
	kube.PrependReactor("update", "leases", func(action clienttesting.Action) (bool, runtime.Object, error) {
		lease := action.(clienttesting.UpdateAction).GetObject().(*coordinationv1.Lease)
		if failRenew.Load() && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == "old-pod" {
			return true, nil, errors.New("simulated failed Lease renewal")
		}
		return false, nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.runLeaderElection(ctx, kube, "old-pod", "default", nil) }()
	select {
	case <-patchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("leader never began the API mutation")
	}
	failRenew.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(renewDeadline + retryPeriod + 5*time.Second):
		t.Fatal("leader did not stop after Lease renewal failed")
	}
	select {
	case <-patchFinished:
	default:
		t.Fatal("old worker still had an API mutation in flight after losing the Lease")
	}
	lease, err := kube.CoordinationV1().Leases("default").Get(context.Background(), leaseName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != "old-pod" {
		t.Fatal("the Lease was explicitly released before the old worker stopped")
	}
}
