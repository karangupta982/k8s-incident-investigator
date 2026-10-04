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

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/correlation"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/diagnosis"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/metrics"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/reporting"
)

// PodReconciler watches Pods and Kubernetes Events for failure signals,
// creates and maintains IncidentReport resources, and manages the incident lifecycle.
//
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
type PodReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	Config               *config.Config
	TriggerEvaluator     investigation.TriggerEvaluatorInterface
	OwnershipResolver    investigation.OwnershipResolverInterface
	Correlator           investigation.IncidentCorrelatorInterface
	RecoveryEvaluator    investigation.RecoveryEvaluatorInterface
	Transitioner         *investigation.ResolutionTransitioner
	EvidenceOrchestrator *evidence.EvidenceOrchestrator
	EvidCorrelator       *correlation.EvidenceCorrelator
	DiagnosisEngine      *diagnosis.DiagnosisEngine
	ReportingEngine      *reporting.ReportingEngine
	Metrics              metrics.RecorderInterface
	Log                  logr.Logger
}

// SetupWithManager registers the controller with the manager and configures watches.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Watches(
			&corev1.Event{},
			handler.EnqueueRequestsFromMapFunc(r.eventToPodRequests),
			builder.WithPredicates(relevantEventPredicate()),
		).
		WithEventFilter(relevancePredicate(r.Config)).
		Complete(r)
}

// eventToPodRequests maps a relevant Kubernetes Event to a reconcile request for its involved Pod.
func (r *PodReconciler) eventToPodRequests(_ context.Context, obj client.Object) []reconcile.Request {
	evt, ok := obj.(*corev1.Event)
	if !ok || evt.InvolvedObject.Kind != "Pod" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: evt.InvolvedObject.Namespace,
			Name:      evt.InvolvedObject.Name,
		},
	}}
}

// relevantEventPredicate filters Kubernetes Events to the three relevant reasons.
func relevantEventPredicate() predicate.Predicate {
	relevant := map[string]bool{
		"FailedMount":      true,
		"Unhealthy":        true,
		"FailedScheduling": true,
	}
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		evt, ok := obj.(*corev1.Event)
		if !ok {
			return false
		}
		return relevant[evt.Reason]
	})
}

// relevancePredicate filters Pod events to exclude obviously non-triggerable states.
func relevancePredicate(cfg *config.Config) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isPodRelevant(e.Object, cfg)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isPodRelevant(e.ObjectNew, cfg)
		},
		DeleteFunc: func(_ event.DeleteEvent) bool {
			// Allow delete events — we may need to handle the active incident after Pod deletion.
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isPodRelevant(e.Object, cfg)
		},
	}
}

// isPodRelevant returns false for Pods that definitely cannot trigger an incident.
func isPodRelevant(obj client.Object, cfg *config.Config) bool {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return true // not a Pod — let it through
	}
	// Skip succeeded Pods.
	if pod.Status.Phase == corev1.PodSucceeded {
		return false
	}
	// Namespace filter: if WatchNamespaces is non-empty, only watch those namespaces.
	if len(cfg.WatchNamespaces) > 0 {
		for _, ns := range cfg.WatchNamespaces {
			if pod.Namespace == ns {
				return true
			}
		}
		return false
	}
	return true
}

