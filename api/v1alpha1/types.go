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

// IncidentPhase represents the current lifecycle state of an incident.
// +kubebuilder:validation:Enum=Investigating;Diagnosed;Unknown;Resolved
type IncidentPhase string

const (
	// PhaseInvestigating is set when the incident is first detected.
	// It remains the active phase until diagnosis or resolution.
	PhaseInvestigating IncidentPhase = "Investigating"

	// PhaseDiagnosed is set when a diagnosis rule matches the collected evidence.
	// Set by future diagnosis specification, not the foundation.
	PhaseDiagnosed IncidentPhase = "Diagnosed"

	// PhaseUnknown is set when investigation completes but no diagnosis rule matched.
	// Set by future diagnosis specification, not the foundation.
	PhaseUnknown IncidentPhase = "Unknown"

	// PhaseResolved is set when the workload has been healthy for the full stability period.
	PhaseResolved IncidentPhase = "Resolved"
)

// TriggerType identifies the class of failure signal that created or updated an incident.
type TriggerType string

const (
	TriggerOOMKilled                  TriggerType = "OOMKilled"
	TriggerCrashLoopBackOff           TriggerType = "CrashLoopBackOff"
	TriggerImagePullBackOff           TriggerType = "ImagePullBackOff"
	TriggerCreateContainerConfigError TriggerType = "CreateContainerConfigError"
	TriggerMountFailure               TriggerType = "MountFailure"
	TriggerReadinessProbeFailure      TriggerType = "ReadinessProbeFailure"
	TriggerLivenessProbeFailure       TriggerType = "LivenessProbeFailure"
	TriggerSchedulingFailure          TriggerType = "SchedulingFailure"
	TriggerEviction                   TriggerType = "Eviction"
)

// TriggerSource identifies how a trigger was detected — from current Pod state or from
// Kubernetes Event counts.
type TriggerSource string

const (
	// TriggerSourceStateBased means the trigger was detected from current Pod object state.
	// State-based triggers are idempotent: the same Pod state on reconciliation N and N+1
	// represents the same condition, not two distinct failures.
	TriggerSourceStateBased TriggerSource = "StateBased"

	// TriggerSourceEventBased means the trigger was detected from Kubernetes Event counts.
	// The threshold check uses event.count >= threshold.
	TriggerSourceEventBased TriggerSource = "EventBased"
)
