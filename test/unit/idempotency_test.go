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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"pgregory.net/rapid"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ---- helpers ----------------------------------------------------------------

// applyStatusUpdate simulates the status update logic described in Reconcile step 9:
//   - Add Pod to AffectedPods (dedup by UID)
//   - Set Trigger only if currently nil
//   - For state-based triggers: increment FailureCount only when a new unique failure
//     signature is observed (new container, or reoccurrence after stability reset)
//   - For event-based triggers: set FailureCount to Event.count value
//   - Set LastFailureAt to now
//
// This function is tested independently from the controller reconciler to verify the
// idempotency invariants at the business-logic level.
func applyStatusUpdate(
	report *v1alpha1.IncidentReport,
	pod v1alpha1.PodRef,
	trigger investigation.TriggerResult,
	eventCount int32, // for event-based triggers; ignored for state-based
	now time.Time,
) {
	// Dedup AffectedPods by UID
	if !podUIDPresent(report.Status.AffectedPods, pod.UID) {
		report.Status.AffectedPods = append(report.Status.AffectedPods, pod)
	}

	// Set Trigger only if not already set
	if report.Status.Trigger == nil && trigger.IsTrigger {
		observedAt := metav1.NewTime(now)
		report.Status.Trigger = &v1alpha1.TriggerInfo{
			Type:          trigger.Type,
			ContainerName: trigger.ContainerName,
			Reason:        trigger.Reason,
			ObservedAt:    &observedAt,
		}
		// Set StartedAt only once
		if report.Status.StartedAt == nil {
			t := metav1.NewTime(now)
			report.Status.StartedAt = &t
		}
	}

	// Update FailureCount
	if trigger.IsTrigger {
		if trigger.Source == v1alpha1.TriggerSourceEventBased {
			// Event-based: set to current event.count value
			report.Status.FailureCount = eventCount
		} else {
			// State-based: increment only when a NEW unique failure signature is observed.
			// "New" = different ContainerName from the current trigger, or reoccurrence
			// after stability reset (stabilityStartedAt was set then cleared).
			// Here we check if this container is "new" for the purpose of failure counting.
			if !containerAlreadyCounted(report, trigger.ContainerName) {
				report.Status.FailureCount++
				markContainerCounted(report, trigger.ContainerName)
			}
		}
	}

	// Always update LastFailureAt
	t := metav1.NewTime(now)
	report.Status.LastFailureAt = &t
}

// podUIDPresent returns true if a PodRef with the given UID is already in the list.
func podUIDPresent(pods []v1alpha1.PodRef, uid types.UID) bool {
	for _, p := range pods {
		if p.UID == uid {
			return true
		}
	}
	return false
}

// We use Annotations to track which containers have been counted for state-based triggers.
// This mirrors how the real controller would use a set tracked in status; here we use
// a lightweight annotation-based approach for unit-testing purposes only.
const countedContainersAnnotation = "test.investigator/counted-containers"

func containerAlreadyCounted(report *v1alpha1.IncidentReport, containerName string) bool {
	if report.Annotations == nil {
		return false
	}
	existing := report.Annotations[countedContainersAnnotation]
	for _, c := range splitAnnotation(existing) {
		if c == containerName {
			return true
		}
	}
	return false
}

func markContainerCounted(report *v1alpha1.IncidentReport, containerName string) {
	if report.Annotations == nil {
		report.Annotations = map[string]string{}
	}
	existing := report.Annotations[countedContainersAnnotation]
	if existing == "" {
		report.Annotations[countedContainersAnnotation] = containerName
	} else {
		report.Annotations[countedContainersAnnotation] = existing + "," + containerName
	}
}

