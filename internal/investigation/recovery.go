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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

// WorkloadSnapshot provides the RecoveryEvaluator with the current state of the workload.
// Only the field corresponding to the workload kind is populated; all others are nil.
type WorkloadSnapshot struct {
	// Ref identifies the workload kind. May be nil for Pod-level fallback.
	Ref *v1alpha1.WorkloadRef

	// Deployment is populated when Ref.Kind == "Deployment".
	Deployment *appsv1.Deployment

	// StatefulSet is populated when Ref.Kind == "StatefulSet".
	StatefulSet *appsv1.StatefulSet

	// DaemonSet is populated when Ref.Kind == "DaemonSet".
	DaemonSet *appsv1.DaemonSet

	// Job is populated when Ref.Kind == "Job".
	Job *batchv1.Job

	// LatestJob is the most recent Job owned by the CronJob, used when Ref.Kind == "CronJob".
	LatestJob *batchv1.Job

	// Pods is populated for the Pod-level fallback (when Ref is nil or ownership unresolved).
	// An empty slice is not considered healthy.
	Pods []corev1.Pod
}

// RecoveryResult is the output of recovery evaluation.
type RecoveryResult struct {
	// WorkloadHealthy indicates the workload is in a healthy state per its type-specific check.
	WorkloadHealthy bool

	// StabilityPeriodElapsed indicates the stability period has been met.
	StabilityPeriodElapsed bool

	// ShouldResolve indicates the incident should transition to Resolved.
	ShouldResolve bool

	// ShouldResetStabilityTimer indicates a new failure occurred and stabilityStartedAt
	// should be cleared to restart the stability window.
	ShouldResetStabilityTimer bool
}

// RecoveryEvaluatorInterface assesses incident recovery state.
type RecoveryEvaluatorInterface interface {
	// Evaluate checks workload health and stability period progress.
	// It is a pure function — identical inputs produce identical outputs.
	Evaluate(ctx context.Context, report *v1alpha1.IncidentReport, workload WorkloadSnapshot, cfg *config.Config) RecoveryResult
}

// RecoveryEvaluator implements RecoveryEvaluatorInterface.
type RecoveryEvaluator struct{}

// NewRecoveryEvaluator creates a new RecoveryEvaluator.
func NewRecoveryEvaluator() *RecoveryEvaluator {
	return &RecoveryEvaluator{}
}

// Evaluate checks workload health using workload-type-aware criteria and manages the
// stability period state machine.
//
// Stability timer contract:
//  1. Workload becomes healthy → if stabilityStartedAt is nil, signal to set it to now.
//  2. Workload stays healthy → check elapsed time; if >= StabilityPeriod, ShouldResolve=true.
//  3. New failure during stability window → ShouldResetStabilityTimer=true (clear the timer).
//  4. Workload unhealthy → ShouldResetStabilityTimer=true if timer was running.
//
// Note: ShouldResetStabilityTimer=true from an unhealthy workload is primarily for
// event-based triggers where the Pod object may transiently appear healthy. State-based
// failures keep readyReplicas below desired, so the timer cannot start while they persist.
func (e *RecoveryEvaluator) Evaluate(_ context.Context, report *v1alpha1.IncidentReport, workload WorkloadSnapshot, cfg *config.Config) RecoveryResult {
	healthy := e.isWorkloadHealthy(workload)

	if !healthy {
		result := RecoveryResult{WorkloadHealthy: false}
		if report.Status.StabilityStartedAt != nil {
			// A failure arrived while the stability timer was running — reset it.
			result.ShouldResetStabilityTimer = true
		}
		return result
	}

	// Workload is healthy.
	if report.Status.StabilityStartedAt == nil {
		// Timer not yet started — signal the caller to set stabilityStartedAt = now.
		// We return WorkloadHealthy=true but ShouldResolve=false and
		// ShouldResetStabilityTimer=false; the caller interprets this as "start timer".
		return RecoveryResult{WorkloadHealthy: true}
	}

	// Timer is running — check whether the stability period has elapsed.
	elapsed := time.Since(report.Status.StabilityStartedAt.Time)
	if elapsed >= cfg.StabilityPeriod {
		return RecoveryResult{
			WorkloadHealthy:        true,
			StabilityPeriodElapsed: true,
			ShouldResolve:          true,
		}
	}

	// Still within the stability window — keep waiting.
	return RecoveryResult{WorkloadHealthy: true}
}

// isWorkloadHealthy dispatches to the type-specific health check.
func (e *RecoveryEvaluator) isWorkloadHealthy(workload WorkloadSnapshot) bool {
	if workload.Ref != nil {
		switch workload.Ref.Kind {
		case "Deployment":
			return isDeploymentHealthy(workload.Deployment)
		case "StatefulSet":
			return isStatefulSetHealthy(workload.StatefulSet)
		case "DaemonSet":
			return isDaemonSetHealthy(workload.DaemonSet)
		case "Job":
			return isJobHealthy(workload.Job)
		case "CronJob":
			return isJobHealthy(workload.LatestJob)
		}
	}
	// Pod-level fallback
	return arePodsHealthy(workload.Pods)
}

// isDeploymentHealthy returns true when readyReplicas and availableReplicas both meet
// the desired replica count. Uses spec.replicas; defaults to 1 when nil.
func isDeploymentHealthy(d *appsv1.Deployment) bool {
	if d == nil {
		return false
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return d.Status.ReadyReplicas >= desired && d.Status.AvailableReplicas >= desired
}

// isStatefulSetHealthy returns true when readyReplicas meets the desired replica count.
func isStatefulSetHealthy(s *appsv1.StatefulSet) bool {
	if s == nil {
		return false
	}
	desired := int32(1)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return s.Status.ReadyReplicas >= desired
}

// isDaemonSetHealthy returns true when numberReady meets desiredNumberScheduled.
func isDaemonSetHealthy(d *appsv1.DaemonSet) bool {
	if d == nil {
		return false
	}
	return d.Status.NumberReady >= d.Status.DesiredNumberScheduled
}

// isJobHealthy returns true when at least one completion has succeeded.
func isJobHealthy(j *batchv1.Job) bool {
	if j == nil {
		return false
	}
	return j.Status.Succeeded >= 1
}

// arePodsHealthy returns true when all Pods are Running with all containers ready.
// An empty list is not considered healthy.
func arePodsHealthy(pods []corev1.Pod) bool {
	if len(pods) == 0 {
		return false
	}
	for _, pod := range pods {
		if pod.Status.Phase != corev1.PodRunning {
			return false
		}
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				return false
			}
		}
	}
	return true
}
