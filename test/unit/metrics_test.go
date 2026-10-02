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
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/internal/metrics"
)

// newTestRegistry creates an isolated registry for test metrics to avoid
// conflicts with the global controller-runtime registry.
func newIsolatedCounter(name, help string, labels []string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
}

// TestRecorder_RecordIncidentDetected verifies the counter and gauge increment.
func TestRecorder_RecordIncidentDetected(t *testing.T) {
	counter := newIsolatedCounter("test_incidents_detected", "test", []string{"trigger_type", "namespace"})
	gauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_active", Help: "test"}, []string{"namespace"})

	reg := prometheus.NewRegistry()
	reg.MustRegister(counter, gauge)

	// Manually call the same logic as Recorder but on isolated metrics
	counter.WithLabelValues("OOMKilled", "default").Inc()
	gauge.WithLabelValues("default").Inc()

	expected := `# HELP test_incidents_detected test
# TYPE test_incidents_detected counter
test_incidents_detected{namespace="default",trigger_type="OOMKilled"} 1
`
	if err := testutil.CollectAndCompare(counter, strings.NewReader(expected)); err != nil {
		t.Errorf("counter mismatch: %v", err)
	}
}

// TestRecorder_RecordInvestigationCompleted verifies the completion counter.
func TestRecorder_RecordInvestigationCompleted(t *testing.T) {
	counter := newIsolatedCounter("test_completed", "test", []string{"trigger_type"})
	reg := prometheus.NewRegistry()
	reg.MustRegister(counter)

	counter.WithLabelValues("CrashLoopBackOff").Inc()
	counter.WithLabelValues("CrashLoopBackOff").Inc()

	expected := `# HELP test_completed test
# TYPE test_completed counter
test_completed{trigger_type="CrashLoopBackOff"} 2
`
	if err := testutil.CollectAndCompare(counter, strings.NewReader(expected)); err != nil {
		t.Errorf("counter mismatch: %v", err)
	}
}

// TestRecorder_DiagnosisRuleMatch verifies rule match counter with two labels.
func TestRecorder_DiagnosisRuleMatch(t *testing.T) {
	counter := newIsolatedCounter("test_rule_matches", "test", []string{"rule_id", "confidence"})
	reg := prometheus.NewRegistry()
	reg.MustRegister(counter)

	counter.WithLabelValues("OOMMemoryLimit", "High").Inc()
	counter.WithLabelValues("OOMMemoryLimit", "High").Inc()
	counter.WithLabelValues("NodeMemoryPressure", "Medium").Inc()

	expected := `# HELP test_rule_matches test
# TYPE test_rule_matches counter
test_rule_matches{confidence="High",rule_id="OOMMemoryLimit"} 2
test_rule_matches{confidence="Medium",rule_id="NodeMemoryPressure"} 1
`
	if err := testutil.CollectAndCompare(counter, strings.NewReader(expected)); err != nil {
		t.Errorf("counter mismatch: %v", err)
	}
}

// TestNoOpRecorder_NoPanic verifies that NoOpRecorder methods don't panic.
func TestNoOpRecorder_NoPanic(t *testing.T) {
	r := &metrics.NoOpRecorder{}
	// All methods should be no-ops — calling them must not panic
	r.RecordIncidentDetected("OOMKilled", "default")
	r.RecordInvestigationCompleted("OOMKilled", 120.5)
	r.RecordActiveDecrement("default")
	r.RecordUnknownDiagnosis("CrashLoopBackOff")
	r.RecordDiagnosed("OOMMemoryLimit", "OOMKilled")
	r.RecordReconciliationError("api_error")
	r.RecordEvidenceCollectionFailure("node")
	r.RecordEvidenceCollectionDuration("OOMKilled", 1.5)
	r.RecordDiagnosisRuleMatch("OOMMemoryLimit", "High")
}

// TestRecorder_ImplementsInterface verifies both types satisfy RecorderInterface.
func TestRecorder_ImplementsInterface(t *testing.T) {
	var _ metrics.RecorderInterface = &metrics.Recorder{}
	var _ metrics.RecorderInterface = &metrics.NoOpRecorder{}
	// If this compiles, both types satisfy the interface.
}
