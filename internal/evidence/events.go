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

package evidence

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// EventCollector collects Layer 2 evidence from Kubernetes Events.
type EventCollector struct{}

// Collect lists Events for the affected Pod and returns bounded, sorted evidence.
func (c *EventCollector) Collect(ctx context.Context, input CollectorInput) ([]v1alpha1.EventEvidence, []v1alpha1.CollectionError) {
	if input.Pod == nil {
		return nil, nil // no pod, no events to collect
	}

	var eventList corev1.EventList
	if err := input.Client.List(ctx, &eventList, client.InNamespace(input.Pod.Namespace)); err != nil {
		return nil, []v1alpha1.CollectionError{{
			Source: "events",
			Reason: fmt.Sprintf("failed to list events in namespace %s: %v", input.Pod.Namespace, err),
		}}
	}

	// Filter to events for this Pod only
	var relevant []corev1.Event
	for i := range eventList.Items {
		ev := &eventList.Items[i]
		if ev.InvolvedObject.Kind == "Pod" && ev.InvolvedObject.Name == input.Pod.Name {
			relevant = append(relevant, *ev)
		}
	}

	// Sort descending by LastTimestamp (most recent first)
	sort.Slice(relevant, func(i, j int) bool {
		ti := relevant[i].LastTimestamp.Time
		tj := relevant[j].LastTimestamp.Time
		return ti.After(tj)
	})

	// Retain only the most recent MaxEventsPerIncident events
	max := input.Config.MaxEventsPerIncident
	if len(relevant) > max {
		relevant = relevant[:max]
	}

	result := make([]v1alpha1.EventEvidence, 0, len(relevant))
	for _, ev := range relevant {
		ee := v1alpha1.EventEvidence{
			Reason:             ev.Reason,
			Message:            TruncateMessage(ev.Message),
			Count:              ev.Count,
			InvolvedObjectKind: ev.InvolvedObject.Kind,
			InvolvedObjectName: ev.InvolvedObject.Name,
		}
		if !ev.FirstTimestamp.IsZero() {
			t := metav1.NewTime(ev.FirstTimestamp.Time)
			ee.FirstTime = &t
		}
		if !ev.LastTimestamp.IsZero() {
			t := metav1.NewTime(ev.LastTimestamp.Time)
			ee.LastTime = &t
		}
		result = append(result, ee)
	}
	return result, nil
}

// truncateMessage truncates a message string to at most 256 characters.
func TruncateMessage(msg string) string {
	const maxLen = 256
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}
