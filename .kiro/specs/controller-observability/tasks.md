# Implementation Plan: Controller Observability

## Overview

This plan adds Prometheus-compatible operational metrics to the controller using controller-runtime's built-in metrics registry. It is a self-contained spec with no API type changes, no CRD regeneration, and no new dependencies (prometheus/client_golang is already a transitive dependency of controller-runtime).

---

## Tasks

- [ ] 1. Create the metrics package
  - [ ] 1.1 Create `internal/metrics/metrics.go`
    - Define all 10 metric variables (CounterVec, GaugeVec, HistogramVec) as package-level vars
    - Register all metrics in an `init()` function via `crmetrics.Registry.MustRegister()`
    - Use the `investigator_` prefix on all metric names
    - Include descriptive `Help` strings on all metrics
    - Define bucket slices for both histograms as specified in the design
    - _Requirements: 1.1, 2.1, 2.3, 2.4, 3.1, 4.1, 4.3, 5.1, 5.3, 6.1, 7.1, 7.2, 7.3, 7.4_
  - [ ] 1.2 Implement `Recorder` struct and methods
    - `RecordIncidentDetected(triggerType, namespace string)`
    - `RecordInvestigationCompleted(triggerType string, durationSeconds float64)`
    - `RecordUnknownDiagnosis(triggerType string)`
    - `RecordDiagnosed(ruleID, triggerType string)`
    - `SetActiveIncidents(namespace string, count float64)`
    - `RecordReconciliationError(errorType string)`
    - `RecordEvidenceCollectionFailure(source string)`
    - `RecordEvidenceCollectionDuration(triggerType string, durationSeconds float64)`
    - `RecordDiagnosisRuleMatch(ruleID, confidence string)`
    - Implement `NoOpRecorder` with identical method signatures and empty bodies
    - _Requirements: 7.5_

- [ ] 2. Add Recorder to PodReconciler
  - Add `Metrics *metrics.Recorder` field to `PodReconciler` struct in `internal/controller/pod_reconciler.go`
  - Construct `&metrics.Recorder{}` in `cmd/main.go` and pass it to `PodReconciler`
  - _Requirements: 7.1_

- [ ] 3. Wire metric recording into Reconcile()
  - [ ] 3.1 Record `incidents_detected_total` after successful IncidentReport creation (step 8)
    - Call `r.Metrics.RecordIncidentDetected(triggerType, namespace)` after `r.Create(ctx, report)` succeeds
    - Increment `active_incidents` gauge on creation
    - _Requirements: 1.1, 1.2, 1.3, 3.1_
  - [ ] 3.2 Record resolution and duration metrics after `Transition()` succeeds (step 11)
    - Call `r.Metrics.RecordInvestigationCompleted(triggerType, durationSecs)` where `durationSecs = report.Status.ResolvedAt.Sub(report.Status.StartedAt.Time).Seconds()`
    - Decrement `active_incidents` gauge on resolution
    - _Requirements: 2.1, 2.2, 5.1, 5.2, 3.2_
  - [ ] 3.3 Record diagnosis phase transition metrics after step 9.6
    - When phase transitions to `Unknown`: call `r.Metrics.RecordUnknownDiagnosis(triggerType)`
    - When phase transitions to `Diagnosed`: call `r.Metrics.RecordDiagnosed(primary.RuleID, triggerType)`
    - For each finding in `DiagnosisResult` (primary + contributing): call `r.Metrics.RecordDiagnosisRuleMatch(finding.RuleID, finding.Confidence)`
    - _Requirements: 2.3, 2.4, 6.1, 6.2_
  - [ ] 3.4 Record evidence collection metrics after step 9.5
    - Observe `evidence_collection_duration_seconds` with elapsed time of the collection cycle
    - Iterate `snapshot.CollectionErrors` and call `r.Metrics.RecordEvidenceCollectionFailure(err.Source)` for each
    - _Requirements: 4.3, 4.4, 5.3, 5.4, 5.5_
  - [ ] 3.5 Record reconciliation errors at error return paths
    - At each `return ctrl.Result{}, fmt.Errorf(...)` or `r.Log.Error(...)` call, add `r.Metrics.RecordReconciliationError("api_error")` with an appropriate error type string
    - _Requirements: 4.1, 4.2_

- [ ] 4. Write unit tests
  - Create `test/unit/metrics_test.go`
  - Test each `Recorder` method increments/observes the correct metric using `github.com/prometheus/client_golang/prometheus/testutil`
  - Test `NoOpRecorder` methods do not panic and return without error
  - _Requirements: 1.1, 2.1, 4.1, 5.1, 6.1_

- [ ] 5. Verify metrics endpoint in integration test
  - Add a short assertion to an existing integration test (or create `test/integration/metrics_test.go`)
  - After a reconcile cycle that creates an IncidentReport, verify `investigator_incidents_detected_total` has been incremented
  - Use `testutil.CollectAndCompare` or a direct HTTP GET to the `/metrics` endpoint
  - _Requirements: 7.1, 7.5_

- [ ] 6. Final checkpoint
  - Run `go build ./...` and `go test ./...`
  - Run `go vet ./...` and `gofmt -l .`
  - Confirm `internal/metrics` does not import any `internal/controller`, `internal/investigation`, `internal/evidence`, or `internal/diagnosis` packages

---

## Notes

- `prometheus/client_golang` is already in `go.sum` as a transitive dependency of controller-runtime — no new `go get` needed
- The `NoOpRecorder` allows all existing unit tests to continue passing without changes — just pass `&metrics.NoOpRecorder{}` wherever `*metrics.Recorder` is expected in tests
- Metric label cardinality: `namespace` and `trigger_type` are the only "variable" labels; all others (`rule_id`, `confidence`, `source`, `error_type`) are drawn from small fixed sets

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["2"] },
    { "id": 2, "tasks": ["3.1", "3.2", "3.3", "3.4", "3.5"] },
    { "id": 3, "tasks": ["4"] },
    { "id": 4, "tasks": ["5"] },
    { "id": 5, "tasks": ["6"] }
  ]
}
```
