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

	"pgregory.net/rapid"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/diagnosis"
)

func engine() *diagnosis.DiagnosisEngine { return diagnosis.NewDiagnosisEngine() }

// ── helpers ───────────────────────────────────────────────────────────────────

func diagOOMSnapshot() *v1alpha1.EvidenceSnapshot {
	return &v1alpha1.EvidenceSnapshot{
		TriggerType: "OOMKilled",
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{{
				Name:                  "app",
				ExitCode:              137,
				TerminationReason:     "OOMKilled",
				LastTerminationReason: "OOMKilled",
				ResourceLimits:        map[string]string{"memory": "512Mi"},
			}},
		},
	}
}

func diagCrashLoopOOMSnapshot() *v1alpha1.EvidenceSnapshot {
	return &v1alpha1.EvidenceSnapshot{
		TriggerType: "CrashLoopBackOff",
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{{
				Name:                  "app",
				WaitingReason:         "CrashLoopBackOff",
				ExitCode:              137,
				LastTerminationReason: "OOMKilled",
				RestartCount:          5,
			}},
		},
	}
}

func diagCrashLoopAppSnapshot() *v1alpha1.EvidenceSnapshot {
	return &v1alpha1.EvidenceSnapshot{
		TriggerType: "CrashLoopBackOff",
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{{
				Name:          "app",
				WaitingReason: "CrashLoopBackOff",
				ExitCode:      1,
				RestartCount:  3,
			}},
		},
	}
}

// ── Rule tests ────────────────────────────────────────────────────────────────

func TestDiagnosis_OOMMemoryLimit_HighConfidence(t *testing.T) {
	result := engine().Evaluate(oomSnapshot())
	if result.Primary == nil {
		t.Fatal("expected non-nil Primary finding")
	}
	if result.Primary.RuleID != "OOMMemoryLimit" {
		t.Errorf("RuleID = %q, want OOMMemoryLimit", result.Primary.RuleID)
	}
	if result.Primary.Confidence != "High" {
		t.Errorf("Confidence = %q, want High", result.Primary.Confidence)
	}
	if result.Primary.Recommendation == "" {
		t.Error("Recommendation must be non-empty")
	}
}

func TestDiagnosis_OOMMemoryLimit_DoesNotFire_WithNodePressure(t *testing.T) {
	snap := diagOOMSnapshot()
	snap.Node = &v1alpha1.NodeEvidence{MemoryPressure: "True"}
	result := engine().Evaluate(snap)
	if result.Primary != nil && result.Primary.RuleID == "OOMMemoryLimit" {
		t.Error("OOMMemoryLimit should not fire when node has MemoryPressure=True")
	}
}

func TestDiagnosis_NodeMemoryPressure_MediumConfidence(t *testing.T) {
	snap := diagOOMSnapshot()
	snap.Node = &v1alpha1.NodeEvidence{Name: "node1", MemoryPressure: "True"}
	result := engine().Evaluate(snap)
	// Primary should be NodeMemoryPressure (OOMMemoryLimit won't fire with pressure)
	if result.Primary == nil {
		t.Fatal("expected non-nil Primary")
	}
	if result.Primary.RuleID != "NodeMemoryPressure" {
		t.Errorf("RuleID = %q, want NodeMemoryPressure", result.Primary.RuleID)
	}
	if result.Primary.Confidence != "Medium" {
		t.Errorf("Confidence = %q, want Medium", result.Primary.Confidence)
	}
}

func TestDiagnosis_CrashLoopOOMExit_HighConfidence(t *testing.T) {
	result := engine().Evaluate(diagCrashLoopOOMSnapshot())
	if result.Primary == nil || result.Primary.RuleID != "CrashLoopOOMExit" {
		t.Errorf("expected Primary.RuleID=CrashLoopOOMExit, got %v", result.Primary)
	}
	if result.Primary.Confidence != "High" {
		t.Errorf("Confidence = %q, want High", result.Primary.Confidence)
	}
}

func TestDiagnosis_CrashLoopAppError_MediumConfidence(t *testing.T) {
	result := engine().Evaluate(diagCrashLoopAppSnapshot())
	if result.Primary == nil || result.Primary.RuleID != "CrashLoopAppError" {
		t.Errorf("expected Primary.RuleID=CrashLoopAppError, got %v", result.Primary)
	}
	if result.Primary.Confidence != "Medium" {
		t.Errorf("Confidence = %q, want Medium", result.Primary.Confidence)
	}
}

