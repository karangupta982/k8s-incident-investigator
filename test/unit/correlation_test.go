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
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/correlation"
)

// helpers

func correlator() *correlation.EvidenceCorrelator {
	return &correlation.EvidenceCorrelator{}
}

func oomSnapshot() *v1alpha1.EvidenceSnapshot {
	return &v1alpha1.EvidenceSnapshot{
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{
				{
					Name:                  "app",
					LastTerminationReason: "OOMKilled",
					ResourceLimits:        map[string]string{"memory": "512Mi"},
				},
			},
		},
	}
}

// ── Causal signal tests ───────────────────────────────────────────────────────

func TestCausalSignals_OOMWithNodePressure(t *testing.T) {
	snap := oomSnapshot()
	snap.Node = &v1alpha1.NodeEvidence{MemoryPressure: "True"}

	result := correlator().Correlate(snap)

	if result.CausalSignals == nil {
		t.Fatal("expected non-nil CausalSignals")
	}
	if !result.CausalSignals.NodeMemoryPressureCoincident {
		t.Error("expected NodeMemoryPressureCoincident=true")
	}
}

func TestCausalSignals_OOMWithLimit(t *testing.T) {
	snap := oomSnapshot()

	result := correlator().Correlate(snap)

	if !result.CausalSignals.ContainerHitConfiguredLimit {
		t.Error("expected ContainerHitConfiguredLimit=true")
	}
}

func TestCausalSignals_OOMNoPressure_NodeNil(t *testing.T) {
	snap := oomSnapshot()
	snap.Node = nil

	result := correlator().Correlate(snap)

	if result.CausalSignals.NodeMemoryPressureCoincident {
		t.Error("NodeMemoryPressureCoincident should be false when node is nil")
	}
	// ContainerHitConfiguredLimit should still be true
	if !result.CausalSignals.ContainerHitConfiguredLimit {
		t.Error("expected ContainerHitConfiguredLimit=true even with nil node")
	}
}

func TestCausalSignals_PVCUnbound(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		Dependencies: &v1alpha1.DependencyEvidence{
			PVCs: []v1alpha1.PVCEvidence{
				{Name: "my-pvc", Phase: "Pending"},
			},
		},
	}

	result := correlator().Correlate(snap)

	if !result.CausalSignals.PVCIsUnbound {
		t.Error("expected PVCIsUnbound=true")
	}
	if len(result.CausalSignals.UnboundPVCNames) != 1 || result.CausalSignals.UnboundPVCNames[0] != "my-pvc" {
		t.Errorf("expected UnboundPVCNames=[my-pvc], got %v", result.CausalSignals.UnboundPVCNames)
	}
}

func TestCausalSignals_PVCBound_NotFlagged(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		Dependencies: &v1alpha1.DependencyEvidence{
			PVCs: []v1alpha1.PVCEvidence{
				{Name: "bound-pvc", Phase: "Bound"},
			},
		},
	}

	result := correlator().Correlate(snap)

	if result.CausalSignals.PVCIsUnbound {
		t.Error("PVCIsUnbound should be false when all PVCs are Bound")
	}
}

func TestCausalSignals_MountFailureLinkedToPVC(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		Events: []v1alpha1.EventEvidence{
			{Reason: "FailedMount", Message: "unable to mount", Count: 3},
		},
		Dependencies: &v1alpha1.DependencyEvidence{
			PVCs: []v1alpha1.PVCEvidence{
				{Name: "data-pvc", Phase: "Bound"},
			},
		},
	}

	result := correlator().Correlate(snap)

	if !result.CausalSignals.MountFailureLinkedToPVC {
		t.Error("expected MountFailureLinkedToPVC=true")
	}
}

func TestCausalSignals_SchedulingConstraintsPresent(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		Events: []v1alpha1.EventEvidence{
			{Reason: "FailedScheduling", Message: "0/1 nodes available", Count: 5},
		},
		Dependencies: &v1alpha1.DependencyEvidence{
			SchedulingConstraints: &v1alpha1.SchedulingConstraints{
				NodeSelector: map[string]string{"gpu": "true"},
			},
		},
	}

	result := correlator().Correlate(snap)

	if !result.CausalSignals.SchedulingConstraintsPresent {
		t.Error("expected SchedulingConstraintsPresent=true")
	}
}

func TestCausalSignals_OOMCrashLoop(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{
		Pod: &v1alpha1.PodEvidence{
			Containers: []v1alpha1.ContainerEvidence{
				{
					Name:                  "app",
					WaitingReason:         "CrashLoopBackOff",
					LastTerminationReason: "OOMKilled",
					ResourceLimits:        map[string]string{"memory": "256Mi"},
				},
			},
		},
	}

	result := correlator().Correlate(snap)

	if !result.CausalSignals.OOMKillCausedCrashLoop {
		t.Error("expected OOMKillCausedCrashLoop=true")
	}
}

// ── Log pattern tests ─────────────────────────────────────────────────────────

func makeLogSnapshot(containerName, line string) *v1alpha1.EvidenceSnapshot {
	return &v1alpha1.EvidenceSnapshot{
		Logs: []v1alpha1.ContainerLogEvidence{
			{ContainerName: containerName, Lines: []string{line}},
		},
	}
}

