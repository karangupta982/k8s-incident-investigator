# Design Document: Diagnosis Engine

## Overview

The diagnosis engine interprets a collected `EvidenceSnapshot` and produces a structured `DiagnosisResult` stored in the `IncidentReport` status. It is implemented as a modular, deterministic rules engine in `internal/diagnosis/`. Each rule is a small, independently testable function that inspects a specific subset of the evidence and returns zero or one `DiagnosisFinding`.

The engine is a **pure function over evidence**: it takes an `*v1alpha1.EvidenceSnapshot` and returns a `DiagnosisResult`. It calls no Kubernetes API, starts no goroutines, and carries no state between invocations.

This design connects two existing pipeline stages:

```
Step 9.5 (evidence-collection spec) → EvidenceSnapshot stored in IncidentReport.Status.Evidence
         ↓
Step 9.6 (this spec) → DiagnosisEngine.Evaluate(snapshot) → DiagnosisResult stored in Status.Diagnosis
         ↓
Step 10 / 11 (foundation spec) → Recovery evaluation and phase-to-Resolved transition
```

The controller (not the engine) writes the `DiagnosisResult` to the `IncidentReport` and sets `Phase` to `Diagnosed` or `Unknown`.

---

## Architecture

### Dependency Direction

```
PodReconciler
      │
      ├── EvidenceOrchestrator  (internal/evidence)
      │        │
      │        └── EvidenceSnapshot → stored in IncidentReport.Status.Evidence
      │
      └── DiagnosisEngine       (internal/diagnosis)
               │
               ├── rules.OOMMemoryLimitRule
               ├── rules.NodeMemoryPressureRule
               ├── rules.CrashLoopAppErrorRule
               ├── rules.CrashLoopOOMExitRule
               ├── rules.ImagePullFailureRule
               ├── rules.MissingConfigReferenceRule
               ├── rules.PVCNotBoundRule
               ├── rules.PVCMountErrorRule
               ├── rules.SchedulingFailureRule
               └── rules.ProbeFailureRule
```

Forbidden imports:

```
internal/diagnosis  MUST NOT import  internal/controller
internal/diagnosis  MUST NOT import  internal/investigation
internal/diagnosis  MUST NOT import  internal/evidence
internal/diagnosis  MAY import       api/v1alpha1
internal/diagnosis  MAY import       standard library packages
```

This keeps every diagnosis rule unit-testable without a Kubernetes cluster.

### Package Structure

```
internal/diagnosis/
├── doc.go              — package documentation
├── engine.go           — DiagnosisEngine struct and Evaluate() method
├── rule.go             — DiagnosisRule interface and confidence constants
└── rules/
    ├── oom.go          — OOMMemoryLimitRule + NodeMemoryPressureRule
    ├── crashloop.go    — CrashLoopAppErrorRule + CrashLoopOOMExitRule
    ├── imagepull.go    — ImagePullFailureRule
    ├── config.go       — MissingConfigReferenceRule
    ├── storage.go      — PVCNotBoundRule + PVCMountErrorRule
    ├── scheduling.go   — SchedulingFailureRule
    └── probe.go        — ProbeFailureRule
```

---

## Components and Interfaces

### DiagnosisRule Interface

```go
// package internal/diagnosis

// DiagnosisRule is the common interface for all diagnosis rules.
// Each implementation inspects a specific subset of the EvidenceSnapshot
// and returns zero or one DiagnosisFinding.
//
// Rules MUST be stateless — Evaluate may be called concurrently from tests.
// Rules MUST NOT call any external API or modify the snapshot.
type DiagnosisRule interface {
    // ID returns the stable rule identifier that appears in DiagnosisFinding.RuleID.
    // IDs must be unique across all registered rules.
    ID() string

    // Priority returns the rule's tie-breaking priority, used when multiple rules
    // match at the same confidence level. Lower value = higher priority (selected first).
    Priority() int

    // Evaluate inspects the evidence and returns a finding, or nil if this rule
    // does not match.
    Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding
}
```

### Confidence Constants

```go
// package internal/diagnosis

const (
    ConfidenceHigh   = "High"
    ConfidenceMedium = "Medium"
    ConfidenceLow    = "Low"
)
```

### DiagnosisEngine

