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

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DiagnosisResult holds the outcome of the diagnosis engine for one evaluation cycle.
//
// +kubebuilder:object:generate=true
type DiagnosisResult struct {
	// EvaluatedAt is the timestamp when diagnosis was last run.
	// +optional
	EvaluatedAt *metav1.Time `json:"evaluatedAt,omitempty"`

	// Primary is the highest-confidence matching diagnosis finding.
	// Nil when no rule matched (phase will be Unknown).
	// +optional
	Primary *DiagnosisFinding `json:"primary,omitempty"`

	// ContributingFactors lists other rules that fired alongside the primary finding
	// at a lower confidence level.
	// +optional
	ContributingFactors []DiagnosisFinding `json:"contributingFactors,omitempty"`

	// AlternativeHypotheses lists rules that matched at the same confidence level as the
	// primary finding, representing plausible alternative explanations.
	// +optional
	AlternativeHypotheses []DiagnosisFinding `json:"alternativeHypotheses,omitempty"`

	// RulesEvaluated is the count of rules that were evaluated.
	RulesEvaluated int `json:"rulesEvaluated"`

	// UnknownReason explains why no diagnosis was made when Primary is nil.
	// +optional
	UnknownReason string `json:"unknownReason,omitempty"`
}

// DiagnosisFinding represents a single rule match with its evidence-backed explanation.
//
// +kubebuilder:object:generate=true
type DiagnosisFinding struct {
	// RuleID is the identifier of the rule that produced this finding (e.g., "OOMMemoryLimit").
	RuleID string `json:"ruleID"`

	// Confidence is the qualitative strength of this finding.
	// +kubebuilder:validation:Enum=High;Medium;Low
	Confidence string `json:"confidence"`

	// Cause is a concise human-readable description of the identified root cause.
	Cause string `json:"cause"`

	// Explanation provides a longer evidence-backed description of why this finding was reached.
	Explanation string `json:"explanation"`

	// SupportingEvidence lists the specific evidence items that led to this finding.
	// Each entry is a short human-readable string.
	// +optional
	SupportingEvidence []string `json:"supportingEvidence,omitempty"`

	// Recommendation is a short, actionable suggestion tied directly to this finding.
	// Must be specific and evidence-backed; must not recommend automatic remediation.
	// +optional
	Recommendation string `json:"recommendation,omitempty"`
}
