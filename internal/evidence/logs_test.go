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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

// TestLogCollector_MaxLogLines_Zero verifies that MaxLogLines=0 disables log collection
// and prevents any pods/log API call from being made.
func TestLogCollector_MaxLogLines_Zero(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxLogLines = 0 // disable log collection

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
	}

	input := CollectorInput{
		Client:     nil, // no controller-runtime client needed
		KubeClient: nil, // deliberately nil — if logs.go makes an API call this will panic
		Pod:        pod,
		Config:     cfg,
		Report: &v1alpha1.IncidentReport{
			Status: v1alpha1.IncidentReportStatus{
				Trigger: &v1alpha1.TriggerInfo{
					Type:          v1alpha1.TriggerOOMKilled,
					ContainerName: "app",
				},
			},
		},
	}

	// Call Collect() with needs that would normally trigger log collection.
	// The MaxLogLines=0 guard should short-circuit before reaching the KubeClient.
	lc := &LogCollector{}
	result, errs := lc.Collect(context.Background(), input, triggerNeeds{
		currentLogs:  true,
		previousLogs: true,
	})

	if result != nil {
		t.Errorf("expected nil log result when MaxLogLines=0, got %d entries", len(result))
	}
	if len(errs) != 0 {
		t.Errorf("expected no collection errors when MaxLogLines=0, got %v", errs)
	}
}

// TestLogCollector_NilKubeClient verifies that nil KubeClient returns nil.
func TestLogCollector_NilKubeClient(t *testing.T) {
	cfg := config.DefaultConfig()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
	}

	input := CollectorInput{
		KubeClient: nil,
		Pod:        pod,
		Config:     cfg,
		Report:     &v1alpha1.IncidentReport{},
	}

	lc := &LogCollector{}
	result, errs := lc.Collect(context.Background(), input, triggerNeeds{
		currentLogs:  true,
		previousLogs: true,
	})

	if result != nil {
		t.Errorf("expected nil result with nil KubeClient")
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

// TestLogCollector_NoLogNeeds verifies that when neither log flag is set, nothing is collected.
func TestLogCollector_NoLogNeeds(t *testing.T) {
	cfg := config.DefaultConfig()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "nginx"}},
		},
	}

	input := CollectorInput{
		KubeClient: nil,
		Pod:        pod,
		Config:     cfg,
		Report:     &v1alpha1.IncidentReport{},
	}

	lc := &LogCollector{}
	result, errs := lc.Collect(context.Background(), input, triggerNeeds{
		currentLogs:  false,
		previousLogs: false,
	})

	if result != nil {
		t.Errorf("expected nil result when no log collection needed")
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}