```go
// package internal/diagnosis

// DiagnosisEngine evaluates a collected EvidenceSnapshot against all registered
// DiagnosisRules and returns a DiagnosisResult.
//
// The engine is stateless: each Evaluate call is independent.
type DiagnosisEngine struct {
    rules []DiagnosisRule
}

// NewDiagnosisEngine returns an engine pre-loaded with all MVP diagnosis rules
// in their default registration order.
func NewDiagnosisEngine() *DiagnosisEngine

// Evaluate runs all registered rules against the snapshot and returns the
// assembled DiagnosisResult.
//
// Evaluate panics if snapshot is nil — callers must guard against nil before calling.
// The controller skips calling Evaluate when the snapshot is nil (Requirement 1.2).
func (e *DiagnosisEngine) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.DiagnosisResult
```

### Engine Algorithm

The algorithm inside `Evaluate` is:

1. Call `rule.Evaluate(snapshot)` for every registered rule.
2. Collect all non-nil findings into a `matches` slice.
3. If `matches` is empty, return a `DiagnosisResult` with nil `Primary` and a populated `UnknownReason`.
4. Sort `matches` by confidence (High > Medium > Low), then by `rule.Priority()` as a tiebreaker.
5. The first element of the sorted slice is the `Primary` finding.
6. Remaining elements are classified:
   - If confidence == Primary's confidence → `AlternativeHypotheses`
   - If confidence < Primary's confidence → `ContributingFactors`
7. Set `RulesEvaluated` to `len(e.rules)`.
8. Set `EvaluatedAt` to `metav1.Now()`.
9. Return the assembled `DiagnosisResult`.

Confidence ordering for sorting:

```go
func confidenceRank(c string) int {
    switch c {
    case ConfidenceHigh:
        return 0
    case ConfidenceMedium:
        return 1
    case ConfidenceLow:
        return 2
    default:
        return 3
    }
}
```

---

## New API Types

### api/v1alpha1/diagnosis_types.go

```go
// DiagnosisResult holds the outcome of the diagnosis engine for one evaluation cycle.
//
// +kubebuilder:object:generate=true
type DiagnosisResult struct {
    // EvaluatedAt is the timestamp when diagnosis was last run.
    // +optional
    EvaluatedAt *metav1.Time `json:"evaluatedAt,omitempty"`

    // Primary is the highest-confidence matching diagnosis finding.
    // Nil when no rule matched (phase will be Unknown).
    // +optional
    Primary *DiagnosisFinding `json:"primary,omitempty"`

    // ContributingFactors lists other rules that fired alongside the primary finding
    // at a lower confidence level.
    // +optional
    ContributingFactors []DiagnosisFinding `json:"contributingFactors,omitempty"`

    // AlternativeHypotheses lists rules that matched at the same confidence level as the
    // primary finding, representing plausible alternative explanations.
    // +optional
    AlternativeHypotheses []DiagnosisFinding `json:"alternativeHypotheses,omitempty"`

    // RulesEvaluated is the count of rules that were evaluated.
    RulesEvaluated int `json:"rulesEvaluated"`

    // UnknownReason explains why no diagnosis was made when Primary is nil.
    // +optional
    UnknownReason string `json:"unknownReason,omitempty"`
}

// DiagnosisFinding represents a single rule match with its evidence-backed explanation.
//
// +kubebuilder:object:generate=true
type DiagnosisFinding struct {
    // RuleID is the identifier of the rule that produced this finding (e.g., "OOMMemoryLimit").
    RuleID string `json:"ruleID"`

    // Confidence is the qualitative strength of this finding.
    // +kubebuilder:validation:Enum=High;Medium;Low
    Confidence string `json:"confidence"`

    // Cause is a concise human-readable description of the identified root cause.
    Cause string `json:"cause"`

    // Explanation provides a longer evidence-backed description of why this finding was reached.
    Explanation string `json:"explanation"`

    // SupportingEvidence lists the specific evidence items that led to this finding.
    // Each entry is a short human-readable string.
    // +optional
    SupportingEvidence []string `json:"supportingEvidence,omitempty"`

    // Recommendation is a short, actionable suggestion tied directly to this finding.
    // Must be specific and evidence-backed; must not recommend automatic remediation.
    // +optional
    Recommendation string `json:"recommendation,omitempty"`
}
```

### Addition to IncidentReportStatus

