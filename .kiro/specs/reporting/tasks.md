# Implementation Plan: Reporting and Presentation

## Overview

This plan implements the reporting layer: the final stage of the investigation pipeline. It adds `IncidentSummary`, `Timeline`, `TimelineEvent`, and recommendation enrichment to the `IncidentReport`. All components are pure functions over existing status fields — no new Kubernetes API calls are required.

Tasks build incrementally: types first, then each builder, then integration wiring, then tests.

---

## Tasks

- [ ] 1. Add configuration field
  - Add `MaxTimelineEvents int` to `internal/config/config.go` with default 50
  - Add validation in `Config.Validate()`: must be positive
  - Add test case in `test/unit/config_test.go` for default value and invalid value
  - _Requirements: 8.1, 8.2_

- [ ] 2. Define new API types and add reporting fields to IncidentReportStatus
  - [ ] 2.1 Create `api/v1alpha1/reporting_types.go`
    - Define `TimelineEvent` struct with `Timestamp metav1.Time`, `Source string`, `Reason string`, `Message string` fields
    - Add `+kubebuilder:object:generate=true` marker
    - Add Apache 2.0 license header
    - _Requirements: 2.1, 6.2_
  - [ ] 2.2 Add `Summary` and `Timeline` fields to `IncidentReportStatus`
    - Add `Summary string` field with `// +optional` and JSON tag `"summary,omitempty"` to `IncidentReportStatus` in `api/v1alpha1/incidentreport_types.go`
    - Add `Timeline []TimelineEvent` field with `// +optional` and JSON tag `"timeline,omitempty"`
    - Reorder the struct fields so JSON serialization follows the intended `kubectl describe` order: `phase`, `summary`, `startedAt`, `resolvedAt`, `stabilityStartedAt`, `lastFailureAt`, `failureCount`, `trigger`, `workloadOwnerResolved`, `affectedPods`, `diagnosis`, `timeline`, `evidence`, `conditions`
    - _Requirements: 1.5, 2.6, 5.1, 5.2, 5.3_
  - [ ] 2.3 Add `Cause` printer column (wide output)
    - Add `// +kubebuilder:printcolumn:name="Cause",type=string,JSONPath=".status.diagnosis.primary.cause",priority=1` to the `IncidentReport` type
    - _Requirements: 4.1, 4.2_

- [ ] 3. Run code generation and update CRD manifests
  - Run `make generate` to regenerate `zz_generated.deepcopy.go` for `TimelineEvent`
  - Run `make manifests` to regenerate the CRD YAML with the new `summary` and `timeline` fields and the `Cause` printer column
  - Verify `go build ./api/...` compiles cleanly
  - _Requirements: 2.1, 4.1_

- [ ] 4. Implement SummaryBuilder
  - [ ] 4.1 Create `internal/reporting/summary.go`
    - Implement `SummaryBuilder` struct with `MaxLength int`
    - Implement `Build(report *v1alpha1.IncidentReport) string`
    - Handle all four phase cases: Diagnosed, Unknown, Investigating, Resolved — as specified in the design
    - Gracefully handle nil fields: nil `Spec.Workload`, nil `Status.Trigger`, nil `Status.Diagnosis`
    - Truncate output to `MaxLength` (default 2048) with "…" suffix
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.6, 6.1, 7.4_
  - [ ]* 4.2 Write unit tests for SummaryBuilder
    - Create `test/unit/reporting/summary_test.go`
    - Test Diagnosed phase: verify summary includes workload, cause, confidence, trigger, pod count
    - Test Unknown phase: verify summary includes UnknownReason and a suggested next step
    - Test Investigating phase: verify "investigation in progress" message
    - Test Resolved phase: verify duration and resolution timestamp
    - Test nil Workload: verify graceful fallback (no panic, non-empty string)
    - Test nil Diagnosis on Diagnosed phase: verify fallback to trigger type
    - Test max length truncation
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 6.1_
  - [ ]* 4.3 Write property tests for SummaryBuilder
    - **Property 1**: For any IncidentReport in any phase, `Build()` returns non-empty string
    - **Property 5**: For identical IncidentReport input, `Build()` returns identical string on N calls
    - **Property 6**: For any combination of nil status fields, `Build()` does not panic
    - Use `pgregory.net/rapid` with minimum 100 iterations
    - Tag: `Feature: reporting, Property 1/5/6`
    - _Requirements: 1.1, 7.4_

