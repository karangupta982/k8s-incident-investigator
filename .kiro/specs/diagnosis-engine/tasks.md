# Implementation Plan: Diagnosis Engine

## Overview

This plan implements the diagnosis engine for the Kubernetes Incident Investigator. It adds new API types for diagnosis storage, a modular rules engine in `internal/diagnosis/`, a `TriggerType` field addition to `EvidenceSnapshot`, wiring into `PodReconciler` as Step 9.6, and a full test suite. Tasks build incrementally: API types and interfaces first, then each rule with its unit test, then engine-level tests, then controller wiring and integration tests.

The implementation language is **Go**, consistent with the rest of the project.

---

## Tasks

- [ ] 1. Define diagnosis API types and add Diagnosis field to IncidentReportStatus
  - Create `api/v1alpha1/diagnosis_types.go` with all diagnosis types as defined in the design: `DiagnosisResult`, `DiagnosisFinding`
  - Add `+kubebuilder:object:generate=true` markers to both struct types
  - Add `+kubebuilder:validation:Enum=High;Medium;Low` marker to `DiagnosisFinding.Confidence`
  - Add `Diagnosis *DiagnosisResult` field to `IncidentReportStatus` in `api/v1alpha1/incidentreport_types.go`
  - Ensure all optional fields carry `// +optional` markers per design
  - _Requirements: 2.1, 2.2, 2.3, 16.1_

- [ ] 2. Add TriggerType field to EvidenceSnapshot
  - Add `TriggerType string` field with `// +optional` and `json:"triggerType,omitempty"` to `EvidenceSnapshot` in `api/v1alpha1/evidence_types.go`
  - Update `EvidenceOrchestrator.Collect()` in `internal/evidence/collector.go` to set `snapshot.TriggerType = string(report.Status.Trigger.Type)` before returning the snapshot
  - This field is required so diagnosis rules remain pure functions over EvidenceSnapshot without accessing the IncidentReport
  - _Requirements: 4.1, 4.2, 4.4, 20.5_

- [ ] 3. Run code generation and update CRD manifests
  - [ ] 3.1 Regenerate DeepCopy methods
    - Run `make generate` to regenerate `zz_generated.deepcopy.go` for `DiagnosisResult` and `DiagnosisFinding`
    - Verify the generated file compiles without errors
    - _Requirements: 2.1_
  - [ ] 3.2 Regenerate CRD manifests
    - Run `make manifests` to regenerate `config/crd/bases/investigation.k8s.io_incidentreports.yaml` with the new `diagnosis` field under `status`
    - Verify the `diagnosis` field appears in the CRD schema with the `confidence` enum constraint
    - _Requirements: 2.1_

- [ ] 4. Create diagnosis package foundation — DiagnosisRule interface and DiagnosisEngine
  - [ ] 4.1 Create `internal/diagnosis/doc.go`
    - Add package documentation comment explaining that this package implements a pure-function diagnosis engine and must NOT import controller, investigation, or evidence packages
    - _Requirements: 20.1, 20.2, 20.3_
  - [ ] 4.2 Create `internal/diagnosis/rule.go`
    - Define `DiagnosisRule` interface with `ID() string`, `Priority() int`, and `Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding` methods
    - Define exported constants `ConfidenceHigh = "High"`, `ConfidenceMedium = "Medium"`, `ConfidenceLow = "Low"`
    - Define `confidenceRank(c string) int` helper used by the engine for sorting
    - _Requirements: 16.1, 4.1_
  - [ ] 4.3 Create `internal/diagnosis/engine.go`
    - Implement `DiagnosisEngine` struct with `rules []DiagnosisRule` field
    - Implement `NewDiagnosisEngine() *DiagnosisEngine` that registers all 10 MVP rules in the order defined in the design priority table
    - Implement `Evaluate(snapshot *v1alpha1.EvidenceSnapshot) v1alpha1.DiagnosisResult` using the algorithm in the design: call all rules, collect non-nil findings, sort by confidence then priority, assign Primary/ContributingFactors/AlternativeHypotheses, set RulesEvaluated and EvaluatedAt, return UnknownReason when no match
    - Engine must not modify the snapshot
    - _Requirements: 1.1, 1.5, 4.3, 4.4, 15.1, 15.3, 17.1, 17.2, 17.3_
  - [ ]* 4.4 Write unit tests for DiagnosisEngine core logic
    - Test: empty rules slice → Unknown result with RulesEvaluated=0
    - Test: single matching rule → Primary set, ContributingFactors and AlternativeHypotheses empty
    - Test: two matching rules different confidence → higher-confidence is Primary, lower is ContributingFactor
    - Test: two matching rules same confidence → first by priority is Primary, second is AlternativeHypothesis
    - Test: Primary RuleID never appears in ContributingFactors or AlternativeHypotheses (Property 6)
    - Test: RulesEvaluated equals the number of registered rules (Property 11)
    - Test: EvaluatedAt is set and non-nil
    - _Requirements: 15.1, 15.3, 17.1, 17.2, 17.3, 17.4, 17.5_

