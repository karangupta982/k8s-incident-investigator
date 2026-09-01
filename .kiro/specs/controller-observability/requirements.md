# Requirements Document

## Introduction

The controller observability feature exposes Prometheus-compatible operational metrics from the Kubernetes Incident Investigator controller itself. These metrics allow Platform Engineers and SREs to monitor the health and performance of the investigator as a component of their infrastructure — distinct from the workloads it investigates.

The metrics expose concepts such as how many incidents have been detected, how many have been diagnosed, how often the unknown path is taken, how long investigations take, and whether the controller is encountering errors.

The tech rules explicitly list the required metrics:

> incidents detected, investigations completed, unknown diagnoses, reconciliation errors, evidence collection failures, investigation duration, diagnosis rule matches

Controller-runtime already provides a `/metrics` endpoint via `sigs.k8s.io/controller-runtime/pkg/metrics`. This spec registers the investigator-specific metrics onto that existing server — no new HTTP server is required.

---

## Glossary

### Prometheus-Compatible Metric

A numeric time-series value exposed at the `/metrics` endpoint in the text-based Prometheus exposition format, suitable for scraping by Prometheus or any compatible system.

### Counter

A metric that only increases. Used for counts of events that have occurred (incidents detected, errors).

### Gauge

A metric that can increase or decrease. Used for current state values (active incidents).

### Histogram

A metric that samples observations and counts them in configurable buckets. Used for latency and duration measurements (investigation duration, reconciliation duration).

### Label

A key-value pair attached to a metric observation that allows filtering and grouping in queries (e.g., `trigger_type="OOMKilled"`, `rule_id="OOMMemoryLimit"`).

---

## Requirements

### Requirement 1: Expose Incident Detection Metrics

**User Story:** As a Platform Engineer, I want to see how many incidents the investigator has detected so that I can verify it is functioning and understand incident volume.

#### Acceptance Criteria

1. THE Investigator SHALL expose a counter metric `investigator_incidents_detected_total` that is incremented once each time a new `IncidentReport` is created.

2. THE metric SHALL carry a `trigger_type` label with the value of the `TriggerType` that caused the incident (e.g., `OOMKilled`, `CrashLoopBackOff`, `ImagePullBackOff`).

3. THE metric SHALL carry a `namespace` label with the namespace of the affected workload.

4. WHEN the controller starts, the counter SHALL be initialized to zero and SHALL NOT include labels for trigger types that have not yet been observed.

---

### Requirement 2: Expose Investigation Completion Metrics

**User Story:** As a Platform Engineer, I want to see how many investigations have completed so that I can track the resolution rate.

#### Acceptance Criteria

1. THE Investigator SHALL expose a counter metric `investigator_investigations_completed_total` that is incremented once each time an `IncidentReport` transitions to `Resolved`.

2. THE metric SHALL carry a `trigger_type` label.

3. THE Investigator SHALL expose a counter metric `investigator_unknown_diagnoses_total` that is incremented once each time an `IncidentReport` transitions to phase `Unknown` (no diagnosis rule matched sufficient evidence).

4. THE Investigator SHALL expose a counter metric `investigator_diagnosed_total` that is incremented once each time an `IncidentReport` transitions to phase `Diagnosed`, with a `rule_id` label identifying which rule produced the primary finding (e.g., `OOMMemoryLimit`).

---

### Requirement 3: Expose Active Incident Gauge

**User Story:** As a Platform Engineer, I want to see the current number of active (non-resolved) incidents so that I can understand the current incident load on my cluster.

#### Acceptance Criteria

1. THE Investigator SHALL expose a gauge metric `investigator_active_incidents` that reflects the current count of `IncidentReport` objects with phase not equal to `Resolved`.

2. THE gauge SHALL be updated on every reconciliation cycle that changes an incident's phase.

3. THE metric SHALL carry a `namespace` label.

---

### Requirement 4: Expose Error Metrics

**User Story:** As a Platform Engineer, I want to see when the controller encounters reconciliation or evidence collection errors so that I can detect problems with the investigator itself.

#### Acceptance Criteria

