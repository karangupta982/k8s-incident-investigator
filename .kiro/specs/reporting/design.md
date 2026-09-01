# Design Document: Reporting and Presentation

## Overview

The reporting layer is the final stage of the investigation pipeline. It takes the existing `EvidenceSnapshot` and `DiagnosisResult` already stored in the `IncidentReport` status and transforms them into two additional status fields: an `IncidentSummary` (concise human-readable description) and a `Timeline` (chronological event sequence). It also enriches `DiagnosisFinding.Recommendation` fields with evidence-specific values.

The reporting layer is a **pure function over IncidentReport.Status**. It calls no Kubernetes API, requires no new RBAC permissions, and introduces no new failure modes.

### Pipeline Position

```
Step 9.5  (evidence-collection spec) → EvidenceSnapshot stored in Status.Evidence
Step 9.6  (diagnosis-engine spec)    → DiagnosisResult stored in Status.Diagnosis
Step 9.7  (this spec)                → ReportingEngine.Render(report) → Summary + Timeline patched
Step 10   (foundation spec)          → Re-fetch report
Step 11   (foundation spec)          → Recovery evaluation
```

Step 9.7 runs after the diagnosis step and before the recovery re-fetch. The output is a single status PATCH combining `Status.Summary` and `Status.Timeline`.

---

## Architecture

### Dependency Direction

```
PodReconciler
      │
      └── ReportingEngine              (internal/reporting)
               │
               ├── TimelineBuilder     ← assembles Timeline from status fields
               ├── SummaryBuilder      ← assembles IncidentSummary from status fields
               └── RecommendationEnricher ← populates DiagnosisFinding.Recommendation
```

Forbidden imports:
```
internal/reporting  MUST NOT import  internal/controller
internal/reporting  MUST NOT import  internal/investigation
internal/reporting  MUST NOT import  internal/evidence
internal/reporting  MUST NOT import  internal/diagnosis
internal/reporting  MAY import       api/v1alpha1
internal/reporting  MAY import       standard library packages
```

This keeps the reporting layer independently testable without any Kubernetes cluster or mock clients.

---

## Components and Interfaces

### ReportingEngine

```go
// package internal/reporting

// ReportingEngine assembles the IncidentSummary and Timeline from the existing
// IncidentReport status fields and enriches DiagnosisFinding recommendations.
// It is a pure function — it does not call the Kubernetes API.
type ReportingEngine struct {
    Config *config.Config
}

// RenderResult holds the assembled reporting output.
// The controller applies this to the IncidentReport status via a single PATCH.
type RenderResult struct {
    Summary  string
    Timeline []v1alpha1.TimelineEvent
    // Diagnosis is a copy of the existing DiagnosisResult with Recommendation
    // fields populated by the RecommendationEnricher.
    // Nil when no DiagnosisResult is present.
    Diagnosis *v1alpha1.DiagnosisResult
}

// Render produces the RenderResult for the given report.
// It never returns an error — partial output is produced gracefully when fields are nil.
func (e *ReportingEngine) Render(report *v1alpha1.IncidentReport) RenderResult
```

### TimelineBuilder

```go
// TimelineBuilder assembles a chronologically ordered, bounded list of TimelineEvents
// from the IncidentReport status fields.
type TimelineBuilder struct {
    MaxEvents int
}

func (b *TimelineBuilder) Build(report *v1alpha1.IncidentReport) []v1alpha1.TimelineEvent
```

Timeline sources (in priority order for same-timestamp tie-breaking):
1. Controller lifecycle events: `StartedAt` ("Incident detected"), `StabilityStartedAt` ("Workload became healthy"), `ResolvedAt` ("Incident resolved")
2. Container terminations from `Status.Evidence.Pod.Containers[*]` where state is "terminated"
3. Kubernetes Events from `Status.Evidence.Events`, keyed by `LastTime`

### SummaryBuilder

```go
// SummaryBuilder produces a concise IncidentSummary string from the IncidentReport status.
type SummaryBuilder struct {
    MaxLength int // default 2048
}

func (b *SummaryBuilder) Build(report *v1alpha1.IncidentReport) string
```

The summary format varies by phase:

**Diagnosed:**
```
Workload: <kind>/<namespace>/<name>
Phase: Diagnosed (Confidence: <High|Medium|Low>)
Cause: <Primary.Cause>
Trigger: <TriggerType> on container <ContainerName>
Affected Pods: <N>
Started: <StartedAt>
```

**Unknown:**
```
Workload: <kind>/<namespace>/<name>
Phase: Unknown — <UnknownReason>
Trigger: <TriggerType>
Affected Pods: <N>
Evidence collected but no known failure pattern matched.
Suggested next step: <derived from available evidence>
```

**Investigating (no diagnosis yet):**
```
Workload: <kind>/<namespace>/<name>
Phase: Investigating
Trigger: <TriggerType>
Affected Pods: <N>
Investigation in progress.
```

