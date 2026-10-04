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

// Package correlation derives higher-order signals from a collected EvidenceSnapshot.
// It is a pure function layer — no Kubernetes API calls are made.
package correlation

import (
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// EvidenceCorrelator derives higher-order signals from a collected EvidenceSnapshot.
// It is a pure function — it calls no external API and does not modify the snapshot.
type EvidenceCorrelator struct {
	// EventCorrelationWindow is the maximum time gap between events to consider
	// them part of the same causal chain. Default: 5 minutes.
	EventCorrelationWindow time.Duration
}

// Correlate derives CorrelatedEvidence from the given snapshot.
// Returns an empty CorrelatedEvidence (never nil) when snapshot is nil.
func (c *EvidenceCorrelator) Correlate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.CorrelatedEvidence {
	now := metav1.Now()
	result := v1alpha1.CorrelatedEvidence{
		CorrelatedAt: &now,
	}

	result.CausalSignals = &v1alpha1.CausalSignals{}
	result.LogPatterns = &v1alpha1.LogPatterns{}

	if snapshot == nil {
		return result
	}

	result.CausalSignals = deriveCausalSignals(snapshot)
	result.LogPatterns = matchLogPatterns(snapshot)
	result.ChainPatterns = buildChainPatterns(snapshot, c.correlationWindow())

	return result
}

func (c *EvidenceCorrelator) correlationWindow() time.Duration {
	if c.EventCorrelationWindow > 0 {
		return c.EventCorrelationWindow
	}
	return 5 * time.Minute
}

// ── Causal signal derivation ──────────────────────────────────────────────────

func deriveCausalSignals(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.CausalSignals {
	s := &v1alpha1.CausalSignals{}

	oomKilled := isOOMKilled(snapshot.Pod)
	if oomKilled {
		if snapshot.Node != nil && snapshot.Node.MemoryPressure == "True" {
			s.NodeMemoryPressureCoincident = true
		}
		if hasMemoryLimit(snapshot.Pod) {
			s.ContainerHitConfiguredLimit = true
		}
		if hasCrashLoopBackOff(snapshot.Pod) {
			s.OOMKillCausedCrashLoop = true
		}
	}

	// PVC signals
	if snapshot.Dependencies != nil {
		for _, pvc := range snapshot.Dependencies.PVCs {
			if pvc.Phase != "Bound" {
				s.PVCIsUnbound = true
				s.UnboundPVCNames = append(s.UnboundPVCNames, pvc.Name)
			}
		}
		if hasFailedMountEvent(snapshot) && len(snapshot.Dependencies.PVCs) > 0 {
			s.MountFailureLinkedToPVC = true
		}
		if hasFailedSchedulingEvent(snapshot) && snapshot.Dependencies.SchedulingConstraints != nil {
			s.SchedulingConstraintsPresent = true
		}
	}

	return s
}

func isOOMKilled(pod *v1alpha1.PodEvidence) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Containers {
		if c.TerminationReason == "OOMKilled" || c.LastTerminationReason == "OOMKilled" {
			return true
		}
	}
	return false
}

func hasMemoryLimit(pod *v1alpha1.PodEvidence) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Containers {
		if _, ok := c.ResourceLimits["memory"]; ok {
			return true
		}
	}
	return false
}

func hasCrashLoopBackOff(pod *v1alpha1.PodEvidence) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Containers {
		if c.WaitingReason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}

func hasFailedMountEvent(snapshot *v1alpha1.EvidenceSnapshot) bool {
	for _, ev := range snapshot.Events {
		if ev.Reason == "FailedMount" {
			return true
		}
	}
	return false
}

func hasFailedSchedulingEvent(snapshot *v1alpha1.EvidenceSnapshot) bool {
	for _, ev := range snapshot.Events {
		if ev.Reason == "FailedScheduling" {
			return true
		}
	}
	return false
}

// ── Log pattern matching ──────────────────────────────────────────────────────

type logPattern struct {
	name    string
	matches func(string) bool
}

var patterns = []logPattern{
	{
		name: "OOMString",
		matches: func(l string) bool {
			l = strings.ToLower(l)
			return strings.Contains(l, "out of memory") ||
				strings.Contains(l, "oom") ||
				strings.Contains(l, "killed")
		},
	},
	{
		name: "ConnectionRefused",
		matches: func(l string) bool {
			l = strings.ToLower(l)
			return strings.Contains(l, "connection refused") ||
				strings.Contains(l, "econnrefused")
		},
	},
	{
		name: "PanicOrFatal",
		matches: func(l string) bool {
			l = strings.ToLower(l)
			return strings.Contains(l, "panic") ||
				strings.Contains(l, "fatal") ||
				strings.Contains(l, "fatal error")
		},
	},
	{
		name: "PermissionDenied",
		matches: func(l string) bool {
			l = strings.ToLower(l)
			return strings.Contains(l, "permission denied") ||
				strings.Contains(l, "eperm")
		},
	},
}

func matchLogPatterns(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.LogPatterns {
	lp := &v1alpha1.LogPatterns{}
	if len(snapshot.Logs) == 0 {
		return lp
	}

	containerWithPattern := map[string]string{}

	for _, logEntry := range snapshot.Logs {
		for _, line := range logEntry.Lines {
			for _, p := range patterns {
				if !p.matches(line) {
					continue
				}
				switch p.name {
				case "OOMString":
					lp.ContainsOOMString = true
				case "ConnectionRefused":
					lp.ContainsConnectionRefused = true
				case "PanicOrFatal":
					lp.ContainsPanicOrFatal = true
				case "PermissionDenied":
					lp.ContainsPermissionDenied = true
				}
				// Record first container per pattern
				if _, seen := containerWithPattern[p.name]; !seen {
					containerWithPattern[p.name] = logEntry.ContainerName
				}
			}
		}
	}

	if len(containerWithPattern) > 0 {
		lp.ContainerWithPattern = containerWithPattern
	}
	return lp
}

// ── Causal chain pattern detection ───────────────────────────────────────────

const (
	ChainOOMToCrashLoop    = "OOMToCrashLoop"
	ChainPVCToBoundToMount = "PVCToBoundToMount"
	ChainScheduleToFail    = "ScheduleToFail"
)

func buildChainPatterns(snapshot *v1alpha1.EvidenceSnapshot, _ time.Duration) []string {
	var chains []string

	// OOMToCrashLoop: OOMKilled present AND CrashLoopBackOff present
	if isOOMKilled(snapshot.Pod) && hasCrashLoopBackOff(snapshot.Pod) {
		chains = append(chains, ChainOOMToCrashLoop)
	}

	// PVCToBoundToMount: PVC evidence exists AND FailedMount events exist
	if snapshot.Dependencies != nil && len(snapshot.Dependencies.PVCs) > 0 &&
		hasFailedMountEvent(snapshot) {
		chains = append(chains, ChainPVCToBoundToMount)
	}

	// ScheduleToFail: multiple FailedScheduling events
	schedulingCount := 0
	for _, ev := range snapshot.Events {
		if ev.Reason == "FailedScheduling" {
			schedulingCount++
		}
	}
	if schedulingCount >= 2 {
		chains = append(chains, ChainScheduleToFail)
	}

	return chains
}
