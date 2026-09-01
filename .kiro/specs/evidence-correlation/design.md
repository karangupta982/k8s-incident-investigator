# Design Document: Evidence Correlation

## Overview

Evidence correlation derives higher-order signals from the raw `EvidenceSnapshot` produced by the evidence collection layer. It runs as a pure function step between evidence collection and diagnosis.

### Pipeline Position

```
Step 9.5  (evidence-collection) → EvidenceSnapshot stored in Status.Evidence
Step 9.55 (this spec)           → EvidenceCorrelator.Correlate(snapshot) → CorrelatedEvidence stored in Status.CorrelatedEvidence
Step 9.6  (diagnosis-engine)    → DiagnosisEngine.Evaluate(snapshot, correlated) → DiagnosisResult
```

The `DiagnosisEngine.Evaluate()` signature is extended to accept `*v1alpha1.CorrelatedEvidence` in addition to `*v1alpha1.EvidenceSnapshot`. Rules that need only raw evidence use the snapshot; rules that need derived signals use the correlated evidence.

---

## Architecture

### Dependency Direction

```
PodReconciler
      │
      └── EvidenceCorrelator              (internal/correlation)
               │
               ├── CausalSignalDeriver    — derives boolean/value signals from multi-source evidence
               ├── LogPatternMatcher      — scans log lines for known patterns
               └── EventChainBuilder      — groups events into causal chains
```

Forbidden imports:
```
internal/correlation  MUST NOT import  internal/controller
internal/correlation  MUST NOT import  internal/investigation
internal/correlation  MUST NOT import  internal/evidence
internal/correlation  MUST NOT import  internal/diagnosis
internal/correlation  MAY import       api/v1alpha1
internal/correlation  MAY import       standard library packages
```

---

## Components and Interfaces

### EvidenceCorrelator

```go
// package internal/correlation

// EvidenceCorrelator derives higher-order signals from a collected EvidenceSnapshot.
// It is a pure function — it calls no external API and does not modify the snapshot.
type EvidenceCorrelator struct {
    // EventCorrelationWindow is the maximum time gap between two events for them to
    // be considered part of the same causal chain.
    // Default: 5 minutes.
    EventCorrelationWindow time.Duration
}

// Correlate derives CorrelatedEvidence from the given snapshot.
// Returns an empty CorrelatedEvidence (not nil) when snapshot is nil.
func (c *EvidenceCorrelator) Correlate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.CorrelatedEvidence
```

---

## New API Types — api/v1alpha1/correlation_types.go

```go
// CorrelatedEvidence holds derived signals computed from the EvidenceSnapshot.
// It is a pure transformation — no Kubernetes API calls are made to produce it.
//
// +kubebuilder:object:generate=true
type CorrelatedEvidence struct {
    // CorrelatedAt is the timestamp when correlation last ran.
    // +optional
    CorrelatedAt *metav1.Time `json:"correlatedAt,omitempty"`

    // CausalSignals holds derived boolean and string signals from multi-source evidence.
    // +optional
    CausalSignals *CausalSignals `json:"causalSignals,omitempty"`

    // LogPatterns holds patterns detected in container log lines.
    // +optional
    LogPatterns *LogPatterns `json:"logPatterns,omitempty"`

    // ChainPatterns lists identified causal event chain types.
    // +optional
    ChainPatterns []string `json:"chainPatterns,omitempty"`
}

// CausalSignals holds derived signals requiring evidence from multiple layers.
//
// +kubebuilder:object:generate=true
type CausalSignals struct {
    // NodeMemoryPressureCoincident is true when OOMKilled occurred
    // while the node was reporting MemoryPressure=True.
    NodeMemoryPressureCoincident bool `json:"nodeMemoryPressureCoincident,omitempty"`

    // ContainerHitConfiguredLimit is true when OOMKilled occurred
    // and the container had a configured memory limit.
    ContainerHitConfiguredLimit bool `json:"containerHitConfiguredLimit,omitempty"`

    // PVCIsUnbound is true when at least one referenced PVC is not in Bound phase.
    PVCIsUnbound bool `json:"pvcIsUnbound,omitempty"`

    // UnboundPVCNames lists the names of PVCs that are not Bound.
    // +optional
    UnboundPVCNames []string `json:"unboundPVCNames,omitempty"`

    // MountFailureLinkedToPVC is true when FailedMount events exist
    // and PVC evidence is present.
    MountFailureLinkedToPVC bool `json:"mountFailureLinkedToPVC,omitempty"`

    // SchedulingConstraintsPresent is true when FailedScheduling events exist
    // and scheduling constraints are present in the evidence.
    SchedulingConstraintsPresent bool `json:"schedulingConstraintsPresent,omitempty"`

    // OOMKillCausedCrashLoop is true when OOMKilled exit codes are detected
    // alongside CrashLoopBackOff waiting state for the same container.
    OOMKillCausedCrashLoop bool `json:"oomKillCausedCrashLoop,omitempty"`
}

// LogPatterns holds signals extracted from container log lines.
//
// +kubebuilder:object:generate=true
type LogPatterns struct {
    // ContainsOOMString is true when any log line contains an OOM-related string.
    ContainsOOMString bool `json:"containsOOMString,omitempty"`

    // ContainsConnectionRefused is true when any log line contains a connection refused error.
    ContainsConnectionRefused bool `json:"containsConnectionRefused,omitempty"`

    // ContainsPanicOrFatal is true when any log line contains a panic or fatal error.
    ContainsPanicOrFatal bool `json:"containsPanicOrFatal,omitempty"`

    // ContainsPermissionDenied is true when any log line contains a permission error.
    ContainsPermissionDenied bool `json:"containsPermissionDenied,omitempty"`

    // ContainerWithPattern maps pattern name to the first container name
    // where the pattern was detected.
    // +optional
    ContainerWithPattern map[string]string `json:"containerWithPattern,omitempty"`
}
```

