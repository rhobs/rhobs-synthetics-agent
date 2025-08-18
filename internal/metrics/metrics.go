package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// API probe list retrieval metrics
	probeListFetchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "rhobs_synthetics_agent_probe_list_fetch_duration_seconds",
			Help: "Duration of fetching probe list from the Synthetic Monitor API",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"api_endpoint", "status"},
	)

	probeListFetchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rhobs_synthetics_agent_probe_list_fetch_total",
			Help: "Total number of probe list fetch attempts from the Synthetic Monitor API",
		},
		[]string{"api_endpoint", "status"},
	)

	// Probe Custom Resources management metrics
	probeResourcesManaged = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rhobs_synthetics_agent_probe_resources_managed",
			Help: "Number of probe Custom Resources currently managed by the Agent",
		},
		[]string{"namespace", "state"},
	)

	probeResourceOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rhobs_synthetics_agent_probe_resource_operations_total",
			Help: "Total number of probe Custom Resource operations",
		},
		[]string{"operation", "status"},
	)

	// Reconciliation loop metrics
	reconciliationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "rhobs_synthetics_agent_reconciliation_duration_seconds",
			Help: "Duration of the reconciliation loop that synchronizes API probe list with Probe CRs",
			Buckets: prometheus.DefBuckets,
		},
	)

	reconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rhobs_synthetics_agent_reconciliation_total",
			Help: "Total number of reconciliation cycles",
		},
		[]string{"status"},
	)

	lastReconciliationTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rhobs_synthetics_agent_last_reconciliation_timestamp_seconds",
			Help: "Timestamp of the last reconciliation cycle",
		},
	)

	// General agent metrics
	agentInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rhobs_synthetics_agent_info",
			Help: "Information about the RHOBS Synthetics Agent",
		},
		[]string{"version", "namespace"},
	)
)

func init() {
	prometheus.MustRegister(
		probeListFetchDuration,
		probeListFetchTotal,
		probeResourcesManaged,
		probeResourceOperations,
		reconciliationDuration,
		reconciliationTotal,
		lastReconciliationTime,
		agentInfo,
	)
}

// RecordProbeListFetch records metrics for probe list fetch operations
func RecordProbeListFetch(apiEndpoint string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	
	probeListFetchDuration.WithLabelValues(apiEndpoint, status).Observe(duration.Seconds())
	probeListFetchTotal.WithLabelValues(apiEndpoint, status).Inc()
}

// SetProbeResourcesManaged sets the number of managed probe resources
func SetProbeResourcesManaged(namespace, state string, count float64) {
	probeResourcesManaged.WithLabelValues(namespace, state).Set(count)
}

// RecordProbeResourceOperation records probe resource operations
func RecordProbeResourceOperation(operation string, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	
	probeResourceOperations.WithLabelValues(operation, status).Inc()
}

// RecordReconciliation records reconciliation cycle metrics
func RecordReconciliation(duration time.Duration, success bool) {
	reconciliationDuration.Observe(duration.Seconds())
	
	status := "success"
	if !success {
		status = "error"
	}
	reconciliationTotal.WithLabelValues(status).Inc()
	
	lastReconciliationTime.SetToCurrentTime()
}

// SetAgentInfo sets agent information metrics
func SetAgentInfo(version, namespace string) {
	agentInfo.WithLabelValues(version, namespace).Set(1)
}

// Handler returns the Prometheus metrics HTTP handler
func Handler() http.Handler {
	return promhttp.Handler()
}