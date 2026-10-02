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

// EvidenceSnapshot holds all collected investigation evidence for one incident cycle.
//
// +kubebuilder:object:generate=true
type EvidenceSnapshot struct {
	// CollectedAt is the time evidence collection last completed (or was interrupted).
	// +optional
	CollectedAt *metav1.Time `json:"collectedAt,omitempty"`

	// TriggerType is the classified failure signal that initiated this investigation.
	// Set by the EvidenceOrchestrator from IncidentReport.Status.Trigger.Type so that
	// diagnosis rules can inspect it without accessing the IncidentReport directly.
	// +optional
	TriggerType string `json:"triggerType,omitempty"`

	// Pod holds evidence from the primary affected Pod.
	// +optional
	Pod *PodEvidence `json:"pod,omitempty"`

	// Workload holds evidence from the owning workload resource.
	// +optional
	Workload *WorkloadEvidence `json:"workload,omitempty"`

	// Node holds evidence from the node the affected Pod ran on.
	// +optional
	Node *NodeEvidence `json:"node,omitempty"`

	// Events is a bounded list of relevant Kubernetes Events.
	// +optional
	Events []EventEvidence `json:"events,omitempty"`

	// Logs holds bounded container log excerpts.
	// +optional
	Logs []ContainerLogEvidence `json:"logs,omitempty"`

	// Dependencies holds evidence about workload dependencies relevant to the trigger type.
	// +optional
	Dependencies *DependencyEvidence `json:"dependencies,omitempty"`

	// CollectionErrors records sources that could not be collected and why.
	// +optional
	CollectionErrors []CollectionError `json:"collectionErrors,omitempty"`
}

// PodEvidence holds evidence extracted from the affected Pod resource.
//
// +kubebuilder:object:generate=true
type PodEvidence struct {
	// Name is the Pod name.
	Name string `json:"name"`

	// Namespace is the Pod namespace.
	Namespace string `json:"namespace"`

	// Phase is the Pod phase at collection time.
	// +optional
	Phase string `json:"phase,omitempty"`

	// NodeName is the node the Pod was scheduled on.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// Containers holds per-container state evidence.
	// +optional
	Containers []ContainerEvidence `json:"containers,omitempty"`

	// InitContainers holds per-init-container state evidence.
	// +optional
	InitContainers []ContainerEvidence `json:"initContainers,omitempty"`

	// VolumeMounts holds volumes relevant to the trigger type.
	// +optional
	VolumeMounts []VolumeMountEvidence `json:"volumeMounts,omitempty"`
}

// ContainerEvidence holds state evidence for one container.
//
// +kubebuilder:object:generate=true
type ContainerEvidence struct {
	// Name is the container name.
	Name string `json:"name"`

	// Image is the full image reference.
	Image string `json:"image"`

	// State is the current container state: running, waiting, or terminated.
	State string `json:"state"`

	// WaitingReason is the Waiting.Reason when state is "waiting".
	// +optional
	WaitingReason string `json:"waitingReason,omitempty"`

	// RestartCount is the number of times the container has been restarted.
	RestartCount int32 `json:"restartCount"`

	// ExitCode is the exit code from the most recent termination.
	// Zero when the container has not yet terminated.
	// +optional
	ExitCode int32 `json:"exitCode,omitempty"`

	// TerminationReason is the reason for the most recent termination (e.g., OOMKilled, Error).
	// +optional
	TerminationReason string `json:"terminationReason,omitempty"`

	// LastTerminationReason is the reason from the previous termination, if any.
	// +optional
	LastTerminationReason string `json:"lastTerminationReason,omitempty"`

	// ResourceLimits holds the configured resource limits for this container.
	// +optional
	ResourceLimits map[string]string `json:"resourceLimits,omitempty"`

	// ResourceRequests holds the configured resource requests for this container.
	// +optional
	ResourceRequests map[string]string `json:"resourceRequests,omitempty"`

	// LivenessProbe summarises the liveness probe configuration.
	// +optional
	LivenessProbe *ProbeSummary `json:"livenessProbe,omitempty"`

	// ReadinessProbe summarises the readiness probe configuration.
	// +optional
	ReadinessProbe *ProbeSummary `json:"readinessProbe,omitempty"`
}

