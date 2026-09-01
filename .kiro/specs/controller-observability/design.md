# Design Document: Controller Observability

## Overview

This spec adds Prometheus-compatible operational metrics to the Kubernetes Incident Investigator using controller-runtime's built-in metrics registry. All metrics are registered on the existing `/metrics` endpoint — no new HTTP server is needed.

---

## Architecture

### Integration Point

```
PodReconciler.Reconcile()
      │
      ├── investigator_incidents_detected_total.Inc()        — on new IncidentReport creation
      ├── investigator_reconciliation_errors_total.Inc()     — on Reconcile() error
      ├── investigator_evidence_collection_failures_total    — per CollectionError after step 9.5
      ├── investigator_diagnosis_rule_matches_total          — per DiagnosisFinding after step 9.6
      ├── investigator_diagnosed_total / unknown_total       — on phase transition to Diagnosed/Unknown
      ├── investigator_investigations_completed_total        — on transition to Resolved
      ├── investigator_active_incidents (gauge update)       — on any phase change
      └── investigator_investigation_duration_seconds        — on transition to Resolved

EvidenceOrchestrator.Collect()
      └── investigator_evidence_collection_duration_seconds  — time the collection took
```

### Package Structure

```
internal/metrics/
└── metrics.go    — metric definitions and registration
```

The `PodReconciler` receives a `*metrics.Recorder` (or calls package-level functions — see below) injected at construction time. This keeps the controller testable by replacing the recorder with a no-op in unit tests.

---

## Metric Definitions

All metrics are defined in `internal/metrics/metrics.go` and registered via `func init()` using `metrics.Registry.MustRegister()` from `sigs.k8s.io/controller-runtime/pkg/metrics`.

```go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
    IncidentsDetected = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_incidents_detected_total",
            Help: "Total number of new IncidentReports created, by trigger type and namespace.",
        },
        []string{"trigger_type", "namespace"},
    )

    InvestigationsCompleted = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_investigations_completed_total",
            Help: "Total number of IncidentReports that transitioned to Resolved, by trigger type.",
        },
        []string{"trigger_type"},
    )

    UnknownDiagnoses = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_unknown_diagnoses_total",
            Help: "Total number of IncidentReports where no diagnosis rule matched sufficient evidence.",
        },
        []string{"trigger_type"},
    )

    DiagnosedTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_diagnosed_total",
            Help: "Total number of IncidentReports that transitioned to Diagnosed, by rule and trigger type.",
        },
        []string{"rule_id", "trigger_type"},
    )

    ActiveIncidents = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "investigator_active_incidents",
            Help: "Current number of non-Resolved IncidentReports, by namespace.",
        },
        []string{"namespace"},
    )

    ReconciliationErrors = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_reconciliation_errors_total",
            Help: "Total number of reconciliation errors encountered by the controller.",
        },
        []string{"error_type"},
    )

    EvidenceCollectionFailures = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_evidence_collection_failures_total",
            Help: "Total number of individual evidence source collection failures.",
        },
        []string{"source"},
    )

    InvestigationDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "investigator_investigation_duration_seconds",
            Help:    "Duration from incident StartedAt to ResolvedAt in seconds.",
            Buckets: []float64{30, 60, 120, 300, 600, 1800, 3600},
        },
        []string{"trigger_type"},
    )

    EvidenceCollectionDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "investigator_evidence_collection_duration_seconds",
            Help:    "Duration of each evidence collection cycle in seconds.",
            Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
        },
        []string{"trigger_type"},
    )

    DiagnosisRuleMatches = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "investigator_diagnosis_rule_matches_total",
            Help: "Total number of diagnosis rule matches, by rule ID and confidence level.",
        },
        []string{"rule_id", "confidence"},
    )
)

func init() {
    crmetrics.Registry.MustRegister(
        IncidentsDetected,
        InvestigationsCompleted,
        UnknownDiagnoses,
        DiagnosedTotal,
        ActiveIncidents,
        ReconciliationErrors,
        EvidenceCollectionFailures,
        InvestigationDuration,
        EvidenceCollectionDuration,
        DiagnosisRuleMatches,
    )
}
```

### Recorder Pattern (for testability)

