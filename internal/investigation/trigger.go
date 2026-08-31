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

package investigation

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

// TriggerContext contains all pre-fetched data needed for trigger evaluation.
// EventCounts represent Kubernetes Event.count field values (aggregated by Kubernetes),
// not the raw number of Event objects. The caller must use the highest event.count
// among all matching Event objects for the same Pod and reason — not a sum.
type TriggerContext struct {
	Pod                         *corev1.Pod
	MountFailureEventCount      int // max Event.count for reason=FailedMount
	ReadinessProbeEventCount    int // max Event.count for reason=Unhealthy (Readiness probe)
	LivenessProbeEventCount     int // max Event.count for reason=Unhealthy (Liveness probe)
	SchedulingFailureEventCount int // max Event.count for reason=FailedScheduling
}

// TriggerResult is the output of trigger evaluation.
type TriggerResult struct {
	// IsTrigger indicates whether this Pod state represents an incident trigger.
	IsTrigger bool

	// Type is the classified trigger type. Zero value if IsTrigger is false.
	Type v1alpha1.TriggerType

	// Source distinguishes state-based from event-based triggers.
	Source v1alpha1.TriggerSource

	// IsImmediate indicates the trigger does not require threshold evaluation.
	IsImmediate bool

	// ContainerName is the container that triggered the incident, if applicable.
	ContainerName string

	// Reason is the raw reason string from Kubernetes.
	Reason string
}

// TriggerEvaluatorInterface classifies Pod state into trigger types.
type TriggerEvaluatorInterface interface {
	Evaluate(ctx context.Context, tc TriggerContext, cfg *config.Config) TriggerResult
}

// TriggerEvaluator implements TriggerEvaluatorInterface.
// It is a pure function — identical inputs always produce identical outputs.
type TriggerEvaluator struct{}

// NewTriggerEvaluator creates a new TriggerEvaluator.
func NewTriggerEvaluator() *TriggerEvaluator {
	return &TriggerEvaluator{}
}

