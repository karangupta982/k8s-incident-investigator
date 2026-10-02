# Scenario: ReadinessProbeFailure

## What it does
Deploys nginx (listening on port 80) with a readiness probe checking port 8080.
The probe always fails. After 3 consecutive failures Kubernetes emits Unhealthy
events and the investigator detects a ReadinessProbeFailure trigger.

## Expected result
- Phase: Diagnosed
- Primary finding: `ProbeFailure` (Medium confidence)
- Evidence: Unhealthy events, probe type HTTPGet port 8080
- Note: may take ~15s to trigger (3 failures × 5s period)
