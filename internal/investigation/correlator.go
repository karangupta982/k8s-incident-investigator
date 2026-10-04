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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

const (
	// LabelWorkloadNamespace labels an IncidentReport with the namespace of its workload.
	LabelWorkloadNamespace = "investigator.k8s.io/workload-namespace"
	// LabelWorkloadName labels an IncidentReport with the name of its workload.
	LabelWorkloadName = "investigator.k8s.io/workload-name"
	// LabelWorkloadKind labels an IncidentReport with the kind of its workload.
	LabelWorkloadKind = "investigator.k8s.io/workload-kind"
	// LabelActive is set to "true" on active incidents and removed (or absent) on resolution.
	LabelActive = "investigator.k8s.io/active"
)

// CorrelationResult is the output of incident correlation.
type CorrelationResult struct {
	// ExistingReport is the currently active IncidentReport, if one was found.
	// Nil when ShouldCreate is true.
	ExistingReport *v1alpha1.IncidentReport

	// ShouldCreate is true when no active IncidentReport was found and a new one must be created.
	ShouldCreate bool
}

// IncidentCorrelatorInterface finds or determines that a new incident should be created.
type IncidentCorrelatorInterface interface {
	// Correlate performs a deterministic GET of the active-named IncidentReport.
	// workload may be nil when workload ownership could not be resolved (Pod-level fallback).
	// podName and podNamespace are used for the Pod-level name fallback.
	Correlate(ctx context.Context, workload *v1alpha1.WorkloadRef, podName, podNamespace string, trigger TriggerResult) (CorrelationResult, error)
}

// IncidentCorrelator implements IncidentCorrelatorInterface using a deterministic GET
// strategy. The active-named IncidentReport is fetched directly by name; label selectors
// are not used as the primary lookup mechanism.
type IncidentCorrelator struct {
	client client.Client
	log    logr.Logger
}

// NewIncidentCorrelator creates a new IncidentCorrelator.
func NewIncidentCorrelator(c client.Client, log logr.Logger) *IncidentCorrelator {
	return &IncidentCorrelator{client: c, log: log}
}

// Correlate performs a deterministic GET of the active-named IncidentReport.
//
// Decision rules:
//   - NotFound                       → ShouldCreate = true
//   - Found, phase != Resolved       → active incident; ExistingReport = found report
//   - Found, phase == Resolved       → stale crash artifact; ShouldCreate = true
func (c *IncidentCorrelator) Correlate(ctx context.Context, workload *v1alpha1.WorkloadRef, podName, podNamespace string, _ TriggerResult) (CorrelationResult, error) {
	activeName := activeNameForWorkload(workload, podName)
	namespace := podNamespace
	if workload != nil {
		namespace = workload.Namespace
	}

	var report v1alpha1.IncidentReport
	err := c.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: activeName}, &report)
	if err != nil {
		if errors.IsNotFound(err) {
			return CorrelationResult{ShouldCreate: true}, nil
		}
		return CorrelationResult{}, fmt.Errorf("getting IncidentReport %s/%s: %w", namespace, activeName, err)
	}

	// Active-named report found — check if it is genuinely active or a stale crash artifact.
	if report.Status.Phase == v1alpha1.PhaseResolved {
		// Stale: the resolution transition completed step 1 (PATCH to Resolved) but the
		// DELETE of the active slot did not finish before a crash. Startup recovery will
		// clean this up; here we signal that a new active incident should be created.
		c.log.Info("found stale resolved active-named IncidentReport, signalling new incident creation",
			"name", activeName, "namespace", namespace)
		return CorrelationResult{ShouldCreate: true}, nil
	}

	return CorrelationResult{ExistingReport: &report, ShouldCreate: false}, nil
}

// activeNameForWorkload builds the deterministic active incident name.
// Falls back to the Pod-based name when workload ownership is not resolved.
func activeNameForWorkload(workload *v1alpha1.WorkloadRef, podName string) string {
	if workload == nil {
		return GeneratePodActiveName(podName)
	}
	return GenerateActiveName(workload.Name, workload.Kind)
}