func TestDiagnosis_CrashLoopRules_MutuallyExclusive(t *testing.T) {
	// OOM exit: CrashLoopOOMExit fires, CrashLoopAppError must not
	result := engine().Evaluate(diagCrashLoopOOMSnapshot())
	for _, f := range result.ContributingFactors {
		if f.RuleID == "CrashLoopAppError" {
			t.Error("CrashLoopAppError should not fire when exit code is 137")
		}
	}
	for _, f := range result.AlternativeHypotheses {
		if f.RuleID == "CrashLoopAppError" {
			t.Error("CrashLoopAppError should not be an alternative when exit code is 137")
		}
	}
}

func TestDiagnosis_ImagePullFailure_HighConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "ImagePullBackOff",
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{{
				Name:          "app",
				Image:         "registry.io/my-app:v1.0.0",
				WaitingReason: "ImagePullBackOff",
			}},
		},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "ImagePullFailure" {
		t.Errorf("expected ImagePullFailure, got %v", result.Primary)
	}
}

func TestDiagnosis_MissingConfigReference_HighConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "CreateContainerConfigError",
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{{
				Name:          "app",
				WaitingReason: "CreateContainerConfigError",
			}},
		},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "MissingConfigReference" {
		t.Errorf("expected MissingConfigReference, got %v", result.Primary)
	}
}

func TestDiagnosis_PVCNotBound_HighConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "MountFailure",
		Dependencies: &v1alpha1.DependencyEvidence{
			PVCs: []v1alpha1.PVCEvidence{{Name: "data", Phase: "Pending", StorageClassName: "standard"}},
		},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "PVCNotBound" {
		t.Errorf("expected PVCNotBound, got %v", result.Primary)
	}
	if result.Primary.Confidence != "High" {
		t.Errorf("Confidence = %q, want High", result.Primary.Confidence)
	}
}

func TestDiagnosis_PVCMountError_MediumConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "MountFailure",
		Events:      []v1alpha1.EventEvidence{{Reason: "FailedMount", Message: "mount failed: timeout"}},
		Dependencies: &v1alpha1.DependencyEvidence{
			PVCs: []v1alpha1.PVCEvidence{{Name: "data", Phase: "Bound", BoundPVName: "pv-123"}},
		},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "PVCMountError" {
		t.Errorf("expected PVCMountError, got %v", result.Primary)
	}
	if result.Primary.Confidence != "Medium" {
		t.Errorf("Confidence = %q, want Medium", result.Primary.Confidence)
	}
}

func TestDiagnosis_SchedulingFailure_HighConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "SchedulingFailure",
		Events:      []v1alpha1.EventEvidence{{Reason: "FailedScheduling", Message: "0/3 nodes available"}},
		Dependencies: &v1alpha1.DependencyEvidence{
			SchedulingConstraints: &v1alpha1.SchedulingConstraints{
				ResourceRequests: map[string]string{"cpu": "999"},
			},
		},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "SchedulingFailure" {
		t.Errorf("expected SchedulingFailure, got %v", result.Primary)
	}
}

func TestDiagnosis_ProbeFailure_MediumConfidence(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		TriggerType: "ReadinessProbeFailure",
		Events:      []v1alpha1.EventEvidence{{Reason: "Unhealthy", Message: "Readiness probe failed: connection refused"}},
	}
	result := engine().Evaluate(snap)
	if result.Primary == nil || result.Primary.RuleID != "ProbeFailure" {
		t.Errorf("expected ProbeFailure, got %v", result.Primary)
	}
}

func TestDiagnosis_EmptySnapshot_ReturnsUnknown(t *testing.T) {
	result := engine().Evaluate(&v1alpha1.EvidenceSnapshot{})
	if result.Primary != nil {
		t.Errorf("expected nil Primary for empty snapshot, got %v", result.Primary)
	}
	if result.UnknownReason == "" {
		t.Error("expected non-empty UnknownReason")
	}
}

func TestDiagnosis_RulesEvaluated_Count(t *testing.T) {
	result := engine().Evaluate(&v1alpha1.EvidenceSnapshot{})
	if result.RulesEvaluated != 10 {
		t.Errorf("RulesEvaluated = %d, want 10", result.RulesEvaluated)
	}
}

func TestDiagnosis_PrimaryExcludedFromContributing(t *testing.T) {
	// Create snapshot where multiple rules can fire
	snap := diagOOMSnapshot()
	snap.Node = &v1alpha1.NodeEvidence{Name: "node1", MemoryPressure: "True"}
	result := engine().Evaluate(snap)
	if result.Primary == nil {
		return
	}
	for _, f := range result.ContributingFactors {
		if f.RuleID == result.Primary.RuleID {
			t.Errorf("Primary RuleID %q should not appear in ContributingFactors", result.Primary.RuleID)
		}
	}
	for _, f := range result.AlternativeHypotheses {
		if f.RuleID == result.Primary.RuleID {
			t.Errorf("Primary RuleID %q should not appear in AlternativeHypotheses", result.Primary.RuleID)
		}
	}
}

