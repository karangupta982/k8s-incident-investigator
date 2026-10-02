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

// CrashLoopOOMExitRule fires when a container is in CrashLoopBackOff due to OOM kills.
// Mutually exclusive with CrashLoopAppErrorRule — exit code 137 or OOMKilled reason.
type CrashLoopOOMExitRule struct{}

func (r *CrashLoopOOMExitRule) ID() string    { return "CrashLoopOOMExit" }
func (r *CrashLoopOOMExitRule) Priority() int { return 11 }

func (r *CrashLoopOOMExitRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Pod == nil {
		return nil
	}
	for _, c := range snapshot.Pod.Containers {
		if c.WaitingReason != "CrashLoopBackOff" {
			continue
		}
		if c.ExitCode != 137 && c.TerminationReason != "OOMKilled" && c.LastTerminationReason != "OOMKilled" {
			continue
		}
		limit := c.ResourceLimits["memory"]
		ev := []string{
			"waiting reason: CrashLoopBackOff",
			"exit code: 137 / termination reason: OOMKilled",
			fmt.Sprintf("restart count: %d", c.RestartCount),
		}
		if limit != "" {
			ev = append(ev, fmt.Sprintf("memory limit: %s", limit))
		}
		rec := "The container is repeatedly being killed due to memory exhaustion. Investigate application memory consumption."
		if limit != "" {
			rec = fmt.Sprintf("The container is repeatedly being killed due to memory exhaustion (limit: %s). Investigate memory consumption and consider increasing the limit. The crash loop indicates the issue occurs consistently.", limit)
		}
		return &v1alpha1.DiagnosisFinding{
			RuleID:             r.ID(),
			Confidence:         "High",
			Cause:              "Container is crash-looping due to repeated OOM kills",
			Explanation:        fmt.Sprintf("Container %q (restart count: %d) is in CrashLoopBackOff because it is repeatedly being killed by the OOM killer (exit code 137).", c.Name, c.RestartCount),
			SupportingEvidence: ev,
			Recommendation:     rec,
		}
	}
	return nil
}

// CrashLoopAppErrorRule fires when a container is in CrashLoopBackOff with a non-OOM exit code.
// Mutually exclusive with CrashLoopOOMExitRule.
type CrashLoopAppErrorRule struct{}

func (r *CrashLoopAppErrorRule) ID() string    { return "CrashLoopAppError" }
func (r *CrashLoopAppErrorRule) Priority() int { return 21 }

func (r *CrashLoopAppErrorRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Pod == nil {
		return nil
	}
	for _, c := range snapshot.Pod.Containers {
		if c.WaitingReason != "CrashLoopBackOff" {
			continue
		}
		// Must NOT be an OOM exit — those are handled by CrashLoopOOMExitRule
		if c.ExitCode == 137 || c.TerminationReason == "OOMKilled" || c.LastTerminationReason == "OOMKilled" {
			continue
		}
		// Exit code 0 would be unusual in CrashLoopBackOff, but rule should not fire for it
		if c.ExitCode == 0 && c.TerminationReason == "" {
			continue
		}
		return &v1alpha1.DiagnosisFinding{
			RuleID:      r.ID(),
			Confidence:  "Medium",
			Cause:       "Container is crash-looping due to a non-OOM application failure",
			Explanation: fmt.Sprintf("Container %q (restart count: %d) is in CrashLoopBackOff. The last exit code was %d, indicating an application-level failure rather than a memory limit exceeded.", c.Name, c.RestartCount, c.ExitCode),
			SupportingEvidence: []string{
				"waiting reason: CrashLoopBackOff",
				fmt.Sprintf("last exit code: %d", c.ExitCode),
				fmt.Sprintf("restart count: %d", c.RestartCount),
			},
			Recommendation: "Examine container logs for application errors immediately preceding the crash. Check whether a recent configuration or dependency change preceded the failure.",
		}
	}
	return nil
}
