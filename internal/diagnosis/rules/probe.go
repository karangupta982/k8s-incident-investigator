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

// ProbeFailureRule fires when readiness or liveness probe failures are detected.
type ProbeFailureRule struct{}

func (r *ProbeFailureRule) ID() string    { return "ProbeFailure" }
func (r *ProbeFailureRule) Priority() int { return 23 }

func (r *ProbeFailureRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.TriggerType != "ReadinessProbeFailure" && snapshot.TriggerType != "LivenessProbeFailure" {
		return nil
	}
	var unhealthyEvents []string
	for _, ev := range snapshot.Events {
		if ev.Reason == "Unhealthy" && len(unhealthyEvents) < 3 {
			unhealthyEvents = append(unhealthyEvents, truncate(ev.Message, 256))
		}
	}
	if len(unhealthyEvents) == 0 {
		return nil
	}

	probeKind := "readiness"
	if snapshot.TriggerType == "LivenessProbeFailure" {
		probeKind = "liveness"
	}

	ev := append([]string{fmt.Sprintf("trigger type: %s", snapshot.TriggerType)}, unhealthyEvents...)

	// Include probe config from pod evidence if available
	if snapshot.Pod != nil {
		for _, c := range snapshot.Pod.Containers {
			probe := c.ReadinessProbe
			if snapshot.TriggerType == "LivenessProbeFailure" {
				probe = c.LivenessProbe
			}
			if probe != nil {
				ev = append(ev, fmt.Sprintf("probe type: %s (failure threshold: %d)", probe.Type, probe.FailureThreshold))
				if probe.HTTPPath != "" {
					ev = append(ev, fmt.Sprintf("probe path: %s:%d", probe.HTTPPath, probe.Port))
				}
			}
		}
	}

	rec := fmt.Sprintf("Verify the application is healthy and the %s probe endpoint is accessible. Review the probe failureThreshold and periodSeconds settings, especially if the application has a slow startup time.", probeKind)
	if snapshot.TriggerType == "LivenessProbeFailure" {
		rec += " If this is a liveness probe failure, check whether the application is deadlocked or unresponsive."
	}

	return &v1alpha1.DiagnosisFinding{
		RuleID:             r.ID(),
		Confidence:         "Medium",
		Cause:              fmt.Sprintf("Container %s probe is failing repeatedly", probeKind),
		Explanation:        fmt.Sprintf("The %s probe is failing repeatedly (trigger type: %s). The application may be unhealthy, starting slowly, or the probe may be misconfigured.", probeKind, snapshot.TriggerType),
		SupportingEvidence: ev,
		Recommendation:     rec,
	}
}