```go
// Diagnosis holds the result of the diagnosis engine evaluation.
// Set after evidence collection; replaced on each reconciliation cycle.
// +optional
Diagnosis *DiagnosisResult `json:"diagnosis,omitempty"`
```

---

## Rule Implementations

### Rule Priority Table

Lower `Priority()` value wins the tiebreaker when two rules match at the same confidence level.

| Rule | RuleID | Confidence | Priority |
|------|--------|------------|----------|
| OOMMemoryLimitRule | `OOMMemoryLimit` | High | 10 |
| NodeMemoryPressureRule | `NodeMemoryPressure` | Medium | 20 |
| CrashLoopOOMExitRule | `CrashLoopOOMExit` | High | 11 |
| CrashLoopAppErrorRule | `CrashLoopAppError` | Medium | 21 |
| ImagePullFailureRule | `ImagePullFailure` | High | 12 |
| MissingConfigReferenceRule | `MissingConfigReference` | High | 13 |
| PVCNotBoundRule | `PVCNotBound` | High | 14 |
| PVCMountErrorRule | `PVCMountError` | Medium | 22 |
| SchedulingFailureRule | `SchedulingFailure` | High | 15 |
| ProbeFailureRule | `ProbeFailure` | Medium | 23 |

### Rule 1: OOMMemoryLimitRule (`oom.go`)

**Matching conditions (ALL must be true):**
- At least one container in `snapshot.Pod.Containers` has `TerminationReason == "OOMKilled"`
- That container's `ExitCode == 137`
- That container has a non-empty memory limit (`ResourceLimits["memory"] != ""`)
- `snapshot.Node` is nil OR `snapshot.Node` does not have a condition with `Type == "MemoryPressure"` and `Status == "True"`

**Returns:** Finding with `RuleID="OOMMemoryLimit"`, `Confidence="High"`.

**SupportingEvidence includes:**
- `"termination reason: OOMKilled"`
- `"exit code: 137"`
- `fmt.Sprintf("memory limit: %s", container.ResourceLimits["memory"])`

**Recommendation:**
> Investigate application memory consumption. If the workload legitimately requires more memory, consider increasing the container memory limit from the current value of `<limit>`.

### Rule 2: NodeMemoryPressureRule (`oom.go`)

**Matching conditions:**
- `snapshot.Node` has a condition with `Type == "MemoryPressure"` and `Status == "True"`
- AND (at least one container has `TerminationReason == "OOMKilled"` OR `snapshot.Pod` has trigger-type-level evidence of Eviction from the parent incident trigger)

The NodeMemoryPressure rule uses the trigger type from the snapshot's context: it inspects containers for OOMKilled termination reason, but also fires when no container termination is visible (e.g., Eviction trigger) as long as node memory pressure is confirmed.

**Implementation note:** The rule cannot directly read the `TriggerType` from the `IncidentReport`; instead it infers context from the `EvidenceSnapshot`. For Eviction detection with no container termination evidence, the rule fires solely on `MemoryPressure == True` when at least one pod container is in a terminated or waiting state with no other matching termination reason. In practice this means the controller should pass the trigger type as a hint if needed, or the rule uses a conservative match on memory pressure alone.

**Design decision:** To keep the rule a pure function over `EvidenceSnapshot`, the `EvidenceSnapshot` will be extended with a `TriggerType` string field set by the evidence collector. This avoids the rule needing to access the `IncidentReport` directly.

```go
// Addition to EvidenceSnapshot (defined in evidence_types.go):
// TriggerType is the classified failure signal from IncidentReport.Status.Trigger.Type.
// Set by the EvidenceOrchestrator so diagnosis rules can use it without accessing the report.
// +optional
TriggerType string `json:"triggerType,omitempty"`
```

**Returns:** Finding with `RuleID="NodeMemoryPressure"`, `Confidence="Medium"`.

**SupportingEvidence includes:**
- `"node memory pressure: True"`
- the termination reason or trigger type that corroborates the pressure

**Recommendation:**
> Check node-level memory utilisation. Review whether other workloads on the same node contributed to memory pressure. Consider node capacity or workload placement constraints.

### Rule 3: CrashLoopAppErrorRule (`crashloop.go`)

**Matching conditions (ALL must be true):**
- At least one container has `WaitingReason == "CrashLoopBackOff"`
- That container's last `ExitCode != 0`
- That container's last `ExitCode != 137`
- That container's last `TerminationReason != "OOMKilled"`