**Resolved:**
```
Workload: <kind>/<namespace>/<name>
Phase: Resolved
Cause: <Primary.Cause if available, otherwise "Unknown">
Duration: <ResolvedAt - StartedAt>
Resolved: <ResolvedAt>
```

### RecommendationEnricher

```go
// RecommendationEnricher populates DiagnosisFinding.Recommendation fields with
// evidence-specific values from the EvidenceSnapshot.
// It returns a deep copy of the DiagnosisResult with Recommendations filled in.
// It does not modify the original DiagnosisResult.
type RecommendationEnricher struct{}

func (r *RecommendationEnricher) Enrich(
    result *v1alpha1.DiagnosisResult,
    snapshot *v1alpha1.EvidenceSnapshot,
) *v1alpha1.DiagnosisResult
```

Recommendation templates per rule (with evidence substitution):

| Rule ID | Template |
|---------|----------|
| `OOMMemoryLimit` | `Container "<name>" was OOMKilled with a memory limit of <limit>. Investigate memory consumption in the application. If the workload legitimately requires more memory, increase the memory limit above <limit>.` |
| `NodeMemoryPressure` | `Node <nodeName> reported MemoryPressure=True. The container may have been killed due to node-level memory contention. Check node memory utilisation and consider redistributing workloads or adding nodes.` |
| `CrashLoopAppError` | `Container "<name>" has restarted <N> times with exit code <code>. Review container logs for the application error causing the non-zero exit. Ensure the container's entrypoint handles termination signals correctly.` |
| `CrashLoopOOMExit` | `Container "<name>" is crash-looping after repeated OOMKills (exit code 137, <N> restarts). Investigate application memory consumption and consider increasing the memory limit above <limit>.` |
| `ImagePullFailure` | `Image "<image>" could not be pulled. Verify the image name, tag, and registry are correct. If the registry requires authentication, check that ImagePullSecrets <secrets> are valid and not expired.` |
| `MissingConfigReference` | `Container "<name>" cannot start because a ConfigMap, Secret, or environment variable source it references does not exist or is invalid. Check the container's env, envFrom, and volume references.` |
| `PVCNotBound` | `PersistentVolumeClaim "<pvcName>" is in phase <phase>. The workload cannot start until the PVC is bound. Check the StorageClass "<storageClass>" and verify a PersistentVolume is available with sufficient capacity.` |
| `PVCMountError` | `PVC "<pvcName>" is bound but the mount is failing. Check the CSI driver logs and node events for mount errors. Verify the node has access to the underlying storage.` |
| `SchedulingFailure` | `Pod could not be scheduled. <specific reason based on constraints: node selector / resource requests / tolerations>. Review node availability and verify resource requests do not exceed available capacity.` |
| `ProbeFailure` | `The <liveness|readiness> probe for container "<name>" is failing (threshold: <N> failures). Check the probe configuration (<type>: <path/port>) and the application health endpoint. Review container logs for startup errors.` |

When an evidence field needed for substitution is unavailable (nil or empty), the template falls back to a generic description without the specific value.

---

## Data Models

### New fields on Config

```go
// MaxTimelineEvents is the maximum number of events stored in the incident timeline.
// Default: 50.
MaxTimelineEvents int
```

### New API Types — api/v1alpha1/reporting_types.go

```go
// TimelineEvent represents a single significant moment during an incident.
//
// +kubebuilder:object:generate=true
type TimelineEvent struct {
    // Timestamp is when this event occurred.
    Timestamp metav1.Time `json:"timestamp"`

    // Source identifies where the event came from:
    // "controller" for lifecycle events, "container" for termination events,
    // "kubernetes-event" for events from the Kubernetes Events API.
    Source string `json:"source"`

    // Reason is the short machine-readable reason string (mirrors Kubernetes Event.Reason
    // for kubernetes-event sources; uses "IncidentDetected", "WorkloadHealthy",
    // "IncidentResolved" for controller sources).
    Reason string `json:"reason"`

    // Message is the human-readable description, truncated to 256 characters.
    Message string `json:"message"`
}
```

### New fields on IncidentReportStatus

Add these two fields to `IncidentReportStatus` in `api/v1alpha1/incidentreport_types.go`:

```go
// Summary is a concise human-readable summary of the incident.
// Updated on every reconciliation cycle.
// +optional
Summary string `json:"summary,omitempty"`

// Timeline is a chronologically ordered list of significant events during the incident.
// Bounded by MaxTimelineEvents. Updated on every reconciliation cycle.
// +optional
Timeline []TimelineEvent `json:"timeline,omitempty"`
```

### Updated kubectl print columns

Add a new `Cause` printer column to the `IncidentReport` type:

```go
// +kubebuilder:printcolumn:name="Cause",type=string,JSONPath=".status.diagnosis.primary.cause",priority=1
```

Using `priority=1` places this column in the "wide" output (`kubectl get -o wide`) to avoid cluttering the default view. The existing six columns remain at priority 0 (default view).

