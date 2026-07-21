#!/usr/bin/env bash
# Integration regression tests for the spec-comparison fix (ROSAENG-59408).
#
# Creates a SEPARATE synthetics-agent-test Deployment (not the GitOps-managed one)
# so qontract-reconcile cannot revert our test image mid-run.
#
# Tests all 6 scenarios:
#   1. Label change
#   2. URL change
#   3. Interval change (private probe)
#   4. Module drift (same probe ID, different config)
#   5. MetricRelabelConfig removal (agent code upgrade simulation)
#   6. K8s normalisation — no false diffs after correction
#
# Prerequisites:
#   ocm backplane login <cluster>
#   Pull secret named $PULL_SECRET must exist in $NAMESPACE with quay.io creds
#
# Usage:
#   NAMESPACE=rhobs-synthetics-int \
#   TEST_IMAGE=quay.io/anispate/rhobs-synthetics-agent:ROSAENG-59408-v4 \
#   ./hack/test-spec-comparison-integration.sh

set -euo pipefail

NAMESPACE="${NAMESPACE:-rhobs-synthetics-int}"
TEST_IMAGE="${TEST_IMAGE:-quay.io/anispate/rhobs-synthetics-agent:ROSAENG-59408-v4}"
PULL_SECRET="${PULL_SECRET:-rosaeng-59408-pull-secret}"
PROBE_NAME="probe-test-59408"
AGENT_DEPLOYMENT="synthetics-agent-test"
ELEVATE_REASON="ROSAENG-59408 regression tests"
RECONCILE_INTERVAL=30

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'

PASS=0; FAIL=0; ERRORS=()

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

e() { ocm backplane elevate "$ELEVATE_REASON" -- "$@" 2>/dev/null; }

get_metric() {
  local decision="$1"
  e exec -n "$NAMESPACE" deployment/"$AGENT_DEPLOYMENT" -- \
    sh -c "curl -s --max-time 10 http://localhost:8080/metrics \
           | grep 'probe_update_decisions_total{decision=\"${decision}\"}' \
           | awk '{print \$2}'" 2>/dev/null || echo "0"
}

get_probe_field() {
  e get probes.monitoring.rhobs "$PROBE_NAME" -n "$NAMESPACE" \
    -o jsonpath="{$1}" 2>/dev/null
}

wait_for_metric() {
  local decision="$1" threshold="$2" timeout="${3:-90}" elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    [ "$(get_metric "$decision")" -ge "$threshold" ] 2>/dev/null && return 0
    sleep 5; elapsed=$((elapsed + 5))
  done
  return 1
}

report() {
  local name="$1" result="$2" detail="${3:-}"
  if [ "$result" = "pass" ]; then
    echo -e "    ${GREEN}✓${NC} $name"; PASS=$((PASS + 1))
  else
    echo -e "    ${RED}✗${NC} $name${detail:+$'\n'      detail: $detail}"
    FAIL=$((FAIL + 1)); ERRORS+=("$name${detail:+ — $detail}")
  fi
}