// Reconcile is the main reconciliation loop for the PodReconciler.
// It follows the 12-step outline described in the design document.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("pod", req.NamespacedName)

	// ---- Step 1: Fetch Pod from cache ----
	var pod corev1.Pod
	if err := r.Get(ctx, req.NamespacedName, &pod); err != nil {
		if apierrors.IsNotFound(err) {
			// Pod deleted — the incident (if any) stays active; recovery will handle it
			// on the next requeue interval. Nothing to do here.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting pod %s: %w", req.NamespacedName, err)
	}

	// ---- Step 2: Quick state-based trigger check (no API calls needed) ----
	// Call the evaluator with zero event counts to get a typed TriggerResult for
	// any state-based signal.
	quickResult := r.TriggerEvaluator.Evaluate(ctx, investigation.TriggerContext{Pod: &pod}, r.Config)
	isStateBased := quickResult.IsTrigger && quickResult.Source == v1alpha1.TriggerSourceStateBased

	var triggerResult investigation.TriggerResult

	if isStateBased {
		// Fast path: use the result from the quick check directly.
		triggerResult = quickResult
		log.V(1).Info("state-based trigger detected", "triggerType", triggerResult.Type)
	} else {
		// ---- Step 3: Fetch Kubernetes Events for count-based evaluation ----
		eventCounts, err := r.fetchEventCounts(ctx, &pod)
		if err != nil {
			log.Error(err, "failed to fetch events for pod, proceeding with zero counts")
			// Non-fatal: proceed with zero event counts.
		}

		// ---- Step 4: Full trigger evaluation ----
		triggerResult = r.TriggerEvaluator.Evaluate(ctx, investigation.TriggerContext{
			Pod:                         &pod,
			MountFailureEventCount:      eventCounts.mountFailure,
			ReadinessProbeEventCount:    eventCounts.readinessProbe,
			LivenessProbeEventCount:     eventCounts.livenessProbe,
			SchedulingFailureEventCount: eventCounts.schedulingFailure,
		}, r.Config)

		if !triggerResult.IsTrigger {
			log.V(1).Info("no trigger detected, skipping")
			return ctrl.Result{}, nil
		}
		log.V(1).Info("event-based trigger detected", "triggerType", triggerResult.Type)
	}

	// ---- Step 5: Resolve workload ownership ----
	workload, err := r.OwnershipResolver.Resolve(ctx, &pod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving ownership for pod %s: %w", req.NamespacedName, err)
	}
	log.V(1).Info("ownership resolved", "workload", workloadString(workload))

	// ---- Steps 6 & 7: Correlate — find or determine creation of IncidentReport ----
	correlation, err := r.Correlator.Correlate(ctx, workload, pod.Name, pod.Namespace, triggerResult)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("correlating incident for pod %s: %w", req.NamespacedName, err)
	}

	var report *v1alpha1.IncidentReport

	if correlation.ShouldCreate {
		// ---- Step 8: CREATE new IncidentReport ----
		report, err = r.createIncidentReport(ctx, &pod, workload, triggerResult)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				// Race condition: another reconciler created it — fetch and continue.
				activeName := r.activeName(workload, pod.Name)
				var existing v1alpha1.IncidentReport
				if getErr := r.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: activeName}, &existing); getErr != nil {
					return ctrl.Result{}, fmt.Errorf("fetching existing report after AlreadyExists: %w", getErr)
				}
				report = &existing
				log.V(1).Info("reusing existing IncidentReport after concurrent create", "name", report.Name)
			} else {
				return ctrl.Result{}, fmt.Errorf("creating incident report for pod %s: %w", req.NamespacedName, err)
			}
		} else {
			log.Info("created IncidentReport",
				"name", report.Name,
				"workload", workloadString(workload),
				"triggerType", triggerResult.Type)
			r.recordMetric(func(m metrics.RecorderInterface) {
				m.RecordIncidentDetected(string(triggerResult.Type), pod.Namespace)
			})
		}
	} else {
		report = correlation.ExistingReport
		log.V(1).Info("reusing existing IncidentReport", "name", report.Name)
	}

	// ---- Step 9: PATCH status — additive update ----
	now := metav1.Now()
	if err := r.updateIncidentStatus(ctx, report, &pod, workload, triggerResult, now); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating incident status for pod %s: %w", req.NamespacedName, err)
	}

	// Re-fetch the report to get the latest resourceVersion before evidence collection.
	var updatedReport v1alpha1.IncidentReport
	if err := r.Get(ctx, types.NamespacedName{Namespace: report.Namespace, Name: report.Name}, &updatedReport); err != nil {
		return ctrl.Result{}, fmt.Errorf("re-fetching report after status update: %w", err)
	}
	report = &updatedReport

	// ---- Step 9.5: Collect evidence ----
	start95 := time.Now()
	if r.EvidenceOrchestrator != nil {
		collectCtx, collectCancel := context.WithTimeout(ctx, r.Config.EvidenceCollectionTimeout)
		defer collectCancel()
		snapshot := r.EvidenceOrchestrator.Collect(collectCtx, report, &pod)
		evidenceBase := report.DeepCopy()
		report.Status.Evidence = &snapshot
		if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(evidenceBase)); patchErr != nil {
			log.Error(patchErr, "failed to patch evidence snapshot, will retry on next reconciliation")
			// Non-fatal: restore the pre-patch report so recovery evaluation continues
			report.Status.Evidence = nil
			// Record evidence collection metrics
			collectionDuration := time.Since(start95)
			trigType := ""
			if report.Status.Trigger != nil {
				trigType = string(report.Status.Trigger.Type)
			}
			r.recordMetric(func(m metrics.RecorderInterface) {
				m.RecordEvidenceCollectionDuration(trigType, collectionDuration.Seconds())
				for _, cerr := range snapshot.CollectionErrors {
					m.RecordEvidenceCollectionFailure(cerr.Source)
				}
			})
		}
	}

	// ---- Step 9.55: Correlate evidence ----
	if r.EvidCorrelator != nil && report.Status.Evidence != nil {
		correlated := r.EvidCorrelator.Correlate(report.Status.Evidence)
		correlBase := report.DeepCopy()
		report.Status.CorrelatedEvidence = &correlated
		if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(correlBase)); patchErr != nil {
			log.Error(patchErr, "failed to patch correlated evidence, will retry on next reconciliation")
			report.Status.CorrelatedEvidence = nil
		}
	}

	// ---- Step 9.6: Run diagnosis engine ----
	if r.DiagnosisEngine != nil && report.Status.Evidence != nil {
		diagResult := r.DiagnosisEngine.Evaluate(report.Status.Evidence)
		r.recordMetric(func(m metrics.RecorderInterface) {
			trigType := ""
			if report.Status.Trigger != nil {
				trigType = string(report.Status.Trigger.Type)
			}
			if diagResult.Primary != nil {
				m.RecordDiagnosed(diagResult.Primary.RuleID, trigType)
				m.RecordDiagnosisRuleMatch(diagResult.Primary.RuleID, diagResult.Primary.Confidence)
			} else {
				m.RecordUnknownDiagnosis(trigType)
			}
			for _, f := range diagResult.ContributingFactors {
				m.RecordDiagnosisRuleMatch(f.RuleID, f.Confidence)
			}
		})
		if applyErr := r.applyDiagnosis(ctx, report, diagResult); applyErr != nil {
			return ctrl.Result{}, fmt.Errorf("applying diagnosis for %s: %w", report.Name, applyErr)
		}
		// Re-fetch after diagnosis patch to get updated resourceVersion.
		var postDiagReport v1alpha1.IncidentReport
		if err := r.Get(ctx, types.NamespacedName{Namespace: report.Namespace, Name: report.Name}, &postDiagReport); err != nil {
			return ctrl.Result{}, fmt.Errorf("re-fetching report after diagnosis: %w", err)
		}
		report = &postDiagReport
	}

	// ---- Step 9.7: Generate report summary and timeline ----
	if r.ReportingEngine != nil {
		renderResult := r.ReportingEngine.Render(report)
		reportBase := report.DeepCopy()
		report.Status.Summary = renderResult.Summary
		report.Status.Timeline = renderResult.Timeline
		if renderResult.Diagnosis != nil {
			report.Status.Diagnosis = renderResult.Diagnosis
		}
		if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(reportBase)); patchErr != nil {
			log.Error(patchErr, "failed to patch reporting output, will retry on next reconciliation")
		}
	}

	// ---- Step 10: Fetch WorkloadSnapshot for recovery evaluation ----
	snapshot, snapshotErr := r.fetchWorkloadSnapshot(ctx, report)
	if snapshotErr != nil {
		log.Error(snapshotErr, "failed to fetch workload snapshot, using pod-level fallback")
		// Non-fatal: recovery evaluator will use whatever snapshot was populated.
	}

	// ---- Step 11: Evaluate recovery (workload-type-aware) ----
	recoveryResult := r.RecoveryEvaluator.Evaluate(ctx, report, snapshot, r.Config)
	log.V(1).Info("recovery evaluation result",
		"workloadHealthy", recoveryResult.WorkloadHealthy,
		"shouldResolve", recoveryResult.ShouldResolve,
		"shouldResetStabilityTimer", recoveryResult.ShouldResetStabilityTimer)

	if recoveryResult.ShouldResolve {
		log.Info("incident resolved, transitioning to historical record",
			"name", report.Name,
			"incidentReport", report.Namespace+"/"+report.Name)
		if transErr := r.Transitioner.Transition(ctx, report, now.Time); transErr != nil {
			return ctrl.Result{}, fmt.Errorf("transitioning incident to resolved: %w", transErr)
		}
		r.recordMetric(func(m metrics.RecorderInterface) {
			trigType := ""
			if report.Status.Trigger != nil {
				trigType = string(report.Status.Trigger.Type)
			}
			var dur float64
			if report.Status.StartedAt != nil && report.Status.ResolvedAt != nil {
				dur = report.Status.ResolvedAt.Sub(report.Status.StartedAt.Time).Seconds()
			}
			m.RecordInvestigationCompleted(trigType, dur)
			m.RecordActiveDecrement(report.Namespace)
		})
		return ctrl.Result{}, nil
	}

	if recoveryResult.ShouldResetStabilityTimer {
		if patchErr := r.clearStabilityTimer(ctx, report); patchErr != nil {
			return ctrl.Result{}, fmt.Errorf("clearing stability timer: %w", patchErr)
		}
	} else if recoveryResult.WorkloadHealthy && report.Status.StabilityStartedAt == nil {
		// Workload just became healthy — start the stability timer.
		if patchErr := r.setStabilityTimer(ctx, report, now); patchErr != nil {
			return ctrl.Result{}, fmt.Errorf("setting stability timer: %w", patchErr)
		}
		log.Info("stability period started", "name", report.Name)
	}

	// ---- Step 12: Requeue for ongoing monitoring ----
	return ctrl.Result{RequeueAfter: r.Config.RequeueInterval}, nil
}

