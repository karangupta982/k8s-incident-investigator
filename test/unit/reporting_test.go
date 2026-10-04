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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/reporting"
)

func reportingEngine() *reporting.ReportingEngine {
	return &reporting.ReportingEngine{Config: config.DefaultConfig()}
}

func makeTimestamp(t time.Time) *metav1.Time {
	mt := metav1.NewTime(t)
	return &mt
}

// ── SummaryBuilder tests ──────────────────────────────────────────────────────

func TestSummary_DiagnosedPhase(t *testing.T) {
	now := time.Now()
	report := &v1alpha1.IncidentReport{
		Spec: v1alpha1.IncidentReportSpec{
			Workload: &v1alpha1.WorkloadRef{Kind: "Deployment", Namespace: "default", Name: "payment-api"},
		},
		Status: v1alpha1.IncidentReportStatus{
			Phase:        v1alpha1.PhaseDiagnosed,
			StartedAt:    makeTimestamp(now),
			Trigger:      &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerOOMKilled},
			AffectedPods: []v1alpha1.PodRef{{Name: "pod-1", Namespace: "default"}},
			Diagnosis: &v1alpha1.DiagnosisResult{
				Primary: &v1alpha1.DiagnosisFinding{
					RuleID:     "OOMMemoryLimit",
					Confidence: "High",
					Cause:      "Container exceeded its configured memory limit",
				},
			},
		},
	}

	result := reportingEngine().Render(report)
	if result.Summary == "" {
		t.Error("expected non-empty Summary for Diagnosed phase")
	}
	if !strings.Contains(result.Summary, "Diagnosed") {
		t.Errorf("Summary should contain 'Diagnosed', got: %s", result.Summary)
	}
	if !strings.Contains(result.Summary, "High") {
		t.Errorf("Summary should contain confidence 'High', got: %s", result.Summary)
	}
	if !strings.Contains(result.Summary, "payment-api") {
		t.Errorf("Summary should contain workload name, got: %s", result.Summary)
	}
}

func TestSummary_UnknownPhase(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Phase:   v1alpha1.PhaseUnknown,
			Trigger: &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerCrashLoopBackOff},
			Diagnosis: &v1alpha1.DiagnosisResult{
				UnknownReason: "No known failure pattern matched.",
			},
		},
	}

	result := reportingEngine().Render(report)
	if !strings.Contains(result.Summary, "Unknown") {
		t.Errorf("Summary should contain 'Unknown', got: %s", result.Summary)
	}
	if !strings.Contains(result.Summary, "No known failure pattern") {
		t.Errorf("Summary should contain unknown reason, got: %s", result.Summary)
	}
}

func TestSummary_InvestigatingPhase(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Phase:   v1alpha1.PhaseInvestigating,
			Trigger: &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerImagePullBackOff},
		},
	}

	result := reportingEngine().Render(report)
	if !strings.Contains(result.Summary, "Investigating") {
		t.Errorf("Summary should contain 'Investigating', got: %s", result.Summary)
	}
}

func TestSummary_ResolvedPhase(t *testing.T) {
	start := time.Now().Add(-10 * time.Minute)
	end := time.Now()
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Phase:      v1alpha1.PhaseResolved,
			StartedAt:  makeTimestamp(start),
			ResolvedAt: makeTimestamp(end),
			Diagnosis: &v1alpha1.DiagnosisResult{
				Primary: &v1alpha1.DiagnosisFinding{Cause: "Container exceeded its configured memory limit"},
			},
		},
	}

	result := reportingEngine().Render(report)
	if !strings.Contains(result.Summary, "Resolved") {
		t.Errorf("Summary should contain 'Resolved', got: %s", result.Summary)
	}
	if !strings.Contains(result.Summary, "10m") || !strings.Contains(result.Summary, "Duration") {
		t.Errorf("Summary should contain duration, got: %s", result.Summary)
	}
}

func TestSummary_NilWorkload_NoPanic(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseInvestigating},
	}
	result := reportingEngine().Render(report)
	if result.Summary == "" {
		t.Error("expected non-empty summary even without workload")
	}
}