- [ ] 5. Checkpoint — ensure existing tests still pass
  - Run `make test` to confirm the existing test suite compiles and passes with the new types and the `EvidenceSnapshot.TriggerType` field addition
  - Resolve any compile errors from the new `Diagnosis` field on `IncidentReportStatus`

- [ ] 6. Implement OOM and Node Memory Pressure rules
  - [ ] 6.1 Create `internal/diagnosis/rules/oom.go`
    - Implement `OOMMemoryLimitRule` with `ID()="OOMMemoryLimit"`, `Priority()=10`
    - Match conditions: container TerminationReason=="OOMKilled" AND ExitCode==137 AND memory limit non-empty AND node does NOT have MemoryPressure=True
    - Populate SupportingEvidence with termination reason, exit code, and memory limit value
    - Implement `NodeMemoryPressureRule` with `ID()="NodeMemoryPressure"`, `Priority()=20`
    - Match conditions: node has MemoryPressure=True AND (any container has TerminationReason=="OOMKilled" OR TriggerType=="Eviction")
    - Handle nil snapshot.Node and nil snapshot.Pod gracefully (return nil finding)
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 6.1, 6.2, 6.3, 6.4_
  - [ ]* 6.2 Write unit tests for OOM rules
    - Test OOMMemoryLimitRule fires: OOMKilled + exit 137 + memory limit + no node pressure → High confidence
    - Test OOMMemoryLimitRule does NOT fire: node has MemoryPressure=True → returns nil
    - Test OOMMemoryLimitRule does NOT fire: ExitCode is not 137 → returns nil
    - Test OOMMemoryLimitRule does NOT fire: memory limit is empty → returns nil
    - Test NodeMemoryPressureRule fires: OOMKilled container + MemoryPressure=True → Medium confidence
    - Test NodeMemoryPressureRule fires: TriggerType==Eviction + MemoryPressure=True → Medium confidence
    - Test NodeMemoryPressureRule does NOT fire: MemoryPressure=False → returns nil
    - Test nil Pod and nil Node inputs do not panic
    - _Requirements: 5.1, 5.5, 6.1_

- [ ] 7. Implement CrashLoop rules
  - [ ] 7.1 Create `internal/diagnosis/rules/crashloop.go`
    - Implement `CrashLoopOOMExitRule` with `ID()="CrashLoopOOMExit"`, `Priority()=11`
    - Match conditions: container WaitingReason=="CrashLoopBackOff" AND (ExitCode==137 OR TerminationReason=="OOMKilled")
    - Implement `CrashLoopAppErrorRule` with `ID()="CrashLoopAppError"`, `Priority()=21`
    - Match conditions: container WaitingReason=="CrashLoopBackOff" AND ExitCode!=0 AND ExitCode!=137 AND TerminationReason!="OOMKilled"
    - Mutual exclusion is enforced by matching conditions, not engine-level special-casing
    - Handle nil snapshot.Pod gracefully
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 8.1, 8.2, 8.3, 8.4, 8.5_
  - [ ]* 7.2 Write unit tests for CrashLoop rules
    - Test CrashLoopOOMExitRule fires: CrashLoopBackOff + ExitCode=137 → High confidence
    - Test CrashLoopOOMExitRule fires: CrashLoopBackOff + TerminationReason=OOMKilled → High confidence
    - Test CrashLoopAppErrorRule fires: CrashLoopBackOff + ExitCode=1 (non-OOM) → Medium confidence
    - Test CrashLoopAppErrorRule does NOT fire: CrashLoopBackOff + ExitCode=137 → returns nil (Rule 8 covers this)
    - Test CrashLoopAppErrorRule does NOT fire: CrashLoopBackOff + TerminationReason=OOMKilled → returns nil
    - Test neither rule fires for CrashLoopBackOff + ExitCode=0 (edge case: normal exit in loop)
    - _Requirements: 7.1, 8.1, 8.5_

