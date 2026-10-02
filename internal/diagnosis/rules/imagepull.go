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

// ImagePullFailureRule fires when a container is failing to pull its image.
type ImagePullFailureRule struct{}

func (r *ImagePullFailureRule) ID() string    { return "ImagePullFailure" }
func (r *ImagePullFailureRule) Priority() int { return 12 }

func (r *ImagePullFailureRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
	if snapshot.Pod == nil {
		return nil
	}
	for _, c := range snapshot.Pod.Containers {
		if c.WaitingReason != "ImagePullBackOff" && c.WaitingReason != "ErrImagePull" {
			continue
		}
		ev := []string{
			fmt.Sprintf("container: %s", c.Name),
			fmt.Sprintf("image: %s", c.Image),
			fmt.Sprintf("waiting reason: %s", c.WaitingReason),
		}

		rec := "Verify that the image reference is correct and the registry is reachable."
		if snapshot.Dependencies != nil && len(snapshot.Dependencies.ImagePullSecretNames) > 0 {
			secrets := strings.Join(snapshot.Dependencies.ImagePullSecretNames, ", ")
			ev = append(ev, fmt.Sprintf("imagePullSecrets: %s", secrets))
			rec = fmt.Sprintf("Verify the image reference is correct. Image pull secrets are configured (%s) — confirm the secrets exist and contain valid credentials for the registry.", secrets)
		} else {
			rec = "Verify the image reference is correct and the registry is reachable. No imagePullSecrets are configured — if the registry requires authentication, you must add appropriate credentials."
		}

		return &v1alpha1.DiagnosisFinding{
			RuleID:             r.ID(),
			Confidence:         "High",
			Cause:              "Container image could not be pulled",
			Explanation:        fmt.Sprintf("Container %q is waiting with reason %q. Image %q cannot be pulled from the registry.", c.Name, c.WaitingReason, c.Image),
			SupportingEvidence: ev,
			Recommendation:     rec,
		}
	}
	return nil
}
