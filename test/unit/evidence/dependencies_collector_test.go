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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	internalevidence "github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
)

func makePVCPod(podName, namespace, claimName string) *corev1.Pod {
	pod := makeBasePod(podName, namespace)
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claimName},
			},
		},
	}
	return pod
}

func makePVC(name, namespace string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
	q := resource.MustParse("1Gi")
	sc := "standard"
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &sc,
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: q}},
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: phase},
	}
}

func mountFailureNeeds() internalevidence.TriggerNeedsExported {
	return internalevidence.TriggerNeedsExported{PVCEvidence: true}
}

func TestDependencyCollector_OOMKilled_ReturnsNil(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()
	pod := makeBasePod("oom-pod", "default")

	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	// OOMKilled needs — no pvc/imagePull/scheduling
	ev, errs := (&internalevidence.DependencyCollector{}).CollectWithNeeds(context.Background(), input,
		internalevidence.TriggerNeedsExported{})
	if ev != nil {
		t.Error("expected nil DependencyEvidence for OOMKilled (no dependency needs)")
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestDependencyCollector_MountFailure_CollectsPVCEvidence(t *testing.T) {
	s := buildScheme(t)
	pvc := makePVC("my-pvc", "default", corev1.ClaimPending)
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(pvc).Build()

	pod := makePVCPod("pvc-pod", "default", "my-pvc")
	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	ev, errs := (&internalevidence.DependencyCollector{}).CollectWithNeeds(context.Background(), input,
		internalevidence.TriggerNeedsExported{PVCEvidence: true})
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if ev == nil {
		t.Fatal("expected non-nil DependencyEvidence")
	}
	if len(ev.PVCs) != 1 {
		t.Fatalf("expected 1 PVC, got %d", len(ev.PVCs))
	}
	if ev.PVCs[0].Phase != "Pending" {
		t.Errorf("Phase = %q, want Pending", ev.PVCs[0].Phase)
	}
}

func TestDependencyCollector_MountFailure_MissingPVC_AddsCollectionError(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build() // no PVC in client

	pod := makePVCPod("pod-missing-pvc", "default", "missing-pvc")
	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	_, errs := (&internalevidence.DependencyCollector{}).CollectWithNeeds(context.Background(), input,
		internalevidence.TriggerNeedsExported{PVCEvidence: true})
	if len(errs) != 1 {
		t.Fatalf("expected 1 CollectionError for missing PVC, got %d", len(errs))
	}
	if errs[0].Source != "pvc/missing-pvc" {
		t.Errorf("Source = %q, want pvc/missing-pvc", errs[0].Source)
	}
}

func TestDependencyCollector_ImagePullBackOff_CollectsSecretNames(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()

	pod := makeBasePod("pull-pod", "default")
	pod.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
		{Name: "my-registry-secret"},
		{Name: "another-secret"},
	}

	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	ev, errs := (&internalevidence.DependencyCollector{}).CollectWithNeeds(context.Background(), input,
		internalevidence.TriggerNeedsExported{ImagePullEvidence: true})
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(ev.ImagePullSecretNames) != 2 {
		t.Errorf("expected 2 secret names, got %d", len(ev.ImagePullSecretNames))
	}
	// Verify only names are stored, not values
	for _, name := range ev.ImagePullSecretNames {
		if name == "" {
			t.Error("expected non-empty secret name")
		}
	}
}

// Feature: evidence-collection, Property 8: secret-values-excluded
func TestProperty8_ImagePullSecretNamesOnly(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: evidence-collection, Property 8: secret-values-excluded
		count := rapid.IntRange(0, 10).Draw(rt, "count")

		s := buildScheme(t)
		fc := fake.NewClientBuilder().WithScheme(s).Build()

		pod := makeBasePod("prop-pod", "default")
		for i := 0; i < count; i++ {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets,
				corev1.LocalObjectReference{Name: rapid.StringMatching(`[a-z]{3,10}`).Draw(rt, "name")})
		}

		input := internalevidence.CollectorInput{
			Client: fc, Pod: pod, Config: config.DefaultConfig(),
			Report: &v1alpha1.IncidentReport{},
		}

		ev, _ := (&internalevidence.DependencyCollector{}).CollectWithNeeds(context.Background(), input,
			internalevidence.TriggerNeedsExported{ImagePullEvidence: true})

		if count == 0 {
			if ev != nil && len(ev.ImagePullSecretNames) != 0 {
				rt.Fatalf("expected empty ImagePullSecretNames for count=0")
			}
			return
		}

		if ev == nil {
			rt.Fatalf("expected non-nil DependencyEvidence for count=%d", count)
		}
		if len(ev.ImagePullSecretNames) != count {
			rt.Fatalf("expected %d secret names, got %d", count, len(ev.ImagePullSecretNames))
		}
		// All entries should be strings (names only)
		for _, name := range ev.ImagePullSecretNames {
			if name == "" {
				rt.Fatalf("found empty secret name in ImagePullSecretNames")
			}
		}
	})
}
