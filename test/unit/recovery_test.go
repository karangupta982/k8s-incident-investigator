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

package unit_test

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ---- helpers ----------------------------------------------------------------

func int32Ptr(i int32) *int32 { return &i }

func evalRecovery(
	t *testing.T,
	report *v1alpha1.IncidentReport,
	snapshot investigation.WorkloadSnapshot,
	cfg *config.Config,
) investigation.RecoveryResult {
	t.Helper()
	return investigation.NewRecoveryEvaluator().Evaluate(context.Background(), report, snapshot, cfg)
}

func emptyReport() *v1alpha1.IncidentReport {
	return &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}
}

func reportWithStability(stabilityStartedAt time.Time) *v1alpha1.IncidentReport {
	t := metav1.NewTime(stabilityStartedAt)
	return &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Phase:              v1alpha1.PhaseInvestigating,
			StabilityStartedAt: &t,
		},
	}
}

// ---- Deployment health -------------------------------------------------------

func TestRecovery_Deployment_Healthy(t *testing.T) {
	replicas := int32(3)
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 3, AvailableReplicas: 3},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
}

func TestRecovery_Deployment_Unhealthy_ReadyBelowDesired(t *testing.T) {
	replicas := int32(3)
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
}

func TestRecovery_Deployment_NilReplicas_DefaultsToOne(t *testing.T) {
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: nil}, // nil defaults to 1
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true for nil Replicas defaulting to 1")
	}
}

// ---- StatefulSet health -----------------------------------------------------

func TestRecovery_StatefulSet_Healthy(t *testing.T) {
	replicas := int32(2)
	s := &appsv1.StatefulSet{
		Spec:   appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:         &v1alpha1.WorkloadRef{Kind: "StatefulSet"},
		StatefulSet: s,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
}

func TestRecovery_StatefulSet_Unhealthy(t *testing.T) {
	replicas := int32(3)
	s := &appsv1.StatefulSet{
		Spec:   appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{ReadyReplicas: 2},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:         &v1alpha1.WorkloadRef{Kind: "StatefulSet"},
		StatefulSet: s,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
}

// ---- DaemonSet health -------------------------------------------------------

func TestRecovery_DaemonSet_Healthy(t *testing.T) {
	ds := &appsv1.DaemonSet{
		Status: appsv1.DaemonSetStatus{NumberReady: 5, DesiredNumberScheduled: 5},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:       &v1alpha1.WorkloadRef{Kind: "DaemonSet"},
		DaemonSet: ds,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
}

func TestRecovery_DaemonSet_Unhealthy(t *testing.T) {
	ds := &appsv1.DaemonSet{
		Status: appsv1.DaemonSetStatus{NumberReady: 3, DesiredNumberScheduled: 5},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:       &v1alpha1.WorkloadRef{Kind: "DaemonSet"},
		DaemonSet: ds,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
}

// ---- Job health -------------------------------------------------------------

func TestRecovery_Job_Healthy(t *testing.T) {
	j := &batchv1.Job{Status: batchv1.JobStatus{Succeeded: 1}}
	snap := investigation.WorkloadSnapshot{
		Ref: &v1alpha1.WorkloadRef{Kind: "Job"},
		Job: j,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
}

func TestRecovery_Job_NotSucceeded(t *testing.T) {
	j := &batchv1.Job{Status: batchv1.JobStatus{Succeeded: 0}}
	snap := investigation.WorkloadSnapshot{
		Ref: &v1alpha1.WorkloadRef{Kind: "Job"},
		Job: j,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
}

// ---- CronJob health ---------------------------------------------------------

func TestRecovery_CronJob_Healthy(t *testing.T) {
	latestJob := &batchv1.Job{Status: batchv1.JobStatus{Succeeded: 1}}
	snap := investigation.WorkloadSnapshot{
		Ref:       &v1alpha1.WorkloadRef{Kind: "CronJob"},
		LatestJob: latestJob,
	}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
}

// ---- Pod-level fallback health ----------------------------------------------

func TestRecovery_Pods_AllReady_Healthy(t *testing.T) {
	pods := []corev1.Pod{
		{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		},
		{
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Ready: true},
				},
			},
		},
	}
	snap := investigation.WorkloadSnapshot{Pods: pods}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true when all pods ready")
	}
}

func TestRecovery_Pods_OneNotReady_Unhealthy(t *testing.T) {
	pods := []corev1.Pod{
		{
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
			},
		},
		{
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Ready: false}},
			},
		},
	}
	snap := investigation.WorkloadSnapshot{Pods: pods}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false when one pod not ready")
	}
}

func TestRecovery_Pods_EmptyList_Unhealthy(t *testing.T) {
	snap := investigation.WorkloadSnapshot{Pods: []corev1.Pod{}}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false for empty pod list")
	}
}

// ---- Stability period -------------------------------------------------------

func TestRecovery_Healthy_StabilityTimerNotStarted(t *testing.T) {
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}
	// stabilityStartedAt is nil
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if !result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=true")
	}
	if result.ShouldResolve {
		t.Error("ShouldResolve should be false when stability timer just started")
	}
	if result.ShouldResetStabilityTimer {
		t.Error("ShouldResetStabilityTimer should be false")
	}
}

func TestRecovery_Healthy_StabilityPeriodElapsed_ShouldResolve(t *testing.T) {
	cfg := &config.Config{
		StabilityPeriod: 1 * time.Millisecond, // very short for test
		RequeueInterval: 30 * time.Second,
	}
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}

	// stabilityStartedAt is well in the past (elapsed > StabilityPeriod)
	pastTime := time.Now().Add(-10 * time.Minute)
	report := reportWithStability(pastTime)

	result := evalRecovery(t, report, snap, cfg)
	if !result.ShouldResolve {
		t.Error("ShouldResolve should be true when stability period elapsed")
	}
	if !result.StabilityPeriodElapsed {
		t.Error("StabilityPeriodElapsed should be true")
	}
}

