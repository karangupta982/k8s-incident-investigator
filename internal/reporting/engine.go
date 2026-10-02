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
	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

// RenderResult holds the assembled reporting output.
// The controller applies this to the IncidentReport status via a single PATCH.
type RenderResult struct {
	Summary  string
	Timeline []v1alpha1.TimelineEvent
	// Diagnosis is a deep copy of the existing DiagnosisResult with Recommendation
	// fields populated by the RecommendationEnricher. Nil when no DiagnosisResult is present.
	Diagnosis *v1alpha1.DiagnosisResult
}

// ReportingEngine assembles the IncidentSummary, Timeline, and enriched Recommendations
// from existing IncidentReport status fields.
// It is a pure function — it does not call the Kubernetes API.
type ReportingEngine struct {
	Config *config.Config
}

// Render produces the RenderResult for the given report.
// It never returns an error — partial output is produced gracefully when fields are nil.
func (e *ReportingEngine) Render(report *v1alpha1.IncidentReport) RenderResult {
	maxTimeline := 50
	if e.Config != nil && e.Config.MaxTimelineEvents > 0 {
		maxTimeline = e.Config.MaxTimelineEvents
	}

	sb := &SummaryBuilder{MaxLength: maxSummaryLength}
	tb := &TimelineBuilder{MaxEvents: maxTimeline}
	re := &RecommendationEnricher{}

	var snapshot *v1alpha1.EvidenceSnapshot
	if report != nil {
		snapshot = report.Status.Evidence
	}

	return RenderResult{
		Summary:   sb.Build(report),
		Timeline:  tb.Build(report),
		Diagnosis: re.Enrich(report.Status.Diagnosis, snapshot),
	}
}
