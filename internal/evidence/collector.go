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
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

// CollectorInput is the read-only context passed to every collector.
// Collectors must not modify any field.
type CollectorInput struct {
	// Client is the controller-runtime client for Kubernetes API access.
	Client client.Client

	// KubeClient is the raw client-go client, required for log streaming.
	KubeClient kubernetes.Interface

	// Report is the current IncidentReport being investigated.
	Report *v1alpha1.IncidentReport

	// Pod is the primary affected Pod, fetched by the orchestrator.
	// May be nil if the Pod was deleted before collection ran.
	Pod *corev1.Pod

	// Config holds the evidence collection limits.
	Config *config.Config
}

// triggerNeeds maps TriggerType to the optional layers it activates.
type triggerNeeds struct {
	previousLogs       bool
	currentLogs        bool
	pvcEvidence        bool
	imagePullEvidence  bool
	schedulingEvidence bool
}

// evidenceNeedsForTrigger returns which optional layers to activate for a given trigger type.
func evidenceNeedsForTrigger(t v1alpha1.TriggerType) triggerNeeds {
	switch t {
	case v1alpha1.TriggerOOMKilled:
		return triggerNeeds{previousLogs: true, currentLogs: true}
	case v1alpha1.TriggerCrashLoopBackOff:
		return triggerNeeds{previousLogs: true, currentLogs: true}
	case v1alpha1.TriggerEviction:
		return triggerNeeds{previousLogs: true, currentLogs: true}
	case v1alpha1.TriggerMountFailure:
		return triggerNeeds{currentLogs: true, pvcEvidence: true}
	case v1alpha1.TriggerImagePullBackOff:
		return triggerNeeds{imagePullEvidence: true}
	case v1alpha1.TriggerCreateContainerConfigError:
		return triggerNeeds{}
	case v1alpha1.TriggerSchedulingFailure:
		return triggerNeeds{schedulingEvidence: true}
	case v1alpha1.TriggerReadinessProbeFailure, v1alpha1.TriggerLivenessProbeFailure:
		return triggerNeeds{currentLogs: true}
	default:
		return triggerNeeds{currentLogs: true}
	}
}

// EvidenceOrchestrator coordinates all evidence layers and returns a complete EvidenceSnapshot.
// It enforces the overall collection timeout and tolerates individual layer failures.
// Collect always returns a populated EvidenceSnapshot even when all sources fail.
type EvidenceOrchestrator struct {
	Client     client.Client
	KubeClient kubernetes.Interface
	Config     *config.Config
	Log        logr.Logger
}

// Collect runs all evidence layers for the given report and returns the snapshot.
// The context should carry the configured EvidenceCollectionTimeout.
func (o *EvidenceOrchestrator) Collect(
	ctx context.Context,
	report *v1alpha1.IncidentReport,
	pod *corev1.Pod,
) v1alpha1.EvidenceSnapshot {
	start := time.Now()
	now := metav1.Now()
	snapshot := v1alpha1.EvidenceSnapshot{
		CollectedAt: &now,
	}

	// Determine which trigger type we are dealing with.
	var triggerType v1alpha1.TriggerType
	if report.Status.Trigger != nil {
		triggerType = report.Status.Trigger.Type
	}
	needs := evidenceNeedsForTrigger(triggerType)

	input := CollectorInput{
		Client:     o.Client,
		KubeClient: o.KubeClient,
		Report:     report,
		Pod:        pod,
		Config:     o.Config,
	}

	// If Pod is not provided, try to fetch the first affected pod from the report.
	if input.Pod == nil && len(report.Status.AffectedPods) > 0 {
		ref := report.Status.AffectedPods[0]
		var fetchedPod corev1.Pod
		if err := o.Client.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &fetchedPod); err != nil {
			if !apierrors.IsNotFound(err) {
				o.Log.Error(err, "failed to fetch pod for evidence collection", "pod", ref.Namespace+"/"+ref.Name)
			}
			snapshot.CollectionErrors = append(snapshot.CollectionErrors, v1alpha1.CollectionError{
				Source: "pod",
				Reason: fmt.Sprintf("pod %s/%s unavailable: %v", ref.Namespace, ref.Name, err),
			})
		} else {
			input.Pod = &fetchedPod
		}
	}

	// Layer 1: Pod evidence
	podEvidence, podErrs := (&PodCollector{}).Collect(ctx, input)
	snapshot.Pod = podEvidence
	snapshot.CollectionErrors = append(snapshot.CollectionErrors, podErrs...)

	// Layer 2: Event evidence
	events, eventErrs := (&EventCollector{}).Collect(ctx, input)
	snapshot.Events = events
	snapshot.CollectionErrors = append(snapshot.CollectionErrors, eventErrs...)

	// Layer 3: Workload evidence
	workloadEvidence, workloadErrs := (&WorkloadCollector{}).Collect(ctx, input)
	snapshot.Workload = workloadEvidence
	snapshot.CollectionErrors = append(snapshot.CollectionErrors, workloadErrs...)

	// Layer 4: Node evidence
	nodeEvidence, nodeErrs := (&NodeCollector{}).Collect(ctx, input)
	snapshot.Node = nodeEvidence
	snapshot.CollectionErrors = append(snapshot.CollectionErrors, nodeErrs...)

	// Layer 6: Log evidence (conditional on trigger type)
	if needs.currentLogs || needs.previousLogs {
		logs, _ := (&LogCollector{}).Collect(ctx, input, needs)
		snapshot.Logs = logs
		// Log unavailability is captured inside ContainerLogEvidence.UnavailableReason
	}

	// Layer 5: Dependency evidence (adaptive)
	if needs.pvcEvidence || needs.imagePullEvidence || needs.schedulingEvidence {
		deps, depErrs := (&DependencyCollector{}).Collect(ctx, input, needs)
		snapshot.Dependencies = deps
		snapshot.CollectionErrors = append(snapshot.CollectionErrors, depErrs...)
	}

	// Check if context deadline was exceeded during collection.
	if ctx.Err() != nil {
		snapshot.CollectionErrors = append(snapshot.CollectionErrors, v1alpha1.CollectionError{
			Source: "collection",
			Reason: fmt.Sprintf("evidence collection timeout after %s: %v", time.Since(start).Round(time.Millisecond), ctx.Err()),
		})
		o.Log.Info("evidence collection timed out, storing partial results",
			"elapsed", time.Since(start).String())
	}

	return snapshot
}
