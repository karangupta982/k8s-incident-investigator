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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// makeOOMKilledPod creates a Pod object with an OOMKilled container status.
func makeOOMKilledPod(name, namespace string, ownerRefs ...metav1.OwnerReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			OwnerReferences: ownerRefs,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "busybox"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason: "CrashLoopBackOff",
						},
					},
				},
			},
		},
	}
}

// makeRunningPod creates a Pod in a healthy Running state with no failure signals.
func makeRunningPod(name, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "busybox"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "app",
					Ready: true,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
		},
	}
}

// makeHealthyDeployment creates a Deployment whose status looks fully healthy.
func makeHealthyDeployment(name, namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:           replicas,
			ReadyReplicas:      replicas,
			AvailableReplicas:  replicas,
			UpdatedReplicas:    replicas,
			ObservedGeneration: 1,
		},
	}
}

// controllerTrue returns a pointer to true, used for OwnerReference.Controller.
func controllerTrue() *bool {
	b := true
	return &b
}

// replicaCount returns a pointer to an int32 for Deployment Spec.Replicas.
func replicaCount(n int32) *int32 {
	return &n
}

// createNamespace creates a unique test namespace and returns its name.
func createNamespace(ctx context.Context, prefix string) string {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: prefix + "-",
			Labels:       map[string]string{"test-namespace": "true"},
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	return ns.Name
}

// deleteNamespace removes the test namespace.
func deleteNamespace(ctx context.Context, name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_ = k8sClient.Delete(ctx, ns)
}

// createPodWithStatus creates the Pod spec and then updates its status via the status subresource.
// This is required because envtest strips status on Create.
// Returns the created pod after status is verified on the API server.
func createPodWithStatus(ctx context.Context, pod *corev1.Pod) {
	// Save the intended status BEFORE creation (Create replaces the object with server response,
	// which has an empty status — we need to restore it before calling Status().Update()).
	intendedStatus := pod.Status.DeepCopy()

	// Create the base object (status is stripped on Create by the API server).
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

	// Restore the saved status on the pod object and update via the status subresource.
	pod.Status = *intendedStatus
	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

	// Verify the status is visible in the API server before returning.
	Eventually(func() int {
		fresh := &corev1.Pod{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, fresh); err != nil {
			return 0
		}
		return len(fresh.Status.ContainerStatuses)
	}, 5*time.Second, 100*time.Millisecond).Should(Equal(len(intendedStatus.ContainerStatuses)),
		"pod status should be visible in API server")
}

// touchPodAnnotation patches the pod's annotation to trigger a fresh reconcile.
// The controller-runtime informer cache may take some time to reflect the status update;
// periodically touching the pod ensures the controller re-reconciles with the latest state.
func touchPodAnnotation(ctx context.Context, podName, namespace, value string) {
	fresh := &corev1.Pod{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: namespace}, fresh); err != nil {
		return
	}
	base := fresh.DeepCopy()
	if fresh.Annotations == nil {
		fresh.Annotations = map[string]string{}
	}
	fresh.Annotations["investigator.k8s.io/reconcile-touch"] = value
	_ = k8sClient.Patch(ctx, fresh, client.MergeFrom(base))
}

// waitForActiveIncidentReport polls until an active IncidentReport (name ends with "-active")
// appears in the namespace. Periodically touches the pod to drive reconciliation in case the
// controller-runtime cache hasn't propagated the status update yet.
func waitForActiveIncidentReport(ctx context.Context, podName, namespace string, timeout time.Duration) *v1alpha1.IncidentReport {
	var found *v1alpha1.IncidentReport
	touchCount := 0
	Eventually(func() bool {
		list := &v1alpha1.IncidentReportList{}
		if err := k8sClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
			return false
		}
		for i := range list.Items {
			if strings.HasSuffix(list.Items[i].Name, "-active") {
				found = &list.Items[i]
				return true
			}
		}
		// Re-touch the pod every ~2 poll iterations to drive reconciliation.
		// This handles the envtest timing case where the status update watch event
		// arrives in the cache after the initial Create reconcile fires.
		touchCount++
		if touchCount%2 == 0 && podName != "" {
			touchPodAnnotation(ctx, podName, namespace, fmt.Sprintf("%d", touchCount))
		}
		return false
	}, timeout, 500*time.Millisecond).Should(BeTrue(), "expected an active IncidentReport in namespace %s", namespace)
	return found
}

