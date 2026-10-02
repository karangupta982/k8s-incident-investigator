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

// OOMMemoryLimitRule fires when a container was OOMKilled with a configured memory
// limit and the node was NOT under memory pressure.
type OOMMemoryLimitRule struct{}

func (r *OOMMemoryLimitRule) ID() string    { return "OOMMemoryLimit" }
func (r *OOMMemoryLimitRule) Priority() int { return 10 }

func (r *OOMMemoryLimitRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Pod == nil {
		return nil
	}
	// Node must not have memory pressure (or be nil)
	if snapshot.Node != nil && snapshot.Node.MemoryPressure == "True" {
		return nil
	}
	for _, c := range snapshot.Pod.Containers {
		if c.TerminationReason != "OOMKilled" && c.LastTerminationReason != "OOMKilled" {
			continue
		}
		if c.ExitCode != 137 && c.LastTerminationReason != "OOMKilled" {
			continue
		}
		limit := c.ResourceLimits["memory"]
		if limit == "" {
			continue
		}
		ev := []string{
			"termination reason: OOMKilled",
			"exit code: 137",
			fmt.Sprintf("memory limit: %s", limit),
		}
		if snapshot.Node != nil {
			ev = append(ev, "node did not report MemoryPressure")
		}
		return &v1alpha1.DiagnosisFinding{
			RuleID:             r.ID(),
			Confidence:         "High",
			Cause:              "Container exceeded its configured memory limit",
			Explanation:        fmt.Sprintf("Container %q was terminated with OOMKilled (exit code 137). The container has a memory limit of %s configured. The node did not report MemoryPressure, indicating the OOM kill was caused by the container exceeding its own limit rather than node-level contention.", c.Name, limit),
			SupportingEvidence: ev,
			Recommendation:     fmt.Sprintf("Investigate application memory consumption. If the workload legitimately requires more memory, consider increasing the container memory limit from its current value of %s.", limit),
		}
	}
	return nil
}

// NodeMemoryPressureRule fires when the node reported memory pressure alongside an OOM or eviction signal.
type NodeMemoryPressureRule struct{}

func (r *NodeMemoryPressureRule) ID() string    { return "NodeMemoryPressure" }
func (r *NodeMemoryPressureRule) Priority() int { return 20 }

func (r *NodeMemoryPressureRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Node == nil || snapshot.Node.MemoryPressure != "True" {
		return nil
	}
	// Must have OOMKilled evidence or eviction trigger
	hasOOM := false
	if snapshot.Pod != nil {
		for _, c := range snapshot.Pod.Containers {
			if c.TerminationReason == "OOMKilled" || c.LastTerminationReason == "OOMKilled" {
				hasOOM = true
				break
			}
		}
	}
	if !hasOOM && snapshot.TriggerType != "Eviction" {
		return nil
	}
	ev := []string{
		fmt.Sprintf("node %q reports MemoryPressure=True", snapshot.Node.Name),
	}
	if snapshot.Node.AllocatableMemory != "" {
		ev = append(ev, fmt.Sprintf("allocatable memory: %s", snapshot.Node.AllocatableMemory))
	}
	return &v1alpha1.DiagnosisFinding{
		RuleID:             r.ID(),
		Confidence:         "Medium",
		Cause:              "Node was under memory pressure",
		Explanation:        fmt.Sprintf("Node %q was reporting MemoryPressure=True at the time of the incident. The container may have been killed or evicted due to node-level memory contention rather than solely exceeding its own configured limit.", snapshot.Node.Name),
		SupportingEvidence: ev,
		Recommendation:     "Check node-level memory utilisation. Review whether other workloads on the same node contributed to memory pressure. Consider node capacity or workload placement constraints.",
	}
}
