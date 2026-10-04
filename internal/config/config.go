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

// Package config provides configuration types and defaults for the Incident Investigator.
package config

import (
	"fmt"
	"time"
)

// Config holds all configurable parameters for the Investigator.
// Loaded once at startup via flags, passed via dependency injection.
type Config struct {
	// WatchNamespaces is the list of namespaces to watch. Empty means all namespaces.
	WatchNamespaces []string

	// StabilityPeriod is the duration a workload must remain healthy before an
	// active incident is marked Resolved. Default: 5 minutes.
	StabilityPeriod time.Duration

	// CorrelationWindow is reserved for future use to gate whether a new failure after
	// resolution is related to the previous incident episode. Default: 10 minutes.
	CorrelationWindow time.Duration

	// ReadinessProbeFailureThreshold is the Kubernetes Event.count value required
	// to create an incident for repeated readiness probe failures. Default: 3.
	ReadinessProbeFailureThreshold int

	// LivenessProbeFailureThreshold is the Kubernetes Event.count value required
	// to create an incident for repeated liveness probe failures. Default: 3.
	LivenessProbeFailureThreshold int

	// MountFailureThreshold is the Kubernetes Event.count value required
	// to create an incident for repeated mount failures. Default: 3.
	MountFailureThreshold int

	// SchedulingFailureThreshold is the Kubernetes Event.count value required
	// to create an incident for repeated scheduling failures. Default: 5.
	SchedulingFailureThreshold int

	// RequeueInterval is how often active incidents are re-evaluated. Default: 30s.
	RequeueInterval time.Duration

	// MaxLogBytes is the maximum number of bytes collected per container log excerpt.
	// Default: 32768 (32 KB).
	MaxLogBytes int

	// MaxLogLines is the maximum number of lines collected per container log excerpt.
	// Default: 200.
	MaxLogLines int

	// MaxEventsPerIncident is the maximum number of Kubernetes Events stored in evidence.
	// Default: 25.
	MaxEventsPerIncident int

	// EvidenceCollectionTimeout is the maximum duration for a single evidence collection cycle.
	// Default: 30 seconds.
	EvidenceCollectionTimeout time.Duration

	// MaxTimelineEvents is the maximum number of events stored in the incident timeline.
	// Default: 50.
	MaxTimelineEvents int
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		WatchNamespaces:                []string{},
		StabilityPeriod:                5 * time.Minute,
		CorrelationWindow:              10 * time.Minute,
		ReadinessProbeFailureThreshold: 3,
		LivenessProbeFailureThreshold:  3,
		MountFailureThreshold:          3,
		SchedulingFailureThreshold:     5,
		RequeueInterval:                30 * time.Second,
		MaxLogBytes:                    32768,
		MaxLogLines:                    200,
		MaxEventsPerIncident:           25,
		EvidenceCollectionTimeout:      30 * time.Second,
		MaxTimelineEvents:              50,
	}
}

// Validate returns an error if the Config contains invalid values.
// Zero or negative durations and zero or negative thresholds are rejected.
func (c *Config) Validate() error {
	if c.StabilityPeriod <= 0 {
		return fmt.Errorf("stabilityPeriod must be positive, got %v", c.StabilityPeriod)
	}
	if c.CorrelationWindow <= 0 {
		return fmt.Errorf("correlationWindow must be positive, got %v", c.CorrelationWindow)
	}
	if c.ReadinessProbeFailureThreshold <= 0 {
		return fmt.Errorf("readinessProbeFailureThreshold must be positive, got %d", c.ReadinessProbeFailureThreshold)
	}
	if c.LivenessProbeFailureThreshold <= 0 {
		return fmt.Errorf("livenessProbeFailureThreshold must be positive, got %d", c.LivenessProbeFailureThreshold)
	}
	if c.MountFailureThreshold <= 0 {
		return fmt.Errorf("mountFailureThreshold must be positive, got %d", c.MountFailureThreshold)
	}
	if c.SchedulingFailureThreshold <= 0 {
		return fmt.Errorf("schedulingFailureThreshold must be positive, got %d", c.SchedulingFailureThreshold)
	}
	if c.RequeueInterval <= 0 {
		return fmt.Errorf("requeueInterval must be positive, got %v", c.RequeueInterval)
	}
	if c.MaxLogBytes <= 0 {
		return fmt.Errorf("maxLogBytes must be positive, got %d", c.MaxLogBytes)
	}
	if c.MaxLogLines <= 0 {
		return fmt.Errorf("maxLogLines must be positive, got %d", c.MaxLogLines)
	}
	if c.MaxEventsPerIncident <= 0 {
		return fmt.Errorf("maxEventsPerIncident must be positive, got %d", c.MaxEventsPerIncident)
	}
	if c.EvidenceCollectionTimeout <= 0 {
		return fmt.Errorf("evidenceCollectionTimeout must be positive, got %v", c.EvidenceCollectionTimeout)
	}
	if c.MaxTimelineEvents <= 0 {
		return fmt.Errorf("maxTimelineEvents must be positive, got %d", c.MaxTimelineEvents)
	}
	return nil
}