- [ ] 5. Implement TimelineBuilder
  - [ ] 5.1 Create `internal/reporting/timeline.go`
    - Implement `TimelineBuilder` struct with `MaxEvents int`
    - Implement `Build(report *v1alpha1.IncidentReport) []v1alpha1.TimelineEvent`
    - Collect events from all three sources: controller lifecycle events (StartedAt, StabilityStartedAt, ResolvedAt), container terminations from Evidence.Pod.Containers, Kubernetes Events from Evidence.Events
    - Sort chronologically ascending by Timestamp; for equal timestamps use the tie-breaking order: controller lifecycle events first (source="controller"), then container terminations (source="container"), then Kubernetes events (source="kubernetes-event")
    - Truncate `TimelineEvent.Message` to 256 characters
    - Bound result to `MaxEvents`; when events are omitted add a note entry at index 0: `TimelineEvent{Source: "controller", Reason: "TimelineTruncated", Message: fmt.Sprintf("<N> earlier events omitted")}`
    - Handle nil Evidence gracefully (build timeline from lifecycle fields only)
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 6.2, 6.3_
  - [ ]* 5.2 Write unit tests for TimelineBuilder
    - Create `test/unit/reporting/timeline_test.go`
    - Test ordering: events with different timestamps appear in ascending order
    - Test tie-breaking: controller event and container event at same timestamp → controller first
    - Test bounding: 60 sources → max MaxTimelineEvents entries + 1 truncation note
    - Test nil Evidence: timeline built from lifecycle fields only
    - Test nil StartedAt: timeline does not panic, returns empty or partial result
    - Test message truncation at 256 chars
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 6.2_
  - [ ]* 5.3 Write property tests for TimelineBuilder
    - **Property 2**: For any input, `len(Build()) <= MaxTimelineEvents`
    - **Property 3**: For any input, events in result are ordered by Timestamp ascending
    - **Property 6**: For any nil field combination, `Build()` does not panic
    - Tag: `Feature: reporting, Property 2/3/6`
    - _Requirements: 2.2, 2.4_

- [ ] 6. Implement RecommendationEnricher
  - [ ] 6.1 Create `internal/reporting/recommendation.go`
    - Implement `RecommendationEnricher` struct
    - Implement `Enrich(result *v1alpha1.DiagnosisResult, snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisResult`
    - Deep copy the DiagnosisResult before modifying it — do not mutate the input
    - Implement all 10 recommendation templates as defined in the design, substituting specific evidence values (memory limit, container name, restart count, PVC name, image name, etc.)
    - Fall back to generic wording when specific evidence fields are nil or empty
    - Truncate each Recommendation to 512 characters with "…" suffix
    - Return nil when `result` is nil
    - _Requirements: 3.1, 3.2, 3.4, 3.5, 3.6, 6.4_
  - [ ]* 6.2 Write unit tests for RecommendationEnricher
    - Create `test/unit/reporting/recommendation_test.go`
    - Test OOMMemoryLimit: verify memory limit value appears in recommendation
    - Test CrashLoopAppError: verify container name and restart count appear
    - Test ImagePullFailure: verify image name and secret names appear
    - Test PVCNotBound: verify PVC name and StorageClass appear
    - Test SchedulingFailure: verify node selector / resource requests referenced
    - Test nil EvidenceSnapshot: verify generic fallback, no panic
    - Test nil Primary finding: verify nil returned gracefully
    - Test recommendation length truncation at 512 chars
    - _Requirements: 3.1, 3.4, 6.4_
  - [ ]* 6.3 Write property test for recommendation enrichment
    - **Property 4**: For OOMMemoryLimit rule with non-nil memory limit evidence, enriched Recommendation contains the memory limit string
    - **Property 6**: For any combination of nil fields in evidence, `Enrich()` does not panic
    - Tag: `Feature: reporting, Property 4/6`
    - _Requirements: 3.1, 7.4_

