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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CorrelatedEvidence holds derived signals computed from the EvidenceSnapshot.
// It is a pure transformation — no Kubernetes API calls are made to produce it.
//
// +kubebuilder:object:generate=true
type CorrelatedEvidence struct {
	// CorrelatedAt is the timestamp when correlation last ran.
	// +optional
	CorrelatedAt *metav1.Time `json:"correlatedAt,omitempty"`

	// CausalSignals holds derived boolean and string signals from multi-source evidence.
	// +optional
	CausalSignals *CausalSignals `json:"causalSignals,omitempty"`

	// LogPatterns holds patterns detected in container log lines.
	// +optional
	LogPatterns *LogPatterns `json:"logPatterns,omitempty"`

	// ChainPatterns lists identified causal event chain types.
	// +optional
	ChainPatterns []string `json:"chainPatterns,omitempty"`
}

// CausalSignals holds derived signals requiring evidence from multiple layers.
//
// +kubebuilder:object:generate=true
type CausalSignals struct {
	// NodeMemoryPressureCoincident is true when OOMKilled occurred
	// while the node was reporting MemoryPressure=True.
	NodeMemoryPressureCoincident bool `json:"nodeMemoryPressureCoincident,omitempty"`

	// ContainerHitConfiguredLimit is true when OOMKilled occurred
	// and the container had a configured memory limit.
	ContainerHitConfiguredLimit bool `json:"containerHitConfiguredLimit,omitempty"`

	// PVCIsUnbound is true when at least one referenced PVC is not in Bound phase.
	PVCIsUnbound bool `json:"pvcIsUnbound,omitempty"`

	// UnboundPVCNames lists the names of PVCs that are not Bound.
	// +optional
	UnboundPVCNames []string `json:"unboundPVCNames,omitempty"`

	// MountFailureLinkedToPVC is true when FailedMount events exist
	// and PVC evidence is present.
	MountFailureLinkedToPVC bool `json:"mountFailureLinkedToPVC,omitempty"`

	// SchedulingConstraintsPresent is true when FailedScheduling events exist
	// and scheduling constraints are present in the evidence.
	SchedulingConstraintsPresent bool `json:"schedulingConstraintsPresent,omitempty"`

	// OOMKillCausedCrashLoop is true when OOMKilled exit codes are detected
	// alongside CrashLoopBackOff waiting state for the same container.
	OOMKillCausedCrashLoop bool `json:"oomKillCausedCrashLoop,omitempty"`
}

// LogPatterns holds signals extracted from container log lines.
//
// +kubebuilder:object:generate=true
type LogPatterns struct {
	// ContainsOOMString is true when any log line contains an OOM-related string.
	ContainsOOMString bool `json:"containsOOMString,omitempty"`

	// ContainsConnectionRefused is true when any log line contains a connection refused error.
	ContainsConnectionRefused bool `json:"containsConnectionRefused,omitempty"`

	// ContainsPanicOrFatal is true when any log line contains a panic or fatal error.
	ContainsPanicOrFatal bool `json:"containsPanicOrFatal,omitempty"`

	// ContainsPermissionDenied is true when any log line contains a permission error.
	ContainsPermissionDenied bool `json:"containsPermissionDenied,omitempty"`

	// ContainerWithPattern maps pattern name to the first container name
	// where the pattern was detected.
	// +optional
	ContainerWithPattern map[string]string `json:"containerWithPattern,omitempty"`
}