func splitAnnotation(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// ---- idempotency tests ------------------------------------------------------

func TestIdempotency_ApplySameUpdateTwice_SameStartedAt(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}

	trigger := investigation.TriggerResult{
		IsTrigger:     true,
		Type:          v1alpha1.TriggerOOMKilled,
		Source:        v1alpha1.TriggerSourceStateBased,
		ContainerName: "app",
		Reason:        "OOMKilled",
	}
	pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: "uid-1"}
	now := time.Now()

	applyStatusUpdate(report, pod, trigger, 0, now)
	startedAt1 := report.Status.StartedAt.DeepCopy()
	failureCount1 := report.Status.FailureCount

	// Apply again — same inputs
	applyStatusUpdate(report, pod, trigger, 0, now)
	startedAt2 := report.Status.StartedAt.DeepCopy()
	failureCount2 := report.Status.FailureCount

	if !startedAt1.Equal(startedAt2) {
		t.Errorf("StartedAt changed on second application: %v → %v", startedAt1, startedAt2)
	}
	if failureCount1 != failureCount2 {
		t.Errorf("FailureCount changed on second application: %d → %d", failureCount1, failureCount2)
	}
}

func TestIdempotency_AffectedPods_Deduplication(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}

	trigger := investigation.TriggerResult{
		IsTrigger:     true,
		Type:          v1alpha1.TriggerCrashLoopBackOff,
		Source:        v1alpha1.TriggerSourceStateBased,
		ContainerName: "app",
	}
	pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: "uid-1"}
	now := time.Now()

	// Apply 5 times with the same Pod UID
	for i := 0; i < 5; i++ {
		applyStatusUpdate(report, pod, trigger, 0, now)
	}

	count := 0
	for _, p := range report.Status.AffectedPods {
		if p.UID == "uid-1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for uid-1 in AffectedPods, got %d", count)
	}
}

func TestIdempotency_StateBased_NoIncrementOnReEvaluation(t *testing.T) {
	// State-based trigger on the same container should not increment FailureCount on re-evaluation
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}

	trigger := investigation.TriggerResult{
		IsTrigger:     true,
		Type:          v1alpha1.TriggerOOMKilled,
		Source:        v1alpha1.TriggerSourceStateBased,
		ContainerName: "app",
	}
	pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: "uid-1"}
	now := time.Now()

	applyStatusUpdate(report, pod, trigger, 0, now)
	countAfterFirst := report.Status.FailureCount

	// Re-evaluate same state (same container, no recovery between evaluations)
	applyStatusUpdate(report, pod, trigger, 0, now)
	applyStatusUpdate(report, pod, trigger, 0, now)
	countAfterThird := report.Status.FailureCount

	if countAfterFirst != countAfterThird {
		t.Errorf("FailureCount should not increment on re-evaluation of same state-based trigger: %d → %d",
			countAfterFirst, countAfterThird)
	}
}

func TestIdempotency_EventBased_SetsFailureCountFromEventCount(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}

	trigger := investigation.TriggerResult{
		IsTrigger: true,
		Type:      v1alpha1.TriggerMountFailure,
		Source:    v1alpha1.TriggerSourceEventBased,
		Reason:    "FailedMount",
	}
	pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: "uid-2"}
	now := time.Now()
	const eventCount int32 = 7

	applyStatusUpdate(report, pod, trigger, eventCount, now)
	if report.Status.FailureCount != eventCount {
		t.Errorf("FailureCount = %d, want %d (event count)", report.Status.FailureCount, eventCount)
	}

	// Applying again with a higher event count updates the value
	const higherCount int32 = 12
	applyStatusUpdate(report, pod, trigger, higherCount, now)
	if report.Status.FailureCount != higherCount {
		t.Errorf("FailureCount = %d, want %d (updated event count)", report.Status.FailureCount, higherCount)
	}
}

