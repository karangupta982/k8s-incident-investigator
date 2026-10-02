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

// DependencyCollector collects Layer 5 adaptive dependency evidence.
// Which evidence is collected depends on the trigger type via triggerNeeds.
type DependencyCollector struct{}

// Collect gathers PVC, image pull secret, or scheduling constraint evidence
// depending on which needs are active for the current trigger type.
func (c *DependencyCollector) Collect(ctx context.Context, input CollectorInput, needs triggerNeeds) (*v1alpha1.DependencyEvidence, []v1alpha1.CollectionError) {
	if !needs.pvcEvidence && !needs.imagePullEvidence && !needs.schedulingEvidence {
		return nil, nil
	}
	if input.Pod == nil {
		return nil, nil
	}

	dep := &v1alpha1.DependencyEvidence{}
	var errs []v1alpha1.CollectionError

	// PVC evidence (for MountFailure)
	if needs.pvcEvidence {
		pvcs, pvcErrs := collectPVCEvidence(ctx, input)
		dep.PVCs = pvcs
		errs = append(errs, pvcErrs...)
	}

	// ImagePullSecret names (for ImagePullBackOff)
	// IMPORTANT: only names are stored — never Secret data values
	if needs.imagePullEvidence {
		for _, ips := range input.Pod.Spec.ImagePullSecrets {
			dep.ImagePullSecretNames = append(dep.ImagePullSecretNames, ips.Name)
		}
	}

	// Scheduling constraints (for SchedulingFailure)
	if needs.schedulingEvidence {
		dep.SchedulingConstraints = collectSchedulingConstraints(input.Pod)
	}

	if dep.PVCs == nil && dep.ImagePullSecretNames == nil && dep.SchedulingConstraints == nil {
		return nil, errs // nothing useful collected
	}
	return dep, errs
}

func collectPVCEvidence(ctx context.Context, input CollectorInput) ([]v1alpha1.PVCEvidence, []v1alpha1.CollectionError) {
	var pvcs []v1alpha1.PVCEvidence
	var errs []v1alpha1.CollectionError

	for _, vol := range input.Pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		claimName := vol.PersistentVolumeClaim.ClaimName

		var pvc corev1.PersistentVolumeClaim
		if err := input.Client.Get(ctx, types.NamespacedName{
			Namespace: input.Pod.Namespace,
			Name:      claimName,
		}, &pvc); err != nil {
			errs = append(errs, v1alpha1.CollectionError{
				Source: fmt.Sprintf("pvc/%s", claimName),
				Reason: fmt.Sprintf("could not fetch PVC %s/%s: %v", input.Pod.Namespace, claimName, err),
			})
			continue
		}

		pe := v1alpha1.PVCEvidence{
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
			Phase:     string(pvc.Status.Phase),
		}

		if pvc.Spec.StorageClassName != nil {
			pe.StorageClassName = *pvc.Spec.StorageClassName
		}
		if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			pe.RequestedStorage = storage.String()
		}
		for _, am := range pvc.Spec.AccessModes {
			pe.AccessModes = append(pe.AccessModes, string(am))
		}
		if pvc.Spec.VolumeName != "" {
			pe.BoundPVName = pvc.Spec.VolumeName

			// Optionally fetch reclaim policy from PV
			var pv corev1.PersistentVolume
			if err := input.Client.Get(ctx, types.NamespacedName{Name: pvc.Spec.VolumeName}, &pv); err == nil {
				pe.PVReclaimPolicy = string(pv.Spec.PersistentVolumeReclaimPolicy)
			}
		}
		pvcs = append(pvcs, pe)
	}
	return pvcs, errs
}

func collectSchedulingConstraints(pod *corev1.Pod) *v1alpha1.SchedulingConstraints {
	sc := &v1alpha1.SchedulingConstraints{}

	if len(pod.Spec.NodeSelector) > 0 {
		sc.NodeSelector = make(map[string]string, len(pod.Spec.NodeSelector))
		for k, v := range pod.Spec.NodeSelector {
			sc.NodeSelector[k] = v
		}
	}

	for _, t := range pod.Spec.Tolerations {
		str := t.Key
		if t.Operator == corev1.TolerationOpEqual {
			str += "=" + t.Value
		}
		if t.Effect != "" {
			str += ":" + string(t.Effect)
		}
		sc.Tolerations = append(sc.Tolerations, str)
	}

	// Aggregate resource requests across all containers
	totalRequests := corev1.ResourceList{}
	for _, c := range pod.Spec.Containers {
		for k, v := range c.Resources.Requests {
			if existing, ok := totalRequests[k]; ok {
				existing.Add(v)
				totalRequests[k] = existing
			} else {
				totalRequests[k] = v.DeepCopy()
			}
		}
	}
	if len(totalRequests) > 0 {
		sc.ResourceRequests = make(map[string]string, len(totalRequests))
		for k, v := range totalRequests {
			sc.ResourceRequests[string(k)] = v.String()
		}
	}

	return sc
}

// TriggerNeedsExported is the exported version of triggerNeeds for use in tests.
type TriggerNeedsExported struct {
	PVCEvidence        bool
	ImagePullEvidence  bool
	SchedulingEvidence bool
}

// CollectWithNeeds is an exported wrapper for testing the DependencyCollector with explicit needs.
func (c *DependencyCollector) CollectWithNeeds(ctx context.Context, input CollectorInput, needs TriggerNeedsExported) (*v1alpha1.DependencyEvidence, []v1alpha1.CollectionError) {
	return c.Collect(ctx, input, triggerNeeds{
		pvcEvidence:        needs.PVCEvidence,
		imagePullEvidence:  needs.ImagePullEvidence,
		schedulingEvidence: needs.SchedulingEvidence,
	})
}