// Evaluate classifies the current Pod state into a TriggerResult.
// State-based triggers are checked first (idempotent, no API calls needed).
// Event-based threshold triggers are checked only when no state-based trigger fires.
func (e *TriggerEvaluator) Evaluate(_ context.Context, tc TriggerContext, cfg *config.Config) TriggerResult {
	pod := tc.Pod
	if pod == nil {
		return TriggerResult{}
	}

	// --- Normal lifecycle exclusions ---

	// Succeeded phase (completed Job) — not a failure
	if pod.Status.Phase == corev1.PodSucceeded {
		return TriggerResult{}
	}

	// Graceful termination with no active failure reason
	if pod.DeletionTimestamp != nil && !hasFailureReason(pod) {
		return TriggerResult{}
	}

	// --- State-based triggers (immediate, idempotent) ---
	for _, cs := range pod.Status.ContainerStatuses {
		// Normal lifecycle waiting reasons — short-circuit before checking failures
		if cs.State.Waiting != nil {
			switch cs.State.Waiting.Reason {
			case "ContainerCreating", "PodInitializing":
				// Normal lifecycle — return false immediately without checking further
				return TriggerResult{}
			}
		}

		// Completed container (exit code 0) — normal lifecycle
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0 {
			return TriggerResult{}
		}

		// OOMKilled — current terminated state
		if cs.State.Terminated != nil && cs.State.Terminated.Reason == "OOMKilled" {
			return TriggerResult{
				IsTrigger:     true,
				Type:          v1alpha1.TriggerOOMKilled,
				Source:        v1alpha1.TriggerSourceStateBased,
				IsImmediate:   true,
				ContainerName: cs.Name,
				Reason:        cs.State.Terminated.Reason,
			}
		}

		// OOMKilled — last terminated state (container restarted, prior termination was OOM)
		if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			return TriggerResult{
				IsTrigger:     true,
				Type:          v1alpha1.TriggerOOMKilled,
				Source:        v1alpha1.TriggerSourceStateBased,
				IsImmediate:   true,
				ContainerName: cs.Name,
				Reason:        cs.LastTerminationState.Terminated.Reason,
			}
		}

		// Waiting-state triggers
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			switch reason {
			case "CrashLoopBackOff":
				return TriggerResult{
					IsTrigger:     true,
					Type:          v1alpha1.TriggerCrashLoopBackOff,
					Source:        v1alpha1.TriggerSourceStateBased,
					IsImmediate:   true,
					ContainerName: cs.Name,
					Reason:        reason,
				}
			case "ImagePullBackOff", "ErrImagePull":
				return TriggerResult{
					IsTrigger:     true,
					Type:          v1alpha1.TriggerImagePullBackOff,
					Source:        v1alpha1.TriggerSourceStateBased,
					IsImmediate:   true,
					ContainerName: cs.Name,
					Reason:        reason,
				}
			case "CreateContainerConfigError":
				return TriggerResult{
					IsTrigger:     true,
					Type:          v1alpha1.TriggerCreateContainerConfigError,
					Source:        v1alpha1.TriggerSourceStateBased,
					IsImmediate:   true,
					ContainerName: cs.Name,
					Reason:        reason,
				}
			}
		}
	}

	// Eviction — check status.reason first
	if pod.Status.Reason == "Evicted" {
		return TriggerResult{
			IsTrigger:   true,
			Type:        v1alpha1.TriggerEviction,
			Source:      v1alpha1.TriggerSourceStateBased,
			IsImmediate: true,
			Reason:      "Evicted",
		}
	}
	// Eviction — check Failed phase with eviction condition
	if pod.Status.Phase == corev1.PodFailed {
		for _, cond := range pod.Status.Conditions {
			if isEvictionCondition(cond) {
				return TriggerResult{
					IsTrigger:   true,
					Type:        v1alpha1.TriggerEviction,
					Source:      v1alpha1.TriggerSourceStateBased,
					IsImmediate: true,
					Reason:      string(cond.Reason),
				}
			}
		}
	}

	// --- Event-based (threshold) triggers ---
	// These use Kubernetes-aggregated Event.count values.

	if tc.ReadinessProbeEventCount >= cfg.ReadinessProbeFailureThreshold {
		return TriggerResult{
			IsTrigger: true,
			Type:      v1alpha1.TriggerReadinessProbeFailure,
			Source:    v1alpha1.TriggerSourceEventBased,
			Reason:    "Unhealthy",
		}
	}

	if tc.LivenessProbeEventCount >= cfg.LivenessProbeFailureThreshold {
		return TriggerResult{
			IsTrigger: true,
			Type:      v1alpha1.TriggerLivenessProbeFailure,
			Source:    v1alpha1.TriggerSourceEventBased,
			Reason:    "Unhealthy",
		}
	}

	if tc.MountFailureEventCount >= cfg.MountFailureThreshold {
		return TriggerResult{
			IsTrigger: true,
			Type:      v1alpha1.TriggerMountFailure,
			Source:    v1alpha1.TriggerSourceEventBased,
			Reason:    "FailedMount",
		}
	}

	if tc.SchedulingFailureEventCount >= cfg.SchedulingFailureThreshold {
		return TriggerResult{
			IsTrigger: true,
			Type:      v1alpha1.TriggerSchedulingFailure,
			Source:    v1alpha1.TriggerSourceEventBased,
			Reason:    "FailedScheduling",
		}
	}

	return TriggerResult{}
}

// hasFailureReason returns true if the Pod has an active non-zero exit or OOMKilled state.
// Used to allow graceful termination with an active failure to still trigger.
func hasFailureReason(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil && cs.State.Terminated.ExitCode != 0 {
			return true
		}
		if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
			return true
		}
	}
	return false
}

// isEvictionCondition returns true if the condition indicates a node eviction.
func isEvictionCondition(cond corev1.PodCondition) bool {
	return string(cond.Reason) == "Evicted" || string(cond.Type) == "DisruptionTarget"
}
