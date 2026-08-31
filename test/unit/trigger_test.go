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

package unit_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pgregory.net/rapid"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/investigation"
)

// ---- helpers ----------------------------------------------------------------

func defaultCfg() *config.Config {
	return config.DefaultConfig()
}


// podWithWaitingContainer builds a Pod where the named container is in a waiting state.
func podWithWaitingContainer(reason string) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: reason},
					},
				},
			},
		},
	}
}

// podWithCurrentTerminated builds a Pod where the named container terminated with given reason/exit code.
func podWithCurrentTerminated(reason string, exitCode int32) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   reason,
							ExitCode: exitCode,
						},
					},
				},
			},
		},
	}
}

// podWithLastTerminated builds a Pod where the named container restarted and last termination was the given reason.
func podWithLastTerminated(reason string) *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "app",
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Reason:   reason,
							ExitCode: 137,
						},
					},
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
}

func runningPod() *corev1.Pod {
	return &corev1.Pod{
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// ---- table-driven tests -----------------------------------------------------

func TestTriggerEvaluator_TableDriven(t *testing.T) {
	cfg := defaultCfg()
	trueVal := true

	tests := []struct {
		name          string
		tc            investigation.TriggerContext
		wantTrigger   bool
		wantType      v1alpha1.TriggerType
		wantSource    v1alpha1.TriggerSource
		wantImmediate bool
	}{
		// --- Immediate state-based triggers ---
		{
			name:          "OOMKilled current terminated",
			tc:            investigation.TriggerContext{Pod: podWithCurrentTerminated("OOMKilled", 137)},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerOOMKilled,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name:          "OOMKilled last terminated state",
			tc:            investigation.TriggerContext{Pod: podWithLastTerminated("OOMKilled")},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerOOMKilled,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name:          "CrashLoopBackOff",
			tc:            investigation.TriggerContext{Pod: podWithWaitingContainer("CrashLoopBackOff")},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerCrashLoopBackOff,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name:          "ImagePullBackOff",
			tc:            investigation.TriggerContext{Pod: podWithWaitingContainer("ImagePullBackOff")},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerImagePullBackOff,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name:          "ErrImagePull",
			tc:            investigation.TriggerContext{Pod: podWithWaitingContainer("ErrImagePull")},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerImagePullBackOff,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name:          "CreateContainerConfigError",
			tc:            investigation.TriggerContext{Pod: podWithWaitingContainer("CreateContainerConfigError")},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerCreateContainerConfigError,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name: "Eviction via status.reason",
			tc: investigation.TriggerContext{
				Pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Phase:  corev1.PodFailed,
						Reason: "Evicted",
					},
				},
			},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerEviction,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},
		{
			name: "Eviction via PodFailed + DisruptionTarget condition",
			tc: investigation.TriggerContext{
				Pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
						Conditions: []corev1.PodCondition{
							{Type: "DisruptionTarget", Status: corev1.ConditionTrue},
						},
					},
				},
			},
			wantTrigger:   true,
			wantType:      v1alpha1.TriggerEviction,
			wantSource:    v1alpha1.TriggerSourceStateBased,
			wantImmediate: true,
		},

		// --- Event-based threshold triggers ---
		{
			name: "readiness probe count = threshold",
			tc: investigation.TriggerContext{
				Pod:                      runningPod(),
				ReadinessProbeEventCount: cfg.ReadinessProbeFailureThreshold,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerReadinessProbeFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "readiness probe count > threshold",
			tc: investigation.TriggerContext{
				Pod:                      runningPod(),
				ReadinessProbeEventCount: cfg.ReadinessProbeFailureThreshold + 1,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerReadinessProbeFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "readiness probe count = threshold-1 (no trigger)",
			tc: investigation.TriggerContext{
				Pod:                      runningPod(),
				ReadinessProbeEventCount: cfg.ReadinessProbeFailureThreshold - 1,
			},
			wantTrigger: false,
		},
		{
			name: "liveness probe count = threshold",
			tc: investigation.TriggerContext{
				Pod:                     runningPod(),
				LivenessProbeEventCount: cfg.LivenessProbeFailureThreshold,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerLivenessProbeFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "liveness probe count > threshold",
			tc: investigation.TriggerContext{
				Pod:                     runningPod(),
				LivenessProbeEventCount: cfg.LivenessProbeFailureThreshold + 5,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerLivenessProbeFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "liveness probe count = threshold-1 (no trigger)",
			tc: investigation.TriggerContext{
				Pod:                     runningPod(),
				LivenessProbeEventCount: cfg.LivenessProbeFailureThreshold - 1,
			},
			wantTrigger: false,
		},
		{
			name: "mount failure count = threshold",
			tc: investigation.TriggerContext{
				Pod:                    runningPod(),
				MountFailureEventCount: cfg.MountFailureThreshold,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerMountFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "mount failure count > threshold",
			tc: investigation.TriggerContext{
				Pod:                    runningPod(),
				MountFailureEventCount: cfg.MountFailureThreshold + 2,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerMountFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "mount failure count = threshold-1 (no trigger)",
			tc: investigation.TriggerContext{
				Pod:                    runningPod(),
				MountFailureEventCount: cfg.MountFailureThreshold - 1,
			},
			wantTrigger: false,
		},
		{
			name: "scheduling failure count = threshold",
			tc: investigation.TriggerContext{
				Pod:                         runningPod(),
				SchedulingFailureEventCount: cfg.SchedulingFailureThreshold,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerSchedulingFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "scheduling failure count > threshold",
			tc: investigation.TriggerContext{
				Pod:                         runningPod(),
				SchedulingFailureEventCount: cfg.SchedulingFailureThreshold + 10,
			},
			wantTrigger: true,
			wantType:    v1alpha1.TriggerSchedulingFailure,
			wantSource:  v1alpha1.TriggerSourceEventBased,
		},
		{
			name: "scheduling failure count = threshold-1 (no trigger)",
			tc: investigation.TriggerContext{
				Pod:                         runningPod(),
				SchedulingFailureEventCount: cfg.SchedulingFailureThreshold - 1,
			},
			wantTrigger: false,
		},

		// --- Normal lifecycle exclusions ---
		{
			name: "Succeeded phase — no trigger",
			tc: investigation.TriggerContext{
				Pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
			},
			wantTrigger: false,
		},
		{
			name:        "ContainerCreating — no trigger",
			tc:          investigation.TriggerContext{Pod: podWithWaitingContainer("ContainerCreating")},
			wantTrigger: false,
		},
		{
			name:        "PodInitializing — no trigger",
			tc:          investigation.TriggerContext{Pod: podWithWaitingContainer("PodInitializing")},
			wantTrigger: false,
		},
		{
			name: "graceful termination with no failure — no trigger",
			tc: investigation.TriggerContext{
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &metav1.Time{},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			wantTrigger: false,
		},
		{
			name: "exit code 0 (completed) — no trigger",
			tc: investigation.TriggerContext{
				Pod: podWithCurrentTerminated("Completed", 0),
			},
			wantTrigger: false,
		},
		{
			name:        "nil pod — no trigger",
			tc:          investigation.TriggerContext{Pod: nil},
			wantTrigger: false,
		},
		{
			name: "no container statuses no events — no trigger",
			tc: investigation.TriggerContext{
				Pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			},
			wantTrigger: false,
		},
	}

	_ = trueVal // used to build bool pointer below if needed
	e := investigation.NewTriggerEvaluator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.Evaluate(context.Background(), tt.tc, cfg)
			if result.IsTrigger != tt.wantTrigger {
				t.Errorf("IsTrigger = %v, want %v", result.IsTrigger, tt.wantTrigger)
			}
			if tt.wantTrigger {
				if result.Type != tt.wantType {
					t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
				}
				if result.Source != tt.wantSource {
					t.Errorf("Source = %v, want %v", result.Source, tt.wantSource)
				}
				if result.IsImmediate != tt.wantImmediate {
					t.Errorf("IsImmediate = %v, want %v", result.IsImmediate, tt.wantImmediate)
				}
			}
		})
	}
}

// ---- property-based tests ---------------------------------------------------

// Feature: incident-investigator-foundation, Property 1
// For any Pod state that contains an OOMKilled/CrashLoopBackOff/ImagePullBackOff/
// CreateContainerConfigError reason or eviction, IsTrigger=true and Source=StateBased.
func TestProperty1_ImmediateTriggersFire(t *testing.T) {
	cfg := defaultCfg()
	e := investigation.NewTriggerEvaluator()

	// Enumerate pods that must trigger immediately.
	immediatePods := []*corev1.Pod{
		podWithCurrentTerminated("OOMKilled", 137),
		podWithLastTerminated("OOMKilled"),
		podWithWaitingContainer("CrashLoopBackOff"),
		podWithWaitingContainer("ImagePullBackOff"),
		podWithWaitingContainer("ErrImagePull"),
		podWithWaitingContainer("CreateContainerConfigError"),
		{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}},
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random index into the immediate pods list
		idx := rapid.IntRange(0, len(immediatePods)-1).Draw(rt, "idx")
		pod := immediatePods[idx]

		tc := investigation.TriggerContext{Pod: pod}
		result := e.Evaluate(context.Background(), tc, cfg)

		if !result.IsTrigger {
			rt.Fatalf("expected IsTrigger=true for pod %d, got false", idx)
		}
		if result.Source != v1alpha1.TriggerSourceStateBased {
			rt.Fatalf("expected Source=StateBased for pod %d, got %v", idx, result.Source)
		}
		if !result.IsImmediate {
			rt.Fatalf("expected IsImmediate=true for pod %d, got false", idx)
		}
	})
}

// Feature: incident-investigator-foundation, Property 2
// For any event count C and threshold T: IsTrigger = (C >= T).
func TestProperty2_ThresholdTriggersRespectThreshold(t *testing.T) {
	e := investigation.NewTriggerEvaluator()

	rapid.Check(t, func(rt *rapid.T) {
		// Generate a threshold in a sane range (1..20).
		threshold := rapid.IntRange(1, 20).Draw(rt, "threshold")
		// Generate a count (0..30).
		count := rapid.IntRange(0, 30).Draw(rt, "count")

		cfg := &config.Config{
			ReadinessProbeFailureThreshold: threshold,
			LivenessProbeFailureThreshold:  threshold,
			MountFailureThreshold:          threshold,
			SchedulingFailureThreshold:     threshold,
			StabilityPeriod:                defaultCfg().StabilityPeriod,
			RequeueInterval:                defaultCfg().RequeueInterval,
			CorrelationWindow:              defaultCfg().CorrelationWindow,
		}

		// Test readiness probe threshold
		tcR := investigation.TriggerContext{
			Pod:                      runningPod(),
			ReadinessProbeEventCount: count,
		}
		resR := e.Evaluate(context.Background(), tcR, cfg)
		wantR := count >= threshold
		if resR.IsTrigger != wantR {
			rt.Fatalf("readiness: count=%d threshold=%d: IsTrigger=%v want=%v",
				count, threshold, resR.IsTrigger, wantR)
		}

		// Test mount failure threshold
		tcM := investigation.TriggerContext{
			Pod:                    runningPod(),
			MountFailureEventCount: count,
		}
		resM := e.Evaluate(context.Background(), tcM, cfg)
		wantM := count >= threshold
		if resM.IsTrigger != wantM {
			rt.Fatalf("mount: count=%d threshold=%d: IsTrigger=%v want=%v",
				count, threshold, resM.IsTrigger, wantM)
		}

		// Test scheduling failure threshold
		tcS := investigation.TriggerContext{
			Pod:                         runningPod(),
			SchedulingFailureEventCount: count,
		}
		resS := e.Evaluate(context.Background(), tcS, cfg)
		wantS := count >= threshold
		if resS.IsTrigger != wantS {
			rt.Fatalf("scheduling: count=%d threshold=%d: IsTrigger=%v want=%v",
				count, threshold, resS.IsTrigger, wantS)
		}
	})
}

// Feature: incident-investigator-foundation, Property 3
// For any Pod in Succeeded state, IsTrigger=false.
func TestProperty3_SucceededNeverTriggers(t *testing.T) {
	cfg := defaultCfg()
	e := investigation.NewTriggerEvaluator()

	rapid.Check(t, func(rt *rapid.T) {
		// Any event count values — Succeeded phase must still block triggering
		count := rapid.IntRange(0, 100).Draw(rt, "count")

		tc := investigation.TriggerContext{
			Pod: &corev1.Pod{
				Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
			},
			ReadinessProbeEventCount:    count,
			LivenessProbeEventCount:     count,
			MountFailureEventCount:      count,
			SchedulingFailureEventCount: count,
		}

		result := e.Evaluate(context.Background(), tc, cfg)
		if result.IsTrigger {
			rt.Fatalf("expected IsTrigger=false for Succeeded pod (count=%d), got true", count)
		}
	})
}

// Feature: incident-investigator-foundation, Property 4
// Identical TriggerContext inputs produce identical TriggerResult outputs (pure function).
func TestProperty4_TriggerEvaluationIsPure(t *testing.T) {
	cfg := defaultCfg()
	e := investigation.NewTriggerEvaluator()

	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random but deterministic TriggerContext
		count := rapid.IntRange(0, 20).Draw(rt, "count")
		useFailedPod := rapid.Bool().Draw(rt, "useFailedPod")

		var pod *corev1.Pod
		if useFailedPod {
			pod = podWithWaitingContainer("CrashLoopBackOff")
		} else {
			pod = runningPod()
		}

		tc := investigation.TriggerContext{
			Pod:                         pod,
			MountFailureEventCount:      count,
			ReadinessProbeEventCount:    count,
			LivenessProbeEventCount:     count,
			SchedulingFailureEventCount: count,
		}

		r1 := e.Evaluate(context.Background(), tc, cfg)
		r2 := e.Evaluate(context.Background(), tc, cfg)
		r3 := e.Evaluate(context.Background(), tc, cfg)

		if r1 != r2 || r2 != r3 {
			rt.Fatalf("non-deterministic: r1=%+v r2=%+v r3=%+v", r1, r2, r3)
		}
	})
}