- [ ] 8. Implement ImagePull and MissingConfig rules
  - [ ] 8.1 Create `internal/diagnosis/rules/imagepull.go`
    - Implement `ImagePullFailureRule` with `ID()="ImagePullFailure"`, `Priority()=12`
    - Match conditions: TriggerType=="ImagePullBackOff" AND container WaitingReason is "ImagePullBackOff" or "ErrImagePull"
    - Include sub-cause logic in Explanation based on ImagePullSecretNames presence
    - Handle nil snapshot.Pod and nil snapshot.Dependencies gracefully
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5_
  - [ ] 8.2 Create `internal/diagnosis/rules/config.go`
    - Implement `MissingConfigReferenceRule` with `ID()="MissingConfigReference"`, `Priority()=13`
    - Match conditions: TriggerType=="CreateContainerConfigError" OR container WaitingReason=="CreateContainerConfigError"
    - Handle nil snapshot.Pod gracefully
    - _Requirements: 10.1, 10.2, 10.3, 10.4_
  - [ ]* 8.3 Write unit tests for ImagePull and MissingConfig rules
    - Test ImagePullFailureRule fires: correct trigger + ErrImagePull waiting reason, no pull secrets → correct sub-cause explanation
    - Test ImagePullFailureRule fires: correct trigger + ImagePullBackOff, with pull secrets → auth failure noted in explanation
    - Test ImagePullFailureRule does NOT fire: trigger is OOMKilled → returns nil
    - Test MissingConfigReferenceRule fires: trigger-based detection
    - Test MissingConfigReferenceRule fires: waiting-reason-based detection
    - Test nil snapshot.Pod does not panic for both rules
    - _Requirements: 9.1, 9.2, 9.3, 10.1_

- [ ] 9. Implement Storage rules
  - [ ] 9.1 Create `internal/diagnosis/rules/storage.go`
    - Implement `PVCNotBoundRule` with `ID()="PVCNotBound"`, `Priority()=14`
    - Match conditions: TriggerType=="MountFailure" AND Dependencies non-nil AND at least one PVC with Phase!="Bound"
    - Implement `PVCMountErrorRule` with `ID()="PVCMountError"`, `Priority()=22`
    - Match conditions: TriggerType=="MountFailure" AND Dependencies non-nil AND at least one PVC with Phase=="Bound" AND Events contain FailedMount reason
    - Include FailedMount event messages (first three, truncated to 256 chars) in SupportingEvidence
    - Handle nil snapshot.Dependencies and nil snapshot.Events gracefully
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 12.1, 12.2, 12.3, 12.4, 12.5_
  - [ ]* 9.2 Write unit tests for Storage rules
    - Test PVCNotBoundRule fires: MountFailure trigger + PVC Phase=Pending → High confidence
    - Test PVCNotBoundRule fires: MountFailure trigger + PVC Phase=Lost → High confidence
    - Test PVCNotBoundRule does NOT fire: PVC Phase=Bound → returns nil
    - Test PVCMountErrorRule fires: MountFailure + Phase=Bound + FailedMount events → Medium confidence
    - Test PVCMountErrorRule does NOT fire: no FailedMount events → returns nil
    - Test that when PVC is Pending, PVCNotBound fires and PVCMountError does NOT fire for the same snapshot (mutual exclusion)
    - Test nil Dependencies does not panic
    - _Requirements: 11.1, 12.1, 12.5_

