# Scenario: SchedulingFailure

## What it does
Requests 999 CPUs — far beyond any node's capacity. The scheduler cannot
place the Pod and emits repeated FailedScheduling events.

## Expected result
- Phase: Diagnosed
- Primary finding: `SchedulingFailure` (High confidence)
- Evidence: FailedScheduling events, resource request cpu=999
