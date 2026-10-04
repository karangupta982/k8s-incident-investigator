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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ---- helpers ----------------------------------------------------------------

func buildOwnershipScheme(t *testing.T) *runtime.Scheme {
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

func boolPtr(b bool) *bool { return &b }

func makeOwnerRef(kind, name string, uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: boolPtr(true),
	}
}

func podWithOwner(kind, name string, uid types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{makeOwnerRef(kind, name, uid)},
		},
	}
}

// ---- table-driven tests -----------------------------------------------------

func TestOwnershipResolver_PodToDeployment(t *testing.T) {
	s := buildOwnershipScheme(t)

	rsUID := types.UID("rs-uid-1")
	deployUID := types.UID("deploy-uid-1")

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rs",
			Namespace: "default",
			UID:       rsUID,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerRef("Deployment", "my-deployment", deployUID),
			},
		},
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment",
			Namespace: "default",
			UID:       deployUID,
		},
	}

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(rs, deploy).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("ReplicaSet", "my-rs", rsUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil {
		t.Fatal("expected non-nil WorkloadRef")
	}
	if ref.Kind != "Deployment" {
		t.Errorf("Kind = %q, want Deployment", ref.Kind)
	}
	if ref.Name != "my-deployment" {
		t.Errorf("Name = %q, want my-deployment", ref.Name)
	}
	if ref.UID != deployUID {
		t.Errorf("UID = %q, want %q", ref.UID, deployUID)
	}
}

func TestOwnershipResolver_PodToStatefulSet(t *testing.T) {
	s := buildOwnershipScheme(t)
	ssUID := types.UID("ss-uid-1")

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ss", Namespace: "default", UID: ssUID},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ss).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("StatefulSet", "my-ss", ssUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil || ref.Kind != "StatefulSet" {
		t.Errorf("expected WorkloadRef{Kind:StatefulSet}, got %+v", ref)
	}
}

func TestOwnershipResolver_PodToDaemonSet(t *testing.T) {
	s := buildOwnershipScheme(t)
	dsUID := types.UID("ds-uid-1")

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-ds", Namespace: "default", UID: dsUID},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(ds).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("DaemonSet", "my-ds", dsUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil || ref.Kind != "DaemonSet" {
		t.Errorf("expected WorkloadRef{Kind:DaemonSet}, got %+v", ref)
	}
}

func TestOwnershipResolver_PodToJob_NoCronJob(t *testing.T) {
	s := buildOwnershipScheme(t)
	jobUID := types.UID("job-uid-1")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-job",
			Namespace: "default",
			UID:       jobUID,
			// No ownerReferences — standalone Job
		},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(job).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("Job", "my-job", jobUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil || ref.Kind != "Job" {
		t.Errorf("expected WorkloadRef{Kind:Job}, got %+v", ref)
	}
}

func TestOwnershipResolver_PodToJobToCronJob(t *testing.T) {
	s := buildOwnershipScheme(t)
	jobUID := types.UID("job-uid-2")
	cronUID := types.UID("cron-uid-2")

	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cron", Namespace: "default", UID: cronUID},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cron-job",
			Namespace: "default",
			UID:       jobUID,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerRef("CronJob", "my-cron", cronUID),
			},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(cj, job).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("Job", "my-cron-job", jobUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref == nil || ref.Kind != "CronJob" {
		t.Errorf("expected WorkloadRef{Kind:CronJob}, got %+v", ref)
	}
}

func TestOwnershipResolver_RSNotFound_FallbackToPod(t *testing.T) {
	s := buildOwnershipScheme(t)
	// No RS in the fake client
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("ReplicaSet", "missing-rs", "rs-uid-missing")
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error (should fall back, not error): %v", err)
	}
	if ref != nil {
		t.Errorf("expected nil WorkloadRef (Pod fallback), got %+v", ref)
	}
}

func TestOwnershipResolver_RSFoundDeploymentNotFound_FallbackToPod(t *testing.T) {
	s := buildOwnershipScheme(t)
	rsUID := types.UID("rs-orphan")
	deployUID := types.UID("deploy-missing")

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-rs",
			Namespace: "default",
			UID:       rsUID,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerRef("Deployment", "missing-deploy", deployUID),
			},
		},
	}
	// Deployment is NOT in the fake client
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(rs).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	pod := podWithOwner("ReplicaSet", "orphan-rs", rsUID)
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Must NOT return ReplicaSet as workload kind — that's not a valid top-level kind
	if ref != nil {
		t.Errorf("expected nil WorkloadRef (Pod fallback), got %+v (kind %q)", ref, ref.Kind)
	}
}

func TestOwnershipResolver_NoControllerOwner_ReturnsNil(t *testing.T) {
	s := buildOwnershipScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())

	// Pod with no ownerReferences
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone-pod", Namespace: "default"},
	}
	ref, err := resolver.Resolve(context.Background(), pod)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref != nil {
		t.Errorf("expected nil WorkloadRef, got %+v", ref)
	}
}

// ---- property-based tests ---------------------------------------------------

// Feature: incident-investigator-foundation, Property 5
// For a fixed owner graph, Resolve returns the same WorkloadRef on every call.
func TestProperty5_OwnershipResolutionIsDeterministic(t *testing.T) {
	s := buildOwnershipScheme(t)

	rsUID := types.UID("rs-det-uid")
	deployUID := types.UID("deploy-det-uid")

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "det-rs",
			Namespace: "default",
			UID:       rsUID,
			OwnerReferences: []metav1.OwnerReference{
				makeOwnerRef("Deployment", "det-deployment", deployUID),
			},
		},
	}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "det-deployment", Namespace: "default", UID: deployUID},
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(rs, deploy).Build()
	resolver := investigation.NewOwnershipResolver(fc, noopLogger())
	pod := podWithOwner("ReplicaSet", "det-rs", rsUID)

	rapid.Check(t, func(rt *rapid.T) {
		// Feature: incident-investigator-foundation, Property 5
		ref1, err1 := resolver.Resolve(context.Background(), pod)
		ref2, err2 := resolver.Resolve(context.Background(), pod)

		if err1 != nil || err2 != nil {
			rt.Fatalf("unexpected errors: %v %v", err1, err2)
		}
		if ref1 == nil || ref2 == nil {
			rt.Fatalf("expected non-nil refs, got %v %v", ref1, ref2)
		}
		if *ref1 != *ref2 {
			rt.Fatalf("non-deterministic: %+v != %+v", *ref1, *ref2)
		}
	})
}