// podEventCounts holds the maximum Kubernetes Event.count values per failure type.
type podEventCounts struct {
	mountFailure      int
	readinessProbe    int
	livenessProbe     int
	schedulingFailure int
}

// fetchEventCounts lists Kubernetes Events for the Pod and extracts the max event.count
// per failure reason. Uses the highest count among all matching Event objects (not a sum).
func (r *PodReconciler) fetchEventCounts(ctx context.Context, pod *corev1.Pod) (podEventCounts, error) {
	var eventList corev1.EventList
	if err := r.List(ctx, &eventList, client.InNamespace(pod.Namespace)); err != nil {
		return podEventCounts{}, fmt.Errorf("listing events in namespace %s: %w", pod.Namespace, err)
	}

	counts := podEventCounts{}
	for _, ev := range eventList.Items {
		if ev.InvolvedObject.Kind != "Pod" || ev.InvolvedObject.Name != pod.Name {
			continue
		}
		c := int(ev.Count)
		switch ev.Reason {
		case "FailedMount":
			if c > counts.mountFailure {
				counts.mountFailure = c
			}
		case "FailedScheduling":
			if c > counts.schedulingFailure {
				counts.schedulingFailure = c
			}
		case "Unhealthy":
			msg := ev.Message
			switch {
			case strings.HasPrefix(msg, "Readiness probe"):
				if c > counts.readinessProbe {
					counts.readinessProbe = c
				}
			case strings.HasPrefix(msg, "Liveness probe"):
				if c > counts.livenessProbe {
					counts.livenessProbe = c
				}
			}
		}
	}
	return counts, nil
}