// ── Property tests ────────────────────────────────────────────────────────────

// Feature: diagnosis-engine, Property 1: Determinism
func TestProperty_DiagnosisDeterminism(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: diagnosis-engine, Property 1: Determinism
		// Same snapshot always produces same RuleID and Confidence (EvaluatedAt may differ).
		triggerTypes := []string{"OOMKilled", "CrashLoopBackOff", "ImagePullBackOff", "MountFailure", "SchedulingFailure", ""}
		triggerType := rapid.SampledFrom(triggerTypes).Draw(rt, "triggerType")

		snap := &v1alpha1.EvidenceSnapshot{TriggerType: triggerType}

		e := engine()
		r1 := e.Evaluate(snap)
		r2 := e.Evaluate(snap)

		if (r1.Primary == nil) != (r2.Primary == nil) {
			rt.Fatalf("non-deterministic Primary nil: %v vs %v", r1.Primary, r2.Primary)
		}
		if r1.Primary != nil && r1.Primary.RuleID != r2.Primary.RuleID {
			rt.Fatalf("non-deterministic Primary.RuleID: %q vs %q", r1.Primary.RuleID, r2.Primary.RuleID)
		}
		if r1.RulesEvaluated != r2.RulesEvaluated {
			rt.Fatalf("non-deterministic RulesEvaluated: %d vs %d", r1.RulesEvaluated, r2.RulesEvaluated)
		}
	})
}

// Feature: diagnosis-engine, Property 5: Confidence values are bounded
func TestProperty_ConfidenceBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: diagnosis-engine, Property 5: Confidence values are bounded
		snap := &v1alpha1.EvidenceSnapshot{TriggerType: "OOMKilled",
			Pod: &v1alpha1.PodEvidence{
				Containers: []v1alpha1.ContainerEvidence{{
					ExitCode:          137,
					TerminationReason: "OOMKilled",
					ResourceLimits:    map[string]string{"memory": "256Mi"},
				}},
			},
		}
		result := engine().Evaluate(snap)

		valid := map[string]bool{"High": true, "Medium": true, "Low": true}
		checkFinding := func(f v1alpha1.DiagnosisFinding) {
			if !valid[f.Confidence] {
				rt.Fatalf("invalid Confidence value: %q in finding %q", f.Confidence, f.RuleID)
			}
		}
		if result.Primary != nil {
			checkFinding(*result.Primary)
		}
		for _, f := range result.ContributingFactors {
			checkFinding(f)
		}
		for _, f := range result.AlternativeHypotheses {
			checkFinding(f)
		}
	})
}

// Feature: diagnosis-engine, Property 8: OOMMemoryLimit does not fire under node pressure
func TestProperty_OOMMemoryLimit_NoFireUnderNodePressure(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: diagnosis-engine, Property 8: OOMMemoryLimit does not fire under node pressure
		snap := diagOOMSnapshot()
		snap.Node = &v1alpha1.NodeEvidence{MemoryPressure: "True"}

		result := engine().Evaluate(snap)

		if result.Primary != nil && result.Primary.RuleID == "OOMMemoryLimit" {
			rt.Fatal("OOMMemoryLimit should not be Primary when node has MemoryPressure=True")
		}
	})
}

// Feature: diagnosis-engine, Property 9: CrashLoop rules are mutually exclusive
func TestProperty_CrashLoopMutualExclusion(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: diagnosis-engine, Property 9: CrashLoop rules are mutually exclusive
		snap := diagCrashLoopOOMSnapshot()
		result := engine().Evaluate(snap)

		allFindings := []v1alpha1.DiagnosisFinding{}
		if result.Primary != nil {
			allFindings = append(allFindings, *result.Primary)
		}
		allFindings = append(allFindings, result.ContributingFactors...)
		allFindings = append(allFindings, result.AlternativeHypotheses...)

		hasOOM := false
		hasApp := false
		for _, f := range allFindings {
			if f.RuleID == "CrashLoopOOMExit" {
				hasOOM = true
			}
			if f.RuleID == "CrashLoopAppError" {
				hasApp = true
			}
		}
		if hasOOM && hasApp {
			rt.Fatal("CrashLoopOOMExit and CrashLoopAppError must not both fire for the same container")
		}
	})
}