**Returns:** Finding with `RuleID="CrashLoopAppError"`, `Confidence="Medium"`.

**SupportingEvidence includes:**
- `"waiting reason: CrashLoopBackOff"`
- `fmt.Sprintf("last exit code: %d", exitCode)`
- `fmt.Sprintf("restart count: %d", restartCount)`

**Recommendation:**
> Examine container logs for application errors immediately preceding the crash. Check whether a recent configuration or dependency change preceded the failure.

### Rule 4: CrashLoopOOMExitRule (`crashloop.go`)

**Matching conditions (ANY must be true):**
- At least one container has `WaitingReason == "CrashLoopBackOff"` AND (`ExitCode == 137` OR `TerminationReason == "OOMKilled"`)

When this rule fires, `CrashLoopAppErrorRule` MUST NOT also fire for the same container. This is enforced by the matching condition in Rule 3 (`TerminationReason != "OOMKilled"` and `ExitCode != 137`), so the two rules are naturally mutually exclusive without requiring engine-level special-casing.

**Returns:** Finding with `RuleID="CrashLoopOOMExit"`, `Confidence="High"`.

**SupportingEvidence includes:**
- `"waiting reason: CrashLoopBackOff"`
- `"last exit code: 137"` or `"termination reason: OOMKilled"`
- `fmt.Sprintf("restart count: %d", restartCount)`

**Recommendation:**
> The container is repeatedly being killed due to memory exhaustion. Investigate application memory consumption. Consider increasing the memory limit. The persistent crash loop suggests the issue occurs consistently, not intermittently.

### Rule 5: ImagePullFailureRule (`imagepull.go`)

**Matching conditions (ALL must be true):**
- `snapshot.TriggerType == "ImagePullBackOff"` (from the added field)
- At least one container has `WaitingReason == "ImagePullBackOff"` OR `WaitingReason == "ErrImagePull"`

**Sub-cause logic in Explanation:**
- If `snapshot.Dependencies != nil && len(snapshot.Dependencies.ImagePullSecretNames) > 0`: note that pull secrets are configured and an auth failure is possible.
- Otherwise: note that no pull secrets are configured; if the registry is private, credentials may be required.

**Returns:** Finding with `RuleID="ImagePullFailure"`, `Confidence="High"`.

**SupportingEvidence includes:**
- `fmt.Sprintf("container: %s", containerName)`
- `fmt.Sprintf("image: %s", image)`
- `fmt.Sprintf("waiting reason: %s", waitingReason)`

**Recommendation:**
> Verify that the image reference is correct and the registry is reachable. If image pull secrets are configured, confirm the secret exists and contains valid credentials. If no secrets are configured, check whether the registry requires authentication.

### Rule 6: MissingConfigReferenceRule (`config.go`)

**Matching conditions (ANY must be true):**
- `snapshot.TriggerType == "CreateContainerConfigError"`
- OR at least one container has `WaitingReason == "CreateContainerConfigError"`

**Returns:** Finding with `RuleID="MissingConfigReference"`, `Confidence="High"`.

**SupportingEvidence includes:**
- `fmt.Sprintf("container: %s", containerName)`
- `fmt.Sprintf("waiting reason: CreateContainerConfigError")`

**Recommendation:**
> Verify that all ConfigMaps, Secrets, and environment variable sources referenced by the container spec exist in the same namespace as the Pod. Check for recently deleted or renamed configuration resources.

### Rule 7: PVCNotBoundRule (`storage.go`)

**Matching conditions (ALL must be true):**
- `snapshot.TriggerType == "MountFailure"`
- `snapshot.Dependencies != nil`
- At least one PVC in `snapshot.Dependencies.PVCs` has `Phase != "Bound"` (i.e., `Phase == "Pending"` or `Phase == "Lost"` or empty)

**Returns:** Finding with `RuleID="PVCNotBound"`, `Confidence="High"`.

**SupportingEvidence includes:**
- `fmt.Sprintf("pvc: %s", pvcName)`
- `fmt.Sprintf("pvc phase: %s", phase)`
- storage class name if non-empty: `fmt.Sprintf("storage class: %s", storageClass)`

**Recommendation:**
> Check the PVC status and the StorageClass provisioner configuration. Verify the cluster has available capacity to fulfil the storage request. If using dynamic provisioning, ensure the provisioner pod is running and healthy.

