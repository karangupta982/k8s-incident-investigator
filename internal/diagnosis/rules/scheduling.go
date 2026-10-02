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
	"strings"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// SchedulingFailureRule fires when a Pod cannot be scheduled.
type SchedulingFailureRule struct{}

func (r *SchedulingFailureRule) ID() string    { return "SchedulingFailure" }
func (r *SchedulingFailureRule) Priority() int { return 15 }

func (r *SchedulingFailureRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.TriggerType != "SchedulingFailure" {
		return nil
	}
	var schedEvents []string
	for _, ev := range snapshot.Events {
		if ev.Reason == "FailedScheduling" && len(schedEvents) < 3 {
			schedEvents = append(schedEvents, truncate(ev.Message, 256))
		}
	}
	if len(schedEvents) == 0 {
		return nil
	}

	ev := append([]string{}, schedEvents...)
	var reasons []string

	if snapshot.Dependencies != nil && snapshot.Dependencies.SchedulingConstraints != nil {
		sc := snapshot.Dependencies.SchedulingConstraints
		if len(sc.NodeSelector) > 0 {
			keys := make([]string, 0, len(sc.NodeSelector))
			for k, v := range sc.NodeSelector {
				keys = append(keys, k+"="+v)
			}
			ev = append(ev, fmt.Sprintf("nodeSelector: %s", strings.Join(keys, ", ")))
			reasons = append(reasons, "nodeSelector labels may not match any available nodes")
		}
		if len(sc.ResourceRequests) > 0 {
			for k, v := range sc.ResourceRequests {
				ev = append(ev, fmt.Sprintf("resource request %s: %s", k, v))
			}
			reasons = append(reasons, "resource requests may exceed available cluster capacity")
		}
	}

	explanation := "The Pod could not be scheduled to any available node."
	if len(reasons) > 0 {
		explanation += " Possible causes: " + strings.Join(reasons, "; ") + "."
	}

	return &v1alpha1.DiagnosisFinding{
		RuleID:             r.ID(),
		Confidence:         "High",
		Cause:              "Pod could not be scheduled to any available node",
		Explanation:        explanation,
		SupportingEvidence: ev,
		Recommendation:     "Inspect node labels, taints, and available resource capacity. If a nodeSelector is configured, verify matching nodes exist. If resource requests are high, verify the cluster has nodes with sufficient available capacity.",
	}
}