1. THE Investigator SHALL expose a counter metric `investigator_reconciliation_errors_total` that is incremented each time `PodReconciler.Reconcile()` returns a non-nil error or calls `r.Log.Error()` for an unrecoverable operation.

2. THE `investigator_reconciliation_errors_total` metric SHALL carry an `error_type` label with a short identifier of the error category (e.g., `"api_error"`, `"status_patch_failed"`, `"evidence_collection"`).

3. THE Investigator SHALL expose a counter metric `investigator_evidence_collection_failures_total` that is incremented once for each `CollectionError` recorded in `EvidenceSnapshot.CollectionErrors` after an evidence collection cycle.

4. THE `investigator_evidence_collection_failures_total` metric SHALL carry a `source` label matching the `CollectionError.Source` field (e.g., `"pod"`, `"node"`, `"events"`, `"logs/app"`).

---

### Requirement 5: Expose Duration Metrics

**User Story:** As a Platform Engineer, I want latency histograms for key operations so that I can detect performance regressions in the investigator.

#### Acceptance Criteria

1. THE Investigator SHALL expose a histogram metric `investigator_investigation_duration_seconds` measuring the elapsed time from incident `StartedAt` to `ResolvedAt` for each resolved incident.

2. THE histogram SHALL use buckets appropriate for investigation durations: `[30, 60, 120, 300, 600, 1800, 3600]` seconds.

3. THE Investigator SHALL expose a histogram metric `investigator_evidence_collection_duration_seconds` measuring the elapsed time for each evidence collection cycle.

4. THE histogram SHALL use buckets appropriate for collection durations: `[0.5, 1, 2, 5, 10, 30, 60]` seconds.

5. Both histograms SHALL carry a `trigger_type` label.

---

### Requirement 6: Expose Diagnosis Rule Match Metrics

**User Story:** As a Platform Engineer, I want to know which diagnosis rules are firing most frequently so that I can understand the failure patterns in my cluster.

#### Acceptance Criteria

1. THE Investigator SHALL expose a counter metric `investigator_diagnosis_rule_matches_total` that is incremented each time a diagnosis rule produces a finding during evaluation, regardless of whether the finding becomes the primary diagnosis.

2. THE metric SHALL carry a `rule_id` label (e.g., `OOMMemoryLimit`, `PVCNotBound`) and a `confidence` label (`High`, `Medium`, `Low`).

---

### Requirement 7: Use Prometheus-Compatible Implementation

**User Story:** As a Platform Engineer, I want the metrics to be compatible with standard Prometheus scraping so that I can use them with any Prometheus-compatible monitoring stack.

#### Acceptance Criteria

1. THE Investigator SHALL register all metrics using `sigs.k8s.io/controller-runtime/pkg/metrics` (the controller-runtime metrics registry) so they are exposed on the existing `/metrics` endpoint.

2. THE Investigator SHALL NOT start a second HTTP server for metrics.

3. ALL metric names SHALL use the `investigator_` prefix.

4. ALL metric help strings SHALL be descriptive and non-empty.

5. THE metrics SHALL be registered in `cmd/main.go` or an `init()` function in the metrics package, ensuring they are available before the first reconciliation.

---

### Requirement 8: Metrics Are Safe and Non-Leaking

**User Story:** As a Platform Engineer, I want metric labels to never expose sensitive values so that the metrics endpoint cannot be a source of information leakage.

#### Acceptance Criteria

1. Metric labels SHALL NOT include Pod names, workload names, image names, or other values that could contain sensitive or high-cardinality information that would cause label explosion.

2. The `namespace` label is permitted as it is operator-controlled and bounded in well-managed clusters.

3. The `trigger_type`, `rule_id`, `confidence`, `source`, and `error_type` labels are permitted as they are drawn from fixed enumeration sets.

---

## Out of Scope

- Grafana dashboard JSON (future enhancement)
- AlertManager rule definitions (future enhancement)
- Prometheus Operator `ServiceMonitor` (future enhancement — operators vary by cluster)
- Metrics about the investigated workloads themselves (that is Prometheus integration, out of MVP scope)
- Distributed tracing