// createIncidentReport creates a new active IncidentReport for the given Pod and trigger.
// The spec and initial metadata are written in the CREATE call; status is written via the
// status subresource immediately after.
func (r *PodReconciler) createIncidentReport(
	ctx context.Context,
	pod *corev1.Pod,
	workload *v1alpha1.WorkloadRef,
	trigger investigation.TriggerResult,
) (*v1alpha1.IncidentReport, error) {
	activeName := r.activeName(workload, pod.Name)
	now := metav1.Now()

	labels := map[string]string{
		investigation.LabelActive: "true",
	}
	if workload != nil {
		labels[investigation.LabelWorkloadNamespace] = workload.Namespace
		labels[investigation.LabelWorkloadName] = workload.Name
		labels[investigation.LabelWorkloadKind] = workload.Kind
	}

	report := &v1alpha1.IncidentReport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeName,
			Namespace: pod.Namespace,
			Labels:    labels,
		},
		Spec: v1alpha1.IncidentReportSpec{
			Workload: workload,
		},
	}

	if err := r.Create(ctx, report); err != nil {
		return nil, err
	}

	// Populate the status via the status subresource.
	observedAt := now.DeepCopy()
	report.Status = v1alpha1.IncidentReportStatus{
		Phase:     v1alpha1.PhaseInvestigating,
		StartedAt: &now,
		Trigger: &v1alpha1.TriggerInfo{
			Type:          trigger.Type,
			ContainerName: trigger.ContainerName,
			Reason:        trigger.Reason,
			ObservedAt:    observedAt,
		},
		AffectedPods: []v1alpha1.PodRef{
			{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID},
		},
		WorkloadOwnerResolved: workload != nil,
		FailureCount:          1,
		LastFailureAt:         &now,
	}
	setConditions(report, now)

	if err := r.Status().Update(ctx, report); err != nil {
		// Non-fatal: the next reconciliation will re-apply the status.
		r.Log.Error(err, "failed to set initial status on IncidentReport", "name", report.Name)
	}

	return report, nil
}

