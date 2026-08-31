#!/usr/bin/env bash
# validate.sh — End-to-end validation of the Incident Investigator on a Kind cluster.
#
# Validates:
#   1. OOMKilled workload creates an IncidentReport (phase=Investigating)
#   2. A second OOM event does not create a duplicate IncidentReport
#   3. Replacing workload with healthy image → IncidentReport resolves
#   4. Historical resolved IncidentReport is preserved when a new incident is created
#
# Prerequisites: kubectl pointed at the Kind cluster, controller deployed via setup.sh
# Usage: ./deploy/kind/validate.sh

set -euo pipefail

NAMESPACE="demo"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

PASS=0
FAIL=0

pass() { echo "  ✓ $1"; PASS=$((PASS+1)); }
fail() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }

wait_for_condition() {
  local description="$1"
  local timeout_seconds="$2"
  local interval=3
  local elapsed=0
  shift 2
  echo "  ... waiting for: ${description} (up to ${timeout_seconds}s)"
  while [[ $elapsed -lt $timeout_seconds ]]; do
    if eval "$@" 2>/dev/null; then
      return 0
    fi
    sleep $interval
    elapsed=$((elapsed + interval))
  done
  echo "  TIMEOUT: ${description} did not occur within ${timeout_seconds}s"
  return 1
}

echo ""
echo "==> Incident Investigator — E2E Validation"
echo "    Namespace : ${NAMESPACE}"
echo ""

# ── Ensure namespace exists ───────────────────────────────────────────────────
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# ── Clean up any previous test resources ─────────────────────────────────────
echo ">>> Cleaning up previous test resources..."
kubectl delete deployment oom-crasher image-pull-fail -n "${NAMESPACE}" --ignore-not-found=true >/dev/null 2>&1
kubectl delete incidentreports -n "${NAMESPACE}" --all --ignore-not-found=true >/dev/null 2>&1
sleep 3
echo ">>> Cleanup done."
echo ""

# ─────────────────────────────────────────────────────────────────────────────
# Step 1: Deploy the OOM crasher workload
# ─────────────────────────────────────────────────────────────────────────────
echo ">>> Step 1: Deploying OOM crasher workload..."
kubectl apply -f "${SCRIPT_DIR}/test-workload/oom-crasher.yaml" >/dev/null
echo "    Applied oom-crasher.yaml"

# Wait for the Pod to OOMKill at least once
echo "  ... waiting for Pod to enter CrashLoopBackOff/OOMKilled state (up to 120s)"
if wait_for_condition "Pod OOMKilled" 120 \
  "kubectl get pods -n ${NAMESPACE} -l app=oom-crasher -o jsonpath='{.items[0].status.containerStatuses[0].lastTerminationState.terminated.reason}' 2>/dev/null | grep -q OOMKilled"; then
  pass "Pod entered OOMKilled state"
else
  fail "Pod did not OOMKill within 120s — check if polinux/stress image is accessible"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Step 2: Assert IncidentReport created with Investigating phase
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo ">>> Step 2: Waiting for IncidentReport to appear..."
if wait_for_condition "IncidentReport created" 60 \
  "kubectl get incidentreports -n ${NAMESPACE} --no-headers 2>/dev/null | grep -q 'oom-crasher'"; then
  pass "IncidentReport created"
else
  fail "IncidentReport was not created within 60s"
  echo "    Debug: $(kubectl get incidentreports -A --no-headers 2>/dev/null || echo 'none')"
fi

# Check phase
IR_NAME=$(kubectl get incidentreports -n "${NAMESPACE}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [[ -n "${IR_NAME}" ]]; then
  PHASE=$(kubectl get incidentreport "${IR_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "")
  if [[ "${PHASE}" == "Investigating" ]]; then
    pass "IncidentReport phase = Investigating"
  else
    fail "IncidentReport phase = '${PHASE}' (expected Investigating)"
  fi

  TRIGGER=$(kubectl get incidentreport "${IR_NAME}" -n "${NAMESPACE}" -o jsonpath='{.status.trigger.type}' 2>/dev/null || echo "")
  if [[ "${TRIGGER}" == "OOMKilled" ]]; then
    pass "Trigger type = OOMKilled"
  else
    fail "Trigger type = '${TRIGGER}' (expected OOMKilled)"
  fi
fi

# ─────────────────────────────────────────────────────────────────────────────
# Step 3: Assert no duplicate IncidentReport on second OOM event
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo ">>> Step 3: Waiting for a second OOM event (deduplication check)..."
sleep 15  # Let the crasher OOMKill again

IR_COUNT=$(kubectl get incidentreports -n "${NAMESPACE}" --no-headers 2>/dev/null | grep -c "oom-crasher" || echo "0")
# Only active reports count — there should be exactly 1 active-named report
ACTIVE_COUNT=$(kubectl get incidentreports -n "${NAMESPACE}" --no-headers 2>/dev/null | grep "\-active" | grep -c "oom-crasher" || echo "0")
if [[ "${ACTIVE_COUNT}" -eq 1 ]]; then
  pass "No duplicate IncidentReport created (exactly 1 active report)"
else
  fail "Expected 1 active IncidentReport, found ${ACTIVE_COUNT}"
fi

echo ""
echo "    Current IncidentReports in namespace ${NAMESPACE}:"
kubectl get incidentreports -n "${NAMESPACE}" 2>/dev/null || echo "    (none)"

# ─────────────────────────────────────────────────────────────────────────────
# Step 4: Fix the workload — replace the crasher with a healthy image
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo ">>> Step 4: Replacing crasher with healthy image to trigger recovery..."
kubectl set image deployment/oom-crasher crasher=nginx:alpine -n "${NAMESPACE}"
kubectl patch deployment oom-crasher -n "${NAMESPACE}" \
  --type=json \
  -p='[{"op":"remove","path":"/spec/template/spec/containers/0/args"}]' \
  2>/dev/null || true
kubectl patch deployment oom-crasher -n "${NAMESPACE}" \
  --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"128Mi"}]' \
  2>/dev/null || true

