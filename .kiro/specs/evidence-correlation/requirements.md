# Requirements Document

## Introduction

Evidence correlation is the step between evidence collection and diagnosis that derives higher-order signals from the raw `EvidenceSnapshot`. Raw evidence contains individual facts — an OOMKilled exit code, a MemoryPressure node condition, a FailedMount event. Evidence correlation groups these facts into causal chains, extracts patterns from container logs, and produces a `CorrelatedEvidence` type that gives the diagnosis rules richer, pre-computed signals to work with.

Without correlation, each diagnosis rule must perform its own ad hoc correlation inline, leading to duplicated logic and harder-to-test rules. With correlation, rules become pure pattern matchers over a well-defined derived type.

The architecture document explicitly includes "Evidence Correlation" as a distinct pipeline step:

```
Evidence Collection → Evidence Correlation → Diagnosis Engine
```

Evidence correlation operates as a pure function over `EvidenceSnapshot`. It calls no Kubernetes API.

---

## Glossary

### CorrelatedEvidence

The output of the evidence correlation step. A derived, enriched view of the `EvidenceSnapshot` containing causal chains, log patterns, and computed signals. Used by the diagnosis engine as its primary input.

### Causal Chain

A sequence of events where each event is causally linked to the next by Kubernetes semantics, time proximity, and resource relationships. Example: `PVC Pending → FailedMount Event → Container cannot start`.

### Log Pattern

A classified signal extracted from container log lines. For example, detecting "connection refused" or "out of memory" strings in logs without LLM assistance.

### CausalSignal

A named boolean or value derived from correlating multiple evidence pieces. Examples: `NodeWasUnderMemoryPressure`, `ContainerHitMemoryLimit`, `PVCIsUnbound`.

---

## Requirements

### Requirement 1: Derive Causal Signals from Multi-Source Evidence

**User Story:** As a diagnosis rule author, I want pre-computed causal signals so that I do not need to re-implement the same evidence cross-referencing in every rule.

#### Acceptance Criteria

1. WHEN the `EvidenceSnapshot.Pod` contains an OOMKilled container AND `EvidenceSnapshot.Node.MemoryPressure == "True"`, THE correlator SHALL set `CausalSignals.NodeMemoryPressureCoincident = true`.

2. WHEN the `EvidenceSnapshot.Pod` contains an OOMKilled container AND the container's configured memory limit is non-empty, THE correlator SHALL set `CausalSignals.ContainerHitConfiguredLimit = true`.

3. WHEN `EvidenceSnapshot.Dependencies.PVCs` contains a PVC with `Phase != "Bound"`, THE correlator SHALL set `CausalSignals.PVCIsUnbound = true` and record the PVC name in `CausalSignals.UnboundPVCNames`.

4. WHEN `EvidenceSnapshot.Events` contains a `FailedMount` event AND `EvidenceSnapshot.Dependencies.PVCs` is non-empty, THE correlator SHALL set `CausalSignals.MountFailureLinkedToPVC = true`.

5. WHEN `EvidenceSnapshot.Events` contains a `FailedScheduling` event AND `EvidenceSnapshot.Dependencies.SchedulingConstraints` is non-nil, THE correlator SHALL set `CausalSignals.SchedulingConstraintsPresent = true`.

6. WHEN `EvidenceSnapshot.Events` contains events for both `OOMKilled`/exit-code-137 AND `CrashLoopBackOff` for the same container, THE correlator SHALL set `CausalSignals.OOMKillCausedCrashLoop = true`.

---

### Requirement 2: Extract Log Patterns

**User Story:** As a diagnosis rule author, I want pre-extracted log patterns so that rules can reference log signals without re-scanning log lines.

#### Acceptance Criteria

1. WHEN `EvidenceSnapshot.Logs` is non-empty, THE correlator SHALL scan each `ContainerLogEvidence.Lines` for known error patterns and record matching patterns in `LogPatterns`.

2. THE correlator SHALL detect the following patterns:
   - `LogPatterns.ContainsOOMString`: any line containing "out of memory", "OOM", or "Killed" (case-insensitive)
   - `LogPatterns.ContainsConnectionRefused`: any line containing "connection refused" or "ECONNREFUSED"
   - `LogPatterns.ContainsPanicOrFatal`: any line containing "panic", "FATAL", or "fatal error"
   - `LogPatterns.ContainsPermissionDenied`: any line containing "permission denied" or "EPERM"

3. WHEN a pattern is detected, THE correlator SHALL also record the container name that contained the matching line in `LogPatterns.ContainerWithPattern`.

4. WHEN no log lines are available (empty or unavailable), THE correlator SHALL produce an empty `LogPatterns` struct without error.

5. THE correlator SHALL NOT apply any LLM, ML, or probabilistic model to log content. Pattern matching SHALL be deterministic string matching only.

---

### Requirement 3: Build an Event Causal Chain

**User Story:** As a diagnosis rule author, I want related events grouped by causal chain so that I can reason about sequences rather than individual events.

#### Acceptance Criteria

1. WHEN `EvidenceSnapshot.Events` contains multiple events, THE correlator SHALL group events into a `CausalChain` slice ordered by time.

2. A `CausalChain` SHALL link events that share the same `InvolvedObjectName` and occur within a configurable time window (default 5 minutes).

3. THE correlator SHALL identify the following chain patterns and record them as named chain types in `CorrelatedEvidence.ChainPatterns`:
   - `"OOMToCrashLoop"`: OOMKilled event(s) followed by CrashLoopBackOff within the window
   - `"PVCToBoundToMount"`: PVC Pending → PVC Bound → FailedMount (storage provisioning issue)
   - `"ScheduleToFail"`: repeated FailedScheduling events without any successful scheduling

4. WHEN no chain pattern is detected, THE `ChainPatterns` field SHALL be an empty slice.

---

### Requirement 4: Correlation Is a Pure Function

**User Story:** As a developer, I want evidence correlation to be a pure function so that it is easy to unit test and does not introduce new failure modes.

#### Acceptance Criteria

1. THE `EvidenceCorrelator.Correlate()` function SHALL NOT call the Kubernetes API.

2. THE `EvidenceCorrelator.Correlate()` function SHALL NOT modify the input `EvidenceSnapshot`.

3. For identical `EvidenceSnapshot` inputs, `Correlate()` SHALL return identical `CorrelatedEvidence` outputs.

4. WHEN the input `EvidenceSnapshot` is nil, `Correlate()` SHALL return an empty `CorrelatedEvidence` without panicking.

---

### Requirement 5: Store CorrelatedEvidence in IncidentReport

**User Story:** As an engineer, I want to see what the correlator derived from the evidence so that I can understand how the diagnosis was reached.

#### Acceptance Criteria

1. THE Investigator SHALL store the `CorrelatedEvidence` in `IncidentReport.Status.CorrelatedEvidence` after correlation runs.

2. THE `CorrelatedEvidence` SHALL be updated on every reconciliation cycle alongside the `EvidenceSnapshot`.

3. THE `CorrelatedEvidence` field SHALL be bounded in size. The `CausalChain` slice SHALL be limited to 20 entries. The `LogPatterns.ContainerWithPattern` map SHALL store at most one container name per pattern.

---

## Out of Scope

- LLM-based log analysis
- Cross-workload correlation (multiple incident reports linked together)
- Correlation using Prometheus metrics
- Automated remediation based on correlated signals