### Rule 8: PVCMountErrorRule (`storage.go`)

**Matching conditions (ALL must be true):**
- `snapshot.TriggerType == "MountFailure"`
- `snapshot.Dependencies != nil`
- At least one PVC in `snapshot.Dependencies.PVCs` has `Phase == "Bound"`
- `snapshot.Events` contains at least one event with `Reason == "FailedMount"`

**Natural priority over Rule 7:** A PVC with `Phase != "Bound"` will trigger Rule 7 (PVCNotBound) rather than Rule 8, because Rule 8's matching condition requires `Phase == "Bound"`. Both rules cannot fire for the same PVC.

**Returns:** Finding with `RuleID="PVCMountError"`, `Confidence="Medium"`.

**SupportingEvidence includes:**
- `fmt.Sprintf("pvc: %s (bound)", pvcName)`
- FailedMount event messages (first three, truncated to 256 chars each)
- node name if available from `snapshot.Pod.NodeName`

**Recommendation:**
> The PVC is bound but the mount operation is failing. Check CSI driver health, node conditions, and whether other Pods on the same node are experiencing mount failures. Review the kubelet logs on the affected node for additional context.

### Rule 9: SchedulingFailureRule (`scheduling.go`)

**Matching conditions (ALL must be true):**
- `snapshot.TriggerType == "SchedulingFailure"`
- `snapshot.Events` contains at least one event with `Reason == "FailedScheduling"`

**Sub-cause logic in Explanation:**
- If `snapshot.Dependencies.SchedulingConstraints.NodeSelector` is non-empty: note possible node label mismatch.
- If `snapshot.Dependencies.SchedulingConstraints.Tolerations` is empty and taint evidence is inferable: note possible taint/toleration mismatch.
- If `snapshot.Dependencies.SchedulingConstraints.ResourceRequests` is non-empty: note possible insufficient cluster resources.

**Returns:** Finding with `RuleID="SchedulingFailure"`, `Confidence="High"`.

**SupportingEvidence includes:**
- FailedScheduling event messages (first three, truncated to 256 chars each)
- Scheduling constraints from DependencyEvidence (node selector keys, resource request values)

**Recommendation:**
> Inspect node labels, taints, and available resource capacity. If a nodeSelector is configured, verify matching nodes exist. If resource requests are high, verify the cluster has nodes with sufficient available capacity.

### Rule 10: ProbeFailureRule (`probe.go`)

**Matching conditions (ALL must be true):**
- `snapshot.TriggerType == "ReadinessProbeFailure"` OR `snapshot.TriggerType == "LivenessProbeFailure"`
- `snapshot.Events` contains at least one event with `Reason == "Unhealthy"`

**Returns:** Finding with `RuleID="ProbeFailure"`, `Confidence="Medium"`.

**SupportingEvidence includes:**
- The trigger type (readiness or liveness)
- Probe summary from `PodEvidence.Containers[*]` (type, path or port, failureThreshold) if available
- Unhealthy event messages (first three, truncated to 256 chars each)

**Recommendation:**
> Verify the application is healthy and the probe endpoint is accessible. Review the probe `failureThreshold` and `periodSeconds` settings, especially if the application has a slow startup time. If this is a liveness probe failure, check whether the application is deadlocked or unresponsive.

---

## Data Models

### EvidenceSnapshot — TriggerType Addition

The `EvidenceSnapshot` type (defined in `api/v1alpha1/evidence_types.go` by the evidence-collection spec) requires one additional field to support pure-function rules:

```go
// TriggerType is the classified failure signal that initiated this investigation.
// Set by the EvidenceOrchestrator from IncidentReport.Status.Trigger.Type so that
// diagnosis rules can inspect it without accessing the IncidentReport directly.
// +optional
TriggerType string `json:"triggerType,omitempty"`
```

This field is populated by `EvidenceOrchestrator.Collect()` before the snapshot is passed to the `DiagnosisEngine`.

---

## Phase Transition Table

The controller is responsible for setting `IncidentReport.Status.Phase` after receiving the `DiagnosisResult`. The `DiagnosisEngine` itself does not set phase.

