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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IncidentReportSpec contains the immutable identity of the incident.
// Fields in spec are set at creation and not modified during the lifecycle.
//
// Note: The incident namespace is derived from metadata.namespace (standard Kubernetes
// convention for namespace-scoped resources). There is no spec.namespace field — storing
// the namespace in spec would create a second, potentially conflicting source of truth.
type IncidentReportSpec struct {
	// Workload is the workload associated with the incident.
	// Set when ownership can be resolved; may be empty if resolution failed.
	// +optional
	Workload *WorkloadRef `json:"workload,omitempty"`
}

// IncidentReportStatus holds the dynamic investigation state of the incident.
// Updated throughout the incident lifecycle via the status subresource.
type IncidentReportStatus struct {
	// Phase is the current lifecycle state of the incident.
	// +optional
	Phase IncidentPhase `json:"phase,omitempty"`

	// Summary is a concise human-readable summary of the incident.
	// Updated on every reconciliation cycle.
	// +optional
	Summary string `json:"summary,omitempty"`

	// StartedAt is the time the incident was first detected.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// ResolvedAt is the time the incident was resolved.
	// Set only when Phase = Resolved.
	// +optional
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

	// StabilityStartedAt records when the workload first became healthy after the incident.
	// Used to track the stability period without relying on in-memory state.
	// Reset to nil if a new failure occurs during the stability window.
	// +optional
	StabilityStartedAt *metav1.Time `json:"stabilityStartedAt,omitempty"`

	// AffectedPods is the list of Pods associated with this incident.
	// Pods are appended; existing entries are preserved even after Pod deletion.
	// +optional
	AffectedPods []PodRef `json:"affectedPods,omitempty"`

	// Trigger is the primary failure signal that created the incident.
	// +optional
	Trigger *TriggerInfo `json:"trigger,omitempty"`

	// WorkloadOwnerResolved indicates whether workload ownership was successfully determined.
	// False means the incident is tracked at Pod level (see AffectedPods).
	// +optional
	WorkloadOwnerResolved bool `json:"workloadOwnerResolved,omitempty"`

	// FailureCount tracks the number of distinct failure observations associated with this
	// incident. For state-based triggers, this increments only when a new unique failure
	// signature is observed (new container affected, or reoccurrence after recovery).
	// For event-based triggers, this is set to the current Kubernetes Event.count value.
	// +optional
	FailureCount int32 `json:"failureCount,omitempty"`

	// LastFailureAt records the time the most recent failure was observed.
	// +optional
	LastFailureAt *metav1.Time `json:"lastFailureAt,omitempty"`

	// Evidence holds the collected investigation evidence.
	// Populated after initial trigger detection; refreshed on subsequent reconciliations.
	// +optional
	Evidence *EvidenceSnapshot `json:"evidence,omitempty"`

	// Diagnosis holds the result of the diagnosis engine evaluation.
	// Set after evidence collection; replaced on each reconciliation cycle.
	// +optional
	Diagnosis *DiagnosisResult `json:"diagnosis,omitempty"`

	// Timeline is a chronologically ordered list of significant events during the incident.
	// Bounded by MaxTimelineEvents. Updated on every reconciliation cycle.
	// +optional
	Timeline []TimelineEvent `json:"timeline,omitempty"`

	// CorrelatedEvidence holds derived signals computed from the EvidenceSnapshot.
	// Updated after each evidence collection cycle.
	// +optional
	CorrelatedEvidence *CorrelatedEvidence `json:"correlatedEvidence,omitempty"`

	// Conditions provides standard Kubernetes condition semantics for the incident state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// IncidentReport is the Schema for the incidentreports API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=`api-approved.kubernetes.io=unapproved, is a demo project for local development`
// +kubebuilder:resource:shortName=ir,categories=investigator
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=".spec.workload.name"
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=".spec.workload.kind"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Trigger",type=string,JSONPath=".status.trigger.type"
// +kubebuilder:printcolumn:name="Started",type=date,JSONPath=".status.startedAt"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Cause",type=string,JSONPath=".status.diagnosis.primary.cause",priority=1
type IncidentReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IncidentReportSpec   `json:"spec,omitempty"`
	Status IncidentReportStatus `json:"status,omitempty"`
}

// IncidentReportList contains a list of IncidentReport.
//
// +kubebuilder:object:root=true
type IncidentReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IncidentReport `json:"items"`
}