func TestLogPatterns_OOMString(t *testing.T) {
	snap := makeLogSnapshot("app", "2026/01/01 Killed process due to memory")
	result := correlator().Correlate(snap)

	if !result.LogPatterns.ContainsOOMString {
		t.Error("expected ContainsOOMString=true")
	}
	if result.LogPatterns.ContainerWithPattern["OOMString"] != "app" {
		t.Errorf("ContainerWithPattern[OOMString] = %q, want app", result.LogPatterns.ContainerWithPattern["OOMString"])
	}
}

func TestLogPatterns_ConnectionRefused(t *testing.T) {
	snap := makeLogSnapshot("app", "dial tcp: connection refused")
	result := correlator().Correlate(snap)

	if !result.LogPatterns.ContainsConnectionRefused {
		t.Error("expected ContainsConnectionRefused=true")
	}
}

func TestLogPatterns_PanicOrFatal(t *testing.T) {
	snap := makeLogSnapshot("app", "panic: runtime error: index out of range")
	result := correlator().Correlate(snap)

	if !result.LogPatterns.ContainsPanicOrFatal {
		t.Error("expected ContainsPanicOrFatal=true")
	}
}

func TestLogPatterns_PermissionDenied(t *testing.T) {
	snap := makeLogSnapshot("app", "open /etc/secret: permission denied")
	result := correlator().Correlate(snap)

	if !result.LogPatterns.ContainsPermissionDenied {
		t.Error("expected ContainsPermissionDenied=true")
	}
}

func TestLogPatterns_NoMatch(t *testing.T) {
	snap := makeLogSnapshot("app", "server started on :8080")
	result := correlator().Correlate(snap)

	if result.LogPatterns.ContainsOOMString || result.LogPatterns.ContainsConnectionRefused ||
		result.LogPatterns.ContainsPanicOrFatal || result.LogPatterns.ContainsPermissionDenied {
		t.Error("expected no log patterns for normal log line")
	}
}

func TestLogPatterns_EmptyLogs(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{}
	result := correlator().Correlate(snap)

	if result.LogPatterns == nil {
		t.Error("expected non-nil LogPatterns even with no logs")
	}
}

// ── Nil and edge case tests ───────────────────────────────────────────────────

func TestNilSnapshot_ReturnsEmptyNoPanic(t *testing.T) {
	result := correlator().Correlate(nil)

	if result.CorrelatedAt == nil {
		t.Error("expected non-nil CorrelatedAt even for nil snapshot")
	}
	if result.CausalSignals == nil {
		t.Error("expected non-nil CausalSignals even for nil snapshot")
	}
}

func TestEmptySnapshot_NoSignals(t *testing.T) {
	snap := &v1alpha1.EvidenceSnapshot{}
	result := correlator().Correlate(snap)

	if result.CausalSignals.PVCIsUnbound || result.CausalSignals.NodeMemoryPressureCoincident ||
		result.CausalSignals.ContainerHitConfiguredLimit {
		t.Error("empty snapshot should produce no true causal signals")
	}
}

// ── Property tests ────────────────────────────────────────────────────────────

// Feature: evidence-correlation, Property: pure-function
func TestProperty_CorrelationIsPure(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: evidence-correlation, Property: pure-function
		// For any EvidenceSnapshot, Correlate() returns identical results on repeated calls.
		hasLogs := rapid.Bool().Draw(rt, "hasLogs")
		hasPVC := rapid.Bool().Draw(rt, "hasPVC")

		snap := &v1alpha1.EvidenceSnapshot{}
		if hasLogs {
			snap.Logs = []v1alpha1.ContainerLogEvidence{
				{ContainerName: "app", Lines: []string{"some log line"}},
			}
		}
		if hasPVC {
			snap.Dependencies = &v1alpha1.DependencyEvidence{
				PVCs: []v1alpha1.PVCEvidence{{Name: "pvc", Phase: "Pending"}},
			}
		}

		c := correlator()
		r1 := c.Correlate(snap)
		r2 := c.Correlate(snap)

		// Key fields should be identical
		if r1.CausalSignals.PVCIsUnbound != r2.CausalSignals.PVCIsUnbound {
			rt.Fatalf("non-deterministic PVCIsUnbound: %v vs %v", r1.CausalSignals.PVCIsUnbound, r2.CausalSignals.PVCIsUnbound)
		}
		if r1.LogPatterns.ContainsOOMString != r2.LogPatterns.ContainsOOMString {
			rt.Fatalf("non-deterministic ContainsOOMString")
		}
		if len(r1.ChainPatterns) != len(r2.ChainPatterns) {
			rt.Fatalf("non-deterministic ChainPatterns length: %d vs %d", len(r1.ChainPatterns), len(r2.ChainPatterns))
		}
	})
}

// Feature: evidence-correlation, Property: nil-no-panic
func TestProperty_NilSnapshotNoPanic(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: evidence-correlation, Property: nil-no-panic
		// Correlate(nil) must never panic and must return a non-nil CorrelatedAt.
		result := correlator().Correlate(nil)
		if result.CorrelatedAt == nil {
			rt.Fatal("CorrelatedAt must be set even for nil snapshot")
		}
	})
}