| EvidenceSnapshot | DiagnosisResult.Primary | Current Phase | New Phase |
|---|---|---|---|
| nil | — | Investigating | Investigating (unchanged) |
| non-nil | non-nil finding | Investigating | Diagnosed |
| non-nil | nil | Investigating | Unknown |
| non-nil | non-nil finding | Diagnosed | Diagnosed (unchanged) |
| non-nil | nil | Diagnosed | Unknown |
| non-nil | non-nil finding | Unknown | Diagnosed |
| non-nil | nil | Unknown | Unknown (unchanged) |
| any | any | Resolved | Resolved (unchanged, skip diagnosis) |

**Key rule:** The controller MUST NOT set phase if the current phase is `Resolved`.

---

## Integration with PodReconciler

The diagnosis step is inserted as **Step 9.6** in `PodReconciler.Reconcile()`, immediately after evidence collection (Step 9.5) and before the workload snapshot re-fetch (Step 10).

```go
// ---- Step 9.6: Run diagnosis engine ----
if report.Status.Evidence != nil {
    diagResult := r.DiagnosisEngine.Evaluate(report.Status.Evidence)
    if err := r.applyDiagnosis(ctx, report, diagResult); err != nil {
        return ctrl.Result{}, fmt.Errorf("applying diagnosis for %s: %w", report.Name, err)
    }
    // Re-fetch report to get updated resourceVersion after diagnosis patch.
    if err := r.Get(ctx, types.NamespacedName{Namespace: report.Namespace, Name: report.Name}, &updatedReport); err != nil {
        return ctrl.Result{}, fmt.Errorf("re-fetching report after diagnosis: %w", err)
    }
    report = &updatedReport
}
```

`applyDiagnosis` performs a single status patch that sets `Diagnosis` and updates `Phase`:

```go
func (r *PodReconciler) applyDiagnosis(
    ctx context.Context,
    report *v1alpha1.IncidentReport,
    result v1alpha1.DiagnosisResult,
) error {
    if report.Status.Phase == v1alpha1.PhaseResolved {
        return nil // never change a resolved incident
    }
    base := report.DeepCopy()
    report.Status.Diagnosis = &result
    if result.Primary != nil {
        report.Status.Phase = v1alpha1.PhaseDiagnosed
    } else {
        report.Status.Phase = v1alpha1.PhaseUnknown
    }
    return r.Status().Patch(ctx, report, client.MergeFrom(base))
}
```

### PodReconciler struct addition

```go
// DiagnosisEngine evaluates EvidenceSnapshots and produces DiagnosisResults.
// Injected at startup; must be non-nil when evidence collection is enabled.
DiagnosisEngine *diagnosis.DiagnosisEngine
```

### Registration in cmd/main.go

```go
diagEngine := diagnosis.NewDiagnosisEngine()

if err := (&controller.PodReconciler{
    // ...existing fields...
    DiagnosisEngine: diagEngine,
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "Pod")
    os.Exit(1)
}
```

---

## Error Handling

| Scenario | Behaviour |
|---|---|
| `snapshot` is nil | Skip diagnosis, leave phase as `Investigating` |
| `snapshot.Pod` is nil (pod layer failed to collect) | Rules that require pod evidence return nil; Unknown outcome likely |
| All rules return nil | Return Unknown outcome with explanatory `UnknownReason` |
| `snapshot.Node` is nil | Rules that inspect node conditions treat nil node as "no pressure conditions present" |
| `snapshot.Dependencies` is nil | Rules that inspect dependencies treat nil as "no dependency evidence"; they may return nil or lower-confidence findings |
| Status patch fails | Return error to controller; requeue will re-attempt diagnosis on next cycle |
| DiagnosisEngine panics (programming error) | Let panic propagate — controller-runtime will log and requeue |

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Determinism

*For any* `EvidenceSnapshot`, evaluating it twice with the `DiagnosisEngine` produces `DiagnosisResult` values that are structurally identical in all fields except `EvaluatedAt`.

**Validates: Requirements 4.3, 18.1**

---

### Property 2: Phase Correctness on Match

*For any* `EvidenceSnapshot` that causes at least one rule to produce a non-nil finding, the resulting `DiagnosisResult` has a non-nil `Primary` field, and the controller transitions the phase to `Diagnosed`.

**Validates: Requirements 1.1, 3.1**

---

### Property 3: Phase Correctness on No Match

