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

package evidence

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// NodeCollector collects Layer 4 evidence from the node the affected Pod ran on.
type NodeCollector struct{}

// Collect fetches the Node and extracts conditions, capacity, and kernel version.
// Returns nil evidence (no error) when the Pod has no NodeName (not yet scheduled).
func (c *NodeCollector) Collect(ctx context.Context, input CollectorInput) (*v1alpha1.NodeEvidence, []v1alpha1.CollectionError) {
	if input.Pod == nil || input.Pod.Spec.NodeName == "" {
		return nil, nil // Pod not scheduled yet — not an error
	}

	var node corev1.Node
	if err := input.Client.Get(ctx, types.NamespacedName{Name: input.Pod.Spec.NodeName}, &node); err != nil {
		return nil, []v1alpha1.CollectionError{{
			Source: "node",
			Reason: fmt.Sprintf("could not fetch node %s: %v", input.Pod.Spec.NodeName, err),
		}}
	}

	ne := &v1alpha1.NodeEvidence{
		Name:          node.Name,
		KernelVersion: node.Status.NodeInfo.KernelVersion,
	}

	// Map conditions
	for _, cond := range node.Status.Conditions {
		status := string(cond.Status)
		switch cond.Type {
		case corev1.NodeReady:
			ne.Ready = status
		case corev1.NodeMemoryPressure:
			ne.MemoryPressure = status
		case corev1.NodeDiskPressure:
			ne.DiskPressure = status
		case corev1.NodePIDPressure:
			ne.PIDPressure = status
		case corev1.NodeNetworkUnavailable:
			ne.NetworkUnavailable = status
		}
	}

	// Allocatable resources
	if cpu, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
		ne.AllocatableCPU = cpu.String()
	}
	if mem, ok := node.Status.Allocatable[corev1.ResourceMemory]; ok {
		ne.AllocatableMemory = mem.String()
	}

	return ne, nil
}
