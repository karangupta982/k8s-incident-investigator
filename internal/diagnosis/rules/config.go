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

// MissingConfigReferenceRule fires when a container cannot start due to a missing
// ConfigMap, Secret, or environment variable source.
type MissingConfigReferenceRule struct{}

func (r *MissingConfigReferenceRule) ID() string    { return "MissingConfigReference" }
func (r *MissingConfigReferenceRule) Priority() int { return 13 }

func (r *MissingConfigReferenceRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Pod == nil {
		return nil
	}
	for _, c := range snapshot.Pod.Containers {
		if c.WaitingReason != "CreateContainerConfigError" {
			continue
		}
		return &v1alpha1.DiagnosisFinding{
			RuleID:      r.ID(),
			Confidence:  "High",
			Cause:       "Container cannot start due to a missing or invalid configuration reference",
			Explanation: fmt.Sprintf("Container %q is waiting with reason %q. A ConfigMap, Secret, or environment variable source referenced by the container spec does not exist or is invalid.", c.Name, c.WaitingReason),
			SupportingEvidence: []string{
				fmt.Sprintf("container: %s", c.Name),
				"waiting reason: CreateContainerConfigError",
			},
			Recommendation: "Verify that all ConfigMaps, Secrets, and environment variable sources referenced by the container spec exist in the same namespace as the Pod. Check for recently deleted or renamed configuration resources.",
		}
	}
	// Also check trigger type for cases where container status is not available
	if snapshot.TriggerType == "CreateContainerConfigError" {
		return &v1alpha1.DiagnosisFinding{
			RuleID:             r.ID(),
			Confidence:         "High",
			Cause:              "Container cannot start due to a missing or invalid configuration reference",
			Explanation:        "The incident was triggered by a CreateContainerConfigError signal. A ConfigMap, Secret, or environment variable source referenced by the container spec does not exist or is invalid.",
			SupportingEvidence: []string{"trigger type: CreateContainerConfigError"},
			Recommendation:     "Verify that all ConfigMaps, Secrets, and environment variable sources referenced by the container spec exist in the same namespace as the Pod.",
		}
	}
	return nil
}