- [ ] 10. Implement Scheduling and Probe rules
  - [ ] 10.1 Create `internal/diagnosis/rules/scheduling.go`
    - Implement `SchedulingFailureRule` with `ID()="SchedulingFailure"`, `Priority()=15`
    - Match conditions: TriggerType=="SchedulingFailure" AND Events contain FailedScheduling reason
    - Include sub-cause logic for nodeSelector, tolerations, and resource requests from SchedulingConstraints
    - Include FailedScheduling event messages (first three, truncated to 256 chars) in SupportingEvidence
    - Handle nil snapshot.Events and nil snapshot.Dependencies gracefully
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5, 13.6_
  - [ ] 10.2 Create `internal/diagnosis/rules/probe.go`
    - Implement `ProbeFailureRule` with `ID()="ProbeFailure"`, `Priority()=23`
    - Match conditions: (TriggerType=="ReadinessProbeFailure" OR TriggerType=="LivenessProbeFailure") AND Events contain Unhealthy reason
    - Include probe summary from PodEvidence.Containers if available
    - Include Unhealthy event messages (first three, truncated to 256 chars) in SupportingEvidence
    - Handle nil snapshot.Events and nil snapshot.Pod gracefully
    - _Requirements: 14.1, 14.2, 14.3, 14.4_
  - [ ]* 10.3 Write unit tests for Scheduling and Probe rules
    - Test SchedulingFailureRule fires: correct trigger + FailedScheduling events, with nodeSelector → nodeSelector noted in explanation
    - Test SchedulingFailureRule fires: correct trigger + FailedScheduling events, with resource requests → resource requests noted
    - Test SchedulingFailureRule does NOT fire: no FailedScheduling events → returns nil
    - Test ProbeFailureRule fires: ReadinessProbeFailure trigger + Unhealthy events → Medium confidence
    - Test ProbeFailureRule fires: LivenessProbeFailure trigger + Unhealthy events → Medium confidence
    - Test ProbeFailureRule does NOT fire: Unhealthy events but trigger is OOMKilled → returns nil
    - _Requirements: 13.1, 14.1_

- [ ] 11. Checkpoint — run full unit test suite
  - Run `make test` to confirm all rule unit tests pass
  - Verify no rule imports `internal/controller`, `internal/investigation`, or `internal/evidence`
  - Run `go vet ./internal/diagnosis/...` to check for any code quality issues