func TestRecovery_Healthy_StabilityPeriodNotElapsed_ShouldNotResolve(t *testing.T) {
	cfg := &config.Config{
		StabilityPeriod: 10 * time.Minute, // long period
		RequeueInterval: 30 * time.Second,
	}
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}

	// stabilityStartedAt is just now (not elapsed)
	report := reportWithStability(time.Now())

	result := evalRecovery(t, report, snap, cfg)
	if result.ShouldResolve {
		t.Error("ShouldResolve should be false when stability period not elapsed")
	}
	if !result.WorkloadHealthy {
		t.Error("WorkloadHealthy should be true")
	}
}

func TestRecovery_NewFailureDuringStability_ResetTimer(t *testing.T) {
	// Unhealthy workload with stability timer running
	d := &appsv1.Deployment{
		Spec:   appsv1.DeploymentSpec{Replicas: int32Ptr(3)},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 0, AvailableReplicas: 0},
	}
	snap := investigation.WorkloadSnapshot{
		Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
		Deployment: d,
	}
	report := reportWithStability(time.Now().Add(-1 * time.Minute))

	result := evalRecovery(t, report, snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
	if !result.ShouldResetStabilityTimer {
		t.Error("ShouldResetStabilityTimer should be true when unhealthy during stability window")
	}
}

func TestRecovery_Unhealthy_WithStabilityTimer_ResetTimer(t *testing.T) {
	// Pod-level fallback — no pods → unhealthy
	snap := investigation.WorkloadSnapshot{Pods: nil}
	report := reportWithStability(time.Now().Add(-30 * time.Second))

	result := evalRecovery(t, report, snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
	if !result.ShouldResetStabilityTimer {
		t.Error("ShouldResetStabilityTimer should be true when stability timer was set and workload is unhealthy")
	}
}

func TestRecovery_Unhealthy_WithoutStabilityTimer_NoReset(t *testing.T) {
	snap := investigation.WorkloadSnapshot{Pods: nil}
	result := evalRecovery(t, emptyReport(), snap, defaultCfg())
	if result.WorkloadHealthy {
		t.Error("expected WorkloadHealthy=false")
	}
	if result.ShouldResetStabilityTimer {
		t.Error("ShouldResetStabilityTimer should be false when stability timer was not set")
	}
}

// ---- property-based tests ---------------------------------------------------

// Feature: incident-investigator-foundation, Property 8
// For any IncidentReport with non-nil stabilityStartedAt and any unhealthy workload,
// ShouldResetStabilityTimer = true.
func TestProperty8_StabilityResetOnUnhealthyWorkload(t *testing.T) {
	e := investigation.NewRecoveryEvaluator()
	cfg := defaultCfg()

	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 8
		// Generate a random stabilityStartedAt in the past
		secondsAgo := rapid.Int64Range(1, 3600).Draw(rt, "secondsAgo")
		stabilityTime := metav1.NewTime(time.Now().Add(-time.Duration(secondsAgo) * time.Second))

		report := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{
				Phase:              v1alpha1.PhaseInvestigating,
				StabilityStartedAt: &stabilityTime,
			},
		}

		// Unhealthy workload: Deployment with 0 ready replicas
		desired := int32(rapid.IntRange(1, 10).Draw(rt, "desired"))
		d := &appsv1.Deployment{
			Spec:   appsv1.DeploymentSpec{Replicas: &desired},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 0, AvailableReplicas: 0},
		}
		snap := investigation.WorkloadSnapshot{
			Ref:        &v1alpha1.WorkloadRef{Kind: "Deployment"},
			Deployment: d,
		}

		result := e.Evaluate(context.Background(), report, snap, cfg)
		if !result.ShouldResetStabilityTimer {
			rt.Fatalf("expected ShouldResetStabilityTimer=true for unhealthy workload with stability timer set")
		}
		if result.WorkloadHealthy {
			rt.Fatalf("expected WorkloadHealthy=false for 0 ready replicas")
		}
	})
}

// Feature: incident-investigator-foundation, Property 10
// Threshold monotonicity: if trigger fires for count C1, it also fires for C2 >= C1.
func TestProperty10_ThresholdMonotonicity(t *testing.T) {
	e := investigation.NewTriggerEvaluator()

	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 10
		threshold := rapid.IntRange(1, 20).Draw(rt, "threshold")
		c1 := rapid.IntRange(0, 30).Draw(rt, "c1")
		c2 := rapid.IntRange(c1, 30).Draw(rt, "c2") // c2 >= c1

		cfg := &config.Config{
			MountFailureThreshold:          threshold,
			ReadinessProbeFailureThreshold: threshold,
			LivenessProbeFailureThreshold:  threshold,
			SchedulingFailureThreshold:     threshold,
			StabilityPeriod:                defaultCfg().StabilityPeriod,
			RequeueInterval:                defaultCfg().RequeueInterval,
			CorrelationWindow:              defaultCfg().CorrelationWindow,
		}

		tc1 := investigation.TriggerContext{Pod: runningPod(), MountFailureEventCount: c1}
		tc2 := investigation.TriggerContext{Pod: runningPod(), MountFailureEventCount: c2}

		r1 := e.Evaluate(context.Background(), tc1, cfg)
		r2 := e.Evaluate(context.Background(), tc2, cfg)

		// If trigger fires for c1, it must also fire for c2 (monotonicity)
		if r1.IsTrigger && !r2.IsTrigger {
			rt.Fatalf("monotonicity violated: trigger fired for c1=%d but not c2=%d (threshold=%d)",
				c1, c2, threshold)
		}
	})
}