// ProbeSummary holds enough probe configuration to understand the failure context.
//
// +kubebuilder:object:generate=true
type ProbeSummary struct {
	// Type is "HTTPGet", "TCPSocket", or "Exec".
	Type string `json:"type"`

	// HTTPPath is the HTTP GET path when Type is HTTPGet.
	// +optional
	HTTPPath string `json:"httpPath,omitempty"`

	// Port is the numeric port for HTTPGet or TCPSocket probes.
	// +optional
	Port int32 `json:"port,omitempty"`

	// FailureThreshold is the number of consecutive failures before the probe fails.
	FailureThreshold int32 `json:"failureThreshold"`
}

// VolumeMountEvidence captures a volume mount relevant to the incident.
//
// +kubebuilder:object:generate=true
type VolumeMountEvidence struct {
	// Name is the volume name.
	Name string `json:"name"`

	// MountPath is the path inside the container.
	MountPath string `json:"mountPath"`

	// VolumeType is the backing volume type (PVC, ConfigMap, Secret, EmptyDir, etc.).
	VolumeType string `json:"volumeType"`

	// ClaimName is the PVC name when VolumeType is PVC.
	// +optional
	ClaimName string `json:"claimName,omitempty"`
}

// WorkloadEvidence holds evidence from the owning workload resource.
//
// +kubebuilder:object:generate=true
type WorkloadEvidence struct {
	// Kind is the workload type.
	Kind string `json:"kind"`

	// Name is the workload name.
	Name string `json:"name"`

	// Namespace is the workload namespace.
	Namespace string `json:"namespace"`

	// DesiredReplicas is the configured replica count.
	// +optional
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`

	// ReadyReplicas is the current ready replica count.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// UpdateStrategy is the update strategy type (e.g., RollingUpdate, Recreate, OnDelete).
	// +optional
	UpdateStrategy string `json:"updateStrategy,omitempty"`

	// Conditions holds the workload's status conditions (capped at 10).
	// +optional
	Conditions []WorkloadCondition `json:"conditions,omitempty"`
}

// WorkloadCondition captures one condition from the workload's status.
//
// +kubebuilder:object:generate=true
type WorkloadCondition struct {
	// Type is the condition type.
	Type string `json:"type"`

	// Status is "True", "False", or "Unknown".
	Status string `json:"status"`

	// Reason is the machine-readable reason string.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is the human-readable condition message.
	// +optional
	Message string `json:"message,omitempty"`

	// LastTransitionTime is when the condition last changed.
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// NodeEvidence holds evidence from the node the affected Pod ran on.
//
// +kubebuilder:object:generate=true
type NodeEvidence struct {
	// Name is the node name.
	Name string `json:"name"`

	// Ready is the node's Ready condition status: "True", "False", or "Unknown".
	Ready string `json:"ready"`

	// MemoryPressure is the MemoryPressure condition status.
	MemoryPressure string `json:"memoryPressure"`

	// DiskPressure is the DiskPressure condition status.
	DiskPressure string `json:"diskPressure"`

	// PIDPressure is the PIDPressure condition status.
	PIDPressure string `json:"pidPressure"`

	// NetworkUnavailable is the NetworkUnavailable condition status.
	// +optional
	NetworkUnavailable string `json:"networkUnavailable,omitempty"`

	// AllocatableCPU is the node's allocatable CPU in millicores as a string.
	// +optional
	AllocatableCPU string `json:"allocatableCPU,omitempty"`

	// AllocatableMemory is the node's allocatable memory as a human-readable string.
	// +optional
	AllocatableMemory string `json:"allocatableMemory,omitempty"`

	// KernelVersion is the node's kernel version string.
	// +optional
	KernelVersion string `json:"kernelVersion,omitempty"`
}

// EventEvidence holds evidence from a single Kubernetes Event.
//
// +kubebuilder:object:generate=true
type EventEvidence struct {
	// Reason is the short machine-readable reason string (e.g., OOMKilled, FailedMount).
	Reason string `json:"reason"`

	// Message is the human-readable event message, truncated to 256 characters.
	Message string `json:"message"`

	// Count is the number of times this event has occurred.
	Count int32 `json:"count"`

	// FirstTime is when the event was first observed.
	// +optional
	FirstTime *metav1.Time `json:"firstTime,omitempty"`

	// LastTime is when the event was most recently observed.
	// +optional
	LastTime *metav1.Time `json:"lastTime,omitempty"`

	// InvolvedObjectKind is the kind of the object this event refers to.
	InvolvedObjectKind string `json:"involvedObjectKind"`

	// InvolvedObjectName is the name of the object this event refers to.
	InvolvedObjectName string `json:"involvedObjectName"`
}

// ContainerLogEvidence holds a bounded excerpt of a container's logs.
//
// +kubebuilder:object:generate=true
type ContainerLogEvidence struct {
	// ContainerName is the container whose logs were collected.
	ContainerName string `json:"containerName"`

	// IsPrevious indicates whether these are logs from the previous container instance.
	IsPrevious bool `json:"isPrevious"`

	// Lines holds the individual log lines collected.
	// +optional
	Lines []string `json:"lines,omitempty"`

	// Truncated is true when the log was cut short due to byte or line limits.
	Truncated bool `json:"truncated"`

	// UnavailableReason describes why logs could not be retrieved.
	// Empty when logs were retrieved successfully.
	// +optional
	UnavailableReason string `json:"unavailableReason,omitempty"`
}

// DependencyEvidence holds evidence about workload dependencies relevant to the trigger type.
//
// +kubebuilder:object:generate=true
type DependencyEvidence struct {
	// PVCs holds evidence for PersistentVolumeClaims referenced by the affected Pod.
	// Populated for MountFailure triggers.
	// +optional
	PVCs []PVCEvidence `json:"pvcs,omitempty"`

	// ImagePullSecretNames holds the names of ImagePullSecrets referenced by the Pod.
	// Values (secret data) are never stored.
	// Populated for ImagePullBackOff triggers.
	// +optional
	ImagePullSecretNames []string `json:"imagePullSecretNames,omitempty"`

	// SchedulingConstraints holds scheduling-relevant Pod spec fields.
	// Populated for SchedulingFailure triggers.
	// +optional
	SchedulingConstraints *SchedulingConstraints `json:"schedulingConstraints,omitempty"`
}

// PVCEvidence holds evidence about a PersistentVolumeClaim.
//
// +kubebuilder:object:generate=true
type PVCEvidence struct {
	// Name is the PVC name.
	Name string `json:"name"`

	// Namespace is the PVC namespace.
	Namespace string `json:"namespace"`

	// StorageClassName is the storage class used by the PVC.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// RequestedStorage is the storage capacity requested by the PVC.
	// +optional
	RequestedStorage string `json:"requestedStorage,omitempty"`

	// Phase is the PVC phase: Pending, Bound, Lost.
	Phase string `json:"phase"`

	// AccessModes is the list of access modes requested by the PVC.
	// +optional
	AccessModes []string `json:"accessModes,omitempty"`

	// BoundPVName is the name of the PersistentVolume the PVC is bound to.
	// +optional
	BoundPVName string `json:"boundPVName,omitempty"`

	// PVReclaimPolicy is the reclaim policy of the bound PersistentVolume.
	// +optional
	PVReclaimPolicy string `json:"pvReclaimPolicy,omitempty"`
}

// SchedulingConstraints holds Pod scheduling fields relevant to scheduling failure diagnosis.
//
// +kubebuilder:object:generate=true
type SchedulingConstraints struct {
	// NodeSelector holds the Pod's nodeSelector labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations holds the Pod's tolerations as strings (key=value:effect).
	// +optional
	Tolerations []string `json:"tolerations,omitempty"`

	// ResourceRequests holds aggregated resource requests across all containers.
	// +optional
	ResourceRequests map[string]string `json:"resourceRequests,omitempty"`
}

// CollectionError records a source that could not be collected and why.
//
// +kubebuilder:object:generate=true
type CollectionError struct {
	// Source identifies which evidence source failed
	// (e.g., "pod", "events", "node", "workload", "logs/my-container", "pvc/my-pvc").
	Source string `json:"source"`

	// Reason is a human-readable description of the failure.
	Reason string `json:"reason"`
}
