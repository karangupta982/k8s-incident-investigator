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
	"testing"
	"time"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.StabilityPeriod != 5*time.Minute {
		t.Errorf("expected StabilityPeriod=5m, got %v", cfg.StabilityPeriod)
	}
	if cfg.CorrelationWindow != 10*time.Minute {
		t.Errorf("expected CorrelationWindow=10m, got %v", cfg.CorrelationWindow)
	}
	if cfg.ReadinessProbeFailureThreshold != 3 {
		t.Errorf("expected ReadinessProbeFailureThreshold=3, got %d", cfg.ReadinessProbeFailureThreshold)
	}
	if cfg.LivenessProbeFailureThreshold != 3 {
		t.Errorf("expected LivenessProbeFailureThreshold=3, got %d", cfg.LivenessProbeFailureThreshold)
	}
	if cfg.MountFailureThreshold != 3 {
		t.Errorf("expected MountFailureThreshold=3, got %d", cfg.MountFailureThreshold)
	}
	if cfg.SchedulingFailureThreshold != 5 {
		t.Errorf("expected SchedulingFailureThreshold=5, got %d", cfg.SchedulingFailureThreshold)
	}
	if cfg.RequeueInterval != 30*time.Second {
		t.Errorf("expected RequeueInterval=30s, got %v", cfg.RequeueInterval)
	}
	if cfg.WatchNamespaces == nil {
		t.Error("expected WatchNamespaces to be non-nil empty slice, got nil")
	}
	if len(cfg.WatchNamespaces) != 0 {
		t.Errorf("expected WatchNamespaces to be empty, got %v", cfg.WatchNamespaces)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{
			name:    "default config is valid",
			mutate:  func(c *config.Config) {},
			wantErr: false,
		},
		{
			name:    "zero StabilityPeriod is invalid",
			mutate:  func(c *config.Config) { c.StabilityPeriod = 0 },
			wantErr: true,
		},
		{
			name:    "negative StabilityPeriod is invalid",
			mutate:  func(c *config.Config) { c.StabilityPeriod = -1 * time.Second },
			wantErr: true,
		},
		{
			name:    "zero CorrelationWindow is invalid",
			mutate:  func(c *config.Config) { c.CorrelationWindow = 0 },
			wantErr: true,
		},
		{
			name:    "negative CorrelationWindow is invalid",
			mutate:  func(c *config.Config) { c.CorrelationWindow = -1 * time.Minute },
			wantErr: true,
		},
		{
			name:    "zero ReadinessProbeFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.ReadinessProbeFailureThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "negative ReadinessProbeFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.ReadinessProbeFailureThreshold = -1 },
			wantErr: true,
		},
		{
			name:    "zero LivenessProbeFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.LivenessProbeFailureThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "negative LivenessProbeFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.LivenessProbeFailureThreshold = -1 },
			wantErr: true,
		},
		{
			name:    "zero MountFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.MountFailureThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "negative MountFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.MountFailureThreshold = -5 },
			wantErr: true,
		},
		{
			name:    "zero SchedulingFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.SchedulingFailureThreshold = 0 },
			wantErr: true,
		},
		{
			name:    "negative SchedulingFailureThreshold is invalid",
			mutate:  func(c *config.Config) { c.SchedulingFailureThreshold = -3 },
			wantErr: true,
		},
		{
			name:    "zero RequeueInterval is invalid",
			mutate:  func(c *config.Config) { c.RequeueInterval = 0 },
			wantErr: true,
		},
		{
			name:    "negative RequeueInterval is invalid",
			mutate:  func(c *config.Config) { c.RequeueInterval = -10 * time.Second },
			wantErr: true,
		},
		{
			name:    "threshold of 1 is valid",
			mutate:  func(c *config.Config) { c.ReadinessProbeFailureThreshold = 1 },
			wantErr: false,
		},
		{
			name: "large valid config is accepted",
			mutate: func(c *config.Config) {
				c.StabilityPeriod = 30 * time.Minute
				c.CorrelationWindow = 60 * time.Minute
				c.ReadinessProbeFailureThreshold = 100
				c.LivenessProbeFailureThreshold = 100
				c.MountFailureThreshold = 100
				c.SchedulingFailureThreshold = 100
				c.RequeueInterval = 5 * time.Minute
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
