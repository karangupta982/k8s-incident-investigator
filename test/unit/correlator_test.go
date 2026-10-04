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
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ---- helpers ----------------------------------------------------------------

func buildCorrelatorScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add clientgo scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add v1alpha1 scheme: %v", err)
	}
	return s
}

func makeActiveReport(name, namespace string, phase v1alpha1.IncidentPhase) *v1alpha1.IncidentReport {
	return &v1alpha1.IncidentReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				investigation.LabelActive: "true",
			},
		},
		Status: v1alpha1.IncidentReportStatus{
			Phase: phase,
		},
	}
}

func workloadRef(kind, name, namespace string) *v1alpha1.WorkloadRef {
	return &v1alpha1.WorkloadRef{Kind: kind, Name: name, Namespace: namespace}
}

// ---- naming tests -----------------------------------------------------------

func TestGenerateActiveName_Examples(t *testing.T) {
	tests := []struct {
		workloadName string
		workloadKind string
		want         string
	}{
		{"payment-api", "Deployment", "payment-api-deployment-active"},
		{"worker", "DaemonSet", "worker-daemonset-active"},
		{"my-app", "StatefulSet", "my-app-statefulset-active"},
		{"batch-job", "Job", "batch-job-job-active"},
		{"periodic", "CronJob", "periodic-cronjob-active"},
	}
	for _, tt := range tests {
		t.Run(tt.workloadName+"/"+tt.workloadKind, func(t *testing.T) {
			got := investigation.GenerateActiveName(tt.workloadName, tt.workloadKind)
			if got != tt.want {
				t.Errorf("GenerateActiveName(%q, %q) = %q, want %q",
					tt.workloadName, tt.workloadKind, got, tt.want)
			}
		})
	}
}

func TestGenerateActiveName_LongName_TruncatedTo63(t *testing.T) {
	longName := strings.Repeat("a", 100)
	kind := "Deployment"
	got := investigation.GenerateActiveName(longName, kind)
	if len(got) > 63 {
		t.Errorf("name length = %d, want <= 63; got %q", len(got), got)
	}
	if !strings.HasSuffix(got, "-deployment-active") {
		t.Errorf("expected suffix -deployment-active, got %q", got)
	}
}

func TestGenerateHistoricalName_Deterministic(t *testing.T) {
	ts := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC)
	n1 := investigation.GenerateHistoricalName("payment-api", "Deployment", "default", ts)
	n2 := investigation.GenerateHistoricalName("payment-api", "Deployment", "default", ts)
	if n1 != n2 {
		t.Errorf("expected deterministic name, got %q and %q", n1, n2)
	}
	if len(n1) > 63 {
		t.Errorf("name length %d > 63", len(n1))
	}
}

func TestGenerateHistoricalName_DifferentForDifferentStartedAt(t *testing.T) {
	t1 := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC)
	t2 := t1.Add(10 * time.Minute)
	n1 := investigation.GenerateHistoricalName("payment-api", "Deployment", "default", t1)
	n2 := investigation.GenerateHistoricalName("payment-api", "Deployment", "default", t2)
	if n1 == n2 {
		t.Errorf("expected different names for different startedAt, got %q for both", n1)
	}
}

func TestGenerateHistoricalName_ContainsDate(t *testing.T) {
	ts := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	n := investigation.GenerateHistoricalName("my-app", "Deployment", "prod", ts)
	if !strings.Contains(n, "20260315") {
		t.Errorf("expected date 20260315 in name %q", n)
	}
}

// ---- correlator tests -------------------------------------------------------

