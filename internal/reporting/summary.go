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

// Package reporting assembles the IncidentSummary, Timeline, and enriched
// Recommendations from existing IncidentReport status fields.
// It is a pure function layer — no Kubernetes API calls are made.
package reporting

import (
	"fmt"
	"strings"
	"time"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

const maxSummaryLength = 2048

// SummaryBuilder produces a concise IncidentSummary string from the IncidentReport status.
type SummaryBuilder struct {
	MaxLength int
}

// Build generates the summary. Never panics on nil fields.
func (b *SummaryBuilder) Build(report *v1alpha1.IncidentReport) string {
	if report == nil {
		return "Incident report unavailable."
	}
	maxLen := b.MaxLength
	if maxLen <= 0 {
		maxLen = maxSummaryLength
	}

	var sb strings.Builder

	// Workload identity
	if report.Spec.Workload != nil {
		sb.WriteString(fmt.Sprintf("Workload: %s/%s/%s\n",
			report.Spec.Workload.Kind,
			report.Spec.Workload.Namespace,
			report.Spec.Workload.Name))
	} else {
		sb.WriteString(fmt.Sprintf("Incident: %s\n", report.Name))
	}

	phase := report.Status.Phase
	trigger := ""
	if report.Status.Trigger != nil {
		trigger = string(report.Status.Trigger.Type)
	}
	podCount := len(report.Status.AffectedPods)

	switch phase {
	case v1alpha1.PhaseDiagnosed:
		if report.Status.Diagnosis != nil && report.Status.Diagnosis.Primary != nil {
			p := report.Status.Diagnosis.Primary
			sb.WriteString(fmt.Sprintf("Phase: Diagnosed (Confidence: %s)\n", p.Confidence))
			sb.WriteString(fmt.Sprintf("Cause: %s\n", p.Cause))
		} else {
			sb.WriteString("Phase: Diagnosed\n")
		}
		if trigger != "" {
			sb.WriteString(fmt.Sprintf("Trigger: %s\n", trigger))
		}
		sb.WriteString(fmt.Sprintf("Affected Pods: %d\n", podCount))
		if report.Status.StartedAt != nil {
			sb.WriteString(fmt.Sprintf("Started: %s\n", report.Status.StartedAt.UTC().Format(time.RFC3339)))
		}

	case v1alpha1.PhaseUnknown:
		sb.WriteString("Phase: Unknown\n")
		if report.Status.Diagnosis != nil && report.Status.Diagnosis.UnknownReason != "" {
			sb.WriteString(fmt.Sprintf("Reason: %s\n", report.Status.Diagnosis.UnknownReason))
		}
		if trigger != "" {
			sb.WriteString(fmt.Sprintf("Trigger: %s\n", trigger))
		}
		sb.WriteString(fmt.Sprintf("Affected Pods: %d\n", podCount))
		sb.WriteString(unknownNextStep(report))

	case v1alpha1.PhaseResolved:
		sb.WriteString("Phase: Resolved\n")
		cause := "Unknown"
		if report.Status.Diagnosis != nil && report.Status.Diagnosis.Primary != nil {
			cause = report.Status.Diagnosis.Primary.Cause
		}
		sb.WriteString(fmt.Sprintf("Cause: %s\n", cause))
		if report.Status.StartedAt != nil && report.Status.ResolvedAt != nil {
			duration := report.Status.ResolvedAt.Sub(report.Status.StartedAt.Time).Round(time.Second)
			sb.WriteString(fmt.Sprintf("Duration: %s\n", duration))
			sb.WriteString(fmt.Sprintf("Resolved: %s\n", report.Status.ResolvedAt.UTC().Format(time.RFC3339)))
		}

	default: // Investigating or empty
		sb.WriteString("Phase: Investigating\n")
		if trigger != "" {
			sb.WriteString(fmt.Sprintf("Trigger: %s\n", trigger))
		}
		sb.WriteString(fmt.Sprintf("Affected Pods: %d\n", podCount))
		sb.WriteString("Investigation in progress.\n")
	}

	out := strings.TrimRight(sb.String(), "\n")
	if len(out) > maxLen {
		return out[:maxLen-3] + "…"
	}
	return out
}

// unknownNextStep derives a suggested next investigation step from available evidence.
func unknownNextStep(report *v1alpha1.IncidentReport) string {
	if report.Status.Evidence == nil {
		return "Suggested: Check controller logs and verify RBAC permissions.\n"
	}
	ev := report.Status.Evidence
	// Check for log availability
	for _, log := range ev.Logs {
		if len(log.Lines) > 0 {
			return "Suggested: Review container logs for application errors.\n"
		}
		if log.UnavailableReason != "" {
			return fmt.Sprintf("Suggested: Container logs unavailable (%s). Inspect node events and Pod conditions.\n", log.UnavailableReason)
		}
	}
	// Node memory pressure
	if ev.Node != nil && ev.Node.MemoryPressure == "True" {
		return "Suggested: Node is under memory pressure. Check node memory utilisation.\n"
	}
	// Collection errors only
	if len(ev.CollectionErrors) > 0 && ev.Pod == nil {
		return "Suggested: Evidence collection encountered errors. Check controller logs and verify RBAC permissions.\n"
	}
	return "Suggested: No known failure pattern matched. Review Evidence and Events sections manually.\n"
}