- [ ] 7. Implement ReportingEngine orchestrator
  - Create `internal/reporting/engine.go`
  - Implement `ReportingEngine` struct with `Config *config.Config`
  - Implement `Render(report *v1alpha1.IncidentReport) RenderResult`
  - Compose `SummaryBuilder`, `TimelineBuilder`, `RecommendationEnricher` calls
  - Return `RenderResult{Summary, Timeline, Diagnosis}` — never return error
  - _Requirements: 1.5, 2.6, 7.1, 7.4_

- [ ] 8. Checkpoint — run existing tests
  - Run `go build ./...` and `go test ./test/unit/...` to confirm all existing tests pass after the new API types and struct field reordering
  - Fix any compilation errors before proceeding

- [ ] 9. Wire ReportingEngine into PodReconciler
  - [ ] 9.1 Add `ReportingEngine` field to `PodReconciler`
    - Add `ReportingEngine *reporting.ReportingEngine` field to `PodReconciler` struct in `internal/controller/pod_reconciler.go`
    - _Requirements: 7.1_
  - [ ] 9.2 Insert step 9.7 into `Reconcile()`
    - Add step 9.7 between the diagnosis step (9.6) and the recovery re-fetch (step 10)
    - Call `r.ReportingEngine.Render(report)` and patch `Status.Summary`, `Status.Timeline`, and `Status.Diagnosis` (enriched) in a single status PATCH
    - Treat patch failure as non-fatal — log and continue to recovery evaluation
    - _Requirements: 1.5, 2.6, 7.3_
  - [ ] 9.3 Construct ReportingEngine in cmd/main.go
    - Add `reportingEngine := &reporting.ReportingEngine{Config: cfg}` before `PodReconciler` construction
    - Pass it to `PodReconciler`
    - _Requirements: 7.1_
  - [ ] 9.4 Add `MaxTimelineEvents` flag to cmd/main.go
    - Add `--timeline-events` flag with default 50
    - Wire into `cfg.MaxTimelineEvents`
    - _Requirements: 8.1_

- [ ] 10. Write integration test
  - [ ] 10.1 Create `test/integration/reporting_test.go`
    - Create an OOMKilled Pod and reconcile (reuse the pattern from existing integration tests)
    - Wait for `Status.Summary` to be non-empty (use `Eventually`)
    - Assert `Status.Summary` contains the workload name or trigger type string
    - Assert `Status.Timeline` contains at least one entry with `Reason = "IncidentDetected"`
    - If `Status.Diagnosis != nil && Status.Diagnosis.Primary != nil`, assert `Status.Diagnosis.Primary.Recommendation` is non-empty
    - _Requirements: 1.1, 2.1, 3.1_

- [ ] 11. Final checkpoint
  - Run `make generate manifests` to confirm CRD YAML is updated with summary, timeline fields, and Cause printer column
  - Run `make test` to confirm all unit, property, and integration tests pass
  - Run `go vet ./...` and `gofmt -l .` to ensure code quality and formatting
  - Verify `internal/reporting` does not import `internal/controller`, `internal/investigation`, `internal/evidence`, or `internal/diagnosis`

---

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP pass
- Property tests use `pgregory.net/rapid` (already in go.mod) with minimum 100 iterations
- The struct field reordering in `IncidentReportStatus` (task 2.2) is a non-breaking change to the CRD since all fields are optional — existing IncidentReports remain valid
- The `Cause` printer column uses `priority=1` so it appears only in `kubectl get -o wide`, preserving the clean default table view
- `RecommendationEnricher` deep-copies the DiagnosisResult before modifying; this is critical since the engine shares the input report with the recovery evaluator
- When Evidence or Diagnosis are nil (not yet collected / computed), ReportingEngine still produces a valid Summary and Timeline from whatever lifecycle fields are available

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2.1"] },
    { "id": 1, "tasks": ["2.2", "2.3"] },
    { "id": 2, "tasks": ["3"] },
    { "id": 3, "tasks": ["4.1", "5.1", "6.1"] },
    { "id": 4, "tasks": ["4.2", "4.3", "5.2", "5.3", "6.2", "6.3"] },
    { "id": 5, "tasks": ["7"] },
    { "id": 6, "tasks": ["8"] },
    { "id": 7, "tasks": ["9.1"] },
    { "id": 8, "tasks": ["9.2", "9.3", "9.4"] },
    { "id": 9, "tasks": ["10.1"] },
    { "id": 10, "tasks": ["11"] }
  ]
}
```