func TestCorrelator_ActiveReportExists_Investigating(t *testing.T) {
	s := buildCorrelatorScheme(t)

	wl := workloadRef("Deployment", "payment-api", "default")
	activeName := investigation.GenerateActiveName("payment-api", "Deployment")
	report := makeActiveReport(activeName, "default", v1alpha1.PhaseInvestigating)

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(report).
		WithStatusSubresource(report).Build()
	correlator := investigation.NewIncidentCorrelator(fc, noopLogger())

	result, err := correlator.Correlate(context.Background(), wl, "some-pod", "default", investigation.TriggerResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldCreate {
		t.Error("ShouldCreate should be false when active report exists")
	}
	if result.ExistingReport == nil {
		t.Error("ExistingReport should be non-nil")
	}
	if result.ExistingReport.Name != activeName {
		t.Errorf("ExistingReport.Name = %q, want %q", result.ExistingReport.Name, activeName)
	}
}

func TestCorrelator_NoActiveReport_ShouldCreate(t *testing.T) {
	s := buildCorrelatorScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	correlator := investigation.NewIncidentCorrelator(fc, noopLogger())

	wl := workloadRef("Deployment", "payment-api", "default")
	result, err := correlator.Correlate(context.Background(), wl, "some-pod", "default", investigation.TriggerResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ShouldCreate {
		t.Error("ShouldCreate should be true when no active report exists")
	}
	if result.ExistingReport != nil {
		t.Error("ExistingReport should be nil when ShouldCreate=true")
	}
}

func TestCorrelator_StaleResolvedActiveReport_ShouldCreate(t *testing.T) {
	s := buildCorrelatorScheme(t)

	wl := workloadRef("Deployment", "payment-api", "default")
	activeName := investigation.GenerateActiveName("payment-api", "Deployment")
	// Stale: active-named report but already Resolved (crash mid-resolution-transition)
	report := makeActiveReport(activeName, "default", v1alpha1.PhaseResolved)

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(report).
		WithStatusSubresource(report).Build()
	correlator := investigation.NewIncidentCorrelator(fc, noopLogger())

	result, err := correlator.Correlate(context.Background(), wl, "some-pod", "default", investigation.TriggerResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ShouldCreate {
		t.Error("ShouldCreate should be true for stale resolved active-named report")
	}
}

func TestCorrelator_PodFallback_UsePodActiveName(t *testing.T) {
	s := buildCorrelatorScheme(t)

	podActiveName := investigation.GeneratePodActiveName("my-pod")
	report := makeActiveReport(podActiveName, "default", v1alpha1.PhaseInvestigating)

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(report).
		WithStatusSubresource(report).Build()
	correlator := investigation.NewIncidentCorrelator(fc, noopLogger())

	// nil workload → Pod-level fallback
	result, err := correlator.Correlate(context.Background(), nil, "my-pod", "default", investigation.TriggerResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ShouldCreate {
		t.Error("ShouldCreate should be false — active pod report exists")
	}
	if result.ExistingReport == nil {
		t.Error("ExistingReport should be non-nil")
	}
}

// ---- property-based tests ---------------------------------------------------

// Feature: incident-investigator-foundation, Property 6
// GenerateActiveName is deterministic: same inputs always produce same output.
func TestProperty6_ActiveNameIsDeterministic(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 6
		workloadName := rapid.StringMatching(`[a-z][a-z0-9-]{0,29}`).Draw(rt, "workloadName")
		workloadKind := rapid.SampledFrom([]string{
			"Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob",
		}).Draw(rt, "workloadKind")

		n1 := investigation.GenerateActiveName(workloadName, workloadKind)
		n2 := investigation.GenerateActiveName(workloadName, workloadKind)
		n3 := investigation.GenerateActiveName(workloadName, workloadKind)

		if n1 != n2 || n2 != n3 {
			rt.Fatalf("non-deterministic: %q %q %q", n1, n2, n3)
		}
		if len(n1) > 63 {
			rt.Fatalf("name length %d > 63 for input %q/%q", len(n1), workloadName, workloadKind)
		}
		suffix := "-" + strings.ToLower(workloadKind) + "-active"
		if !strings.HasSuffix(n1, suffix) {
			rt.Fatalf("name %q does not end with expected suffix %q", n1, suffix)
		}
	})
}

// TestGeneratePodActiveName_LongPodName verifies the suffix is always correct.
func TestGeneratePodActiveName_LongPodName(t *testing.T) {
	longPod := strings.Repeat("p", 100)
	got := investigation.GeneratePodActiveName(longPod)
	if len(got) > 63 {
		t.Errorf("name length %d > 63", len(got))
	}
	if !strings.HasSuffix(got, "-pod-active") {
		t.Errorf("expected -pod-active suffix, got %q", got)
	}
}

// TestGenerateHistoricalName_LengthBound verifies long names are always ≤ 63.
func TestGenerateHistoricalName_LengthBound(t *testing.T) {
	longName := strings.Repeat("x", 100)
	ts := time.Now()
	got := investigation.GenerateHistoricalName(longName, "Deployment", "default", ts)
	if len(got) > 63 {
		t.Errorf("name length %d > 63", len(got))
	}
}