- [ ] 12. Write property-based tests for the diagnosis engine
  - [ ]* 12.1 Write property test: Determinism (Property 1)
    - **Property 1: Determinism**
    - **Validates: Requirements 4.3, 18.1**
    - Use `pgregory.net/rapid` to generate random `EvidenceSnapshot` structs
    - For each generated snapshot, call `engine.Evaluate(snapshot)` twice and assert structural equality (excluding `EvaluatedAt`)
    - Tag: `Feature: diagnosis-engine, Property 1: determinism`
  - [ ]* 12.2 Write property test: Phase Correctness on Match (Property 2)
    - **Property 2: Phase Correctness on Match**
    - **Validates: Requirements 1.1, 3.1**
    - Generate snapshots that are guaranteed to trigger at least one rule (e.g., OOMKilled container, exit code 137, memory limit set, no node pressure)
    - Verify `result.Primary != nil`
    - Tag: `Feature: diagnosis-engine, Property 2: phase-correctness-on-match`
  - [ ]* 12.3 Write property test: Phase Correctness on No Match (Property 3)
    - **Property 3: Phase Correctness on No Match**
    - **Validates: Requirements 3.2, 15.1, 15.2**
    - Generate snapshots with no failure signals (all containers running healthy, no relevant waiting reasons, no relevant events)
    - Verify `result.Primary == nil` and `result.UnknownReason != ""`
    - Tag: `Feature: diagnosis-engine, Property 3: phase-correctness-no-match`
  - [ ]* 12.4 Write property test: Confidence Values Bounded (Property 5)
    - **Property 5: Confidence Values Are Bounded**
    - **Validates: Requirements 16.1**
    - For any snapshot (random generation), for all findings in Primary, ContributingFactors, AlternativeHypotheses, assert Confidence ∈ {"High", "Medium", "Low"}
    - Tag: `Feature: diagnosis-engine, Property 5: confidence-values-bounded`
  - [ ]* 12.5 Write property test: Primary Excluded from Lists (Property 6)
    - **Property 6: Primary Excluded from Contributing and Alternative Lists**
    - **Validates: Requirements 17.4, 17.5**
    - For any snapshot where result.Primary != nil, assert Primary.RuleID does not appear in ContributingFactors or AlternativeHypotheses
    - Tag: `Feature: diagnosis-engine, Property 6: primary-excluded-from-lists`
  - [ ]* 12.6 Write property test: Primary Recommendation Non-Empty (Property 7)
    - **Property 7: Primary Recommendation Is Non-Empty**
    - **Validates: Requirements 19.1, 19.2**
    - For any snapshot where result.Primary != nil, assert Primary.Recommendation != ""
    - Tag: `Feature: diagnosis-engine, Property 7: primary-recommendation-non-empty`
  - [ ]* 12.7 Write property test: OOMMemoryLimit Does Not Fire Under Node Pressure (Property 8)
    - **Property 8: OOMMemoryLimit Rule Does Not Fire Under Node Pressure**
    - **Validates: Requirements 5.5, 6.5**
    - Generate snapshots with OOMKilled + exit 137 + memory limit AND MemoryPressure=True
    - Assert result.Primary.RuleID != "OOMMemoryLimit" (or Primary is nil)
    - Tag: `Feature: diagnosis-engine, Property 8: oom-limit-no-fire-under-node-pressure`
  - [ ]* 12.8 Write property test: CrashLoop Rules Mutually Exclusive (Property 9)
    - **Property 9: CrashLoop Rules Are Mutually Exclusive**
    - **Validates: Requirements 8.5**
    - Generate CrashLoopBackOff snapshots with exit code 137 or OOMKilled termination
    - Assert no finding with RuleID == "CrashLoopAppError" appears anywhere in the result
    - Tag: `Feature: diagnosis-engine, Property 9: crashloop-rules-mutually-exclusive`
  - [ ]* 12.9 Write property test: RulesEvaluated Count Correct (Property 11)
    - **Property 11: RulesEvaluated Count Is Correct**
    - **Validates: Requirements 2.2, 15.3**
    - For any snapshot, assert result.RulesEvaluated equals len(engine.rules) (10 for the MVP engine)
    - Tag: `Feature: diagnosis-engine, Property 11: rules-evaluated-count-correct`

- [ ] 13. Wire DiagnosisEngine into PodReconciler
  - [ ] 13.1 Add DiagnosisEngine field to PodReconciler
    - Add `DiagnosisEngine *diagnosis.DiagnosisEngine` field to `PodReconciler` struct in `internal/controller/pod_reconciler.go`
    - Add import for `internal/diagnosis`
    - _Requirements: 1.1, 1.2, 4.1_
  - [ ] 13.2 Implement applyDiagnosis helper
    - Add `applyDiagnosis(ctx, report, result) error` method to `PodReconciler` in `internal/controller/pod_reconciler.go`
    - Guard: if `report.Status.Phase == PhaseResolved`, return nil without patching
    - Patch `Status.Diagnosis = &result`
    - Set `Status.Phase = PhaseDiagnosed` when `result.Primary != nil`
    - Set `Status.Phase = PhaseUnknown` when `result.Primary == nil`
    - Use `r.Status().Patch(ctx, report, client.MergeFrom(base))`
    - _Requirements: 1.3, 3.1, 3.2, 3.3_
  - [ ] 13.3 Insert Step 9.6 in Reconcile()
    - After evidence collection (Step 9.5, after the `r.Status().Patch` for evidence and re-fetch), add Step 9.6:
    - Guard: skip if `report.Status.Evidence == nil`
    - Call `r.DiagnosisEngine.Evaluate(report.Status.Evidence)`
    - Call `r.applyDiagnosis(ctx, report, diagResult)`
    - Re-fetch `report` from the API after the diagnosis patch (for correct resourceVersion before recovery eval)
    - _Requirements: 1.1, 1.2, 1.3, 4.1_
  - [ ] 13.4 Register DiagnosisEngine in cmd/main.go
    - Import `internal/diagnosis` in `cmd/main.go`
    - Create `diagEngine := diagnosis.NewDiagnosisEngine()` before controller setup
    - Pass `DiagnosisEngine: diagEngine` when constructing `PodReconciler`
    - _Requirements: 1.1_
  - [ ]* 13.5 Write unit tests for applyDiagnosis
    - Test: Primary non-nil, Phase=Investigating → Phase becomes Diagnosed, Diagnosis field set
    - Test: Primary nil, Phase=Investigating → Phase becomes Unknown, Diagnosis.UnknownReason non-empty
    - Test: Phase=Resolved → Phase stays Resolved regardless of diagnosis result
    - Test: Primary non-nil, Phase=Unknown → Phase becomes Diagnosed (re-diagnosis path)
    - _Requirements: 3.1, 3.2, 3.3, 3.5_