// updateIncidentStatus additively patches the IncidentReport status with the current
// trigger and Pod information. Immutable fields (StartedAt, initial Trigger) are only
// written when currently nil/zero.
func (r *PodReconciler) updateIncidentStatus(
	ctx context.Context,
	report *v1alpha1.IncidentReport,
	pod *corev1.Pod,
	workload *v1alpha1.WorkloadRef,
	trigger investigation.TriggerResult,
	now metav1.Time,
) error {
	base := report.DeepCopy()

	// Deduplicate AffectedPods by UID before appending.
	podRef := v1alpha1.PodRef{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID}
	if !podUIDPresent(report.Status.AffectedPods, pod.UID) {
		report.Status.AffectedPods = append(report.Status.AffectedPods, podRef)
	}

	// Set Trigger only if not already set (immutable after first assignment).
	if report.Status.Trigger == nil {
		observedAt := now.DeepCopy()
		report.Status.Trigger = &v1alpha1.TriggerInfo{
			Type:          trigger.Type,
			ContainerName: trigger.ContainerName,
			Reason:        trigger.Reason,
			ObservedAt:    observedAt,
		}
	}

	// StartedAt is immutable — only set if nil.
	if report.Status.StartedAt == nil {
		nowCopy := now.DeepCopy()
		report.Status.StartedAt = nowCopy
	}

	// Mark workload ownership as resolved if we now have a workload.
	if workload != nil {
		report.Status.WorkloadOwnerResolved = true
	}

	// Update LastFailureAt to now.
	nowCopy := now.DeepCopy()
	report.Status.LastFailureAt = nowCopy

	// Refresh standard status conditions.
	setConditions(report, now)

	return r.Status().Patch(ctx, report, client.MergeFrom(base))
}

