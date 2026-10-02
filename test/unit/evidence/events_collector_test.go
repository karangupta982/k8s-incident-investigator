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

package evidence_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"pgregory.net/rapid"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	internalevidence "github.com/k8s-incident-investigator/k8s-incident-investigator/internal/evidence"
)

func buildScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("add clientgo scheme: %v", err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add v1alpha1 scheme: %v", err)
	}
	return s
}

func makeEventForPod(podName, namespace, reason, message string, count int32, lastTime time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%d", podName, reason, count),
			Namespace: namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind: "Pod",
			Name: podName,
		},
		Reason:        reason,
		Message:       message,
		Count:         count,
		LastTimestamp: metav1.NewTime(lastTime),
	}
}

func TestEventCollector_ReturnsEventsForPod(t *testing.T) {
	s := buildScheme(t)
	pod := makeBasePod("my-pod", "default")

	evt := makeEventForPod("my-pod", "default", "OOMKilling", "container killed", 1, time.Now())
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(evt).Build()

	cfg := config.DefaultConfig()
	input := internalevidence.CollectorInput{
		Client: fc,
		Pod:    pod,
		Config: cfg,
		Report: &v1alpha1.IncidentReport{},
	}

	evs, errs := (&internalevidence.EventCollector{}).Collect(context.Background(), input)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(evs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evs))
	}
	if evs[0].Reason != "OOMKilling" {
		t.Errorf("Reason = %q, want OOMKilling", evs[0].Reason)
	}
}

func TestEventCollector_BoundsToMaxEventsPerIncident(t *testing.T) {
	s := buildScheme(t)
	pod := makeBasePod("bound-pod", "default")

	cfg := config.DefaultConfig()
	cfg.MaxEventsPerIncident = 3

	// Create 10 events
	var objs []runtime.Object
	base := time.Now()
	for i := 0; i < 10; i++ {
		objs = append(objs, makeEventForPod("bound-pod", "default", "TestEvent",
			fmt.Sprintf("event %d", i), int32(i+1), base.Add(time.Duration(i)*time.Second)))
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

	input := internalevidence.CollectorInput{
		Client: fc,
		Pod:    pod,
		Config: cfg,
		Report: &v1alpha1.IncidentReport{},
	}

	evs, _ := (&internalevidence.EventCollector{}).Collect(context.Background(), input)
	if len(evs) > cfg.MaxEventsPerIncident {
		t.Errorf("expected at most %d events, got %d", cfg.MaxEventsPerIncident, len(evs))
	}
}

func TestEventCollector_TruncatesLongMessage(t *testing.T) {
	s := buildScheme(t)
	pod := makeBasePod("msg-pod", "default")
	longMsg := strings.Repeat("x", 300)

	evt := makeEventForPod("msg-pod", "default", "TestEvent", longMsg, 1, time.Now())
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(evt).Build()

	cfg := config.DefaultConfig()
	input := internalevidence.CollectorInput{
		Client: fc,
		Pod:    pod,
		Config: cfg,
		Report: &v1alpha1.IncidentReport{},
	}

	evs, _ := (&internalevidence.EventCollector{}).Collect(context.Background(), input)
	if len(evs) != 1 {
		t.Fatalf("expected 1 event")
	}
	if len(evs[0].Message) > 256 {
		t.Errorf("message length %d exceeds 256", len(evs[0].Message))
	}
}

func TestEventCollector_NilPod_ReturnsEmpty(t *testing.T) {
	s := buildScheme(t)
	fc := fake.NewClientBuilder().WithScheme(s).Build()

	input := internalevidence.CollectorInput{
		Client: fc,
		Pod:    nil,
		Config: config.DefaultConfig(),
		Report: &v1alpha1.IncidentReport{},
	}

	evs, errs := (&internalevidence.EventCollector{}).Collect(context.Background(), input)
	if len(errs) != 0 {
		t.Errorf("unexpected errors for nil pod: %v", errs)
	}
	if len(evs) != 0 {
		t.Errorf("expected empty events for nil pod, got %d", len(evs))
	}
}

// Feature: evidence-collection, Property 4: event-count-bounded
func TestProperty4_EventCountIsBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: evidence-collection, Property 4: event-count-bounded
		n := rapid.IntRange(0, 100).Draw(rt, "n")
		maxEvents := rapid.IntRange(1, 50).Draw(rt, "maxEvents")

		s := buildScheme(t)
		pod := makeBasePod("prop-pod", "default")
		base := time.Now()

		var objs []runtime.Object
		for i := 0; i < n; i++ {
			objs = append(objs, makeEventForPod("prop-pod", "default", "TestEvent",
				fmt.Sprintf("msg %d", i), int32(i+1), base.Add(time.Duration(i)*time.Second)))
		}
		fc := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		cfg := config.DefaultConfig()
		cfg.MaxEventsPerIncident = maxEvents
		input := internalevidence.CollectorInput{
			Client: fc,
			Pod:    pod,
			Config: cfg,
			Report: &v1alpha1.IncidentReport{},
		}

		evs, _ := (&internalevidence.EventCollector{}).Collect(context.Background(), input)

		expected := n
		if expected > maxEvents {
			expected = maxEvents
		}
		if len(evs) != expected {
			rt.Fatalf("n=%d maxEvents=%d: expected %d events, got %d", n, maxEvents, expected, len(evs))
		}
	})
}

// Feature: evidence-collection, Property 7: event-message-truncation
func TestProperty7_EventMessageLengthBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Feature: evidence-collection, Property 7: event-message-truncation
		length := rapid.IntRange(0, 500).Draw(rt, "length")
		msg := strings.Repeat("a", length)

		result := internalevidence.TruncateMessage(msg)

		if len(result) > 256 {
			rt.Fatalf("message length %d > 256 for input length %d", len(result), length)
		}
		if length <= 256 && result != msg {
			rt.Fatalf("message was truncated unnecessarily for length %d", length)
		}
	})
}
