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

package rules

import (
	"fmt"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// PVCNotBoundRule fires when a MountFailure incident has an unbound PVC.
// Mutually exclusive with PVCMountErrorRule (which requires Phase == Bound).
type PVCNotBoundRule struct{}

func (r *PVCNotBoundRule) ID() string    { return "PVCNotBound" }
func (r *PVCNotBoundRule) Priority() int { return 14 }

func (r *PVCNotBoundRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.TriggerType != "MountFailure" {
		return nil
	}
	if snapshot.Dependencies == nil {
		return nil
	}
	for _, pvc := range snapshot.Dependencies.PVCs {
		if pvc.Phase == "Bound" {
			continue
		}
		ev := []string{
			fmt.Sprintf("pvc: %s", pvc.Name),
			fmt.Sprintf("pvc phase: %s", pvc.Phase),
		}
		if pvc.StorageClassName != "" {
			ev = append(ev, fmt.Sprintf("storage class: %s", pvc.StorageClassName))
		}
		if pvc.RequestedStorage != "" {
			ev = append(ev, fmt.Sprintf("requested storage: %s", pvc.RequestedStorage))
		}
		rec := fmt.Sprintf("PVC %q is in phase %q. Check the StorageClass provisioner configuration and verify the cluster has available capacity to fulfil the storage request.", pvc.Name, pvc.Phase)
		if pvc.StorageClassName != "" {
			rec = fmt.Sprintf("PVC %q is in phase %q using StorageClass %q. Verify the provisioner is running and healthy, and that the cluster has available storage capacity.", pvc.Name, pvc.Phase, pvc.StorageClassName)
		}
		return &v1alpha1.DiagnosisFinding{
			RuleID:             r.ID(),
			Confidence:         "High",
			Cause:              "PersistentVolumeClaim is not bound to a PersistentVolume",
			Explanation:        fmt.Sprintf("PVC %q is in phase %q. The workload cannot start until the PVC is successfully bound to a PersistentVolume.", pvc.Name, pvc.Phase),
			SupportingEvidence: ev,
			Recommendation:     rec,
		}
	}
	return nil
}

// PVCMountErrorRule fires when a MountFailure incident has a bound PVC but mount events exist.
type PVCMountErrorRule struct{}

func (r *PVCMountErrorRule) ID() string    { return "PVCMountError" }
func (r *PVCMountErrorRule) Priority() int { return 22 }

func (r *PVCMountErrorRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.TriggerType != "MountFailure" {
		return nil
	}
	if snapshot.Dependencies == nil {
		return nil
	}

	// Find a bound PVC
	var boundPVC *v1alpha1.PVCEvidence
	for i := range snapshot.Dependencies.PVCs {
		if snapshot.Dependencies.PVCs[i].Phase == "Bound" {
			boundPVC = &snapshot.Dependencies.PVCs[i]
			break
		}
	}
	if boundPVC == nil {
		return nil
	}

	// Must have FailedMount events
	var mountEvents []string
	for _, ev := range snapshot.Events {
		if ev.Reason == "FailedMount" {
			if len(mountEvents) < 3 {
				mountEvents = append(mountEvents, truncate(ev.Message, 256))
			}
		}
	}
	if len(mountEvents) == 0 {
		return nil
	}

	ev := []string{fmt.Sprintf("pvc: %s (bound)", boundPVC.Name)}
	ev = append(ev, mountEvents...)
	if snapshot.Pod != nil && snapshot.Pod.NodeName != "" {
		ev = append(ev, fmt.Sprintf("node: %s", snapshot.Pod.NodeName))
	}

	return &v1alpha1.DiagnosisFinding{
		RuleID:             r.ID(),
		Confidence:         "Medium",
		Cause:              "PVC is bound but the mount operation is failing",
		Explanation:        fmt.Sprintf("PVC %q is bound to volume %q but the mount operation is failing. This typically indicates a node-level or CSI driver issue rather than a provisioning problem.", boundPVC.Name, boundPVC.BoundPVName),
		SupportingEvidence: ev,
		Recommendation:     "Check CSI driver health, node conditions, and whether other Pods on the same node are experiencing mount failures. Review kubelet logs on the affected node for additional context.",
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
