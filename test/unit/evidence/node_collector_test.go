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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	internalevidence "github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
)

func makeTestNode(name string, memoryPressure corev1.ConditionStatus) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				{Type: corev1.NodeMemoryPressure, Status: memoryPressure},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
				{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse},
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			NodeInfo: corev1.NodeSystemInfo{KernelVersion: "5.15.0"},
		},
	}
}

func TestNodeCollector_EmptyNodeName_ReturnsNil(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()

	pod := makeBasePod("no-node-pod", "default")
	pod.Spec.NodeName = "" // not scheduled

	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	ev, errs := (&internalevidence.NodeCollector{}).Collect(context.Background(), input)
	if ev != nil {
		t.Error("expected nil NodeEvidence for unscheduled pod")
	}
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestNodeCollector_MemoryPressureTrue_MappedCorrectly(t *testing.T) {
	s := buildScheme(t)
	node := makeTestNode("my-node", corev1.ConditionTrue)
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(node).Build()

	pod := makeBasePod("scheduled-pod", "default")
	pod.Spec.NodeName = "my-node"

	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	ev, errs := (&internalevidence.NodeCollector{}).Collect(context.Background(), input)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if ev == nil {
		t.Fatal("expected non-nil NodeEvidence")
	}
	if ev.MemoryPressure != "True" {
		t.Errorf("MemoryPressure = %q, want True", ev.MemoryPressure)
	}
	if ev.Ready != "True" {
		t.Errorf("Ready = %q, want True", ev.Ready)
	}
	if ev.KernelVersion != "5.15.0" {
		t.Errorf("KernelVersion = %q, want 5.15.0", ev.KernelVersion)
	}
	if ev.AllocatableMemory == "" {
		t.Error("expected non-empty AllocatableMemory")
	}
}

func TestNodeCollector_NodeNotFound_ReturnsCollectionError(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build() // no node in client

	pod := makeBasePod("pod-with-missing-node", "default")
	pod.Spec.NodeName = "missing-node"

	input := internalevidence.CollectorInput{
		Client: fc, Pod: pod, Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	ev, errs := (&internalevidence.NodeCollector{}).Collect(context.Background(), input)
	if ev != nil {
		t.Error("expected nil NodeEvidence when node not found")
	}
	if len(errs) != 1 || errs[0].Source != "node" {
		t.Errorf("expected 1 'node' CollectionError, got %v", errs)
	}
}
