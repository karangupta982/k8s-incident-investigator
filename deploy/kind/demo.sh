#!/usr/bin/env bash
# demo.sh — Guided Incident Investigator walkthrough for Kind clusters.
#
# Usage:
#   ./deploy/kind/demo.sh <scenario>             Run a single scenario
#   ./deploy/kind/demo.sh --all                  Run all 6 scenarios
#   ./deploy/kind/demo.sh <scenario> --no-cleanup Keep resources after demo
#
# Prerequisites: kubectl pointed at the Kind cluster, controller deployed via setup.sh
# Available scenarios:
#   oom-killed, crash-loop, image-pull-backoff, mount-failure,
#   scheduling-failure, readiness-probe-failure

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCENARIOS_DIR="${SCRIPT_DIR}/scenarios"
ALL_SCENARIOS="oom-killed crash-loop image-pull-backoff mount-failure scheduling-failure readiness-probe-failure"

CLEANUP=true
RUN_ALL=false
SCENARIO=""

# ── Argument parsing ──────────────────────────────────────────────────────────
for arg in "$@"; do
  case "$arg" in
    --all)         RUN_ALL=true ;;
    --no-cleanup)  CLEANUP=false ;;
    -*)            echo "Unknown flag: $arg"; exit 1 ;;
    *)             SCENARIO="$arg" ;;
  esac
done

if [[ "$RUN_ALL" == "false" && -z "$SCENARIO" ]]; then
  echo "Usage: $0 <scenario> [--no-cleanup]"
  echo "       $0 --all [--no-cleanup]"
  echo ""
  echo "Available scenarios:"
  for s in $ALL_SCENARIOS; do
    echo "  $s"
  done
  exit 1
fi

# ── Helpers ───────────────────────────────────────────────────────────────────
section() { echo ""; echo "══════════════════════════════════════════════════"; echo "  $1"; echo "══════════════════════════════════════════════════"; }
step()    { echo "  ➤  $1"; }
ok()      { echo "  ✓  $1"; }
warn()    { echo "  ⚠  $1"; }

cleanup_scenario() {
  local scenario="$1"
  local ns="demo-${scenario}"
  step "Cleaning up namespace ${ns}..."
  kubectl delete namespace "${ns}" --ignore-not-found=true --wait=false >/dev/null 2>&1 || true
}

run_scenario() {
  local scenario="$1"
  local ns="demo-${scenario}"
  local scenario_dir="${SCENARIOS_DIR}/${scenario}"

  if [[ ! -d "$scenario_dir" ]]; then
    echo "ERROR: Scenario directory not found: ${scenario_dir}"
    exit 1
  fi

  section "Scenario: ${scenario}"

  # Clean up any previous run
  step "Removing any previous resources for this scenario..."
  kubectl delete namespace "${ns}" --ignore-not-found=true --wait=false >/dev/null 2>&1 || true
  sleep 2

  # Deploy the scenario
  step "Deploying scenario manifests from ${scenario_dir}..."
  kubectl apply -f "${scenario_dir}/" >/dev/null
  ok "Manifests applied to namespace ${ns}"

  # Wait for an IncidentReport to appear (up to 120s)
  step "Waiting for IncidentReport to appear (up to 120s)..."
  ir_name=""
  for i in $(seq 1 40); do
    ir_name=$(kubectl get incidentreports -n "${ns}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [[ -n "$ir_name" ]]; then
      ok "IncidentReport created: ${ir_name}"
      break
    fi
    printf "."
    sleep 3
  done
  echo ""

  if [[ -z "$ir_name" ]]; then
    warn "No IncidentReport appeared within 120s. The controller may not be running or the trigger threshold was not reached."
    warn "Try: kubectl get pods -n incident-investigator-system"
    warn "Try: kubectl logs -n incident-investigator-system deployment/controller-manager"
    if [[ "$CLEANUP" == "true" ]]; then cleanup_scenario "$scenario"; fi
    return 1
  fi

  # Wait for investigation to complete (phase != Investigating, up to 8 min)
  step "Waiting for investigation to complete (phase != Investigating, up to 8 min)..."
  for i in $(seq 1 48); do
    phase=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [[ "$phase" != "Investigating" && -n "$phase" ]]; then
      ok "Investigation complete: phase=${phase}"
      break
    fi
    printf "."
    sleep 10
  done
  echo ""

  # Print the full IncidentReport
  section "IncidentReport: ${ir_name}"
  kubectl describe incidentreport "${ir_name}" -n "${ns}" 2>/dev/null || true

  # Print a concise summary
  section "Investigation Summary"
  phase=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
  cause=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.diagnosis.primary.cause}' 2>/dev/null || echo "")
  rule=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.diagnosis.primary.ruleID}' 2>/dev/null || echo "")
  confidence=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.diagnosis.primary.confidence}' 2>/dev/null || echo "")
  summary=$(kubectl get incidentreport "${ir_name}" -n "${ns}" -o jsonpath='{.status.summary}' 2>/dev/null || echo "")

  echo "  Phase:      ${phase}"
  if [[ -n "$rule" ]]; then
    echo "  Rule:       ${rule} (Confidence: ${confidence})"
    echo "  Cause:      ${cause}"
  else
    echo "  Diagnosis:  Unknown — no rule matched the collected evidence"
  fi
  if [[ -n "$summary" ]]; then
    echo ""
    echo "  Summary:"
    echo "$summary" | while IFS= read -r line; do echo "    ${line}"; done
  fi

  if [[ "$CLEANUP" == "true" ]]; then
    echo ""
    step "Cleaning up namespace ${ns}..."
    kubectl delete namespace "${ns}" --ignore-not-found=true --wait=false >/dev/null 2>&1 || true
    ok "Namespace ${ns} marked for deletion."
  else
    warn "Skipping cleanup (--no-cleanup). Resources remain in namespace ${ns}."
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
section "Incident Investigator — Demo"
echo "  Controller: $(kubectl get deployment controller-manager -n incident-investigator-system -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo '?')/1 replicas ready"
echo ""

if [[ "$RUN_ALL" == "true" ]]; then
  pass=0
  fail=0
  for s in $ALL_SCENARIOS; do
    if run_scenario "$s"; then
      pass=$((pass + 1))
    else
      fail=$((fail + 1))
    fi
    sleep 3
  done
  section "All scenarios complete: ${pass} passed, ${fail} failed"
else
  run_scenario "$SCENARIO"
fi
