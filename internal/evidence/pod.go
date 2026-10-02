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

	corev1 "k8s.io/api/core/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// PodCollector collects Layer 1 evidence from the affected Pod resource.
type PodCollector struct{}

// Collect extracts evidence from the affected Pod.
// Returns a CollectionError when the Pod is unavailable.
func (c *PodCollector) Collect(_ context.Context, input CollectorInput) (*v1alpha1.PodEvidence, []v1alpha1.CollectionError) {
	if input.Pod == nil {
		return nil, []v1alpha1.CollectionError{{
			Source: "pod",
			Reason: "pod not found or not yet available",
		}}
	}
	pod := input.Pod

	ev := &v1alpha1.PodEvidence{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Phase:     string(pod.Status.Phase),
		NodeName:  pod.Spec.NodeName,
	}

	// Map container statuses
	for i := range pod.Spec.Containers {
		spec := &pod.Spec.Containers[i]
		ce := buildContainerEvidence(spec, findContainerStatus(pod.Status.ContainerStatuses, spec.Name))
		ev.Containers = append(ev.Containers, ce)
	}

	// Map init container statuses
	for i := range pod.Spec.InitContainers {
		spec := &pod.Spec.InitContainers[i]
		ce := buildContainerEvidence(spec, findContainerStatus(pod.Status.InitContainerStatuses, spec.Name))
		ev.InitContainers = append(ev.InitContainers, ce)
	}

	// Map volume mounts
	for _, vol := range pod.Spec.Volumes {
		vme := buildVolumeMountEvidence(pod, vol)
		if vme != nil {
			ev.VolumeMounts = append(ev.VolumeMounts, *vme)
		}
	}

	return ev, nil
}

func findContainerStatus(statuses []corev1.ContainerStatus, name string) *corev1.ContainerStatus {
	for i := range statuses {
		if statuses[i].Name == name {
			return &statuses[i]
		}
	}
	return nil
}

func buildContainerEvidence(spec *corev1.Container, status *corev1.ContainerStatus) v1alpha1.ContainerEvidence {
	ce := v1alpha1.ContainerEvidence{
		Name:  spec.Name,
		Image: spec.Image,
		State: "unknown",
	}

	// Resource limits and requests
	if spec.Resources.Limits != nil {
		ce.ResourceLimits = quantityMapToStrings(spec.Resources.Limits)
	}
	if spec.Resources.Requests != nil {
		ce.ResourceRequests = quantityMapToStrings(spec.Resources.Requests)
	}

	// Probe summaries
	if spec.LivenessProbe != nil {
		ce.LivenessProbe = buildProbeSummary(spec.LivenessProbe)
	}
	if spec.ReadinessProbe != nil {
		ce.ReadinessProbe = buildProbeSummary(spec.ReadinessProbe)
	}

	if status == nil {
		return ce
	}

	ce.RestartCount = status.RestartCount

	// Current state
	switch {
	case status.State.Running != nil:
		ce.State = "running"
	case status.State.Waiting != nil:
		ce.State = "waiting"
		ce.WaitingReason = status.State.Waiting.Reason
	case status.State.Terminated != nil:
		ce.State = "terminated"
		ce.ExitCode = status.State.Terminated.ExitCode
		ce.TerminationReason = status.State.Terminated.Reason
	}

	// Last termination reason
	if status.LastTerminationState.Terminated != nil {
		ce.LastTerminationReason = status.LastTerminationState.Terminated.Reason
	}

	return ce
}

func buildProbeSummary(probe *corev1.Probe) *v1alpha1.ProbeSummary {
	ps := &v1alpha1.ProbeSummary{
		FailureThreshold: probe.FailureThreshold,
	}
	switch {
	case probe.HTTPGet != nil:
		ps.Type = "HTTPGet"
		ps.HTTPPath = probe.HTTPGet.Path
		ps.Port = probe.HTTPGet.Port.IntVal
	case probe.TCPSocket != nil:
		ps.Type = "TCPSocket"
		ps.Port = probe.TCPSocket.Port.IntVal
	case probe.Exec != nil:
		ps.Type = "Exec"
	default:
		ps.Type = "Unknown"
	}
	return ps
}

func quantityMapToStrings(rl corev1.ResourceList) map[string]string {
	out := make(map[string]string, len(rl))
	for k, v := range rl {
		q := v.DeepCopy()
		out[string(k)] = q.String()
	}
	return out
}

func buildVolumeMountEvidence(pod *corev1.Pod, vol corev1.Volume) *v1alpha1.VolumeMountEvidence {
	vme := &v1alpha1.VolumeMountEvidence{Name: vol.Name}

	// Find first mount path for this volume across containers
	for _, c := range pod.Spec.Containers {
		for _, vm := range c.VolumeMounts {
			if vm.Name == vol.Name {
				vme.MountPath = vm.MountPath
				break
			}
		}
		if vme.MountPath != "" {
			break
		}
	}

	switch {
	case vol.PersistentVolumeClaim != nil:
		vme.VolumeType = "PVC"
		vme.ClaimName = vol.PersistentVolumeClaim.ClaimName
	case vol.ConfigMap != nil:
		vme.VolumeType = "ConfigMap"
	case vol.Secret != nil:
		vme.VolumeType = "Secret"
	case vol.EmptyDir != nil:
		vme.VolumeType = "EmptyDir"
	case vol.HostPath != nil:
		vme.VolumeType = "HostPath"
	default:
		vme.VolumeType = "Other"
	}
	return vme
}