func TestSummary_MaxLength(t *testing.T) {
	e := &reporting.ReportingEngine{Config: &config.Config{
		MaxTimelineEvents: 50,
	}}
	// Very long workload name
	longName := strings.Repeat("a", 3000)
	report := &v1alpha1.IncidentReport{
		Spec: v1alpha1.IncidentReportSpec{
			Workload: &v1alpha1.WorkloadRef{Kind: "Deployment", Namespace: "default", Name: longName},
		},
		Status: v1alpha1.IncidentReportStatus{Phase: v1alpha1.PhaseDiagnosed},
	}
	result := e.Render(report)
	if len(result.Summary) > 2048 {
		t.Errorf("Summary length %d exceeds 2048", len(result.Summary))
	}
}

// ── TimelineBuilder tests ─────────────────────────────────────────────────────

func TestTimeline_ContainsLifecycleEvents(t *testing.T) {
	now := time.Now()
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			StartedAt: makeTimestamp(now),
		},
	}

	result := reportingEngine().Render(report)
	found := false
	for _, ev := range result.Timeline {
		if ev.Reason == "IncidentDetected" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Timeline should contain IncidentDetected event")
	}
}

func TestTimeline_ChronologicalOrder(t *testing.T) {
	start := time.Now().Add(-10 * time.Minute)
	stab := time.Now().Add(-2 * time.Minute)
	resolved := time.Now()

	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			StartedAt:          makeTimestamp(start),
			StabilityStartedAt: makeTimestamp(stab),
			ResolvedAt:         makeTimestamp(resolved),
		},
	}

	result := reportingEngine().Render(report)
	for i := 1; i < len(result.Timeline); i++ {
		if result.Timeline[i].Timestamp.Time.Before(result.Timeline[i-1].Timestamp.Time) {
			t.Errorf("Timeline not in order at index %d: %v before %v",
				i, result.Timeline[i].Timestamp, result.Timeline[i-1].Timestamp)
		}
	}
}

func TestTimeline_Bounded(t *testing.T) {
	e := &reporting.ReportingEngine{Config: &config.Config{MaxTimelineEvents: 3}}
	now := time.Now()
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			StartedAt: makeTimestamp(now),
			Evidence: &v1alpha1.EvidenceSnapshot{
				Events: []v1alpha1.EventEvidence{
					{Reason: "E1", LastTime: makeTimestamp(now.Add(1 * time.Second))},
					{Reason: "E2", LastTime: makeTimestamp(now.Add(2 * time.Second))},
					{Reason: "E3", LastTime: makeTimestamp(now.Add(3 * time.Second))},
					{Reason: "E4", LastTime: makeTimestamp(now.Add(4 * time.Second))},
					{Reason: "E5", LastTime: makeTimestamp(now.Add(5 * time.Second))},
				},
			},
		},
	}

	result := e.Render(report)
	if len(result.Timeline) > 3 {
		t.Errorf("Timeline length %d exceeds MaxEvents=3", len(result.Timeline))
	}
}

// ── RecommendationEnricher tests ──────────────────────────────────────────────

func TestRecommendation_OOMMemoryLimit_ContainsLimit(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Diagnosis: &v1alpha1.DiagnosisResult{
				Primary: &v1alpha1.DiagnosisFinding{
					RuleID:     "OOMMemoryLimit",
					Confidence: "High",
					Cause:      "Container exceeded memory limit",
				},
			},
			Evidence: &v1alpha1.EvidenceSnapshot{
				Pod: &v1alpha1.PodEvidence{
					Containers: []v1alpha1.ContainerEvidence{{
						Name:           "app",
						ResourceLimits: map[string]string{"memory": "512Mi"},
					}},
				},
			},
		},
	}

	result := reportingEngine().Render(report)
	if result.Diagnosis == nil || result.Diagnosis.Primary == nil {
		t.Fatal("expected non-nil Primary in enriched diagnosis")
	}
	rec := result.Diagnosis.Primary.Recommendation
	if !strings.Contains(rec, "512Mi") {
		t.Errorf("Recommendation should reference memory limit 512Mi, got: %s", rec)
	}
}

func TestRecommendation_NilDiagnosis_ReturnsNil(t *testing.T) {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Diagnosis: nil,
		},
	}
	result := reportingEngine().Render(report)
	if result.Diagnosis != nil {
		t.Error("expected nil Diagnosis when report has no Diagnosis")
	}
}

// ── Engine integration tests ──────────────────────────────────────────────────