---

## Status Field Ordering for kubectl describe

The JSON tag ordering in `IncidentReportStatus` controls the render order in `kubectl describe`. The desired top-to-bottom order is:

1. `phase`
2. `summary` ← new
3. `startedAt`
4. `resolvedAt`
5. `stabilityStartedAt`
6. `lastFailureAt`
7. `failureCount`
8. `trigger`
9. `workloadOwnerResolved`
10. `affectedPods`
11. `diagnosis` ← from diagnosis spec
12. `timeline` ← new
13. `evidence` ← from evidence spec
14. `conditions`

This requires reordering the struct fields in `incidentreport_types.go` so JSON serialization (and hence `kubectl describe`) follows the intended order. Go struct field order determines JSON serialization order when using `encoding/json`.

---

## Integration Point with PodReconciler

### Step 9.7 Insertion

```go
// ---- Step 9.7: Generate report summary and timeline ----
renderResult := r.ReportingEngine.Render(report)

reportBase := report.DeepCopy()
report.Status.Summary = renderResult.Summary
report.Status.Timeline = renderResult.Timeline
if renderResult.Diagnosis != nil {
    report.Status.Diagnosis = renderResult.Diagnosis
}
if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(reportBase)); patchErr != nil {
    // Non-fatal: the next reconciliation will attempt reporting again.
    log.Error(patchErr, "failed to patch reporting output, will retry on next reconciliation")
}
```

### PodReconciler field addition

```go
type PodReconciler struct {
    // ... existing fields ...
    ReportingEngine *reporting.ReportingEngine
}
```

Constructed in `cmd/main.go`:
```go
reportingEngine := &reporting.ReportingEngine{Config: cfg}
```

---

## Unknown Phase Suggested Investigation Steps

When phase is `Unknown`, the `SummaryBuilder` derives a suggested next step from the available evidence:

| Available evidence | Suggested next step |
|--------------------|---------------------|
| Logs available (non-empty Lines) | "Review container logs for application errors." |
| Logs unavailable (UnavailableReason set) | "Container logs unavailable (<reason>). Inspect node events and Pod conditions." |
| Node MemoryPressure = True | "Node is under memory pressure. Check node memory utilisation." |
| No evidence at all (CollectionErrors only) | "Evidence collection encountered errors. Check controller logs and verify RBAC permissions." |
| Default (evidence present, no match) | "No known failure pattern matched the collected evidence. Review Evidence and Events sections manually." |

---

## Correctness Properties

### Property 1: Summary Always Produced

For any `IncidentReport` in any phase with any combination of nil/non-nil status fields, `SummaryBuilder.Build()` SHALL return a non-empty string.

### Property 2: Timeline Is Bounded

For any `IncidentReport` with N total event sources, the timeline returned by `TimelineBuilder.Build()` SHALL contain at most `MaxTimelineEvents` entries.

### Property 3: Timeline Is Chronologically Ordered

For any `IncidentReport`, the `TimelineEvent` entries produced by `TimelineBuilder.Build()` SHALL be ordered by `Timestamp` ascending. For equal timestamps, controller lifecycle events come before container terminations, which come before Kubernetes Events.

### Property 4: Recommendation Templates Reference Evidence Values

For any `DiagnosisResult.Primary` with `RuleID = "OOMMemoryLimit"` and a non-nil `EvidenceSnapshot.Pod.Containers[0].ResourceLimits["memory"]`, the enriched `Recommendation` SHALL contain the memory limit value string.

### Property 5: Rendering Is Idempotent

For any fixed `IncidentReport` status, calling `ReportingEngine.Render()` twice produces identical `RenderResult` values.

### Property 6: No Panic on Nil Fields

For any `IncidentReport` with any combination of nil status sub-fields (`Evidence`, `Diagnosis`, `Trigger`, `Spec.Workload`), `ReportingEngine.Render()` SHALL NOT panic.

---

## Testing Strategy

### Unit Tests (`test/unit/reporting/`)

- `summary_builder_test.go` — one test per phase (Diagnosed, Unknown, Investigating, Resolved), nil workload fallback, nil diagnosis fallback
- `timeline_builder_test.go` — ordering correctness, bounding at MaxTimelineEvents, mixed sources, same-timestamp tie-breaking
- `recommendation_enricher_test.go` — template substitution per rule, nil evidence fields produce graceful fallback

### Property-Based Tests

- Property 2: random event counts → timeline always ≤ MaxTimelineEvents
- Property 3: random event timestamps → timeline always chronologically ordered
- Property 5: same report → same RenderResult on N calls
- Property 6: random nil field combinations → no panic

### Integration Test

`test/integration/reporting_test.go` — create an OOMKilled Pod, reconcile, verify:
- `Status.Summary` is non-empty
- `Status.Timeline` contains at least the "Incident detected" entry
- `Status.Diagnosis.Primary.Recommendation` is non-empty and references "OOMKilled"