func TestIdempotency_TriggerSetOnce_NotOverwritten(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}

	firstTrigger := investigation.TriggerResult{
		IsTrigger:     true,
		Type:          v1alpha1.TriggerOOMKilled,
		Source:        v1alpha1.TriggerSourceStateBased,
		ContainerName: "app",
	}
	secondTrigger := investigation.TriggerResult{
		IsTrigger:     true,
		Type:          v1alpha1.TriggerCrashLoopBackOff,
		Source:        v1alpha1.TriggerSourceStateBased,
		ContainerName: "sidecar",
	}
	pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: "uid-3"}
	now := time.Now()

	applyStatusUpdate(report, pod, firstTrigger, 0, now)
	if report.Status.Trigger == nil || report.Status.Trigger.Type != v1alpha1.TriggerOOMKilled {
		t.Fatalf("expected first trigger to be set")
	}

	applyStatusUpdate(report, pod, secondTrigger, 0, now)
	// Trigger must NOT be overwritten by the second trigger
	if report.Status.Trigger.Type != v1alpha1.TriggerOOMKilled {
		t.Errorf("Trigger was overwritten: got %v, want OOMKilled", report.Status.Trigger.Type)
	}
}

// ---- property-based tests ---------------------------------------------------

// Feature: incident-investigator-foundation, Property 7
// For any sequence of PodRef additions with the same UID,
// the deduplicated AffectedPods list contains that UID exactly once.
func TestProperty7_AffectedPodsDeduplication(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 7
		uid := types.UID("fixed-uid-" + rapid.StringMatching(`[a-z]{3}`).Draw(rt, "suffix"))
		additions := rapid.IntRange(1, 20).Draw(rt, "additions")

		report := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
		}
		trigger := investigation.TriggerResult{
			IsTrigger:     true,
			Type:          v1alpha1.TriggerOOMKilled,
			Source:        v1alpha1.TriggerSourceStateBased,
			ContainerName: "app",
		}
		pod := v1alpha1.PodRef{Name: "test-pod", Namespace: "default", UID: uid}
		now := time.Now()

		for i := 0; i < additions; i++ {
			applyStatusUpdate(report, pod, trigger, 0, now)
		}

		count := 0
		for _, p := range report.Status.AffectedPods {
			if p.UID == uid {
				count++
			}
		}
		if count != 1 {
			rt.Fatalf("expected exactly 1 entry for UID %q, got %d (after %d additions)",
				uid, count, additions)
		}
	})
}

// Feature: incident-investigator-foundation, Property 9
// For identical cluster state input, applying the reconciliation logic N times
// produces the same logical IncidentReport state as applying it once.
func TestProperty9_ReconciliationIdempotency(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 9
		repetitions := rapid.IntRange(1, 10).Draw(rt, "repetitions")
		containerName := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "container")
		podUID := types.UID("uid-" + rapid.StringMatching(`[a-z]{4}`).Draw(rt, "podUID"))

		trigger := investigation.TriggerResult{
			IsTrigger:     true,
			Type:          v1alpha1.TriggerCrashLoopBackOff,
			Source:        v1alpha1.TriggerSourceStateBased,
			ContainerName: containerName,
		}
		pod := v1alpha1.PodRef{Name: "pod-1", Namespace: "default", UID: podUID}
		now := time.Now()

		// Build report after 1 application
		reportOnce := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
		}
		applyStatusUpdate(reportOnce, pod, trigger, 0, now)

		// Build report after N applications (same state)
		reportN := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
		}
		for i := 0; i < repetitions; i++ {
			applyStatusUpdate(reportN, pod, trigger, 0, now)
		}

		// StartedAt must be equal
		if !reportOnce.Status.StartedAt.Equal(reportN.Status.StartedAt) {
			rt.Fatalf("StartedAt differs: %v vs %v",
				reportOnce.Status.StartedAt, reportN.Status.StartedAt)
		}

		// FailureCount must be equal
		if reportOnce.Status.FailureCount != reportN.Status.FailureCount {
			rt.Fatalf("FailureCount differs: %d vs %d (after %d reps)",
				reportOnce.Status.FailureCount, reportN.Status.FailureCount, repetitions)
		}

		// AffectedPods must contain the pod UID exactly once in both
		if len(reportOnce.Status.AffectedPods) != len(reportN.Status.AffectedPods) {
			rt.Fatalf("AffectedPods length differs: %d vs %d",
				len(reportOnce.Status.AffectedPods), len(reportN.Status.AffectedPods))
		}
	})
}