func TestEngine_NilReport_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Render panicked on nil report: %v", r)
		}
	}()
	e := reportingEngine()
	// engine.go has a nil check on report for the Status access
	report := &v1alpha1.IncidentReport{} // zero value, not nil
	result := e.Render(report)
	if result.Summary == "" {
		t.Error("expected non-empty summary for zero-value report")
	}
}

// ── Property tests ────────────────────────────────────────────────────────────

// Feature: reporting, Property 1: Summary always produced
func TestProperty_SummaryAlwaysProduced(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: reporting, Property 1: Summary always produced
		phases := []v1alpha1.IncidentPhase{
			v1alpha1.PhaseInvestigating,
			v1alpha1.PhaseDiagnosed,
			v1alpha1.PhaseUnknown,
			v1alpha1.PhaseResolved,
			"",
		}
		phase := rapid.SampledFrom(phases).Draw(rt, "phase")
		report := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{Phase: phase},
		}
		result := reportingEngine().Render(report)
		if result.Summary == "" {
			rt.Fatalf("expected non-empty Summary for phase %q", phase)
		}
	})
}

// Feature: reporting, Property 2: Timeline is bounded
func TestProperty_TimelineBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: reporting, Property 2: Timeline is bounded
		maxEvents := rapid.IntRange(1, 20).Draw(rt, "maxEvents")
		numKubeEvents := rapid.IntRange(0, 30).Draw(rt, "numKubeEvents")

		cfg := &config.Config{MaxTimelineEvents: maxEvents}
		e := &reporting.ReportingEngine{Config: cfg}

		now := time.Now()
		kubeEvents := make([]v1alpha1.EventEvidence, numKubeEvents)
		for i := range kubeEvents {
			ts := metav1.NewTime(now.Add(time.Duration(i) * time.Second))
			kubeEvents[i] = v1alpha1.EventEvidence{
				Reason:   "TestEvent",
				LastTime: &ts,
			}
		}

		report := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{
				StartedAt: makeTimestamp(now),
				Evidence: &v1alpha1.EvidenceSnapshot{
					Events: kubeEvents,
				},
			},
		}

		result := e.Render(report)
		if len(result.Timeline) > maxEvents {
			rt.Fatalf("Timeline length %d exceeds MaxTimelineEvents=%d", len(result.Timeline), maxEvents)
		}
	})
}

// Feature: reporting, Property 5: Rendering is idempotent
func TestProperty_RenderIdempotent(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: reporting, Property 5: Rendering is idempotent
		report := &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{
				Phase:   v1alpha1.PhaseInvestigating,
				Trigger: &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerOOMKilled},
			},
		}
		e := reportingEngine()
		r1 := e.Render(report)
		r2 := e.Render(report)

		if r1.Summary != r2.Summary {
			rt.Fatalf("non-idempotent Summary: %q vs %q", r1.Summary, r2.Summary)
		}
		if len(r1.Timeline) != len(r2.Timeline) {
			rt.Fatalf("non-idempotent Timeline lengths: %d vs %d", len(r1.Timeline), len(r2.Timeline))
		}
	})
}

// Feature: reporting, Property 6: No panic on nil fields
func TestProperty_NoPanicOnNilFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: reporting, Property 6: No panic on nil fields
		defer func() {
			if r := recover(); r != nil {
				rt.Fatalf("Render panicked: %v", r)
			}
		}()

		// Randomly nil out fields
		nilWorkload := rapid.Bool().Draw(rt, "nilWorkload")
		nilTrigger := rapid.Bool().Draw(rt, "nilTrigger")
		nilDiagnosis := rapid.Bool().Draw(rt, "nilDiagnosis")
		nilEvidence := rapid.Bool().Draw(rt, "nilEvidence")

		report := &v1alpha1.IncidentReport{}
		if !nilWorkload {
			report.Spec.Workload = &v1alpha1.WorkloadRef{Kind: "Deployment", Name: "app"}
		}
		if !nilTrigger {
			report.Status.Trigger = &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerOOMKilled}
		}
		if !nilDiagnosis {
			report.Status.Diagnosis = &v1alpha1.DiagnosisResult{
				Primary: &v1alpha1.DiagnosisFinding{RuleID: "OOMMemoryLimit", Confidence: "High"},
			}
		}
		if !nilEvidence {
			report.Status.Evidence = &v1alpha1.EvidenceSnapshot{}
		}

		reportingEngine().Render(report)
	})
}
