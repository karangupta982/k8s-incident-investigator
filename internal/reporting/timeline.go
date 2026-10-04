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
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

const (
	sourceController   = "controller"
	sourceContainer    = "container"
	sourceKubeEvent    = "kubernetes-event"
	maxEventMessageLen = 256
)

// sourceOrder defines tie-breaking priority for equal timestamps.
// Lower = higher priority.
var sourceOrder = map[string]int{
	sourceController: 0,
	sourceContainer:  1,
	sourceKubeEvent:  2,
}

// TimelineBuilder assembles a chronologically ordered, bounded list of TimelineEvents.
type TimelineBuilder struct {
	MaxEvents int
}

// Build collects events from all sources and returns a sorted, bounded timeline.
func (b *TimelineBuilder) Build(report *v1alpha1.IncidentReport) []v1alpha1.TimelineEvent {
	if report == nil {
		return nil
	}
	maxEv := b.MaxEvents
	if maxEv <= 0 {
		maxEv = 50
	}

	var events []v1alpha1.TimelineEvent

	// Source 1: controller lifecycle events
	if report.Status.StartedAt != nil {
		events = append(events, v1alpha1.TimelineEvent{
			Timestamp: *report.Status.StartedAt,
			Source:    sourceController,
			Reason:    "IncidentDetected",
			Message:   "Incident detected and IncidentReport created.",
		})
	}
	if report.Status.StabilityStartedAt != nil {
		events = append(events, v1alpha1.TimelineEvent{
			Timestamp: *report.Status.StabilityStartedAt,
			Source:    sourceController,
			Reason:    "WorkloadHealthy",
			Message:   "Workload became healthy; stability period started.",
		})
	}
	if report.Status.ResolvedAt != nil {
		events = append(events, v1alpha1.TimelineEvent{
			Timestamp: *report.Status.ResolvedAt,
			Source:    sourceController,
			Reason:    "IncidentResolved",
			Message:   "Incident resolved after stability period elapsed.",
		})
	}

	// Source 2: container termination events from evidence
	if report.Status.Evidence != nil && report.Status.Evidence.Pod != nil {
		for _, c := range report.Status.Evidence.Pod.Containers {
			if c.TerminationReason == "" && c.ExitCode == 0 {
				continue
			}
			if c.TerminationReason == "" {
				continue
			}
			// Use LastFailureAt as proxy timestamp when container-level timestamp unavailable
			if report.Status.LastFailureAt != nil {
				msg := fmt.Sprintf("Container %q terminated with reason %q (exit code %d, restart count %d).",
					c.Name, c.TerminationReason, c.ExitCode, c.RestartCount)
				events = append(events, v1alpha1.TimelineEvent{
					Timestamp: *report.Status.LastFailureAt,
					Source:    sourceContainer,
					Reason:    c.TerminationReason,
					Message:   truncateMsg(msg),
				})
			}
		}
	}

	// Source 3: Kubernetes Events from evidence
	if report.Status.Evidence != nil {
		for _, ev := range report.Status.Evidence.Events {
			ts := metav1.Now()
			if ev.LastTime != nil {
				ts = *ev.LastTime
			} else if ev.FirstTime != nil {
				ts = *ev.FirstTime
			}
			events = append(events, v1alpha1.TimelineEvent{
				Timestamp: ts,
				Source:    sourceKubeEvent,
				Reason:    ev.Reason,
				Message:   truncateMsg(ev.Message),
			})
		}
	}

	// Sort chronologically ascending, with source-based tie-breaking
	sort.SliceStable(events, func(i, j int) bool {
		ti := events[i].Timestamp.Time
		tj := events[j].Timestamp.Time
		if ti.Equal(tj) {
			oi := sourceOrder[events[i].Source]
			oj := sourceOrder[events[j].Source]
			return oi < oj
		}
		return ti.Before(tj)
	})

	// Bound the result
	if len(events) > maxEv {
		omitted := len(events) - maxEv
		// Keep the most recent events; prepend a truncation note
		events = events[len(events)-maxEv:]
		note := v1alpha1.TimelineEvent{
			Timestamp: events[0].Timestamp,
			Source:    sourceController,
			Reason:    "TimelineTruncated",
			Message:   fmt.Sprintf("%d earlier events omitted due to MaxTimelineEvents limit.", omitted),
		}
		events = append([]v1alpha1.TimelineEvent{note}, events...)
		// Re-trim to exactly maxEv after prepending note
		if len(events) > maxEv {
			events = events[:maxEv]
		}
	}

	return events
}

func truncateMsg(s string) string {
	if len(s) <= maxEventMessageLen {
		return s
	}
	return s[:maxEventMessageLen]
}