*For any* `EvidenceSnapshot` that causes zero rules to produce a finding, the resulting `DiagnosisResult` has a nil `Primary` field and a non-empty `UnknownReason`, and the controller transitions the phase to `Unknown`.

**Validates: Requirements 3.2, 15.1, 15.2**

---

### Property 4: Resolved Phase Immutability

*For any* `IncidentReport` with `Phase == Resolved`, running the diagnosis engine and calling `applyDiagnosis` leaves the phase unchanged at `Resolved`.

**Validates: Requirements 3.3**

---

### Property 5: Confidence Values Are Bounded

*For any* `DiagnosisResult` returned by the engine, every `DiagnosisFinding` in `Primary`, `ContributingFactors`, and `AlternativeHypotheses` has a `Confidence` value that is exactly one of `"High"`, `"Medium"`, or `"Low"`.

**Validates: Requirements 16.1**

---

### Property 6: Primary Excluded from Contributing and Alternative Lists

*For any* `DiagnosisResult` with a non-nil `Primary`, the `Primary.RuleID` does not appear in any element of `ContributingFactors` or `AlternativeHypotheses`.

**Validates: Requirements 17.4, 17.5**

---

### Property 7: Primary Recommendation Is Non-Empty

*For any* `DiagnosisResult` with a non-nil `Primary`, the `Primary.Recommendation` field is a non-empty string.

**Validates: Requirements 19.1, 19.2**

---

### Property 8: OOMMemoryLimit Rule Does Not Fire Under Node Pressure

*For any* `EvidenceSnapshot` where a container has `TerminationReason == "OOMKilled"` AND exit code 137 AND a non-empty memory limit AND the node has `MemoryPressure == True`, the `DiagnosisResult` does NOT contain a finding with `RuleID == "OOMMemoryLimit"` as the `Primary` finding.

**Validates: Requirements 5.5, 6.5**

---

### Property 9: CrashLoop Rules Are Mutually Exclusive

*For any* `EvidenceSnapshot` where any container has `WaitingReason == "CrashLoopBackOff"` AND (`ExitCode == 137` OR `TerminationReason == "OOMKilled"`), the `DiagnosisResult` does NOT contain a finding with `RuleID == "CrashLoopAppError"`.

**Validates: Requirements 8.5**

---

### Property 10: PVC Rules Are Mutually Exclusive Per PVC

*For any* `EvidenceSnapshot` with trigger type `MountFailure` where a PVC has `Phase != "Bound"`, the `DiagnosisResult` does NOT contain a finding with `RuleID == "PVCMountError"` for that PVC as the `Primary` finding.

**Validates: Requirements 12.5**

---

### Property 11: RulesEvaluated Count Is Correct

*For any* `DiagnosisResult` returned by the engine, `RulesEvaluated` equals the number of rules registered in the engine.

**Validates: Requirements 2.2, 15.3**

---

## Testing Strategy

### Unit Tests

Unit tests cover specific examples and edge cases for each rule. They verify:

- Each rule fires exactly on the documented conditions
- Each rule does NOT fire when one required condition is absent
- Rules that are mutually exclusive do not both fire on the same snapshot
- Nil sub-fields (nil Pod, nil Node, nil Dependencies) are handled without panics
- The engine selects the correct primary when multiple rules match
- The engine produces the Unknown outcome for a minimal/empty snapshot
- `applyDiagnosis` correctly sets Phase to `Diagnosed`, `Unknown`, or leaves `Resolved` unchanged

Unit tests live in `test/unit/diagnosis_*_test.go`.

### Property-Based Tests

Property-based tests use `pgregory.net/rapid` (consistent with the evidence-collection spec test strategy). Each property corresponds to a numbered property in this document. Each test generates 100+ random inputs and verifies the stated property.

Test configuration tags follow the pattern:
`Feature: diagnosis-engine, Property N: <property-text>`

Property tests live in `test/unit/diagnosis_properties_test.go`.

### Integration Tests

Integration tests use `envtest` and verify the full reconciliation path:

- An OOMKilled Pod incident reaches `Phase == Diagnosed` with `Primary.RuleID == "OOMMemoryLimit"` after evidence collection and diagnosis.
- A Pod with no matching failure signals reaches `Phase == Unknown`.
- A `Resolved` incident is not re-diagnosed.

Integration tests live in `test/integration/diagnosis_integration_test.go`.
