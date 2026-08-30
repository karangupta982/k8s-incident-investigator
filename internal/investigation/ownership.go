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

package investigation

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// OwnershipResolverInterface resolves a Pod's owning workload.
type OwnershipResolverInterface interface {
	// Resolve returns the top-level workload owning the Pod, or (nil, nil) if there is
	// no controller owner. Returns a non-nil error only on unexpected API failures that
	// are not handled by the fallback path.
	Resolve(ctx context.Context, pod *corev1.Pod) (*v1alpha1.WorkloadRef, error)
}

// OwnershipResolver traverses ownerReferences to find the top-level workload.
// It makes bounded, read-only API calls to fetch intermediate resources
// (ReplicaSet, Job) whose identity is only known after inspecting the Pod's owner references.
type OwnershipResolver struct {
	client client.Client
	log    logr.Logger
}

// NewOwnershipResolver creates a new OwnershipResolver.
func NewOwnershipResolver(c client.Client, log logr.Logger) *OwnershipResolver {
	return &OwnershipResolver{client: c, log: log}
}

// Resolve traverses ownerReferences on the Pod to identify the top-level workload.
//
// Returns (nil, nil) when:
//   - No controller owner exists (standalone Pod)
//   - An intermediate resource is unavailable (falls back to Pod identity)
//
// WorkloadRef.Kind is restricted to: Deployment, StatefulSet, DaemonSet, Job, CronJob.
// Intermediate resources (e.g. ReplicaSet) are never returned as the workload kind.
func (r *OwnershipResolver) Resolve(ctx context.Context, pod *corev1.Pod) (*v1alpha1.WorkloadRef, error) {
	owner := controllerOwner(pod.OwnerReferences)
	if owner == nil {
		// Standalone Pod — no workload identity
		return nil, nil
	}

	ns := pod.Namespace

	switch owner.Kind {
	case "ReplicaSet":
		return r.resolveFromReplicaSet(ctx, ns, owner.Name, pod)
	case "StatefulSet":
		return &v1alpha1.WorkloadRef{
			Kind:      "StatefulSet",
			Name:      owner.Name,
			Namespace: ns,
			UID:       owner.UID,
		}, nil
	case "DaemonSet":
		return &v1alpha1.WorkloadRef{
			Kind:      "DaemonSet",
			Name:      owner.Name,
			Namespace: ns,
			UID:       owner.UID,
		}, nil
	case "Job":
		return r.resolveFromJob(ctx, ns, owner.Name, pod)
	default:
		r.log.Info("unrecognised owner kind, falling back to Pod identity",
			"pod", pod.Namespace+"/"+pod.Name,
			"ownerKind", owner.Kind)
		return nil, nil
	}
}

// resolveFromReplicaSet fetches the RS then walks up to the Deployment.
// Falls back to Pod identity (nil, nil) if the RS or Deployment cannot be fetched.
func (r *OwnershipResolver) resolveFromReplicaSet(ctx context.Context, ns, rsName string, pod *corev1.Pod) (*v1alpha1.WorkloadRef, error) {
	var rs appsv1.ReplicaSet
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: rsName}, &rs); err != nil {
		r.log.Info("could not fetch ReplicaSet, falling back to Pod identity",
			"pod", pod.Namespace+"/"+pod.Name,
			"replicaSet", ns+"/"+rsName,
			"error", err.Error())
		return nil, nil
	}

	deployOwner := controllerOwner(rs.OwnerReferences)
	if deployOwner == nil || deployOwner.Kind != "Deployment" {
		// Bare ReplicaSet (no Deployment owner) — fall back to Pod identity.
		// ReplicaSet is not a valid top-level workload kind.
		r.log.Info("ReplicaSet has no Deployment owner, falling back to Pod identity",
			"pod", pod.Namespace+"/"+pod.Name,
			"replicaSet", ns+"/"+rsName)
		return nil, nil
	}

	var deploy appsv1.Deployment
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: deployOwner.Name}, &deploy); err != nil {
		r.log.Info("could not fetch Deployment, falling back to Pod identity",
			"pod", pod.Namespace+"/"+pod.Name,
			"deployment", ns+"/"+deployOwner.Name,
			"error", err.Error())
		return nil, nil
	}

	return &v1alpha1.WorkloadRef{
		Kind:      "Deployment",
		Name:      deploy.Name,
		Namespace: ns,
		UID:       deploy.UID,
	}, nil
}

// resolveFromJob fetches the Job then walks up to the CronJob if present.
// Falls back to Job identity when the CronJob cannot be fetched, and to Pod identity
// when the Job itself cannot be fetched.
func (r *OwnershipResolver) resolveFromJob(ctx context.Context, ns, jobName string, pod *corev1.Pod) (*v1alpha1.WorkloadRef, error) {
	var job batchv1.Job
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: jobName}, &job); err != nil {
		r.log.Info("could not fetch Job, falling back to Pod identity",
			"pod", pod.Namespace+"/"+pod.Name,
			"job", ns+"/"+jobName,
			"error", err.Error())
		return nil, nil
	}

	cronOwner := controllerOwner(job.OwnerReferences)
	if cronOwner != nil && cronOwner.Kind == "CronJob" {
		var cj batchv1.CronJob
		if err := r.client.Get(ctx, client.ObjectKey{Namespace: ns, Name: cronOwner.Name}, &cj); err != nil {
			r.log.Info("could not fetch CronJob, falling back to Job identity",
				"pod", pod.Namespace+"/"+pod.Name,
				"cronJob", ns+"/"+cronOwner.Name,
				"error", err.Error())
			// Job is a valid top-level workload kind — use it as fallback
		} else {
			return &v1alpha1.WorkloadRef{
				Kind:      "CronJob",
				Name:      cj.Name,
				Namespace: ns,
				UID:       cj.UID,
			}, nil
		}
	}

	return &v1alpha1.WorkloadRef{
		Kind:      "Job",
		Name:      job.Name,
		Namespace: ns,
		UID:       job.UID,
	}, nil
}

// controllerOwner returns the first ownerReference with controller=true.
func controllerOwner(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}
