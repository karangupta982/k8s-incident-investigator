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
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	internalevidence "github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
)

func makeBasePod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:alpine",
					Resources: corev1.ResourceRequirements{
						Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
						Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
					},
				},
			},
		},
	}
}

func makeCollectorInput(pod *corev1.Pod) internalevidence.CollectorInput {
	report := &v1alpha1.IncidentReport{
		Status: v1alpha1.IncidentReportStatus{
			Trigger: &v1alpha1.TriggerInfo{Type: v1alpha1.TriggerOOMKilled},
		},
	}
	return internalevidence.CollectorInput{
		Report: report,
		Pod:    pod,
	}
}

func TestPodCollector_NilPod_ReturnsCollectionError(t *testing.T) {
	input := makeCollectorInput(nil)
	ev, errs := (&internalevidence.PodCollector{}).Collect(context.Background(), input)
	if ev != nil {
		t.Error("expected nil PodEvidence for nil pod")
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 CollectionError, got %d", len(errs))
	}
	if errs[0].Source != "pod" {
		t.Errorf("expected source 'pod', got %q", errs[0].Source)
	}
}

func TestPodCollector_OOMKilledContainer(t *testing.T) {
	pod := makeBasePod("oom-pod", "default")
	pod.Status = corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name:         "app",
				RestartCount: 3,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
				},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:   "OOMKilled",
						ExitCode: 137,
					},
				},
			},
		},
	}

	input := makeCollectorInput(pod)
	ev, errs := (&internalevidence.PodCollector{}).Collect(context.Background(), input)

	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	if ev == nil {
		t.Fatal("expected non-nil PodEvidence")
	}
	if len(ev.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ev.Containers))
	}
	ce := ev.Containers[0]
	if ce.LastTerminationReason != "OOMKilled" {
		t.Errorf("LastTerminationReason = %q, want OOMKilled", ce.LastTerminationReason)
	}
	if ce.RestartCount != 3 {
		t.Errorf("RestartCount = %d, want 3", ce.RestartCount)
	}
	if _, ok := ce.ResourceLimits["memory"]; !ok {
		t.Error("expected memory limit in ResourceLimits")
	}
}

func TestPodCollector_HTTPGetProbe(t *testing.T) {
	pod := makeBasePod("probe-pod", "default")
	pod.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/healthz",
				Port: intstr.FromInt32(8080),
			},
		},
		FailureThreshold: 3,
	}

	input := makeCollectorInput(pod)
	ev, errs := (&internalevidence.PodCollector{}).Collect(context.Background(), input)

	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if ev.Containers[0].ReadinessProbe == nil {
		t.Fatal("expected ReadinessProbe summary")
	}
	rp := ev.Containers[0].ReadinessProbe
	if rp.Type != "HTTPGet" {
		t.Errorf("Type = %q, want HTTPGet", rp.Type)
	}
	if rp.HTTPPath != "/healthz" {
		t.Errorf("HTTPPath = %q, want /healthz", rp.HTTPPath)
	}
	if rp.Port != 8080 {
		t.Errorf("Port = %d, want 8080", rp.Port)
	}
	if rp.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", rp.FailureThreshold)
	}
}

func TestPodCollector_MultipleContainers(t *testing.T) {
	pod := makeBasePod("multi-pod", "default")
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "sidecar", Image: "busybox"})

	input := makeCollectorInput(pod)
	ev, errs := (&internalevidence.PodCollector{}).Collect(context.Background(), input)

	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(ev.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(ev.Containers))
	}
}

func TestPodCollector_PVCVolumeMount(t *testing.T) {
	pod := makeBasePod("pvc-pod", "default")
	pod.Spec.Volumes = []corev1.Volume{
		{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "my-pvc"},
			},
		},
	}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: "data", MountPath: "/data"},
	}

	input := makeCollectorInput(pod)
	ev, _ := (&internalevidence.PodCollector{}).Collect(context.Background(), input)

	if len(ev.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(ev.VolumeMounts))
	}
	vme := ev.VolumeMounts[0]
	if vme.VolumeType != "PVC" {
		t.Errorf("VolumeType = %q, want PVC", vme.VolumeType)
	}
	if vme.ClaimName != "my-pvc" {
		t.Errorf("ClaimName = %q, want my-pvc", vme.ClaimName)
	}
}
