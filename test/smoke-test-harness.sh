#!/bin/bash
# Test harness for smoke-template.yaml script logic.
# Spins up mock HTTP servers and runs the smoke script against them.
# Usage: bash test/smoke-test-harness.sh

set -u

PIDS=()

cleanup() {
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null
  rm -f /tmp/metrics /tmp/probe_result /tmp/fresh_result
}
trap cleanup EXIT

start_mock_server() {
  local port=$1 handler=$2
  python3 -c "
import http.server, json, sys

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        $handler
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        self.rfile.read(length)
        $handler

http.server.HTTPServer(('127.0.0.1', $port), Handler).serve_forever()
" &
  PIDS+=($!)
  sleep 0.5
}

run_smoke() {
  local agent_url=$1 thanos_url=$2 desc=$3
  echo ""
  echo "========================================"
  echo "TEST: $desc"
  echo "  AGENT_URL=$agent_url"
  echo "  THANOS_URL=$thanos_url"
  echo "========================================"

  AGENT_URL="$agent_url" THANOS_URL="$thanos_url" /bin/bash -cu '
FAILURES=0
log() { echo "$(date +%H:%M:%S) $*"; }

log "Synthetics Agent smoke check"
log "  Agent URL: ${AGENT_URL}"
log "  Thanos URL: ${THANOS_URL}"
echo ""

log "--- Waiting for Agent readiness (up to 2m) ---"
READY=false
for i in $(seq 1 2); do
  if curl -sf --max-time 2 "${AGENT_URL}/readyz" >/dev/null 2>&1; then
    READY=true
    break
  fi
  log "  attempt $i/2: agent not ready, retrying in 1s..."
  sleep 1
done
if [[ "$READY" != "true" ]]; then
  log "FAIL  agent not ready after 2 minutes"
  FAILURES=$((FAILURES + 1))
else
  log "PASS  agent is ready"
fi
echo ""

log "--- Agent Metrics Endpoint ---"
METRICS=""
if curl -fsSL --retry 1 --retry-delay 1 --max-time 5 "${AGENT_URL}/metrics" -o /tmp/metrics 2>/dev/null; then
  METRICS=$(cat /tmp/metrics)
fi
if [[ -z "$METRICS" ]]; then
  log "FAIL  /metrics: no response or request failed"
  FAILURES=$((FAILURES + 1))
else
  if echo "$METRICS" | grep -q "synthetics_agent"; then
    log "PASS  /metrics: agent metrics present"
  else
    log "WARN  /metrics: responding but no synthetics_agent metrics"
  fi
fi

echo ""
log "--- Probe Data Flowing (Thanos) ---"
PROBE_COUNT="0"
THANOS_REACHABLE=false
if curl -fsSL --retry 1 --retry-delay 1 --max-time 5 \
  "${THANOS_URL}/api/v1/query" \
  --data-urlencode "query=count(probe_success)" -o /tmp/probe_result 2>/dev/null; then
  THANOS_REACHABLE=true
  PROBE_COUNT=$(jq -r ".data.result[0].value[1] // \"0\"" /tmp/probe_result 2>/dev/null || echo "0")
fi
if [[ "$THANOS_REACHABLE" != "true" ]]; then
  log "FAIL  Thanos unreachable at ${THANOS_URL}"
  FAILURES=$((FAILURES + 1))
elif [[ "$PROBE_COUNT" == "0" ]]; then
  log "WARN  probe_success: no data (may be expected on small cells)"
else
  log "PASS  probe_success: $(printf "%.0f" "$PROBE_COUNT") series"
fi

echo ""
log "--- Probe Data Freshness ---"
FRESH_COUNT="0"
if [[ "$THANOS_REACHABLE" == "true" ]]; then
  if curl -fsSL --retry 1 --retry-delay 1 --max-time 5 \
    "${THANOS_URL}/api/v1/query" \
    --data-urlencode "query=count(probe_success{} unless probe_success{} offset 10m)" -o /tmp/fresh_result 2>/dev/null; then
    FRESH_COUNT=$(jq -r ".data.result[0].value[1] // \"0\"" /tmp/fresh_result 2>/dev/null || echo "0")
  else
    log "FAIL  Thanos freshness query failed"
    FAILURES=$((FAILURES + 1))
  fi
fi
if [[ "$THANOS_REACHABLE" == "true" && "$PROBE_COUNT" != "0" && "$FRESH_COUNT" == "0" ]]; then
  log "FAIL  probe freshness: data exists but no new samples in 10m"
  FAILURES=$((FAILURES + 1))
elif [[ "$THANOS_REACHABLE" == "true" && "$PROBE_COUNT" != "0" ]]; then
  log "PASS  probe freshness: active data flowing"
fi

echo ""
if [[ $FAILURES -gt 0 ]]; then
  log "FAILED: $FAILURES check(s) failed"
  exit 1
fi
log "PASSED: all checks passed"
'
  local exit_code=$?
  echo "EXIT CODE: $exit_code"
  return $exit_code
}