// setConditions updates the three standard status conditions on the IncidentReport.
// Uses meta.SetStatusCondition from k8s.io/apimachinery/pkg/api/meta.
func setConditions(report *v1alpha1.IncidentReport, now metav1.Time) {
	// Active condition
	activeStatus := metav1.ConditionTrue
	activeReason := "ActiveIncident"
	activeMsg := "Incident is active and under investigation"
	if report.Status.Phase == v1alpha1.PhaseResolved {
		activeStatus = metav1.ConditionFalse
		activeReason = "Resolved"
		activeMsg = "Incident has been resolved"
	}
	apimeta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "Active",
		Status:             activeStatus,
		ObservedGeneration: report.Generation,
		LastTransitionTime: now,
		Reason:             activeReason,
		Message:            activeMsg,
	})

	// WorkloadOwnerResolved condition
	ownerStatus := metav1.ConditionFalse
	ownerReason := "UnresolvableOwner"
	ownerMsg := "Workload ownership could not be determined; incident tracked at Pod level"
	if report.Status.WorkloadOwnerResolved {
		ownerStatus = metav1.ConditionTrue
		ownerReason = "OwnerResolved"
		ownerMsg = "Workload ownership successfully determined"
	}
	apimeta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "WorkloadOwnerResolved",
		Status:             ownerStatus,
		ObservedGeneration: report.Generation,
		LastTransitionTime: now,
		Reason:             ownerReason,
		Message:            ownerMsg,
	})

	// StabilityPeriodStarted condition
	stabilityStatus := metav1.ConditionFalse
	stabilityReason := "StabilityPeriodNotStarted"
	stabilityMsg := "Workload has not yet become healthy"
	if report.Status.StabilityStartedAt != nil {
		stabilityStatus = metav1.ConditionTrue
		stabilityReason = "StabilityPeriodStarted"
		stabilityMsg = "Workload is healthy; waiting for stability period to elapse"
	}
	apimeta.SetStatusCondition(&report.Status.Conditions, metav1.Condition{
		Type:               "StabilityPeriodStarted",
		Status:             stabilityStatus,
		ObservedGeneration: report.Generation,
		LastTransitionTime: now,
		Reason:             stabilityReason,
		Message:            stabilityMsg,
	})
}

// fetchWorkloadSnapshot builds the WorkloadSnapshot used by RecoveryEvaluator.
// When workload ownership is resolved, it fetches the specific workload resource.
// Otherwise it falls back to fetching the Pods listed in AffectedPods.
func (r *PodReconciler) fetchWorkloadSnapshot(ctx context.Context, report *v1alpha1.IncidentReport) (investigation.WorkloadSnapshot, error) {
	if report.Spec.Workload == nil || !report.Status.WorkloadOwnerResolved {
		// Pod-level fallback: fetch Pods from AffectedPods that still exist.
		var pods []corev1.Pod
		for _, ref := range report.Status.AffectedPods {
			var pod corev1.Pod
			if err := r.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &pod); err != nil {
				if !apierrors.IsNotFound(err) {
					r.Log.Error(err, "failed to fetch affected pod", "pod", ref.Namespace+"/"+ref.Name)
				}
				// IsNotFound is expected — Pod may have been deleted.
				continue
			}
			pods = append(pods, pod)
		}
		return investigation.WorkloadSnapshot{Pods: pods}, nil
	}

	wl := report.Spec.Workload
	snap := investigation.WorkloadSnapshot{Ref: wl}

	switch wl.Kind {
	case "Deployment":
		var d appsv1.Deployment
		if err := r.Get(ctx, types.NamespacedName{Namespace: wl.Namespace, Name: wl.Name}, &d); err != nil {
			return snap, fmt.Errorf("getting Deployment %s/%s: %w", wl.Namespace, wl.Name, err)
		}
		snap.Deployment = &d

	case "StatefulSet":
		var s appsv1.StatefulSet
		if err := r.Get(ctx, types.NamespacedName{Namespace: wl.Namespace, Name: wl.Name}, &s); err != nil {
			return snap, fmt.Errorf("getting StatefulSet %s/%s: %w", wl.Namespace, wl.Name, err)
		}
		snap.StatefulSet = &s

	case "DaemonSet":
		var d appsv1.DaemonSet
		if err := r.Get(ctx, types.NamespacedName{Namespace: wl.Namespace, Name: wl.Name}, &d); err != nil {
			return snap, fmt.Errorf("getting DaemonSet %s/%s: %w", wl.Namespace, wl.Name, err)
		}
		snap.DaemonSet = &d

	case "Job":
		var j batchv1.Job
		if err := r.Get(ctx, types.NamespacedName{Namespace: wl.Namespace, Name: wl.Name}, &j); err != nil {
			return snap, fmt.Errorf("getting Job %s/%s: %w", wl.Namespace, wl.Name, err)
		}
		snap.Job = &j

	case "CronJob":
		// Find the most recent Job owned by this CronJob.
		var jobList batchv1.JobList
		if err := r.List(ctx, &jobList, client.InNamespace(wl.Namespace)); err != nil {
			return snap, fmt.Errorf("listing jobs for CronJob %s/%s: %w", wl.Namespace, wl.Name, err)
		}
		var latest *batchv1.Job
		for i := range jobList.Items {
			j := &jobList.Items[i]
			for _, owner := range j.OwnerReferences {
				if owner.Kind == "CronJob" && owner.Name == wl.Name {
					if latest == nil || j.CreationTimestamp.After(latest.CreationTimestamp.Time) {
						latest = j
					}
				}
			}
		}
		snap.LatestJob = latest
	}

	return snap, nil
}

