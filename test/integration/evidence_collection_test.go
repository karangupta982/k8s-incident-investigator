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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

var _ = Describe("Evidence Collection", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t-evid")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("populates Evidence on an IncidentReport after OOMKilled incident", func() {
		// Create an OOMKilled Pod — this triggers incident creation and evidence collection
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oom-ev-pod",
				Namespace: namespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app", Image: "nginx:alpine"},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						RestartCount: 2,
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
						},
						LastTerminationState: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:   "OOMKilled",
								ExitCode: 137,
							},
						},
					},
				},
			},
		}
		createPodWithStatus(ctx, pod)

		// Wait for an IncidentReport with Investigating phase
		ir := waitForActiveIncidentReport(ctx, "oom-ev-pod", namespace, 15*time.Second)
		Expect(ir).NotTo(BeNil())

		// Wait for an active phase (diagnosis may transition to Diagnosed/Unknown after evidence is collected)
		Eventually(func() bool {
			fresh := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, fresh); err != nil {
				return false
			}
			p := fresh.Status.Phase
			return p == v1alpha1.PhaseInvestigating || p == v1alpha1.PhaseDiagnosed || p == v1alpha1.PhaseUnknown
		}, 10*time.Second, 200*time.Millisecond).Should(BeTrue(), "phase should be an active phase")

		// Wait for Evidence to be populated (evidence collection happens after status update)
		Eventually(func() bool {
			fresh := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, fresh); err != nil {
				return false
			}
			return fresh.Status.Evidence != nil && fresh.Status.Evidence.CollectedAt != nil
		}, 15*time.Second, 500*time.Millisecond).Should(BeTrue(),
			"Evidence should be populated after reconciliation")

		// Fetch the latest report with evidence
		fresh := &v1alpha1.IncidentReport{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, fresh)).To(Succeed())

		ev := fresh.Status.Evidence
		Expect(ev).NotTo(BeNil(), "Evidence should be non-nil")
		Expect(ev.CollectedAt).NotTo(BeNil(), "CollectedAt should be set")

		// Pod evidence should be populated (pod exists in envtest)
		Expect(ev.Pod).NotTo(BeNil(), "Pod evidence should be collected")
		Expect(ev.Pod.Name).To(Equal("oom-ev-pod"))
		Expect(len(ev.Pod.Containers)).To(BeNumerically(">=", 1),
			"At least one container should be in evidence")

		// There should be no "pod" layer CollectionError
		for _, cerr := range ev.CollectionErrors {
			Expect(cerr.Source).NotTo(Equal("pod"),
				fmt.Sprintf("Pod evidence should not have failed, got error: %s", cerr.Reason))
		}
	})
})