echo "  ... waiting for Deployment to stabilize with healthy image (up to 120s)"
kubectl rollout status deployment/oom-crasher -n "${NAMESPACE}" --timeout=120s || true

# Wait for IncidentReport to transition to Resolved (stability period: 5 minutes default)
# For e2e we give it up to 7 minutes total
echo ""
echo ">>> Step 5: Waiting for IncidentReport to resolve (stability period = 5m, timeout = 7m)..."
echo "  (This step takes the full stability period. Go get a coffee.)"

if wait_for_condition "IncidentReport Resolved" 420 \
  "kubectl get incidentreports -n ${NAMESPACE} -o jsonpath='{.items[*].status.phase}' 2>/dev/null | grep -q Resolved"; then
  pass "IncidentReport transitioned to Resolved"

  # Check that a historical record exists (does not end with -active)
  HISTORICAL=$(kubectl get incidentreports -n "${NAMESPACE}" --no-headers 2>/dev/null | grep -v "\-active" | grep "oom-crasher" | head -1 | awk '{print $1}' || echo "")
  if [[ -n "${HISTORICAL}" ]]; then
    pass "Historical IncidentReport preserved: ${HISTORICAL}"
  else
    fail "No historical IncidentReport found after resolution"
  fi

  # Active slot should be gone
  ACTIVE_AFTER=$(kubectl get incidentreports -n "${NAMESPACE}" --no-headers 2>/dev/null | grep "\-active" | grep -c "oom-crasher" || echo "0")
  if [[ "${ACTIVE_AFTER}" -eq 0 ]]; then
    pass "Active IncidentReport slot freed after resolution"
  else
    fail "Active IncidentReport slot still present after resolution"
  fi
else
  fail "IncidentReport did not resolve within 7 minutes"
  echo "    Current state:"
  kubectl get incidentreports -n "${NAMESPACE}" 2>/dev/null || true
fi

# ─────────────────────────────────────────────────────────────────────────────
# Step 6: Deploy image-pull-fail to verify a new incident is created
#         while the historical record is preserved
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo ">>> Step 6: Deploying image-pull-fail workload..."
kubectl apply -f "${SCRIPT_DIR}/test-workload/image-pull-fail.yaml" >/dev/null

if wait_for_condition "ImagePullBackOff IncidentReport" 60 \
  "kubectl get incidentreports -n ${NAMESPACE} --no-headers 2>/dev/null | grep -q 'image-pull-fail'"; then
  pass "New IncidentReport created for ImagePullBackOff workload"

  # Historical OOM record must still be present
  HIST_STILL=$(kubectl get incidentreports -n "${NAMESPACE}" --no-headers 2>/dev/null | grep -v "\-active" | grep -c "oom-crasher" || echo "0")
  if [[ "${HIST_STILL}" -ge 1 ]]; then
    pass "Historical OOM IncidentReport preserved alongside new incident"
  else
    fail "Historical OOM IncidentReport was deleted when new incident was created"
  fi
else
  fail "IncidentReport for ImagePullBackOff was not created within 60s"
fi

# ─────────────────────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo "==> Final IncidentReports:"
kubectl get incidentreports -n "${NAMESPACE}" 2>/dev/null || echo "  (none)"
echo ""
echo "==> Validation summary: ${PASS} passed, ${FAIL} failed"
echo ""

if [[ "${FAIL}" -gt 0 ]]; then
  echo "VALIDATION FAILED"
  exit 1
else
  echo "VALIDATION PASSED"
  exit 0
fi
