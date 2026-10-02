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

// TimelineEvent represents a single significant moment during an incident.
//
// +kubebuilder:object:generate=true
type TimelineEvent struct {
	// Timestamp is when this event occurred.
	Timestamp metav1.Time `json:"timestamp"`

	// Source identifies where the event came from.
	// "controller" for lifecycle events (IncidentDetected, WorkloadHealthy, IncidentResolved),
	// "container" for container termination events,
	// "kubernetes-event" for events from the Kubernetes Events API.
	Source string `json:"source"`

	// Reason is the short machine-readable reason string.
	Reason string `json:"reason"`

	// Message is the human-readable description, truncated to 256 characters.
	Message string `json:"message"`
}