```go
// Recorder wraps all metric vectors and allows injection of a no-op recorder in tests.
type Recorder struct{}

// RecordIncidentDetected increments the incidents detected counter.
func (r *Recorder) RecordIncidentDetected(triggerType, namespace string) {
    IncidentsDetected.WithLabelValues(triggerType, namespace).Inc()
}

// RecordInvestigationCompleted increments the completed counter and observes duration.
func (r *Recorder) RecordInvestigationCompleted(triggerType string, durationSeconds float64) {
    InvestigationsCompleted.WithLabelValues(triggerType).Inc()
    InvestigationDuration.WithLabelValues(triggerType).Observe(durationSeconds)
}

// RecordUnknownDiagnosis increments the unknown diagnosis counter.
func (r *Recorder) RecordUnknownDiagnosis(triggerType string) {
    UnknownDiagnoses.WithLabelValues(triggerType).Inc()
}

// RecordDiagnosed increments the diagnosed counter.
func (r *Recorder) RecordDiagnosed(ruleID, triggerType string) {
    DiagnosedTotal.WithLabelValues(ruleID, triggerType).Inc()
}

// SetActiveIncidents sets the gauge for a namespace.
func (r *Recorder) SetActiveIncidents(namespace string, count float64) {
    ActiveIncidents.WithLabelValues(namespace).Set(count)
}

// RecordReconciliationError increments the reconciliation error counter.
func (r *Recorder) RecordReconciliationError(errorType string) {
    ReconciliationErrors.WithLabelValues(errorType).Inc()
}

// RecordEvidenceCollectionFailure increments per-source failure counter.
func (r *Recorder) RecordEvidenceCollectionFailure(source string) {
    EvidenceCollectionFailures.WithLabelValues(source).Inc()
}

// RecordEvidenceCollectionDuration observes the duration of a collection cycle.
func (r *Recorder) RecordEvidenceCollectionDuration(triggerType string, durationSeconds float64) {
    EvidenceCollectionDuration.WithLabelValues(triggerType).Observe(durationSeconds)
}

// RecordDiagnosisRuleMatch increments the rule match counter.
func (r *Recorder) RecordDiagnosisRuleMatch(ruleID, confidence string) {
    DiagnosisRuleMatches.WithLabelValues(ruleID, confidence).Inc()
}

// NoOpRecorder is a Recorder that discards all observations. Used in tests.
type NoOpRecorder struct{}
// ... (implements the same interface with empty bodies)
```

---

## Integration with PodReconciler

```go
type PodReconciler struct {
    // ... existing fields ...
    Metrics *metrics.Recorder
}
```

### Where each metric is recorded

| Metric | Location in Reconcile() |
|--------|------------------------|
| `incidents_detected_total` | After successful `r.Create(ctx, report)` in step 8 |
| `investigations_completed_total` | After `r.Transitioner.Transition()` succeeds in step 11 |
| `unknown_diagnoses_total` | After step 9.6 sets `Phase = Unknown` |
| `diagnosed_total` | After step 9.6 sets `Phase = Diagnosed` |
| `active_incidents` | After any phase change (inc on create, dec on resolve) |
| `reconciliation_errors_total` | At the top of error return paths |
| `evidence_collection_failures_total` | After step 9.5, iterate `snapshot.CollectionErrors` |
| `evidence_collection_duration_seconds` | After step 9.5, observe elapsed time |
| `investigation_duration_seconds` | In `RecordInvestigationCompleted`, compute `ResolvedAt - StartedAt` |
| `diagnosis_rule_matches_total` | After step 9.6, iterate all findings (primary + contributing) |

---

## Testing Strategy

### Unit Tests

`test/unit/metrics_test.go`:
- Verify counter increments correctly for each recorder method
- Verify histogram observes correct values
- Verify gauge set/dec operations
- Use `prometheus/testutil` package for assertions:
  ```go
  testutil.CollectAndCompare(metrics.IncidentsDetected, strings.NewReader(expected))
  ```

### Integration Test

No dedicated integration test is needed — the metrics endpoint is exercised as a side effect of the existing integration tests once metrics are wired in. A simple smoke test asserting the `/metrics` endpoint returns HTTP 200 and contains `investigator_incidents_detected_total` is sufficient.

---

## Correctness Properties

### Property 1: Counter Monotonicity
For any sequence of reconcile calls, `incidents_detected_total` SHALL be non-decreasing.

### Property 2: Active Gauge Consistency
At any point, `active_incidents{namespace=X}` SHALL equal the number of non-Resolved IncidentReports in namespace X in etcd (eventual consistency — may lag by at most one reconcile cycle).
