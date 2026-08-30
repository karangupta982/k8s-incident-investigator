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
	"k8s.io/apimachinery/pkg/types"
)

// WorkloadRef identifies the Kubernetes workload associated with an incident.
type WorkloadRef struct {
	// Kind is the workload type: Deployment, StatefulSet, DaemonSet, Job, CronJob.
	Kind string `json:"kind"`

	// Name is the workload name.
	Name string `json:"name"`

	// Namespace is the workload namespace.
	Namespace string `json:"namespace"`

	// UID is the workload UID, used to detect workload replacement.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}

// PodRef identifies a Pod affected by an incident.
type PodRef struct {
	// Name is the Pod name.
	Name string `json:"name"`

	// Namespace is the Pod namespace.
	Namespace string `json:"namespace"`

	// UID is the Pod UID, used to distinguish replaced Pods with the same name.
	// +optional
	UID types.UID `json:"uid,omitempty"`
}

// TriggerInfo records the failure signal that created or most recently updated the incident.
type TriggerInfo struct {
	// Type is the classified trigger type.
	Type TriggerType `json:"type"`

	// Reason is the raw Kubernetes reason string from the Pod or Event, if available.
	// +optional
	Reason string `json:"reason,omitempty"`

	// ContainerName is the container that triggered the incident, if applicable.
	// +optional
	ContainerName string `json:"containerName,omitempty"`

	// ObservedAt is the time at which this trigger was first observed.
	// +optional
	ObservedAt *metav1.Time `json:"observedAt,omitempty"`

	// Message is a short human-readable description of the failure signal.
	// +optional
	Message string `json:"message,omitempty"`
}
