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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// WorkloadCollector collects Layer 3 evidence from the owning workload resource.
type WorkloadCollector struct{}

const maxWorkloadConditions = 10

// Collect fetches the workload resource and extracts evidence.
// Returns nil evidence (no error) when workload ownership is unresolved.
func (c *WorkloadCollector) Collect(ctx context.Context, input CollectorInput) (*v1alpha1.WorkloadEvidence, []v1alpha1.CollectionError) {
	if input.Report.Spec.Workload == nil || !input.Report.Status.WorkloadOwnerResolved {
		return nil, nil // expected — no workload resolved
	}
	wl := input.Report.Spec.Workload
	key := types.NamespacedName{Namespace: wl.Namespace, Name: wl.Name}

	switch wl.Kind {
	case "Deployment":
		var d appsv1.Deployment
		if err := input.Client.Get(ctx, key, &d); err != nil {
			return nil, []v1alpha1.CollectionError{{Source: "workload", Reason: fmt.Sprintf("fetch Deployment %s/%s: %v", wl.Namespace, wl.Name, err)}}
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		return &v1alpha1.WorkloadEvidence{
			Kind:            "Deployment",
			Name:            d.Name,
			Namespace:       d.Namespace,
			DesiredReplicas: desired,
			ReadyReplicas:   d.Status.ReadyReplicas,
			UpdateStrategy:  string(d.Spec.Strategy.Type),
			Conditions:      deploymentConditions(d.Status.Conditions),
		}, nil

	case "StatefulSet":
		var s appsv1.StatefulSet
		if err := input.Client.Get(ctx, key, &s); err != nil {
			return nil, []v1alpha1.CollectionError{{Source: "workload", Reason: fmt.Sprintf("fetch StatefulSet %s/%s: %v", wl.Namespace, wl.Name, err)}}
		}
		desired := int32(1)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		return &v1alpha1.WorkloadEvidence{
			Kind:            "StatefulSet",
			Name:            s.Name,
			Namespace:       s.Namespace,
			DesiredReplicas: desired,
			ReadyReplicas:   s.Status.ReadyReplicas,
			UpdateStrategy:  string(s.Spec.UpdateStrategy.Type),
		}, nil

	case "DaemonSet":
		var ds appsv1.DaemonSet
		if err := input.Client.Get(ctx, key, &ds); err != nil {
			return nil, []v1alpha1.CollectionError{{Source: "workload", Reason: fmt.Sprintf("fetch DaemonSet %s/%s: %v", wl.Namespace, wl.Name, err)}}
		}
		return &v1alpha1.WorkloadEvidence{
			Kind:            "DaemonSet",
			Name:            ds.Name,
			Namespace:       ds.Namespace,
			DesiredReplicas: ds.Status.DesiredNumberScheduled,
			ReadyReplicas:   ds.Status.NumberReady,
			UpdateStrategy:  string(ds.Spec.UpdateStrategy.Type),
		}, nil

	case "Job":
		var j batchv1.Job
		if err := input.Client.Get(ctx, key, &j); err != nil {
			return nil, []v1alpha1.CollectionError{{Source: "workload", Reason: fmt.Sprintf("fetch Job %s/%s: %v", wl.Namespace, wl.Name, err)}}
		}
		return &v1alpha1.WorkloadEvidence{
			Kind:          "Job",
			Name:          j.Name,
			Namespace:     j.Namespace,
			ReadyReplicas: j.Status.Succeeded,
		}, nil

	default:
		return nil, []v1alpha1.CollectionError{{
			Source: "workload",
			Reason: fmt.Sprintf("unsupported workload kind: %s", wl.Kind),
		}}
	}
}

func deploymentConditions(conditions []appsv1.DeploymentCondition) []v1alpha1.WorkloadCondition {
	out := make([]v1alpha1.WorkloadCondition, 0, len(conditions))
	for i, c := range conditions {
		if i >= maxWorkloadConditions {
			break
		}
		wc := v1alpha1.WorkloadCondition{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Reason:  c.Reason,
			Message: c.Message,
		}
		if !c.LastTransitionTime.IsZero() {
			t := metav1.NewTime(c.LastTransitionTime.Time)
			wc.LastTransitionTime = &t
		}
		out = append(out, wc)
	}
	return out
}
