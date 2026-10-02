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

package diagnosis

import "github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"

// Confidence level constants for DiagnosisFinding.
const (
	ConfidenceHigh   = "High"
	ConfidenceMedium = "Medium"
	ConfidenceLow    = "Low"
)

// DiagnosisRule is the common interface for all diagnosis rules.
// Each implementation inspects a specific subset of the EvidenceSnapshot
// and returns zero or one DiagnosisFinding.
//
// Rules MUST be stateless — Evaluate may be called concurrently from tests.
// Rules MUST NOT call any external API or modify the snapshot.
type DiagnosisRule interface {
	// ID returns the stable rule identifier that appears in DiagnosisFinding.RuleID.
	ID() string

	// Priority returns the rule's tie-breaking priority when multiple rules match
	// at the same confidence level. Lower value = higher priority.
	Priority() int

	// Evaluate inspects the evidence and returns a finding, or nil if this rule
	// does not match.
	Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding
}

// confidenceRank maps confidence strings to sort order (lower = higher confidence).
func confidenceRank(c string) int {
	switch c {
	case ConfidenceHigh:
		return 0
	case ConfidenceMedium:
		return 1
	case ConfidenceLow:
		return 2
	default:
		return 3
	}
}