- [ ] 14. Write integration test — OOMKilled incident reaches Diagnosed phase
  - Create `test/integration/diagnosis_integration_test.go`
  - Using envtest, create a fake Pod with OOMKilled container state (TerminationReason=OOMKilled, ExitCode=137, memory limit set)
  - Trigger reconciliation and verify:
    1. An `IncidentReport` is created
    2. Evidence is collected (snapshot.Pod is non-nil)
    3. Diagnosis runs and `Status.Diagnosis.Primary.RuleID == "OOMMemoryLimit"`
    4. `Status.Phase == "Diagnosed"`
  - Also test the Unknown path: create a Pod with a normal running state to verify Phase=Unknown after diagnosis
  - _Requirements: 1.1, 1.3, 3.1, 3.2_

- [ ] 15. Final checkpoint — full test suite and code quality
  - Run `make test` to confirm all unit, property, and integration tests pass
  - Run `make lint` or `go vet ./...` to check for any code quality issues
  - Run `make manifests` to confirm the updated CRD YAML is committed
  - Verify `internal/diagnosis` does not import `internal/controller`, `internal/investigation`, or `internal/evidence` by running `go list -f '{{ .Imports }}' ./internal/diagnosis/...`

---

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP implementation, though they are strongly recommended for catching correctness issues in the rules
- Each property test uses `pgregory.net/rapid` (already a dependency from the evidence-collection spec) with a minimum of 100 iterations
- The `TriggerType` field addition to `EvidenceSnapshot` (Task 2) requires the evidence-collection spec to be implemented first, or at minimum the `EvidenceSnapshot` type to be defined
- The `applyDiagnosis` method performs a single status patch; this replaces the Diagnosis field and updates Phase atomically from the controller's perspective
- Rule mutual exclusion (CrashLoop, PVC rules) is achieved naturally through matching conditions rather than engine-level special-casing
- When `snapshot.Node` is nil (node layer failed to collect), OOMMemoryLimitRule treats it as "no pressure present" and can still fire at High confidence — this is the correct conservative choice because absence of node evidence should not suppress a direct OOM signal

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2"] },
    { "id": 1, "tasks": ["3.1", "3.2"] },
    { "id": 2, "tasks": ["4.1", "4.2"] },
    { "id": 3, "tasks": ["4.3"] },
    { "id": 4, "tasks": ["4.4", "6.1", "7.1", "8.1", "8.2", "9.1", "10.1", "10.2"] },
    { "id": 5, "tasks": ["6.2", "7.2", "8.3", "9.2", "10.3"] },
    { "id": 6, "tasks": ["12.1", "12.2", "12.3", "12.4", "12.5", "12.6", "12.7", "12.8", "12.9"] },
    { "id": 7, "tasks": ["13.1"] },
    { "id": 8, "tasks": ["13.2", "13.3", "13.4"] },
    { "id": 9, "tasks": ["13.5"] },
    { "id": 10, "tasks": ["14"] }
  ]
}
```
