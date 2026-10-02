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

// Package diagnosis implements the deterministic rules engine that interprets
// a collected EvidenceSnapshot and produces a structured DiagnosisResult.
package diagnosis

import (
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/diagnosis/rules"
)

// DiagnosisEngine evaluates a collected EvidenceSnapshot against all registered
// DiagnosisRules and returns a DiagnosisResult.
// The engine is stateless — each Evaluate call is independent.
type DiagnosisEngine struct {
	rules []DiagnosisRule
}

// NewDiagnosisEngine returns an engine pre-loaded with all MVP diagnosis rules.
func NewDiagnosisEngine() *DiagnosisEngine {
	return &DiagnosisEngine{
		rules: []DiagnosisRule{
			&rules.OOMMemoryLimitRule{},
			&rules.NodeMemoryPressureRule{},
			&rules.CrashLoopOOMExitRule{},
			&rules.CrashLoopAppErrorRule{},
			&rules.ImagePullFailureRule{},
			&rules.MissingConfigReferenceRule{},
			&rules.PVCNotBoundRule{},
			&rules.PVCMountErrorRule{},
			&rules.SchedulingFailureRule{},
			&rules.ProbeFailureRule{},
		},
	}
}

// Evaluate runs all registered rules against the snapshot and returns the assembled result.
// snapshot must not be nil — callers should skip calling Evaluate when snapshot is nil.
func (e *DiagnosisEngine) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.DiagnosisResult {
	now := metav1.Now()
	result := v1alpha1.DiagnosisResult{
		EvaluatedAt:    &now,
		RulesEvaluated: len(e.rules),
	}

	// Collect all matching findings.
	var matches []v1alpha1.DiagnosisFinding
	for _, rule := range e.rules {
		if f := rule.Evaluate(snapshot); f != nil {
			matches = append(matches, *f)
		}
	}

	if len(matches) == 0 {
		result.UnknownReason = "No known failure pattern matched the collected evidence. " +
			"Review the Evidence section and Events for manual investigation."
		return result
	}

	// Sort by confidence (High first), then by priority as a tiebreaker.
	sort.SliceStable(matches, func(i, j int) bool {
		ri := confidenceRank(matches[i].Confidence)
		rj := confidenceRank(matches[j].Confidence)
		if ri != rj {
			return ri < rj
		}
		// Same confidence — use rule priority if we can look it up.
		// Priority is embedded in finding metadata; we infer from registration order.
		return false // stable sort preserves registration order for equal confidence
	})

	result.Primary = &matches[0]
	primaryConfidence := matches[0].Confidence

	for _, f := range matches[1:] {
		finding := f // copy to avoid aliasing
		if f.Confidence == primaryConfidence {
			result.AlternativeHypotheses = append(result.AlternativeHypotheses, finding)
		} else {
			result.ContributingFactors = append(result.ContributingFactors, finding)
		}
	}

	return result
}
