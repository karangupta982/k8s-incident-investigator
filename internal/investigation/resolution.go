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

package investigation

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// ResolutionTransitioner performs the recoverable two-step transition from an active
// IncidentReport to a historical one.
//
// Steps:
//  1. Single PATCH: set phase=Resolved, resolvedAt=now, clear stabilityStartedAt,
//     remove the active label — one atomic Kubernetes operation.
//  2. CREATE a new historical IncidentReport (copy of active, unique historical name).
//  3. DELETE the active-named slot.
//
// If the controller crashes after step 2, startup recovery detects both objects exist
// (active one is Resolved) and completes the DELETE. No history is lost.
type ResolutionTransitioner struct {
	client client.Client
	log    logr.Logger
}

// NewResolutionTransitioner creates a new ResolutionTransitioner.
func NewResolutionTransitioner(c client.Client, log logr.Logger) *ResolutionTransitioner {
	return &ResolutionTransitioner{client: c, log: log}
}

// Transition executes the three-step resolution transition.
// `active` must be the currently active (non-Resolved) IncidentReport.
// `now` is the resolution timestamp.
func (t *ResolutionTransitioner) Transition(ctx context.Context, active *v1alpha1.IncidentReport, now time.Time) error {
	// Determine historical name before mutating the active object.
	historicalName, err := t.historicalName(active, now)
	if err != nil {
		return err
	}

	// ---- Step 1: Single PATCH ----
	// Atomically set phase=Resolved, resolvedAt=now, clear stabilityStartedAt, remove active label.
	// We patch both metadata and status in two sub-steps because the status subresource
	// only accepts status changes, and metadata labels require a separate patch.

	// Make a base copy for status patch.
	baseCopy := active.DeepCopy()

	resolved := metav1.NewTime(now)
	active.Status.Phase = v1alpha1.PhaseResolved
	active.Status.ResolvedAt = &resolved
	active.Status.StabilityStartedAt = nil

	if err := t.client.Status().Patch(ctx, active, client.MergeFrom(baseCopy)); err != nil {
		return fmt.Errorf("patching IncidentReport status to Resolved: %w", err)
	}

	// Patch metadata labels to remove the active label.
	labelBase := active.DeepCopy()
	if active.Labels == nil {
		active.Labels = map[string]string{}
	}
	delete(active.Labels, LabelActive)
	if err := t.client.Patch(ctx, active, client.MergeFrom(labelBase)); err != nil {
		return fmt.Errorf("patching IncidentReport labels on resolution: %w", err)
	}

	// ---- Step 2: CREATE historical copy ----
	historical := buildHistoricalReport(active, historicalName)
	if err := t.client.Create(ctx, historical); err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("creating historical IncidentReport %s/%s: %w", active.Namespace, historicalName, err)
	}
	t.log.Info("created historical IncidentReport",
		"name", historicalName,
		"namespace", active.Namespace)

	// ---- Step 3: DELETE the active slot ----
	if err := t.client.Delete(ctx, active); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("deleting active IncidentReport %s/%s: %w", active.Namespace, active.Name, err)
	}
	t.log.Info("deleted active IncidentReport slot",
		"name", active.Name,
		"namespace", active.Namespace)

	return nil
}

// historicalName computes the historical name for the active report.
// Falls back to a date-based name when workload identity or startedAt is unavailable.
func (t *ResolutionTransitioner) historicalName(active *v1alpha1.IncidentReport, now time.Time) (string, error) {
	if active.Spec.Workload != nil && active.Status.StartedAt != nil {
		return GenerateHistoricalName(
			active.Spec.Workload.Name,
			active.Spec.Workload.Kind,
			active.Namespace,
			active.Status.StartedAt.Time,
		), nil
	}
	// Pod-level fallback: use active name as base with date suffix.
	base := active.Name
	// Strip the -pod-active suffix if present to keep the name clean.
	const podActiveSuffix = "-pod-active"
	if len(base) > len(podActiveSuffix) && base[len(base)-len(podActiveSuffix):] == podActiveSuffix {
		base = base[:len(base)-len(podActiveSuffix)]
	}
	date := now.UTC().Format("20060102")
	name := fmt.Sprintf("%s-%s", base, date)
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}
	return name, nil
}

// buildHistoricalReport constructs a fresh IncidentReport with the historical name.
// Server-managed metadata fields (UID, ResourceVersion, CreationTimestamp,
// ManagedFields, DeletionTimestamp, Finalizers) are explicitly excluded.
func buildHistoricalReport(active *v1alpha1.IncidentReport, historicalName string) *v1alpha1.IncidentReport {
	labels := make(map[string]string, len(active.Labels))
	for k, v := range active.Labels {
		if k != LabelActive {
			labels[k] = v
		}
	}

	annotations := make(map[string]string, len(active.Annotations))
	for k, v := range active.Annotations {
		annotations[k] = v
	}

	specCopy := active.Spec.DeepCopy()
	statusCopy := active.Status.DeepCopy()

	return &v1alpha1.IncidentReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:        historicalName,
			Namespace:   active.Namespace,
			Labels:      labels,
			Annotations: annotations,
			// Explicitly excluded: UID, ResourceVersion, CreationTimestamp,
			// ManagedFields, DeletionTimestamp, Finalizers
		},
		Spec:   *specCopy,
		Status: *statusCopy,
	}
}