# Run one mutation test.
# Args: test_name  expected_field  expected_value  <oc patch args...>
#
# Uses the applied counter as the primary signal (not resourceVersion)
# so we wait for the agent's response, not our own patch.
run_mutation_test() {
  local test_name="$1" expected_field="$2" expected_value="$3"
  shift 3; local patch_args=("$@")

  echo -e "\n  ${YELLOW}→${NC} $test_name"

  local applied_before skipped_before
  applied_before=$(get_metric "applied")
  skipped_before=$(get_metric "skipped")

  # Mutate the Probe CR
  e patch probes.monitoring.rhobs "$PROBE_NAME" -n "$NAMESPACE" \
    "${patch_args[@]}" >/dev/null

  # Check 1: agent detects diff and writes a correction (applied counter increments)
  local wait_time=$(( RECONCILE_INTERVAL * 2 + 15 ))
  if wait_for_metric "applied" $((applied_before + 1)) $wait_time; then
    report "agent detects drift and writes correction" "pass"
  else
    report "agent detects drift and writes correction" "fail" \
      "applied stuck at $(get_metric applied) after ${wait_time}s"
    return
  fi

  sleep 3  # let the write propagate

  # Check 2: spec field is restored to the expected value
  if [ -n "$expected_field" ]; then
    local actual
    actual=$(get_probe_field "$expected_field")
    if [ "$actual" = "$expected_value" ]; then
      report "spec restored to desired value (${expected_field}=${expected_value})" "pass"
    else
      report "spec restored to desired value" "fail" \
        "expected '$expected_value', got '$actual'"
    fi
  fi

  # Check 3: returns to skipping (no spurious follow-up updates)
  if wait_for_metric "skipped" $((skipped_before + 1)) $wait_time; then
    local applied_final; applied_final=$(get_metric "applied")
    if [ "$applied_final" -eq $((applied_before + 1)) ]; then
      report "returns to skipping after correction" "pass"
    else
      report "returns to skipping after correction" "fail" \
        "applied went from $applied_before to $applied_final (expected $((applied_before + 1)))"
    fi
  else
    report "returns to skipping after correction" "fail" \
      "skipped counter didn't increment within ${wait_time}s"
  fi
}

# ---------------------------------------------------------------------------
# Setup — creates a fresh test deployment (not the GitOps-managed one)
# ---------------------------------------------------------------------------

setup() {
  echo -e "\n${BOLD}Setup${NC}"

  echo "  Deploying mock API..."
  e apply -n "$NAMESPACE" -f - >/dev/null <<'MOCK_EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: mock-api-script
data:
  server.py: |
    from http.server import HTTPServer, BaseHTTPRequestHandler
    from urllib.parse import urlparse, parse_qs, unquote
    import json

    PROBE = {
      "id": "test-59408",
      "static_url": "https://console.test.example.com",
      "status": "active",
      "labels": {
        "private": "true",
        "region": "us-west-2",
        "cluster_id": "test-cluster-123"
      }
    }

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args): pass
        def do_GET(self):
            sel = unquote(parse_qs(urlparse(self.path).query).get("label_selector", [""])[0])
            probes = [] if ("status=pending" in sel or "status=terminating" in sel) else [PROBE]
            body = json.dumps({"probes": probes}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body)
        def do_PATCH(self):
            self.rfile.read(int(self.headers.get("Content-Length", 0)))
            self.send_response(200); self.end_headers(); self.wfile.write(b"{}")
        def do_DELETE(self):
            self.send_response(200); self.end_headers(); self.wfile.write(b"{}")

    HTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
MOCK_EOF

  e apply -n "$NAMESPACE" -f - >/dev/null <<'PODEOF'
apiVersion: v1
kind: Pod
metadata:
  name: mock-synthetics-api
  labels:
    app: mock-synthetics-api
spec:
  containers:
  - name: mock
    image: registry.access.redhat.com/ubi9/python-311:latest
    command: ["python3", "/scripts/server.py"]
    ports:
    - containerPort: 8081
    volumeMounts:
    - name: script
      mountPath: /scripts
  volumes:
  - name: script
    configMap:
      name: mock-api-script
---
apiVersion: v1
kind: Service
metadata:
  name: mock-synthetics-api
spec:
  selector:
    app: mock-synthetics-api
  ports:
  - port: 8081
    targetPort: 8081
