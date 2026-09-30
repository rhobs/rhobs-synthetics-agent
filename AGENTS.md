# rhobs-synthetics-agent AGENTS.md

Provides guidance to coding agents when working with code in this repository.

## What this is

A Go agent that polls one or more RHOBS Synthetics Probes APIs and reconciles the results into Kubernetes: it creates `Probe` CRs, and it also manages the blackbox-exporter Deployment/Service and the Prometheus CR that scrapes them and remote-writes to Thanos. The API is the source of truth. The agent reports probe status back to the API with PATCH calls.

## Commands

```bash
make build                 # builds ./rhobs-synthetics-agent from ./cmd/agent
make build-mock-api        # builds ./mock-api from ./cmd/mock-api
make lint                  # golangci-lint v2 (auto-installed to $GOPATH/bin); lint-fix to autofix
make test                  # go test -cover ./cmd/... ./internal/...
make test-race
make test-integration      # TestWorker_FullIntegration in ./internal/agent
make test-e2e              # only TestAgent_E2E_WithAPI in ./test/e2e (in-process mock API); `go test ./test/e2e` runs the other mock-API tests too
make test-e2e-real         # TestAgent_E2E_*RealAPI; needs RHOBS_SYNTHETICS_API_PATH (path to a rhobs-synthetics-api checkout with a built binary)
make test-e2e-all          # everything in ./test/e2e; also needs RHOBS_SYNTHETICS_API_PATH
make coverage              # hack/codecov.sh -> coverage.out

go test -v ./internal/agent -run TestWorker_fetchProbeList   # single test
go test ./templates/...    # template.yaml structure tests (NOT included in `make test`)
```

To run locally, point the agent at a cluster with `--kubeconfig`. Otherwise pass `--leader-elect=false`: leader election is on by default (flag and viper default), and it needs a kubeconfig or in-cluster config plus Lease RBAC, or startup fails. `make run-local` passes neither this flag nor any API URLs, so outside a cluster it fails at leader-election startup. With leader election off but no `api_urls`/`API_URLS`, the worker runs in standalone mode and skips probe processing. To exercise the no-cluster fallback you need both, plus `blackbox.probing.prober_url: ""` in the config YAML: the pre-flight check defaults to the in-cluster Service `synthetics-blackbox-prober-default-service:9115` and is skipped only when that key is empty, so otherwise every probe stays pending until the 10 min pre-flight timeout. The worker then logs the Probe CR it "would create" and attempts to PATCH the probe `active` (API update failures are only logged).

## Architecture

- `cmd/agent/main.go`: cobra `start` command. Flags are bound to viper keys. `agent.LoadConfig()` merges defaults, env (`viper.AutomaticEnv`, so env names are the uppercased config keys, e.g. `API_URLS`, `LEADER_ELECT`) and the `--config` YAML. `api_urls` accepts a comma-separated string. There is no env key replacer, so nested keys (`prometheus.*`, `blackbox.*`) can only come from flags or the YAML. Some keys have no flag at all (`label_selector`, `jwt_token`, `log_format`, `metrics_addr`, `blackbox.*`). The `NAMESPACE` env var (set from the pod in the template) overrides `--namespace` for both the worker and the Lease.
- `internal/agent/agent.go`: an `oklog/run` group with three actors: the signal handler (graceful drain via `taskWG` up to `GracefulTimeout`), the worker (wrapped in leader election when `LeaderElect`), and the HTTP server (`metrics_addr`, default `:8080`) serving `/metrics`, `/livez`, `/readyz`.
- `internal/agent/worker.go`: the reconcile loop. At startup and on each tick it runs `processProbers` (blackbox exporter shard `"default"`), then `processPrometheus` (OIDC secret, Prometheus CR, delete+recreate on config drift), then `processProbes`. `processProbes` has three status phases (pending, active, terminating), each fetching from every API client with `label_selector + rhobs-synthetics/status=<x>`, followed by orphan cleanup, which lists managed CRs first and fetches from the API only if any exist:
  - `pending`: runs a pre-flight connectivity check through the blackbox exporter, then creates the CR and PATCHes `active`. A probe that keeps failing pre-flight stays pending for up to 10 min (`defaultPreflightTimeout`), and then the CR is created anyway so real outages alert.
  - `active`: re-applies the CRs so code changes to the CR shape roll out. The update is skipped when the spec is unchanged, to avoid Prometheus reloads.
  - `terminating`: deletes the CR, then PATCHes the probe to `deleted` via `apiClients[0]` only (the client's `DeleteProbe` is a status PATCH, not an HTTP DELETE).
  - orphan cleanup: deletes managed CRs whose `probe-<cluster-id>` / `probe-<id>` name is absent from the API.
  Fetched probes are **deduplicated by `StaticURL`**, not by ID. A fetch fails only when every API endpoint fails.
- `internal/k8s/probe.go` (`ProbeManager`): builds `Probe` CRs through the dynamic client. The API group is auto-detected, preferring `monitoring.rhobs` over `monitoring.coreos.com`, unless `prometheus.api_group` is set. Managed CRs carry the label `rhobs.monitoring/managed-by=rhobs-synthetics-agent`.
- `internal/k8s/prober.go` (`BlackBoxProberManager`): implements both the `ProberManager` and `PrometheusManager` interfaces. The worker depends on these interfaces, so tests substitute fakes. The prober manager exists only when there is a kubeconfig or the agent runs in-cluster.
- `internal/api`: HTTP client for the Probes API. It authenticates with a static JWT (`jwt_token`) or with OIDC client-credentials (`internal/auth`, token cached and refreshed). The same OIDC credentials also go into a Secret for Prometheus remote-write OAuth2.
- `internal/metrics`: Prometheus metrics for the agent itself, including leader status and reconcile/fetch/probe-operation counters.
- `templates/template.yaml`: the OpenShift template used for deployment, with 2 replicas, Lease RBAC and `maxSurge:0/maxUnavailable:1`. `templates/template_test.go` asserts its structure, so update both together.

### Leader election / HA invariants (see `internal/agent/ha_test.go`)

These were tuned carefully. Keep them when changing `agent.go` or `worker.go`:
- `/readyz` means **process health, not leadership or last-reconcile success**. With leader election on, the worker's readiness callback is disabled and both standby and leader report ready. Otherwise rollouts stall with `maxSurge:0`.
- Lease RBAC is checked up front, and a failure is fatal. Never fall back to running the worker without the lease, because that causes split-brain on the Probe CRs.
- `ReleaseOnCancel: false`: the Lease is not released until the leader's worker goroutine has actually returned (`workerDone`).
- After 3 consecutive cycles in which the pending, active and terminating phases all return errors (`consecutiveFetchFailuresToYield`; orphan-cleanup errors don't count), the leader's worker returns an error. The pod then goes unready and waits `leaseDuration + retryPeriod` before exiting, so a healthy standby takes the lease first. Context cancellation must not be treated as such a failure.
- K8s writes take the leader context (`*WithContext` methods), so a demoted leader stops writing.

## Notes

- `go.mod` depends on `github.com/rhobs/rhobs-synthetics-api` (for `kubeclient` and the real-API e2e tests). There is a commented `replace` directive for local development against a sibling checkout.
- The container build uses the `Dockerfile` with `entrypoint.sh`, which runs `rhobs-synthetics-agent start "$@"`. CI runs on Konflux (`.tekton/`) and ci-operator. CodeRabbit reviews PRs and is told to focus on major issues, not nitpicks.
