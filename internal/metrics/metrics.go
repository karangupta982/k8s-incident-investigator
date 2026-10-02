/*
Copyright 2024 The Kubernetes Incident Investigator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package metrics registers Prometheus-compatible operational metrics for the
// Kubernetes Incident Investigator controller.
// All metrics are exposed on the existing controller-runtime /metrics endpoint.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// IncidentsDetected counts new IncidentReports created, labelled by trigger type and namespace.
	IncidentsDetected = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_incidents_detected_total",
			Help: "Total number of new IncidentReports created, by trigger type and namespace.",
		},
		[]string{"trigger_type", "namespace"},
	)

	// InvestigationsCompleted counts IncidentReports that transitioned to Resolved.
	InvestigationsCompleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_investigations_completed_total",
			Help: "Total number of IncidentReports that transitioned to Resolved.",
		},
		[]string{"trigger_type"},
	)

	// UnknownDiagnoses counts IncidentReports where no diagnosis rule matched.
	UnknownDiagnoses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_unknown_diagnoses_total",
			Help: "Total number of IncidentReports where no diagnosis rule matched.",
		},
		[]string{"trigger_type"},
	)

	// DiagnosedTotal counts IncidentReports that transitioned to Diagnosed.
	DiagnosedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_diagnosed_total",
			Help: "Total number of IncidentReports that transitioned to Diagnosed, by rule ID.",
		},
		[]string{"rule_id", "trigger_type"},
	)

	// ActiveIncidents is the current number of non-Resolved IncidentReports per namespace.
	ActiveIncidents = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "investigator_active_incidents",
			Help: "Current number of non-Resolved IncidentReports, by namespace.",
		},
		[]string{"namespace"},
	)

	// ReconciliationErrors counts reconciliation errors by category.
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_reconciliation_errors_total",
			Help: "Total number of reconciliation errors encountered by the controller.",
		},
		[]string{"error_type"},
	)

	// EvidenceCollectionFailures counts individual evidence source failures.
	EvidenceCollectionFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "investigator_evidence_collection_failures_total",
			Help: "Total number of individual evidence source collection failures.",
		},
		[]string{"source"},
	)

	// InvestigationDuration observes elapsed time from StartedAt to ResolvedAt.
	InvestigationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "investigator_investigation_duration_seconds",
			Help:    "Duration from incident StartedAt to ResolvedAt in seconds.",
			Buckets: []float64{30, 60, 120, 300, 600, 1800, 3600},
		},
		[]string{"trigger_type"},
	)

	// EvidenceCollectionDuration observes the duration of each evidence collection cycle.
	EvidenceCollectionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "investigator_evidence_collection_duration_seconds",
			Help:    "Duration of each evidence collection cycle in seconds.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"trigger_type"},
	)

	// DiagnosisRuleMatches counts rule match events by rule ID and confidence.
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

// ── RecorderInterface ─────────────────────────────────────────────────────────

// RecorderInterface is the interface satisfied by both Recorder and NoOpRecorder.
// Injecting this into PodReconciler allows tests to use NoOpRecorder.
type RecorderInterface interface {
	RecordIncidentDetected(triggerType, namespace string)
	RecordInvestigationCompleted(triggerType string, durationSeconds float64)
	RecordActiveDecrement(namespace string)
	RecordUnknownDiagnosis(triggerType string)
	RecordDiagnosed(ruleID, triggerType string)
	RecordReconciliationError(errorType string)
	RecordEvidenceCollectionFailure(source string)
	RecordEvidenceCollectionDuration(triggerType string, durationSeconds float64)
	RecordDiagnosisRuleMatch(ruleID, confidence string)
}

// ── Recorder ─────────────────────────────────────────────────────────────────

// Recorder implements RecorderInterface using the real Prometheus metric vectors.
type Recorder struct{}

func (r *Recorder) RecordIncidentDetected(triggerType, namespace string) {
	IncidentsDetected.WithLabelValues(triggerType, namespace).Inc()
	ActiveIncidents.WithLabelValues(namespace).Inc()
}

func (r *Recorder) RecordInvestigationCompleted(triggerType string, durationSeconds float64) {
	InvestigationsCompleted.WithLabelValues(triggerType).Inc()
	InvestigationDuration.WithLabelValues(triggerType).Observe(durationSeconds)
}

func (r *Recorder) RecordActiveDecrement(namespace string) {
	ActiveIncidents.WithLabelValues(namespace).Dec()
}

func (r *Recorder) RecordUnknownDiagnosis(triggerType string) {
	UnknownDiagnoses.WithLabelValues(triggerType).Inc()
}

func (r *Recorder) RecordDiagnosed(ruleID, triggerType string) {
	DiagnosedTotal.WithLabelValues(ruleID, triggerType).Inc()
}

func (r *Recorder) RecordReconciliationError(errorType string) {
	ReconciliationErrors.WithLabelValues(errorType).Inc()
}

func (r *Recorder) RecordEvidenceCollectionFailure(source string) {
	EvidenceCollectionFailures.WithLabelValues(source).Inc()
}

func (r *Recorder) RecordEvidenceCollectionDuration(triggerType string, durationSeconds float64) {
	EvidenceCollectionDuration.WithLabelValues(triggerType).Observe(durationSeconds)
}

func (r *Recorder) RecordDiagnosisRuleMatch(ruleID, confidence string) {
	DiagnosisRuleMatches.WithLabelValues(ruleID, confidence).Inc()
}

// ── NoOpRecorder ──────────────────────────────────────────────────────────────

// NoOpRecorder discards all observations. Used in unit and integration tests.
type NoOpRecorder struct{}

func (r *NoOpRecorder) RecordIncidentDetected(_, _ string)                   {}
func (r *NoOpRecorder) RecordInvestigationCompleted(_ string, _ float64)     {}
func (r *NoOpRecorder) RecordActiveDecrement(_ string)                       {}
func (r *NoOpRecorder) RecordUnknownDiagnosis(_ string)                      {}
func (r *NoOpRecorder) RecordDiagnosed(_, _ string)                          {}
func (r *NoOpRecorder) RecordReconciliationError(_ string)                   {}
func (r *NoOpRecorder) RecordEvidenceCollectionFailure(_ string)             {}
func (r *NoOpRecorder) RecordEvidenceCollectionDuration(_ string, _ float64) {}
func (r *NoOpRecorder) RecordDiagnosisRuleMatch(_, _ string)                 {}