// waitForIncidentReportByNameWithTouch polls for an IncidentReport while periodically
// touching a pod to drive reconciliation.
func waitForIncidentReportByNameWithTouch(ctx context.Context, name, namespace, podName string, timeout time.Duration) {
	touchCount := 0
	Eventually(func() bool {
		report := &v1alpha1.IncidentReport{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, report); err == nil {
			return true
		}
		touchCount++
		if touchCount%2 == 0 && podName != "" {
			touchPodAnnotation(ctx, podName, namespace, fmt.Sprintf("%d", touchCount))
		}
		return false
	}, timeout, 500*time.Millisecond).Should(BeTrue(), "expected IncidentReport %s/%s to exist", namespace, name)
}

// waitForIncidentPhase polls until the IncidentReport reaches the expected phase.
// Periodically touches the pod to drive reconciliation.
func waitForIncidentPhase(ctx context.Context, name, namespace, podName string, phase v1alpha1.IncidentPhase, timeout time.Duration) {
	touchCount := 0
	Eventually(func() v1alpha1.IncidentPhase {
		report := &v1alpha1.IncidentReport{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, report); err != nil {
			return ""
		}
		if report.Status.Phase == phase {
			return phase
		}
		touchCount++
		if touchCount%2 == 0 && podName != "" {
			touchPodAnnotation(ctx, podName, namespace, fmt.Sprintf("phase-%d", touchCount))
		}
		return report.Status.Phase
	}, timeout, 500*time.Millisecond).Should(Equal(phase), "IncidentReport %s/%s did not reach phase %s", namespace, name, phase)
}

// countActiveIncidentReports counts IncidentReports whose name ends with "-active".
func countActiveIncidentReports(ctx context.Context, namespace string) int {
	list := &v1alpha1.IncidentReportList{}
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	count := 0
	for _, ir := range list.Items {
		if strings.HasSuffix(ir.Name, "-active") {
			count++
		}
	}
	return count
}

// countAllIncidentReports returns total IncidentReports in the namespace.
func countAllIncidentReports(ctx context.Context, namespace string) int {
	list := &v1alpha1.IncidentReportList{}
	Expect(k8sClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
	return len(list.Items)
}

// ─── Task 12.2: OOMKilled Pod creates an IncidentReport ──────────────────────

var _ = Describe("OOMKilled Pod creates an IncidentReport", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t122")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("creates an IncidentReport with Investigating phase for an OOMKilled standalone Pod", func() {
		pod := makeOOMKilledPod("oom-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Wait for the controller to create an active IncidentReport.
		// Periodically re-touch the pod to drive reconciliation in case the
		// informer cache needs an extra event to see the updated status.
		ir := waitForActiveIncidentReport(ctx, "oom-pod", namespace, 20*time.Second)
		Expect(ir).NotTo(BeNil())

		// Wait for the controller to set the phase via the status subresource.
		// The status update is asynchronous after object creation.
		Eventually(func() v1alpha1.IncidentPhase {
			fresh := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, fresh); err != nil {
				return ""
			}
			return fresh.Status.Phase
		}, 10*time.Second, 200*time.Millisecond).Should(Equal(v1alpha1.PhaseInvestigating),
			"IncidentReport phase should be Investigating")

		// Re-fetch with current status for subsequent assertions.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, ir)).To(Succeed())

		// Phase must be Investigating.
		Expect(ir.Status.Phase).To(Equal(v1alpha1.PhaseInvestigating))

		// AffectedPods must contain the pod.
		Expect(ir.Status.AffectedPods).NotTo(BeEmpty())
		Expect(ir.Status.AffectedPods[0].Name).To(Equal("oom-pod"))

		// Trigger type must be OOMKilled.
		Expect(ir.Status.Trigger).NotTo(BeNil())
		Expect(ir.Status.Trigger.Type).To(Equal(v1alpha1.TriggerOOMKilled))

		// Trigger a second reconcile by annotating the Pod — assert no duplicate.
		touchPodAnnotation(ctx, "oom-pod", namespace, "second")
		time.Sleep(1 * time.Second)

		// Still exactly one active IncidentReport.
		Expect(countActiveIncidentReports(ctx, namespace)).To(Equal(1),
			"expected no duplicate IncidentReport after second reconcile")
	})
})

// ─── Task 12.3: Pod ownership resolution through RS and Deployment ────────────