// clearStabilityTimer patches the IncidentReport status to clear stabilityStartedAt.
func (r *PodReconciler) clearStabilityTimer(ctx context.Context, report *v1alpha1.IncidentReport) error {
	base := report.DeepCopy()
	report.Status.StabilityStartedAt = nil
	return r.Status().Patch(ctx, report, client.MergeFrom(base))
}

// setStabilityTimer patches the IncidentReport status to set stabilityStartedAt = now.
func (r *PodReconciler) setStabilityTimer(ctx context.Context, report *v1alpha1.IncidentReport, now metav1.Time) error {
	base := report.DeepCopy()
	nowCopy := now.DeepCopy()
	report.Status.StabilityStartedAt = nowCopy
	return r.Status().Patch(ctx, report, client.MergeFrom(base))
}

// activeName returns the deterministic active IncidentReport name for a workload or Pod.
func (r *PodReconciler) activeName(workload *v1alpha1.WorkloadRef, podName string) string {
	if workload == nil {
		return investigation.GeneratePodActiveName(podName)
	}
	return investigation.GenerateActiveName(workload.Name, workload.Kind)
}

// podUIDPresent returns true if a PodRef with the given UID already exists in the list.
func podUIDPresent(pods []v1alpha1.PodRef, uid types.UID) bool {
	for _, p := range pods {
		if p.UID == uid {
			return true
		}
	}
	return false
}

// workloadString returns a human-readable workload identity string suitable for log fields.
func workloadString(w *v1alpha1.WorkloadRef) string {
	if w == nil {
		return "<pod-level>"
	}
	return fmt.Sprintf("%s/%s/%s", w.Kind, w.Namespace, w.Name)
}

// applyDiagnosis patches the IncidentReport status with the diagnosis result and
// transitions the phase to Diagnosed or Unknown. Never changes a Resolved incident.
func (r *PodReconciler) applyDiagnosis(
	ctx context.Context,
	report *v1alpha1.IncidentReport,
	result v1alpha1.DiagnosisResult,
) error {
	if report.Status.Phase == v1alpha1.PhaseResolved {
		return nil
	}
	base := report.DeepCopy()
	report.Status.Diagnosis = &result
	if result.Primary != nil {
		report.Status.Phase = v1alpha1.PhaseDiagnosed
	} else {
		report.Status.Phase = v1alpha1.PhaseUnknown
	}
	return r.Status().Patch(ctx, report, client.MergeFrom(base))
}

// recordMetric calls f only when r.Metrics is non-nil, allowing tests to use NoOpRecorder.
func (r *PodReconciler) recordMetric(f func(metrics.RecorderInterface)) {
	if r.Metrics != nil {
		f(r.Metrics)
	}
}