PASS=0
FAIL=0
check_result() {
  local expected=$1 actual=$2 desc=$3
  if [[ "$expected" == "$actual" ]]; then
    echo "  ✓ $desc (expected exit=$expected, got exit=$actual)"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $desc (expected exit=$expected, got exit=$actual)"
    FAIL=$((FAIL + 1))
  fi
}

# --- Test 1: Unreachable endpoints (should FAIL) ---
run_smoke "http://127.0.0.1:19999" "http://127.0.0.1:19998" \
  "Both endpoints unreachable"
check_result 1 $? "Unreachable endpoints should fail"

# --- Test 2: Healthy agent + healthy Thanos with probe data ---
cleanup
PIDS=()
start_mock_server 18081 "
        if '/readyz' in self.path:
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
        elif '/metrics' in self.path:
            self.send_response(200); self.end_headers()
            self.wfile.write(b'synthetics_agent_reconcile_total 42\nsynthetics_agent_up 1\n')
        else:
            self.send_response(404); self.end_headers()
"
start_mock_server 18082 "
        self.send_response(200); self.end_headers()
        import json
        self.wfile.write(json.dumps({'status':'success','data':{'resultType':'vector','result':[{'value':[0,'5']}]}}).encode())
"

run_smoke "http://127.0.0.1:18081" "http://127.0.0.1:18082" \
  "Healthy agent + Thanos with probe data"
check_result 0 $? "Healthy endpoints should pass"

# --- Test 3: Agent returns 503 (should FAIL) ---
cleanup
PIDS=()
start_mock_server 18083 "
        self.send_response(503); self.end_headers()
        self.wfile.write(b'Service Unavailable')
"
start_mock_server 18084 "
        self.send_response(200); self.end_headers()
        import json
        self.wfile.write(json.dumps({'status':'success','data':{'resultType':'vector','result':[{'value':[0,'5']}]}}).encode())
"

run_smoke "http://127.0.0.1:18083" "http://127.0.0.1:18084" \
  "Agent returns HTTP 503"
check_result 1 $? "HTTP 503 agent should fail"

# --- Test 4: Healthy agent, Thanos returns 500 (should FAIL) ---
cleanup
PIDS=()
start_mock_server 18085 "
        if '/readyz' in self.path:
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
        elif '/metrics' in self.path:
            self.send_response(200); self.end_headers()
            self.wfile.write(b'synthetics_agent_reconcile_total 42\n')
        else:
            self.send_response(404); self.end_headers()
"
start_mock_server 18086 "
        self.send_response(500); self.end_headers()
        self.wfile.write(b'Internal Server Error')
"

run_smoke "http://127.0.0.1:18085" "http://127.0.0.1:18086" \
  "Healthy agent, Thanos returns HTTP 500"
check_result 1 $? "HTTP 500 Thanos should fail"

# --- Test 5: Healthy agent, Thanos returns zero probes (should pass with WARN) ---
cleanup
PIDS=()
start_mock_server 18087 "
        if '/readyz' in self.path:
            self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
        elif '/metrics' in self.path:
            self.send_response(200); self.end_headers()
            self.wfile.write(b'synthetics_agent_reconcile_total 42\n')
        else:
            self.send_response(404); self.end_headers()
"
start_mock_server 18088 "
        self.send_response(200); self.end_headers()
        import json
        self.wfile.write(json.dumps({'status':'success','data':{'resultType':'vector','result':[]}}).encode())
"

run_smoke "http://127.0.0.1:18087" "http://127.0.0.1:18088" \
  "Healthy agent, Thanos returns zero probes"
check_result 0 $? "Zero probes should pass (WARN only)"

# --- Summary ---
echo ""
echo "========================================"
echo "RESULTS: $PASS passed, $FAIL failed"
echo "========================================"
[[ $FAIL -eq 0 ]]
