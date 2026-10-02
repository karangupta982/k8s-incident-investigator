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

package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/controller"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/correlation"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

var (
	k8sClient  client.Client
	testEnv    *envtest.Environment
	cancelFunc context.CancelFunc
	testCfg    *config.Config
	scheme     = runtime.NewScheme()
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

var _ = BeforeSuite(func() {
	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	// Use a very short stability period for tests so resolution transitions happen quickly.
	testCfg = &config.Config{
		StabilityPeriod:                2 * time.Second,
		CorrelationWindow:              10 * time.Minute,
		ReadinessProbeFailureThreshold: 3,
		LivenessProbeFailureThreshold:  3,
		MountFailureThreshold:          3,
		SchedulingFailureThreshold:     5,
		RequeueInterval:                500 * time.Millisecond,
		MaxLogBytes:                    32768,
		MaxLogLines:                    200,
		MaxEventsPerIncident:           25,
		EvidenceCollectionTimeout:      5 * time.Second,
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		// Disable metrics listener to avoid port conflicts during tests.
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	log := ctrl.Log.WithName("test-controller")
	kubeClient, err := kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	evOrchestrator := &evidence.EvidenceOrchestrator{
		Client:     mgr.GetClient(),
		KubeClient: kubeClient,
		Config:     testCfg,
		Log:        log,
	}

	evCorrelator := &correlation.EvidenceCorrelator{
		EventCorrelationWindow: 5 * time.Minute,
	}

	reconciler := &controller.PodReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		Config:               testCfg,
		TriggerEvaluator:     investigation.NewTriggerEvaluator(),
		OwnershipResolver:    investigation.NewOwnershipResolver(mgr.GetClient(), log),
		Correlator:           investigation.NewIncidentCorrelator(mgr.GetClient(), log),
		RecoveryEvaluator:    investigation.NewRecoveryEvaluator(),
		Transitioner:         investigation.NewResolutionTransitioner(mgr.GetClient(), log),
		EvidenceOrchestrator: evOrchestrator,
		EvidCorrelator:       evCorrelator,
		Log:                  log,
	}
	Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancelFunc = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	if cancelFunc != nil {
		cancelFunc()
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})