PODEOF

  echo "  Deploying test agent (separate from GitOps-managed deployment)..."
  # Use the existing synthetics-agent ServiceAccount (already has Probe CR RBAC).
  # All config via env vars; viper.AutomaticEnv() maps UPPER_SNAKE to nested keys.
  e apply -n "$NAMESPACE" -f - >/dev/null <<AGENTEOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $AGENT_DEPLOYMENT
  namespace: $NAMESPACE
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $AGENT_DEPLOYMENT
  template:
    metadata:
      labels:
        app: $AGENT_DEPLOYMENT
    spec:
      serviceAccountName: synthetics-agent
      imagePullSecrets:
      - name: $PULL_SECRET
      containers:
      - name: synthetics-agent
        image: $TEST_IMAGE
        imagePullPolicy: Always
        env:
        - name: NAMESPACE
          value: $NAMESPACE
        - name: API_URLS
          value: http://mock-synthetics-api:8081/api/metrics/v1/hcp/probes
        - name: LABEL_SELECTOR
          value: "private=true"
        - name: POLLING_INTERVAL
          value: "30s"
        ports:
        - containerPort: 8080
AGENTEOF

  echo "  Waiting for mock API pod to be ready..."
  local deadline=60 elapsed=0
  while [ "$elapsed" -lt "$deadline" ]; do
    e get pod mock-synthetics-api -n "$NAMESPACE" \
      -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null \
      | grep -q "true" && break
    sleep 5; elapsed=$((elapsed + 5))
  done

  echo "  Waiting for test agent pod to be ready..."
  deadline=120 elapsed=0
  while [ "$elapsed" -lt "$deadline" ]; do
    local img ready
    img=$(e get pods -n "$NAMESPACE" -l "app=$AGENT_DEPLOYMENT" \
      -o jsonpath='{.items[0].spec.containers[0].image}' 2>/dev/null)
    ready=$(e get pods -n "$NAMESPACE" -l "app=$AGENT_DEPLOYMENT" \
      -o jsonpath='{.items[0].status.containerStatuses[0].ready}' 2>/dev/null)
    [ "$img" = "$TEST_IMAGE" ] && [ "$ready" = "true" ] && break
    sleep 5; elapsed=$((elapsed + 5))
  done
  if [ "$elapsed" -ge "$deadline" ]; then
    echo -e "  ${RED}Setup failed${NC}: test agent pod not ready after ${deadline}s"
    # Show last pod status for debugging
    e get pods -n "$NAMESPACE" -l "app=$AGENT_DEPLOYMENT" 2>/dev/null || true
    cleanup; exit 1
  fi

  echo "  Waiting for steady-state skipping (≥3 skips, ~90s)..."
  if ! wait_for_metric "skipped" 3 150; then
    echo -e "  ${RED}Setup failed${NC}: test agent never reached steady-state skipping"
    e logs -n "$NAMESPACE" deployment/"$AGENT_DEPLOYMENT" --tail=20 2>/dev/null || true
    cleanup; exit 1
  fi
  echo -e "  ${GREEN}Ready${NC}: Probe CR created, agent skipping in steady state"
}

# ---------------------------------------------------------------------------
# Teardown
# ---------------------------------------------------------------------------

cleanup() {
  echo -e "\n${BOLD}Cleanup${NC}"
  e delete deployment/"$AGENT_DEPLOYMENT" -n "$NAMESPACE" >/dev/null 2>&1 || true
  e delete pod/mock-synthetics-api svc/mock-synthetics-api \
    cm/mock-api-script -n "$NAMESPACE" >/dev/null 2>&1 || true
  e delete probes.monitoring.rhobs "$PROBE_NAME" \
    -n "$NAMESPACE" >/dev/null 2>&1 || true
  # Note: pull secret is intentionally left in place (reused across runs)
  echo "  Done."
}

trap cleanup EXIT

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

setup

# Capture steady-state spec values to use as expected values (no hardcoding)
STEADY_INTERVAL=$(get_probe_field ".spec.interval")
STEADY_MODULE=$(get_probe_field ".spec.module")
STEADY_URL=$(get_probe_field ".spec.targets.staticConfig.static[0]")
STEADY_RELABEL=$(get_probe_field ".spec.metricRelabelings[0].targetLabel")

