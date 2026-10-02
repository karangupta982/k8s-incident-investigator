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

package evidence_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	internalevidence "github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
)

func int32Ptr(n int32) *int32 { return &n }

func makeReportWithWorkload(kind, name, namespace string) *v1alpha1.IncidentReport {
	return &v1alpha1.IncidentReport{
		Spec: v1alpha1.IncidentReportSpec{
			Workload: &v1alpha1.WorkloadRef{Kind: kind, Name: name, Namespace: namespace},
		},
		Status: v1alpha1.IncidentReportStatus{
			WorkloadOwnerResolved: true,
		},
	}
}

func TestWorkloadCollector_NilWorkload_ReturnsNilNoError(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()

	report := &v1alpha1.IncidentReport{} // no workload
	input := internalevidence.CollectorInput{
		Client: fc, Pod: makeBasePod("p", "default"),
		Config: config.DefaultConfig(), Report: report,
	}

	ev, errs := (&internalevidence.WorkloadCollector{}).Collect(context.Background(), input)
	if ev != nil {
		t.Error("expected nil WorkloadEvidence for no workload")
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestWorkloadCollector_Deployment_MapsCorrectly(t *testing.T) {
	s := buildScheme(t)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(3),
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 2,
			Conditions: []appsv1.DeploymentCondition{
				{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
			},
		},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(deploy).Build()

	report := makeReportWithWorkload("Deployment", "my-deploy", "default")
	input := internalevidence.CollectorInput{
		Client: fc, Pod: makeBasePod("p", "default"),
		Config: config.DefaultConfig(), Report: report,
	}

	ev, errs := (&internalevidence.WorkloadCollector{}).Collect(context.Background(), input)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if ev == nil {
		t.Fatal("expected non-nil WorkloadEvidence")
	}
	if ev.Kind != "Deployment" {
		t.Errorf("Kind = %q, want Deployment", ev.Kind)
	}
	if ev.DesiredReplicas != 3 {
		t.Errorf("DesiredReplicas = %d, want 3", ev.DesiredReplicas)
	}
	if ev.ReadyReplicas != 2 {
		t.Errorf("ReadyReplicas = %d, want 2", ev.ReadyReplicas)
	}
	if ev.UpdateStrategy != "RollingUpdate" {
		t.Errorf("UpdateStrategy = %q, want RollingUpdate", ev.UpdateStrategy)
	}
}

func TestWorkloadCollector_DeploymentConditionsCappedAt10(t *testing.T) {
	s := buildScheme(t)
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "many-conds", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	// Add 15 conditions
	for i := 0; i < 15; i++ {
		deploy.Status.Conditions = append(deploy.Status.Conditions,
			appsv1.DeploymentCondition{
				Type:   appsv1.DeploymentConditionType("Cond" + string(rune('A'+i))),
				Status: corev1.ConditionTrue,
			})
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(deploy).Build()

	report := makeReportWithWorkload("Deployment", "many-conds", "default")
	input := internalevidence.CollectorInput{
		Client: fc, Pod: makeBasePod("p", "default"),
		Config: config.DefaultConfig(), Report: report,
	}

	ev, _ := (&internalevidence.WorkloadCollector{}).Collect(context.Background(), input)
	if len(ev.Conditions) > 10 {
		t.Errorf("expected at most 10 conditions, got %d", len(ev.Conditions))
	}
}

func TestWorkloadCollector_FetchError_ReturnsCollectionError(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build() // no deployment

	report := makeReportWithWorkload("Deployment", "missing-deploy", "default")
	input := internalevidence.CollectorInput{
		Client: fc, Pod: makeBasePod("p", "default"),
		Config: config.DefaultConfig(), Report: report,
	}

	ev, errs := (&internalevidence.WorkloadCollector{}).Collect(context.Background(), input)
	if ev != nil {
		t.Error("expected nil WorkloadEvidence on fetch error")
	}
	if len(errs) != 1 || errs[0].Source != "workload" {
		t.Errorf("expected 1 'workload' CollectionError, got %v", errs)
	}
}
