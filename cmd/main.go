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

// Package main is the entrypoint for the Kubernetes Incident Investigator controller manager.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/controller"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/correlation"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/diagnosis"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		stabilityPeriod      time.Duration
		correlationWindow    time.Duration
		readinessThreshold   int
		livenessThreshold    int
		mountThreshold       int
		schedulingThreshold  int
		requeueInterval      time.Duration
		watchNamespaces      string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for the controller manager.")
	flag.DurationVar(&stabilityPeriod, "stability-period", 5*time.Minute, "Duration a workload must remain healthy before resolving an incident.")
	flag.DurationVar(&correlationWindow, "correlation-window", 10*time.Minute, "Correlation window (reserved for future use).")
	flag.IntVar(&readinessThreshold, "readiness-threshold", 3, "Event.count threshold for readiness probe failures.")
	flag.IntVar(&livenessThreshold, "liveness-threshold", 3, "Event.count threshold for liveness probe failures.")
	flag.IntVar(&mountThreshold, "mount-threshold", 3, "Event.count threshold for mount failures.")
	flag.IntVar(&schedulingThreshold, "scheduling-threshold", 5, "Event.count threshold for scheduling failures.")
	flag.DurationVar(&requeueInterval, "requeue-interval", 30*time.Second, "Interval for re-evaluating active incidents.")
	flag.StringVar(&watchNamespaces, "watch-namespaces", "", "Comma-separated list of namespaces to watch. Empty = all namespaces.")
	var maxLogBytes, maxLogLines, maxEventsPerIncident int
	var evidenceTimeout time.Duration
	flag.IntVar(&maxLogBytes, "max-log-bytes", 32768, "Maximum bytes per container log excerpt.")
	flag.IntVar(&maxLogLines, "max-log-lines", 200, "Maximum lines per container log excerpt.")
	flag.IntVar(&maxEventsPerIncident, "max-events", 25, "Maximum Kubernetes Events stored in evidence per incident.")
	flag.DurationVar(&evidenceTimeout, "evidence-timeout", 30*time.Second, "Timeout for evidence collection per reconcile cycle.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Build configuration from flags.
	cfg := &config.Config{
		StabilityPeriod:                stabilityPeriod,
		CorrelationWindow:              correlationWindow,
		ReadinessProbeFailureThreshold: readinessThreshold,
		LivenessProbeFailureThreshold:  livenessThreshold,
		MountFailureThreshold:          mountThreshold,
		SchedulingFailureThreshold:     schedulingThreshold,
		RequeueInterval:                requeueInterval,
		MaxLogBytes:                    maxLogBytes,
		MaxLogLines:                    maxLogLines,
		MaxEventsPerIncident:           maxEventsPerIncident,
		EvidenceCollectionTimeout:      evidenceTimeout,
	}
	if watchNamespaces != "" {
		for _, ns := range strings.Split(watchNamespaces, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				cfg.WatchNamespaces = append(cfg.WatchNamespaces, ns)
			}
		}
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Build cache options — restrict to the configured namespaces when set.
	cacheOpts := cache.Options{}
	if len(cfg.WatchNamespaces) > 0 {
		cacheOpts.DefaultNamespaces = make(map[string]cache.Config, len(cfg.WatchNamespaces))
		for _, ns := range cfg.WatchNamespaces {
			cacheOpts.DefaultNamespaces[ns] = cache.Config{}
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "incident-investigator.k8s.io",
		Cache:                  cacheOpts,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	log := ctrl.Log.WithName("controller")

	kubeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create kubernetes client for evidence collection")
		os.Exit(1)
	}

	evOrchestrator := &evidence.EvidenceOrchestrator{
		Client:     mgr.GetClient(),
		KubeClient: kubeClient,
		Config:     cfg,
		Log:        log,
	}

	evCorrelator := &correlation.EvidenceCorrelator{
		EventCorrelationWindow: 5 * time.Minute,
	}

	diagEngine := diagnosis.NewDiagnosisEngine()

	reconciler := &controller.PodReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Config:               cfg,
		TriggerEvaluator:     investigation.NewTriggerEvaluator(),
		OwnershipResolver:    investigation.NewOwnershipResolver(mgr.GetClient(), log),
		Correlator:           investigation.NewIncidentCorrelator(mgr.GetClient(), log),
		RecoveryEvaluator:    investigation.NewRecoveryEvaluator(),
		Transitioner:         investigation.NewResolutionTransitioner(mgr.GetClient(), log),
		EvidenceOrchestrator: evOrchestrator,
		EvidCorrelator:       evCorrelator,
		DiagnosisEngine:      diagEngine,
		Log:                  log,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up readiness check")
		os.Exit(1)
	}

	// Startup recovery runs in a goroutine after the cache has been synced.
	// It handles:
	//   a) Active incidents whose Pods may have been replaced while the controller was down.
	//   b) Stale resolved active-named IncidentReports from a mid-transition crash.
	ctx := ctrl.SetupSignalHandler()
	go func() {
		if !mgr.GetCache().WaitForCacheSync(ctx) {
			setupLog.Error(fmt.Errorf("cache sync failed or context cancelled"), "startup recovery skipped")
			return
		}
		performStartupRecovery(ctx, mgr.GetClient(), log)
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// performStartupRecovery discovers IncidentReports after controller restart and handles:
//  1. Active (non-Resolved) incidents — logged so operators are aware; Pod and Event
//     watches will trigger reconciliation naturally once the cache is warm.
//  2. Active-named reports with phase=Resolved — crash mid-transition; complete the
//     copy-then-delete by checking for (and optionally creating) the historical record,
//     then deleting the stale active slot.
func performStartupRecovery(ctx context.Context, c client.Client, log logr.Logger) {
	var reportList v1alpha1.IncidentReportList
	if err := c.List(ctx, &reportList); err != nil {
		log.Error(err, "startup recovery: failed to list IncidentReports, continuing without recovery")
		return
	}

	for i := range reportList.Items {
		report := &reportList.Items[i]

		// Only process active-slot objects.
		if !strings.HasSuffix(report.Name, "-active") {
			continue
		}

		if report.Status.Phase != v1alpha1.PhaseResolved {
			// Genuine active incident.  Pod/Event watches will enqueue reconcile
			// requests naturally once the cache is warm.
			log.Info("startup recovery: active incident found, will be managed by watch",
				"name", report.Name, "namespace", report.Namespace,
				"phase", report.Status.Phase)

			hasSurvivingPod := false
			for _, ref := range report.Status.AffectedPods {
				var pod corev1.Pod
				if err := c.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, &pod); err == nil {
					hasSurvivingPod = true
					break
				}
			}
			if !hasSurvivingPod {
				log.Info("startup recovery: active incident has no surviving pods; "+
					"it will be picked up on the next Pod or Event watch event",
					"name", report.Name, "namespace", report.Namespace)
			}
			continue
		}

		// Phase == Resolved but the active slot was not deleted before the crash
		// (step 1 of the resolution transition completed; step 3 did not).
		log.Info("startup recovery: found stale resolved active-named report, completing transition",
			"name", report.Name, "namespace", report.Namespace)

		if report.Spec.Workload != nil && report.Status.StartedAt != nil {
			historicalName := investigation.GenerateHistoricalName(
				report.Spec.Workload.Name,
				report.Spec.Workload.Kind,
				report.Namespace,
				report.Status.StartedAt.Time,
			)

			var existing v1alpha1.IncidentReport
			getErr := c.Get(ctx, types.NamespacedName{Namespace: report.Namespace, Name: historicalName}, &existing)
			if getErr != nil {
				if apierrors.IsNotFound(getErr) {
					// Historical record was not created before the crash — create it now.
					historical := buildHistoricalFromActive(report, historicalName)
					if createErr := c.Create(ctx, historical); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
						log.Error(createErr, "startup recovery: failed to create historical record",
							"historicalName", historicalName)
						// Don't delete the active slot if the historical record creation failed.
						continue
					}
					log.Info("startup recovery: created missing historical record",
						"historicalName", historicalName, "namespace", report.Namespace)
				} else {
					log.Error(getErr, "startup recovery: error checking for historical record",
						"historicalName", historicalName)
					continue
				}
			} else {
				log.Info("startup recovery: historical record already exists",
					"historicalName", historicalName, "namespace", report.Namespace)
			}
		}

		// Delete the stale active slot to free it for future incidents.
		if err := c.Delete(ctx, report); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "startup recovery: failed to delete stale active report", "name", report.Name)
		} else {
			log.Info("startup recovery: deleted stale active slot", "name", report.Name)
		}
	}
}

// buildHistoricalFromActive constructs a fresh IncidentReport with the historical name,
// copying spec, status, and labels/annotations from the active report while explicitly
// excluding all server-managed metadata fields (UID, ResourceVersion, CreationTimestamp,
// ManagedFields, DeletionTimestamp, Finalizers).
func buildHistoricalFromActive(active *v1alpha1.IncidentReport, historicalName string) *v1alpha1.IncidentReport {
	labels := make(map[string]string, len(active.Labels))
	for k, v := range active.Labels {
		if k != investigation.LabelActive {
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
		},
		Spec:   *specCopy,
		Status: *statusCopy,
	}
}