var _ = Describe("Pod ownership through RS to Deployment", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t123")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("associates the IncidentReport with the Deployment, not the RS or Pod", func() {
		// Create the Deployment first.
		deploy := makeHealthyDeployment("my-deploy", namespace, 1)
		Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())

		// Create a ReplicaSet owned by the Deployment.
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deploy-rs",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploy.Name,
						UID:        deploy.UID,
						Controller: controllerTrue(),
					},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: replicaCount(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "my-deploy"},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy-rs", Namespace: namespace}, rs)).To(Succeed())

		// Create an OOMKilled Pod owned by the ReplicaSet.
		pod := makeOOMKilledPod("my-pod", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, pod)

		// Wait for the active IncidentReport with the Deployment-derived name.
		expectedName := investigation.GenerateActiveName("my-deploy", "Deployment")
		waitForIncidentReportByNameWithTouch(ctx, expectedName, namespace, "my-pod", 20*time.Second)

		ir := &v1alpha1.IncidentReport{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: expectedName, Namespace: namespace}, ir)).To(Succeed())

		// Must reference the Deployment, not the RS or Pod.
		Expect(ir.Spec.Workload).NotTo(BeNil())
		Expect(ir.Spec.Workload.Kind).To(Equal("Deployment"))
		Expect(ir.Spec.Workload.Name).To(Equal("my-deploy"))
	})
})

// ─── Task 12.4: Recovery and stability period resolution ─────────────────────

var _ = Describe("Recovery and stability period", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t124")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("transitions IncidentReport to Resolved after workload becomes healthy and stability period elapses", func() {
		const replicas int32 = 1

		// Create the Deployment — start with an unhealthy status.
		deploy := makeHealthyDeployment("my-deploy", namespace, replicas)
		Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
		unhealthyStatus := appsv1.DeploymentStatus{
			Replicas:          replicas,
			ReadyReplicas:     0,
			AvailableReplicas: 0,
		}
		deploy.Status = unhealthyStatus
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())

		// Create a ReplicaSet owned by the Deployment.
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deploy-rs",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploy.Name,
						UID:        deploy.UID,
						Controller: controllerTrue(),
					},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: replicaCount(replicas),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-deploy"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy-rs", Namespace: namespace}, rs)).To(Succeed())

		// Create OOMKilled Pod.
		pod := makeOOMKilledPod("my-pod", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, pod)

		// Wait for investigating phase.
		activeName := investigation.GenerateActiveName("my-deploy", "Deployment")
		waitForIncidentPhase(ctx, activeName, namespace, "my-pod", v1alpha1.PhaseInvestigating, 20*time.Second)

		// Now make the Deployment healthy.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())
		deploy.Status = appsv1.DeploymentStatus{
			Replicas:           replicas,
			ReadyReplicas:      replicas,
			AvailableReplicas:  replicas,
			UpdatedReplicas:    replicas,
			ObservedGeneration: 1,
		}
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())

		// The controller should start the stability timer (2s), then resolve.
		// Historical IncidentReport appears and the active slot is deleted.
		// Allow 30s total for the full stability period + requeue cycles.
		Eventually(func() bool {
			list := &v1alpha1.IncidentReportList{}
			if err := k8sClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
				return false
			}
			for _, ir := range list.Items {
				if !strings.HasSuffix(ir.Name, "-active") && ir.Status.Phase == v1alpha1.PhaseResolved {
					return true
				}
			}
			// Keep touching the pod so the controller re-evaluates.
			touchPodAnnotation(ctx, "my-pod", namespace, fmt.Sprintf("recovery-%d", time.Now().UnixNano()))
			return false
		}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "expected a resolved historical IncidentReport")

		// The active slot must be gone.
		// Use Eventually because the DELETE propagates through the informer cache asynchronously.
		Eventually(func() bool {
			activeIR := &v1alpha1.IncidentReport{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: activeName, Namespace: namespace}, activeIR)
			return errors.IsNotFound(err)
		}, 10*time.Second, 200*time.Millisecond).Should(BeTrue(), "active-named IncidentReport should be deleted after resolution")
	})
})

// ─── Task 12.5: Controller restart recovery ───────────────────────────────────

