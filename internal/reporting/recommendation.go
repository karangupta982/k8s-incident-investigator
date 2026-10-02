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

package reporting

import (
	"fmt"
	"strings"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

const maxRecommendationLength = 512

// RecommendationEnricher populates DiagnosisFinding.Recommendation fields with
// evidence-specific values from the EvidenceSnapshot.
// It returns a deep copy of the DiagnosisResult with Recommendations filled in.
// The original is never modified.
type RecommendationEnricher struct{}

// Enrich returns a deep copy of result with Recommendation fields populated.
// Returns nil when result is nil.
func (r *RecommendationEnricher) Enrich(
	result *v1alpha1.DiagnosisResult,
	snapshot *v1alpha1.EvidenceSnapshot,
) *v1alpha1.DiagnosisResult {
	if result == nil {
		return nil
	}
	copy := result.DeepCopy()
	if copy.Primary != nil {
		enrichFinding(copy.Primary, snapshot)
	}
	for i := range copy.ContributingFactors {
		enrichFinding(&copy.ContributingFactors[i], snapshot)
	}
	for i := range copy.AlternativeHypotheses {
		enrichFinding(&copy.AlternativeHypotheses[i], snapshot)
	}
	return copy
}

func enrichFinding(f *v1alpha1.DiagnosisFinding, snapshot *v1alpha1.EvidenceSnapshot) {
	rec := buildRecommendation(f.RuleID, snapshot)
	if rec == "" {
		return // keep existing recommendation from the rule
	}
	if len(rec) > maxRecommendationLength {
		rec = rec[:maxRecommendationLength-1] + "…"
	}
	f.Recommendation = rec
}

func buildRecommendation(ruleID string, snap *v1alpha1.EvidenceSnapshot) string {
	if snap == nil {
		return ""
	}
	switch ruleID {
	case "OOMMemoryLimit":
		return oomMemoryLimitRec(snap)
	case "NodeMemoryPressure":
		return nodeMemoryPressureRec(snap)
	case "CrashLoopAppError":
		return crashLoopAppRec(snap)
	case "CrashLoopOOMExit":
		return crashLoopOOMRec(snap)
	case "ImagePullFailure":
		return imagePullRec(snap)
	case "MissingConfigReference":
		return "Verify that all ConfigMaps, Secrets, and environment variable sources referenced by the container spec exist in the same namespace as the Pod. Check for recently deleted or renamed configuration resources."
	case "PVCNotBound":
		return pvcNotBoundRec(snap)
	case "PVCMountError":
		return "Check CSI driver health, node conditions, and whether other Pods on the same node are experiencing mount failures. Review kubelet logs on the affected node for additional context."
	case "SchedulingFailure":
		return schedulingRec(snap)
	case "ProbeFailure":
		return probeRec(snap)
	}
	return ""
}

func firstContainer(snap *v1alpha1.EvidenceSnapshot) *v1alpha1.ContainerEvidence {
	if snap.Pod == nil || len(snap.Pod.Containers) == 0 {
		return nil
	}
	return &snap.Pod.Containers[0]
}

func oomMemoryLimitRec(snap *v1alpha1.EvidenceSnapshot) string {
	c := firstContainer(snap)
	if c == nil {
		return "Investigate application memory consumption. Consider increasing the container memory limit."
	}
	limit := c.ResourceLimits["memory"]
	name := c.Name
	if limit != "" {
		return fmt.Sprintf("Container %q was OOMKilled with a memory limit of %s. Investigate application memory consumption and consider increasing the memory limit if the workload legitimately requires more memory.", name, limit)
	}
	return fmt.Sprintf("Container %q was OOMKilled. Investigate application memory consumption and consider setting an appropriate memory limit.", name)
}

func nodeMemoryPressureRec(snap *v1alpha1.EvidenceSnapshot) string {
	nodeName := ""
	if snap.Node != nil {
		nodeName = snap.Node.Name
	}
	if nodeName != "" {
		return fmt.Sprintf("Node %q reported MemoryPressure=True. Check node-level memory utilisation. Review whether other workloads on the same node contributed to memory pressure. Consider node capacity or workload placement constraints.", nodeName)
	}
	return "Node reported MemoryPressure=True. Check node-level memory utilisation and consider workload placement constraints."
}

func crashLoopAppRec(snap *v1alpha1.EvidenceSnapshot) string {
	c := firstContainer(snap)
	if c == nil {
		return "Examine container logs for application errors immediately preceding the crash. Check whether a recent configuration or dependency change preceded the failure."
	}
	return fmt.Sprintf("Container %q has restarted %d times with exit code %d. Examine container logs for application errors immediately preceding the crash. Check whether a recent configuration or dependency change preceded the failure.", c.Name, c.RestartCount, c.ExitCode)
}

func crashLoopOOMRec(snap *v1alpha1.EvidenceSnapshot) string {
	c := firstContainer(snap)
	if c == nil {
		return "The container is repeatedly being killed due to memory exhaustion. Investigate application memory consumption and consider increasing the memory limit."
	}
	limit := c.ResourceLimits["memory"]
	if limit != "" {
		return fmt.Sprintf("Container %q is crash-looping after repeated OOMKills (exit code 137, %d restarts). Investigate memory consumption and consider increasing the memory limit above %s.", c.Name, c.RestartCount, limit)
	}
	return fmt.Sprintf("Container %q is crash-looping after repeated OOMKills (exit code 137, %d restarts). Investigate memory consumption and consider setting an appropriate memory limit.", c.Name, c.RestartCount)
}

func imagePullRec(snap *v1alpha1.EvidenceSnapshot) string {
	c := firstContainer(snap)
	imageName := ""
	if c != nil {
		imageName = c.Image
	}
	var secrets []string
	if snap.Dependencies != nil {
		secrets = snap.Dependencies.ImagePullSecretNames
	}
	if len(secrets) > 0 {
		if imageName != "" {
			return fmt.Sprintf("Image %q could not be pulled. Verify the image name, tag, and registry are correct. Image pull secrets are configured (%s) — confirm the secrets exist and contain valid credentials and are not expired.", imageName, strings.Join(secrets, ", "))
		}
		return fmt.Sprintf("Image could not be pulled. Verify image reference is correct. Image pull secrets are configured (%s) — confirm the secrets exist and contain valid credentials.", strings.Join(secrets, ", "))
	}
	if imageName != "" {
		return fmt.Sprintf("Image %q could not be pulled. Verify the image name, tag, and registry are correct. If the registry requires authentication, add appropriate imagePullSecrets.", imageName)
	}
	return "Image could not be pulled. Verify the image reference is correct and the registry is reachable."
}

func pvcNotBoundRec(snap *v1alpha1.EvidenceSnapshot) string {
	if snap.Dependencies == nil || len(snap.Dependencies.PVCs) == 0 {
		return "Check the PVC status and the StorageClass provisioner. Verify the cluster has available storage capacity."
	}
	for _, pvc := range snap.Dependencies.PVCs {
		if pvc.Phase != "Bound" {
			if pvc.StorageClassName != "" {
				return fmt.Sprintf("PVC %q is in phase %q using StorageClass %q. Verify the provisioner is running and healthy, and that the cluster has available storage capacity to fulfil the request.", pvc.Name, pvc.Phase, pvc.StorageClassName)
			}
			return fmt.Sprintf("PVC %q is in phase %q. Check the StorageClass provisioner configuration and verify the cluster has available capacity to fulfil the storage request.", pvc.Name, pvc.Phase)
		}
	}
	return "Check the PVC status and the StorageClass provisioner. Verify the cluster has available storage capacity."
}

func schedulingRec(snap *v1alpha1.EvidenceSnapshot) string {
	if snap.Dependencies == nil || snap.Dependencies.SchedulingConstraints == nil {
		return "Inspect node labels, taints, and available resource capacity. Verify the cluster has nodes with sufficient available capacity."
	}
	sc := snap.Dependencies.SchedulingConstraints
	var reasons []string
	if len(sc.NodeSelector) > 0 {
		reasons = append(reasons, "verify matching nodes exist for the configured nodeSelector")
	}
	if len(sc.ResourceRequests) > 0 {
		reasons = append(reasons, "verify nodes have sufficient available capacity for the resource requests")
	}
	if len(reasons) > 0 {
		return "Inspect node availability: " + strings.Join(reasons, "; ") + "."
	}
	return "Inspect node labels, taints, and available resource capacity."
}

func probeRec(snap *v1alpha1.EvidenceSnapshot) string {
	probeKind := "readiness"
	if snap.TriggerType == "LivenessProbeFailure" {
		probeKind = "liveness"
	}
	c := firstContainer(snap)
	if c == nil {
		return fmt.Sprintf("Verify the application is healthy and the %s probe endpoint is accessible.", probeKind)
	}
	probe := c.ReadinessProbe
	if snap.TriggerType == "LivenessProbeFailure" {
		probe = c.LivenessProbe
	}
	if probe != nil && probe.HTTPPath != "" {
		return fmt.Sprintf("Verify the application is healthy and the %s probe at %s:%d is accessible. Review probe failureThreshold=%d and periodSeconds. If this is a liveness probe, check whether the application is deadlocked.", probeKind, probe.HTTPPath, probe.Port, probe.FailureThreshold)
	}
	return fmt.Sprintf("Verify the application is healthy and the %s probe endpoint is accessible. Review the probe configuration and application health check logic.", probeKind)
}