echo -e "\n${BOLD}Steady-state spec${NC}"
echo "  interval=$STEADY_INTERVAL  module=$STEADY_MODULE"
echo "  url=$STEADY_URL"
echo "  metricRelabelings[0].targetLabel=${STEADY_RELABEL:-(none)}"

# --- Test 1: Label change ---
echo -e "\n${BOLD}Test 1: Label change${NC}"
echo "  Injecting a spurious label into spec.targets.staticConfig.labels"
run_mutation_test "Label change" \
  ".spec.targets.staticConfig.labels.injected" "" \
  --type=merge -p '{"spec":{"targets":{"staticConfig":{"labels":{"injected":"bad-label"}}}}}'

# --- Test 2: URL change ---
echo -e "\n${BOLD}Test 2: URL change${NC}"
echo "  Changing probe target URL"
run_mutation_test "URL change" \
  ".spec.targets.staticConfig.static[0]" "$STEADY_URL" \
  --type=json -p '[{"op":"replace","path":"/spec/targets/staticConfig/static/0","value":"https://drift.example.com"}]'

# --- Test 3: Interval change ---
echo -e "\n${BOLD}Test 3: Interval change${NC}"
echo "  Changing scrape interval to wrong value"
run_mutation_test "Interval change" \
  ".spec.interval" "$STEADY_INTERVAL" \
  --type=merge -p '{"spec":{"interval":"999s"}}'

# --- Test 4: Module drift ---
echo -e "\n${BOLD}Test 4: Module drift (same probe ID, drifted config)${NC}"
echo "  Changing module to wrong value"
run_mutation_test "Module drift" \
  ".spec.module" "$STEADY_MODULE" \
  --type=merge -p '{"spec":{"module":"tcp_connect"}}'

# --- Test 5: MetricRelabelConfig removal (agent code upgrade) ---
echo -e "\n${BOLD}Test 5: MetricRelabelConfig removal (agent code upgrade)${NC}"
echo "  Clearing metricRelabelings to simulate pre-upgrade CR state"
if [ -n "$STEADY_RELABEL" ]; then
  run_mutation_test "MetricRelabelConfig restoration" \
    ".spec.metricRelabelings[0].targetLabel" "$STEADY_RELABEL" \
    --type=merge -p '{"spec":{"metricRelabelings":[]}}'
else
  echo -e "    ${YELLOW}skipped${NC}: no metricRelabelings in steady-state spec"
fi

# --- Test 6: K8s normalisation — no false diffs ---
echo -e "\n${BOLD}Test 6: K8s normalisation — no false diffs${NC}"
echo "  Watching for 3 consecutive skip-only cycles after all corrections..."
APPLIED_BEFORE=$(get_metric "applied")
SKIPPED_BEFORE=$(get_metric "skipped")
all_clean=true
for cycle in 1 2 3; do
  if wait_for_metric "skipped" $((SKIPPED_BEFORE + cycle)) $((RECONCILE_INTERVAL * 2 + 10)); then
    applied_now=$(get_metric "applied")
    if [ "$applied_now" != "$APPLIED_BEFORE" ]; then
      report "No false diff in cycle $cycle" "fail" \
        "applied jumped from $APPLIED_BEFORE to $applied_now unexpectedly"
      all_clean=false; break
    fi
  else
    report "Cycle $cycle: agent stopped skipping" "fail" \
      "skipped didn't reach $((SKIPPED_BEFORE + cycle))"
    all_clean=false; break
  fi
done
[ "$all_clean" = "true" ] && report "No false diffs across 3 consecutive cycles" "pass"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

TOTAL=$((PASS + FAIL))
echo -e "\n${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BOLD}Results: $PASS/$TOTAL passed${NC}"
if [ ${#ERRORS[@]} -gt 0 ]; then
  echo -e "${RED}Failed:${NC}"
  for err in "${ERRORS[@]}"; do echo "  • $err"; done
fi
echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

[ "$FAIL" -eq 0 ]