### New field on IncidentReportStatus

```go
// CorrelatedEvidence holds derived signals computed from the EvidenceSnapshot.
// Updated after each evidence collection cycle.
// +optional
CorrelatedEvidence *CorrelatedEvidence `json:"correlatedEvidence,omitempty"`
```

---

## Updated DiagnosisEngine Signature

The diagnosis engine's `Evaluate` method is extended:

```go
// Before (diagnosis spec):
func (e *DiagnosisEngine) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.DiagnosisResult

// After (this spec):
func (e *DiagnosisEngine) Evaluate(
    snapshot *v1alpha1.EvidenceSnapshot,
    correlated *v1alpha1.CorrelatedEvidence,
) v1alpha1.DiagnosisResult
```

Rules that do not need correlated signals simply ignore the second parameter. Rules that do need it access `correlated.CausalSignals.ContainerHitConfiguredLimit` rather than re-deriving it themselves.

---

## Implementation Details

### CausalSignalDeriver

```go
func deriveCausalSignals(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.CausalSignals {
    s := &v1alpha1.CausalSignals{}
    if snapshot == nil {
        return s
    }

    // OOM signals
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
    for _, pvc := range pvcs(snapshot) {
        if pvc.Phase != "Bound" {
            s.PVCIsUnbound = true
            s.UnboundPVCNames = append(s.UnboundPVCNames, pvc.Name)
        }
    }
    if hasFailedMountEvent(snapshot) && len(pvcs(snapshot)) > 0 {
        s.MountFailureLinkedToPVC = true
    }

    // Scheduling signals
    if hasFailedSchedulingEvent(snapshot) && snapshot.Dependencies != nil &&
        snapshot.Dependencies.SchedulingConstraints != nil {
        s.SchedulingConstraintsPresent = true
    }

    return s
}
```

### LogPatternMatcher

Pattern matching uses `strings.Contains` with lowercase normalization — no regex, no LLM:

```go
var logPatterns = []struct {
    name    string
    matches func(string) bool
    field   *bool // pointer to the field to set in LogPatterns
}{
    {"OOMString", func(l string) bool {
        l = strings.ToLower(l)
        return strings.Contains(l, "out of memory") || strings.Contains(l, "oom") || strings.Contains(l, "killed")
    }, nil},
    // ... etc
}
```

---

## Testing Strategy

### Unit Tests

`test/unit/correlation_test.go`:
- `TestCausalSignals_OOMWithNodePressure`: snapshot with OOMKilled + MemoryPressure=True → NodeMemoryPressureCoincident=true
- `TestCausalSignals_OOMWithLimit`: snapshot with OOMKilled + memory limit set → ContainerHitConfiguredLimit=true
- `TestCausalSignals_PVCUnbound`: snapshot with PVC phase=Pending → PVCIsUnbound=true, name in UnboundPVCNames
- `TestLogPatterns_OOMString`: log line "Killed" → ContainsOOMString=true
- `TestLogPatterns_ConnectionRefused`: log line "connection refused" → ContainsConnectionRefused=true
- `TestNilSnapshot`: nil input → empty CorrelatedEvidence, no panic

### Property Tests

- **Property**: For any EvidenceSnapshot, `Correlate()` returns identical output on repeated calls (pure function)
- **Property**: For nil snapshot, no panic and empty CorrelatedEvidence returned