var _ = Describe("Controller restart recovery", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t125")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("continues managing a pre-existing active incident without creating a duplicate", func() {
		// Simulate a pre-existing active IncidentReport.
		activeName := investigation.GenerateActiveName("existing-deploy", "Deployment")
		now := metav1.Now()
		preExisting := &v1alpha1.IncidentReport{
			ObjectMeta: metav1.ObjectMeta{
				Name:      activeName,
				Namespace: namespace,
				Labels: map[string]string{
					investigation.LabelActive:            "true",
					investigation.LabelWorkloadName:      "existing-deploy",
					investigation.LabelWorkloadKind:      "Deployment",
					investigation.LabelWorkloadNamespace: namespace,
				},
			},
			Spec: v1alpha1.IncidentReportSpec{
				Workload: &v1alpha1.WorkloadRef{
					Kind:      "Deployment",
					Name:      "existing-deploy",
					Namespace: namespace,
				},
			},
		}
		Expect(k8sClient.Create(ctx, preExisting)).To(Succeed())
		preExisting.Status = v1alpha1.IncidentReportStatus{
			Phase:     v1alpha1.PhaseInvestigating,
			StartedAt: &now,
		}
		Expect(k8sClient.Status().Update(ctx, preExisting)).To(Succeed())

		// Create the Deployment and RS so ownership resolution succeeds.
		deploy := makeHealthyDeployment("existing-deploy", namespace, 1)
		Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "existing-deploy", Namespace: namespace}, deploy)).To(Succeed())

		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-deploy-rs",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploy.Name,
						UID:        deploy.UID,
						Controller: controllerTrue(),
					},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: replicaCount(1),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "existing-deploy"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "existing-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "existing-deploy-rs", Namespace: namespace}, rs)).To(Succeed())

		// Trigger the controller by creating a new OOMKilled Pod for the same workload.
		pod := makeOOMKilledPod("new-oom-pod", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, pod)

		// Wait for the existing IncidentReport to be updated (new Pod added to AffectedPods).
		Eventually(func() bool {
			ir := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: activeName, Namespace: namespace}, ir); err != nil {
				return false
			}
			for _, ref := range ir.Status.AffectedPods {
				if ref.Name == "new-oom-pod" {
					return true
				}
			}
			touchPodAnnotation(ctx, "new-oom-pod", namespace, fmt.Sprintf("recovery-%d", time.Now().UnixNano()))
			return false
		}, 20*time.Second, 500*time.Millisecond).Should(BeTrue(), "controller should update pre-existing IncidentReport")

		// Still only one active IncidentReport — no duplicate.
		Expect(countActiveIncidentReports(ctx, namespace)).To(Equal(1), "expected no duplicate IncidentReport")
	})
})

// ─── Task 12.6: Missing Pod during reconciliation ─────────────────────────────

var _ = Describe("Missing Pod during reconciliation", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t126")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("preserves the IncidentReport when the affected Pod is deleted", func() {
		pod := makeOOMKilledPod("oom-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Wait for the IncidentReport.
		ir := waitForActiveIncidentReport(ctx, "oom-pod", namespace, 20*time.Second)
		irName := ir.Name

		// Delete the Pod.
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

		// Wait 2 seconds to let the controller process the delete event.
		time.Sleep(2 * time.Second)

		// IncidentReport must still exist and be Investigating.
		refreshed := &v1alpha1.IncidentReport{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: irName, Namespace: namespace}, refreshed)).
			To(Succeed(), "IncidentReport should still exist after Pod deletion")
		Expect(refreshed.Status.Phase).To(Equal(v1alpha1.PhaseInvestigating))

		// AffectedPods must still contain the deleted pod's reference.
		found := false
		for _, ref := range refreshed.Status.AffectedPods {
			if ref.Name == "oom-pod" {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "deleted Pod reference should be preserved in AffectedPods")
	})
})

// ─── Task 12.7: Duplicate CREATE race ────────────────────────────────────────

var _ = Describe("Concurrent reconciliation", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t127")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("creates exactly one IncidentReport even after multiple reconcile triggers", func() {
		pod := makeOOMKilledPod("oom-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Wait for at least one IncidentReport to appear.
		waitForActiveIncidentReport(ctx, "oom-pod", namespace, 20*time.Second)

		// Wait extra to allow any potential duplicate to appear.
		time.Sleep(1 * time.Second)

		// Assert exactly one active IncidentReport.
		Expect(countActiveIncidentReports(ctx, namespace)).To(Equal(1),
			"deterministic name should prevent duplicate IncidentReports")
	})
})

// ─── Task 12.9: Temporal correlation and lifecycle transitions ────────────────

