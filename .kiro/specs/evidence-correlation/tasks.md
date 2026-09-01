# Implementation Plan: Evidence Correlation

## Overview

This plan adds the evidence correlation step between evidence collection and diagnosis. It introduces `internal/correlation/`, new API types for `CorrelatedEvidence`, and updates the `DiagnosisEngine.Evaluate()` signature to accept correlated evidence.

---

## Tasks

- [ ] 1. Define new API types
  - Create `api/v1alpha1/correlation_types.go` with `CorrelatedEvidence`, `CausalSignals`, `LogPatterns` as defined in the design
  - Add `+kubebuilder:object:generate=true` markers to all structs
  - Add `CorrelatedEvidence *CorrelatedEvidence` field to `IncidentReportStatus` in `incidentreport_types.go`
  - Insert `correlatedEvidence` field in the status struct between `diagnosis` and `timeline` (for kubectl describe ordering)
  - _Requirements: 5.1, 5.2_

- [ ] 2. Run code generation
  - Run `make generate` for new DeepCopy methods
  - Run `make manifests` to update CRD YAML
  - Run `go build ./api/...` to verify
  - _Requirements: 5.1_

- [ ] 3. Implement evidence correlator
  - [ ] 3.1 Create `internal/correlation/correlator.go`
    - Implement `EvidenceCorrelator` struct with `EventCorrelationWindow time.Duration` (default 5 minutes)
    - Implement `Correlate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.CorrelatedEvidence`
    - Implement `deriveCausalSignals()` helper covering all 6 causal signal types from requirements
    - Implement `matchLogPatterns()` helper covering all 4 pattern types using `strings.Contains` with lowercased lines
    - Implement `buildChainPatterns()` helper for the 3 chain pattern types
    - Handle nil snapshot gracefully (return empty CorrelatedEvidence)
    - Deep copy is NOT needed since the output is a new value type
    - _Requirements: 1.1–1.6, 2.1–2.5, 3.1–3.4, 4.1–4.4_
  - [ ]* 3.2 Write unit tests for EvidenceCorrelator
    - Create `test/unit/correlation_test.go`
    - Test each causal signal: OOM+pressure, OOM+limit, PVC unbound, mount+PVC, scheduling, OOM+crash loop
    - Test each log pattern: OOMString, ConnectionRefused, PanicOrFatal, PermissionDenied
    - Test nil snapshot: no panic, empty return
    - Test idempotency: two calls with same snapshot → same output
    - _Requirements: 1.1–1.6, 2.1–2.5, 4.3, 4.4_
  - [ ]* 3.3 Write property tests
    - **Property**: `Correlate()` is a pure function — same input always produces same output
    - **Property**: nil input never causes panic
    - Use `pgregory.net/rapid`, 100 iterations minimum
    - Tag: `Feature: evidence-correlation, Property: pure-function`

- [ ] 4. Update DiagnosisEngine signature
  - Extend `DiagnosisEngine.Evaluate()` in `internal/diagnosis/engine.go` to accept `correlated *v1alpha1.CorrelatedEvidence` as a second parameter
  - Update all `DiagnosisRule.Evaluate()` implementations to accept the same second parameter (they may ignore it if they don't need it)
  - Rules that benefit from `CausalSignals` (OOM rules, PVC rules, scheduling rule) should use the pre-computed signals instead of re-deriving them
  - Update all callers of `DiagnosisEngine.Evaluate()` in `PodReconciler`
  - Update all existing diagnosis rule unit tests to pass a nil `CorrelatedEvidence` (backward compatible)
  - _Requirements: 4.2_

- [ ] 5. Wire correlator into PodReconciler
  - Add `Correlator *correlation.EvidenceCorrelator` field to `PodReconciler`
  - Insert step 9.55 between step 9.5 (evidence collection) and step 9.6 (diagnosis):
    ```go
    // ---- Step 9.55: Correlate evidence ----
    correlated := r.Correlator.Correlate(report.Status.Evidence)
    base := report.DeepCopy()
    report.Status.CorrelatedEvidence = &correlated
    if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(base)); patchErr != nil {
        log.Error(patchErr, "failed to patch correlated evidence, continuing")
    }
    ```
  - Pass `&correlated` to `DiagnosisEngine.Evaluate()`
  - Construct `&correlation.EvidenceCorrelator{EventCorrelationWindow: 5 * time.Minute}` in `cmd/main.go`
  - _Requirements: 5.1, 5.2_

- [ ] 6. Final checkpoint
  - Run `make test` — all existing tests must still pass (nil CorrelatedEvidence in existing tests is safe)
  - Run `go vet ./...` and `gofmt -l .`
  - Verify `internal/correlation` does not import any forbidden packages

---

## Notes

- The `DiagnosisEngine.Evaluate()` signature change is backward compatible for tests — pass `nil` for `correlated` and rules that don't use it continue working
- `EventCorrelationWindow` is not exposed as a CLI flag in this spec — it can be added to the config struct as `EvidenceCorrelationWindow time.Duration` in a follow-up if operators need to tune it
- Log pattern matching is purely `strings.Contains` — simple, deterministic, no external dependencies

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1"] },
    { "id": 1, "tasks": ["2"] },
    { "id": 2, "tasks": ["3.1"] },
    { "id": 3, "tasks": ["3.2", "3.3", "4"] },
    { "id": 4, "tasks": ["5"] },
    { "id": 5, "tasks": ["6"] }
  ]
}
```