var _ = Describe("Lifecycle transitions", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createNamespace(ctx, "t129")
	})

	AfterEach(func() {
		deleteNamespace(ctx, namespace)
	})

	It("uses the same IncidentReport for failures from the same workload while active", func() {
		// Create Deployment and RS with unhealthy status.
		deploy := makeHealthyDeployment("my-deploy", namespace, 2)
		Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())

		deploy.Status = appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 0, AvailableReplicas: 0}
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())

		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deploy-rs",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploy.Name,
						UID:        deploy.UID,
						Controller: controllerTrue(),
					},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: replicaCount(2),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-deploy"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy-rs", Namespace: namespace}, rs)).To(Succeed())

		// Pod A — first failure.
		podA := makeOOMKilledPod("pod-a", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, podA)

		activeName := investigation.GenerateActiveName("my-deploy", "Deployment")
		waitForIncidentReportByNameWithTouch(ctx, activeName, namespace, "pod-a", 20*time.Second)

		// Pod B — second failure, same workload.
		podB := makeOOMKilledPod("pod-b", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, podB)

		// Wait for both Pods to appear in AffectedPods.
		Eventually(func() int {
			ir := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: activeName, Namespace: namespace}, ir); err != nil {
				return 0
			}
			return len(ir.Status.AffectedPods)
		}, 20*time.Second, 500*time.Millisecond).Should(BeNumerically(">=", 2),
			"both Pods should be associated with the same IncidentReport")

		// Only one active IncidentReport.
		Expect(countActiveIncidentReports(ctx, namespace)).To(Equal(1))
	})

	It("creates a new IncidentReport after the previous incident resolves", func() {
		const replicas int32 = 1

		deploy := makeHealthyDeployment("my-deploy", namespace, replicas)
		Expect(k8sClient.Create(ctx, deploy)).To(Succeed())
		deploy.Status = appsv1.DeploymentStatus{
			Replicas: replicas, ReadyReplicas: 0, AvailableReplicas: 0,
		}
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())

		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-deploy-rs",
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploy.Name,
						UID:        deploy.UID,
						Controller: controllerTrue(),
					},
				},
			},
			Spec: appsv1.ReplicaSetSpec{
				Replicas: replicaCount(replicas),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "my-deploy"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "my-deploy"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "app", Image: "busybox"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, rs)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy-rs", Namespace: namespace}, rs)).To(Succeed())

		pod := makeOOMKilledPod("pod-first", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		createPodWithStatus(ctx, pod)

		activeName := investigation.GenerateActiveName("my-deploy", "Deployment")
		waitForIncidentPhase(ctx, activeName, namespace, "pod-first", v1alpha1.PhaseInvestigating, 20*time.Second)

		// Make deployment healthy so the incident resolves.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())
		deploy.Status = appsv1.DeploymentStatus{
			Replicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas, UpdatedReplicas: replicas,
		}
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())

		// Wait for the historical IncidentReport (active slot deleted).
		Eventually(func() bool {
			list := &v1alpha1.IncidentReportList{}
			if err := k8sClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
				return false
			}
			for _, ir := range list.Items {
				if !strings.HasSuffix(ir.Name, "-active") && ir.Status.Phase == v1alpha1.PhaseResolved {
					return true
				}
			}
			touchPodAnnotation(ctx, "pod-first", namespace, fmt.Sprintf("resolve-%d", time.Now().UnixNano()))
			return false
		}, 30*time.Second, 500*time.Millisecond).Should(BeTrue(), "expected resolved historical IncidentReport")

		// Verify the active slot is gone.
		// Use Eventually because the DELETE propagates through the informer cache asynchronously.
		Eventually(func() bool {
			activeIR := &v1alpha1.IncidentReport{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: activeName, Namespace: namespace}, activeIR)
			return errors.IsNotFound(err)
		}, 10*time.Second, 200*time.Millisecond).Should(BeTrue(), "active-named IncidentReport should be deleted after resolution")

		countBefore := countAllIncidentReports(ctx, namespace)

		// Create a second OOMKilled Pod — should trigger a NEW incident.
		pod2 := makeOOMKilledPod("pod-second", namespace,
			metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       rs.Name,
				UID:        rs.UID,
				Controller: controllerTrue(),
			},
		)
		// Reset deployment to unhealthy so the new incident stays open.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "my-deploy", Namespace: namespace}, deploy)).To(Succeed())
		deploy.Status = appsv1.DeploymentStatus{Replicas: replicas, ReadyReplicas: 0, AvailableReplicas: 0}
		Expect(k8sClient.Status().Update(ctx, deploy)).To(Succeed())

		createPodWithStatus(ctx, pod2)
		waitForIncidentReportByNameWithTouch(ctx, activeName, namespace, "pod-second", 20*time.Second)

		// Total count must be more (historical + new active).
		Eventually(func() int {
			return countAllIncidentReports(ctx, namespace)
		}, 5*time.Second, 200*time.Millisecond).Should(BeNumerically(">", countBefore),
			"should have historical record plus new active report")

		// The new active report should be Investigating.
		// Use Eventually — the status PATCH is asynchronous after the object is created.
		Eventually(func() v1alpha1.IncidentPhase {
			newIR := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: activeName, Namespace: namespace}, newIR); err != nil {
				return ""
			}
			return newIR.Status.Phase
		}, 10*time.Second, 200*time.Millisecond).Should(Equal(v1alpha1.PhaseInvestigating),
			"new active IncidentReport should have phase Investigating")
	})

	It("does not trigger an incident for a healthy Running Pod with zero event counts", func() {
		pod := makeRunningPod("healthy-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Wait 3 seconds — no IncidentReport should be created.
		time.Sleep(3 * time.Second)
		Expect(countAllIncidentReports(ctx, namespace)).To(Equal(0),
			"healthy Pod should not create an IncidentReport")
	})

	It("does not trigger an incident when event count is below the threshold", func() {
		pod := makeRunningPod("probe-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Create a FailedMount event with count = threshold - 1 = 2.
		evt := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("probe-pod.failedmount.below.%d", time.Now().Unix()),
				Namespace: namespace,
			},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "probe-pod",
				Namespace: namespace,
			},
			Reason:  "FailedMount",
			Message: "Unable to mount volume",
			Count:   int32(testCfg.MountFailureThreshold - 1), // below threshold
			Type:    corev1.EventTypeWarning,
			Source:  corev1.EventSource{Component: "kubelet"},
		}
		Expect(k8sClient.Create(ctx, evt)).To(Succeed())

		// Wait 3 seconds — no IncidentReport should be created.
		time.Sleep(3 * time.Second)
		Expect(countAllIncidentReports(ctx, namespace)).To(Equal(0),
			"event count below threshold should not create an IncidentReport")
	})

	It("triggers an incident when event count reaches the threshold", func() {
		pod := makeRunningPod("mount-pod", namespace)
		createPodWithStatus(ctx, pod)

		// Create a FailedMount event with count = threshold = 3.
		evt := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("mount-pod.failedmount.at.%d", time.Now().Unix()),
				Namespace: namespace,
			},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "mount-pod",
				Namespace: namespace,
			},
			Reason:  "FailedMount",
			Message: "Unable to mount volume: no such file",
			Count:   int32(testCfg.MountFailureThreshold), // exactly at threshold
			Type:    corev1.EventTypeWarning,
			Source:  corev1.EventSource{Component: "kubelet"},
		}
		Expect(k8sClient.Create(ctx, evt)).To(Succeed())

		// Wait for an active IncidentReport to appear.
		// The event watch drives the reconcile; no pod touch needed here.
		ir := waitForActiveIncidentReport(ctx, "mount-pod", namespace, 10*time.Second)
		Expect(ir).NotTo(BeNil())

		// Wait for the status (Trigger) to be set via the status subresource — it is async.
		Eventually(func() v1alpha1.TriggerType {
			fresh := &v1alpha1.IncidentReport{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: ir.Name, Namespace: namespace}, fresh); err != nil {
				return ""
			}
			if fresh.Status.Trigger == nil {
				return ""
			}
			return fresh.Status.Trigger.Type
		}, 10*time.Second, 200*time.Millisecond).Should(Equal(v1alpha1.TriggerMountFailure),
			"IncidentReport trigger type should be MountFailure")
	})

	It("keeps the IncidentReport active after the Pod is deleted during an active incident", func() {
		pod := makeOOMKilledPod("delete-me-pod", namespace)
		createPodWithStatus(ctx, pod)

		ir := waitForActiveIncidentReport(ctx, "delete-me-pod", namespace, 20*time.Second)
		irName := ir.Name

		// Delete the Pod.
		Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
		time.Sleep(2 * time.Second)

		// IncidentReport must still be Investigating.
		refreshed := &v1alpha1.IncidentReport{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: irName, Namespace: namespace}, refreshed)).To(Succeed())
		Expect(refreshed.Status.Phase).To(Equal(v1alpha1.PhaseInvestigating))
	})
})
