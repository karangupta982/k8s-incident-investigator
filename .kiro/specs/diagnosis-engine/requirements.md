# Requirements Document

## Introduction

The diagnosis engine feature adds structured, deterministic interpretation of collected evidence to the Kubernetes Incident Investigator. Once the evidence collection pipeline populates an `EvidenceSnapshot` on an `IncidentReport`, the diagnosis engine evaluates that evidence against a set of modular rules and produces a `DiagnosisResult` stored in the `IncidentReport` status.

The engine is a pure function over the `EvidenceSnapshot`: it calls no Kubernetes API, depends on no external systems, and requires no probabilistic models or LLMs. Every finding is traceable to specific evidence items. When the evidence is insufficient to reach a conclusion, the engine records an explicit `Unknown` outcome rather than fabricating a cause.

The `DiagnosisResult` enables `IncidentReport` phase transitions from `Investigating` to `Diagnosed` (when a rule matches) or `Unknown` (when no rule matches but evidence is sufficient). These are the two phases defined in the foundation specification that currently go unused.

---

## Glossary

### DiagnosisEngine

The Go component in `internal/diagnosis/` that evaluates a `EvidenceSnapshot` against all registered `DiagnosisRule` implementations and returns a `DiagnosisResult`.

### DiagnosisRule

A single, independently testable unit of diagnosis logic. Each rule inspects a specific subset of the `EvidenceSnapshot` and returns zero or one `DiagnosisFinding` depending on whether the evidence matches the rule's conditions.

### DiagnosisResult

The top-level output of the `DiagnosisEngine` for one evaluation cycle. It contains a primary finding, contributing factors, alternative hypotheses, and an explanation when no finding was reached.

### DiagnosisFinding

A structured record produced by a single `DiagnosisRule`. It contains a rule identifier, confidence level, cause description, evidence-backed explanation, supporting evidence list, and a recommendation.

### Confidence Level

A qualitative assessment of how strongly the collected evidence supports a finding. Confidence is `High`, `Medium`, or `Low`. Confidence is never a statistical probability; it reflects the directness and completeness of the supporting evidence.

### Primary Finding

The single `DiagnosisFinding` with the highest confidence level among all matched rules. When multiple rules match at the same confidence level, the engine selects based on rule priority order.

### Contributing Factor

A `DiagnosisFinding` from a rule that matched alongside the primary finding but is not the primary cause. Contributing factors explain additional conditions that may have influenced the incident.

### Alternative Hypothesis

A `DiagnosisFinding` from a rule that matched at the same or similar confidence level as the primary finding, representing a plausible alternative explanation that the evidence does not conclusively rule out.

### Unknown Outcome

The result when no `DiagnosisRule` matches the collected evidence. `Unknown` is a valid and explicit outcome. The engine records why no conclusion was reached.

### RuleID

A stable, human-readable identifier for a `DiagnosisRule`. Rule IDs are used in `DiagnosisFinding.RuleID` and appear in the `IncidentReport` status to identify which rule produced each finding.

---

## Requirements

### Requirement 1: Run Diagnosis After Evidence Collection

**User Story:** As a Platform Engineer, I want diagnosis to run automatically after evidence is collected so that the `IncidentReport` is updated with a structured interpretation without manual intervention.

#### Acceptance Criteria

1. WHEN an `EvidenceSnapshot` is present and non-nil in the `IncidentReport` status, THE DiagnosisEngine SHALL evaluate the snapshot against all registered rules during the same reconciliation cycle in which evidence collection completed.

2. WHEN the `EvidenceSnapshot` is nil or absent (evidence has not yet been collected), THE DiagnosisEngine SHALL skip diagnosis for the current reconciliation cycle and SHALL NOT modify the `Diagnosis` field.

3. WHEN diagnosis evaluation completes, THE Investigator SHALL store the resulting `DiagnosisResult` in `IncidentReport.Status.Diagnosis`, replacing any previously stored result.

4. WHEN a subsequent reconciliation refreshes the `EvidenceSnapshot`, THE DiagnosisEngine SHALL re-evaluate the updated evidence and replace the previous `DiagnosisResult` with the new result.

5. THE DiagnosisEngine SHALL record the timestamp at which evaluation ran in `DiagnosisResult.EvaluatedAt`.

---

### Requirement 2: Store DiagnosisResult in IncidentReport Status

**User Story:** As an engineer investigating an incident, I want the diagnosis result to appear in the `IncidentReport` status so that I can read the primary cause, confidence level, and recommendations with `kubectl describe incidentreport`.

#### Acceptance Criteria

1. THE Investigator SHALL store the `DiagnosisResult` in `IncidentReport.Status.Diagnosis` via the status subresource.

2. THE `DiagnosisResult` SHALL contain: `EvaluatedAt` timestamp, `RulesEvaluated` count, and at least one of `Primary` finding or `UnknownReason` string.

3. WHEN a `Primary` finding is present, THE `DiagnosisResult` SHALL also carry zero or more `ContributingFactors` and zero or more `AlternativeHypotheses`.

4. WHEN no primary finding is present, THE `DiagnosisResult.UnknownReason` SHALL contain a non-empty string explaining why no diagnosis was reached.

5. THE Investigator SHALL NOT store `DiagnosisResult` fields containing Secret values, environment variable values, or raw log content.

---

### Requirement 3: Transition Phase Based on Diagnosis Outcome

**User Story:** As a Platform Engineer, I want the `IncidentReport` phase to reflect the diagnosis outcome so that I can distinguish between incidents that have been diagnosed and those that remain unexplained.

#### Acceptance Criteria

1. WHEN the `DiagnosisEngine` produces a `DiagnosisResult` with a non-nil `Primary` finding, THE Investigator SHALL set `IncidentReport.Status.Phase` to `Diagnosed`.

2. WHEN the `DiagnosisEngine` produces a `DiagnosisResult` with a nil `Primary` finding (no rule matched), THE Investigator SHALL set `IncidentReport.Status.Phase` to `Unknown`.

3. WHEN `IncidentReport.Status.Phase` is already `Resolved`, THE Investigator SHALL NOT modify the phase regardless of the `DiagnosisResult`.

4. WHEN diagnosis is skipped because the `EvidenceSnapshot` is nil, THE Investigator SHALL leave the phase at `Investigating` unchanged.

5. WHEN a subsequent reconciliation produces a different diagnosis outcome (for example re-evaluating updated evidence yields a match where none existed before), THE Investigator SHALL update the phase to reflect the new outcome.

---

### Requirement 4: Diagnosis Is a Pure Function Over Evidence

**User Story:** As a Platform Engineer, I want the diagnosis engine to be deterministic and self-contained so that the same evidence always produces the same result and the engine remains easy to test in isolation.

#### Acceptance Criteria

1. THE DiagnosisEngine SHALL NOT call the Kubernetes API during evaluation.

2. THE DiagnosisEngine SHALL NOT depend on external systems, LLMs, databases, metrics systems, or network services during evaluation.

3. WHEN the same `EvidenceSnapshot` is evaluated multiple times, THE DiagnosisEngine SHALL produce an identical `DiagnosisResult` each time.

4. THE DiagnosisEngine SHALL NOT modify the `EvidenceSnapshot` passed as input.

5. WHEN evidence is incomplete (for example a collection error was recorded for the Node layer), THE DiagnosisEngine SHALL evaluate the evidence that is present and indicate reduced confidence in the finding rather than refusing to evaluate.

---

### Requirement 5: Rule — OOM Memory Limit Exceeded (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to clearly identify when a container was killed for exceeding its own configured memory limit so that I can focus on application-level memory consumption rather than node-level pressure.

#### Acceptance Criteria

1. WHEN a container's `TerminationReason` is `OOMKilled` AND the container's `ExitCode` is `137` AND the container has a non-empty memory limit AND the node does NOT have `MemoryPressure` condition set to `True`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `OOMMemoryLimit` and `Confidence` `High`.

2. WHEN Rule 5 fires, THE finding `Cause` SHALL describe that the container exceeded its configured memory limit.

3. WHEN Rule 5 fires, THE finding `SupportingEvidence` SHALL include the termination reason, exit code, and the container's configured memory limit value.

4. WHEN Rule 5 fires, THE finding `Recommendation` SHALL advise investigating application memory consumption and considering increasing the memory limit if the workload legitimately requires more memory.

5. WHEN the node has `MemoryPressure` condition set to `True`, THE DiagnosisEngine SHALL NOT fire Rule 5, because the kill was more likely caused by node-level pressure (see Rule 6).

---

### Requirement 6: Rule — Node Memory Pressure Eviction (Medium Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to distinguish between a container exceeding its own memory limit and a container evicted or killed due to node-level memory pressure so that I can investigate the correct resource.

#### Acceptance Criteria

1. WHEN a container's `TerminationReason` is `OOMKilled` OR the trigger type is `Eviction` AND the node evidence shows `MemoryPressure` condition set to `True`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `NodeMemoryPressure` and `Confidence` `Medium`.

2. WHEN Rule 6 fires, THE finding `Cause` SHALL describe that the node was under memory pressure and the container may have been killed or evicted by the kubelet.

3. WHEN Rule 6 fires, THE finding `SupportingEvidence` SHALL include the node memory pressure condition status and the termination reason or trigger type.

4. WHEN Rule 6 fires, THE finding `Recommendation` SHALL advise checking node-level memory utilisation and considering whether other workloads on the same node contributed to the pressure.

5. WHEN Rule 5 and Rule 6 both match (OOMKilled with node memory pressure), THE DiagnosisEngine SHALL select Rule 6 as the primary finding and include Rule 5 as a contributing factor, because the node pressure context reduces certainty about whether the container's own limit was the sole cause.

---

### Requirement 7: Rule — CrashLoop Application Error (Medium Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify when a container is crash-looping due to an application-internal failure distinct from OOM so that I can investigate application logs and configuration.

#### Acceptance Criteria

1. WHEN the container's `WaitingReason` is `CrashLoopBackOff` AND the container's last `ExitCode` is non-zero AND the exit code is NOT `137` AND the last `TerminationReason` is NOT `OOMKilled`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `CrashLoopAppError` and `Confidence` `Medium`.

2. WHEN Rule 7 fires, THE finding `Cause` SHALL describe that the container is restarting due to a non-OOM application failure.

3. WHEN Rule 7 fires, THE finding `SupportingEvidence` SHALL include the waiting reason, the last exit code, and the restart count.

4. WHEN Rule 7 fires, THE finding `Recommendation` SHALL advise examining the container logs for application errors and checking whether a configuration or dependency change preceded the failures.

---

### Requirement 8: Rule — CrashLoop OOM Exit (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to specifically identify when a CrashLoopBackOff pattern is caused by repeated OOM kills so that I can treat it as a memory problem rather than a generic crash loop.

#### Acceptance Criteria

1. WHEN the container's `WaitingReason` is `CrashLoopBackOff` AND (the last `ExitCode` is `137` OR the last `TerminationReason` is `OOMKilled`), THE DiagnosisEngine SHALL produce a finding with `RuleID` `CrashLoopOOMExit` and `Confidence` `High`.

2. WHEN Rule 8 fires, THE finding `Cause` SHALL describe that the container is in CrashLoopBackOff due to repeated OOM kills.

3. WHEN Rule 8 fires, THE finding `SupportingEvidence` SHALL include the waiting reason, the last exit code, the last termination reason, and the restart count.

4. WHEN Rule 8 fires, THE finding `Recommendation` SHALL advise investigating memory consumption and considering increasing the container memory limit, and note the crash loop pattern suggests the issue is persistent.

5. WHEN Rule 8 fires, THE DiagnosisEngine SHALL NOT also fire Rule 7 for the same container, since Rule 8 is more specific.

---

### Requirement 9: Rule — Image Pull Failure (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify image pull failures and distinguish between an authentication problem, a non-existent image, and a general pull error so that I know where to focus remediation efforts.

#### Acceptance Criteria

1. WHEN the trigger type is `ImagePullBackOff` AND a container's `WaitingReason` is `ImagePullBackOff` or `ErrImagePull`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `ImagePullFailure` and `Confidence` `High`.

2. WHEN Rule 9 fires and `DependencyEvidence.ImagePullSecretNames` is non-empty, THE finding `Explanation` SHALL note that image pull secrets are configured and an authentication failure is possible.

3. WHEN Rule 9 fires and `DependencyEvidence.ImagePullSecretNames` is empty, THE finding `Explanation` SHALL note that no pull secrets are configured, which may indicate the image is from a private registry requiring credentials.

4. WHEN Rule 9 fires, THE finding `SupportingEvidence` SHALL include the container name, image reference, and waiting reason.

5. WHEN Rule 9 fires, THE finding `Recommendation` SHALL advise verifying that the image reference is correct, the registry is reachable, and the image pull secret (if configured) contains valid credentials.

---

### Requirement 10: Rule — Missing Configuration Reference (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify when a container cannot start because a referenced ConfigMap, Secret, or environment variable source does not exist so that I can locate the missing resource.

#### Acceptance Criteria

1. WHEN the trigger type is `CreateContainerConfigError` OR a container's `WaitingReason` is `CreateContainerConfigError`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `MissingConfigReference` and `Confidence` `High`.

2. WHEN Rule 10 fires, THE finding `Cause` SHALL describe that the container cannot start because a referenced configuration resource does not exist or is invalid.

3. WHEN Rule 10 fires, THE finding `SupportingEvidence` SHALL include the container name and the waiting reason.

4. WHEN Rule 10 fires, THE finding `Recommendation` SHALL advise checking that all ConfigMaps, Secrets, and environment variable sources referenced by the container spec exist in the same namespace as the Pod.

---

### Requirement 11: Rule — Volume Mount Failure, PVC Not Bound (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify when a workload is blocked because its PersistentVolumeClaim has not been provisioned so that I can investigate storage provisioning.

#### Acceptance Criteria

1. WHEN the trigger type is `MountFailure` AND `DependencyEvidence` contains a PVC entry with `Phase` not equal to `Bound`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `PVCNotBound` and `Confidence` `High`.

2. WHEN Rule 11 fires, THE finding `Cause` SHALL describe that the PVC is not bound to a PersistentVolume and the workload is waiting for storage provisioning.

3. WHEN Rule 11 fires, THE finding `SupportingEvidence` SHALL include the PVC name, the PVC phase, and the storage class name if available.

4. WHEN Rule 11 fires, THE finding `Recommendation` SHALL advise checking the PVC status and the StorageClass provisioner, and verifying that the cluster has available capacity to fulfil the storage request.

---

### Requirement 12: Rule — Volume Mount Failure, PVC Bound But Mount Error (Medium Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify the narrower case where a PVC exists and is bound but the mount operation is still failing so that I can investigate node-level or CSI driver issues.

#### Acceptance Criteria

1. WHEN the trigger type is `MountFailure` AND `DependencyEvidence` contains a PVC entry with `Phase` equal to `Bound` AND `EventEvidence` contains one or more events with reason `FailedMount`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `PVCMountError` and `Confidence` `Medium`.

2. WHEN Rule 12 fires, THE finding `Cause` SHALL describe that the PVC is bound but the mount operation is failing, suggesting a node-level or CSI driver issue.

3. WHEN Rule 12 fires, THE finding `SupportingEvidence` SHALL include the PVC name, the event messages from relevant `FailedMount` events (truncated to 256 characters each), and the node name if available.

4. WHEN Rule 12 fires, THE finding `Recommendation` SHALL advise checking CSI driver health, node conditions, and whether other Pods on the same node are experiencing similar mount failures.

5. WHEN Rule 11 and Rule 12 could both apply (PVC phase is not `Bound` but `FailedMount` events also exist), THE DiagnosisEngine SHALL select Rule 11 as the primary finding because a non-Bound PVC is the more direct cause.

---

### Requirement 13: Rule — Scheduling Failure (High Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify why a Pod cannot be scheduled and hint at the specific scheduling constraint that is preventing placement so that I can adjust the workload configuration.

#### Acceptance Criteria

1. WHEN the trigger type is `SchedulingFailure` AND `EventEvidence` contains one or more events with reason `FailedScheduling`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `SchedulingFailure` and `Confidence` `High`.

2. WHEN Rule 13 fires and `DependencyEvidence.SchedulingConstraints.NodeSelector` is non-empty, THE finding `Explanation` SHALL note that a node selector is configured and may not match any available node labels.

3. WHEN Rule 13 fires and `DependencyEvidence.SchedulingConstraints.Tolerations` is empty and node taint information is available from node evidence, THE finding `Explanation` SHALL note the possible taint/toleration mismatch.

4. WHEN Rule 13 fires and `DependencyEvidence.SchedulingConstraints.ResourceRequests` is non-empty, THE finding `Explanation` SHALL note that resource requests are configured and the cluster may lack nodes with sufficient available capacity.

5. WHEN Rule 13 fires, THE finding `SupportingEvidence` SHALL include the `FailedScheduling` event messages (truncated to 256 characters each) and the scheduling constraints from `DependencyEvidence`.

6. WHEN Rule 13 fires, THE finding `Recommendation` SHALL advise inspecting node labels, taints, and resource availability based on the specific constraint identified.

---

### Requirement 14: Rule — Probe Failure (Medium Confidence)

**User Story:** As an engineer, I want the `IncidentReport` to identify repeated probe failures so that I can determine whether the application itself is unhealthy or the probe configuration is incorrect.

#### Acceptance Criteria

1. WHEN the trigger type is `ReadinessProbeFailure` or `LivenessProbeFailure` AND `EventEvidence` contains one or more events with reason `Unhealthy`, THE DiagnosisEngine SHALL produce a finding with `RuleID` `ProbeFailure` and `Confidence` `Medium`.

2. WHEN Rule 14 fires, THE finding `Cause` SHALL describe that the container probe is failing repeatedly, indicating the application may be unhealthy or the probe may be misconfigured.

3. WHEN Rule 14 fires, THE finding `SupportingEvidence` SHALL include the trigger type, the probe type (liveness or readiness) from `PodEvidence.Containers[*].LivenessProbe` or `ReadinessProbe`, and the relevant `Unhealthy` event messages.

4. WHEN Rule 14 fires, THE finding `Recommendation` SHALL advise checking application health, reviewing probe endpoint behaviour, and verifying that the probe `failureThreshold` and `periodSeconds` are configured appropriately for the workload's startup time.

---

### Requirement 15: Handle the Unknown Outcome Explicitly

**User Story:** As a Platform Engineer, I want the `IncidentReport` to explicitly record when no diagnosis could be made so that I can trust that an `Unknown` phase means the evidence was genuinely insufficient, not a silent failure.

#### Acceptance Criteria

1. WHEN no `DiagnosisRule` produces a matching finding for the collected `EvidenceSnapshot`, THE DiagnosisEngine SHALL produce a `DiagnosisResult` with a nil `Primary` field and a non-empty `UnknownReason` string.

2. WHEN the `Unknown` outcome is produced, THE `UnknownReason` SHALL explain that no known failure pattern matched the collected evidence.

3. WHEN the `Unknown` outcome is produced, THE `DiagnosisResult` SHALL still record `RulesEvaluated` with the count of rules that were checked.

4. THE DiagnosisEngine SHALL NOT invent a cause or assign a finding with fabricated supporting evidence when evidence is insufficient.

5. WHEN the `Unknown` outcome is produced, THE Investigator SHALL set `IncidentReport.Status.Phase` to `Unknown` (as required by Requirement 3.2).

---

### Requirement 16: Confidence Levels Are Qualitative and Evidence-Backed

**User Story:** As an engineer, I want confidence levels in findings to reflect the directness of the evidence so that I know how certain the diagnosis is without being misled by false precision.

#### Acceptance Criteria

1. THE DiagnosisEngine SHALL use only three confidence values: `High`, `Medium`, and `Low`. THE DiagnosisEngine SHALL NOT use numerical probabilities or percentage values.

2. THE DiagnosisEngine SHALL assign `High` confidence only when the evidence directly and conclusively identifies the cause (for example: `OOMKilled` + exit code 137 + memory limit set + no node pressure).

3. THE DiagnosisEngine SHALL assign `Medium` confidence when the evidence is circumstantial or when one or more relevant evidence sources are unavailable, leaving the cause plausible but not certain.

4. THE DiagnosisEngine SHALL assign `Low` confidence when only a weak signal is present (for example: an eviction with no pressure condition visible on the node).

5. WHEN a rule uses a confidence level, THE confidence SHALL be justified by the specific evidence conditions in the rule's matching logic, not assigned arbitrarily.

---

### Requirement 17: Primary, Contributing Factors, and Alternative Hypotheses

**User Story:** As an engineer, I want the `IncidentReport` to organise multiple matching diagnoses into primary, contributing, and alternative categories so that I can understand the full picture of the incident without being confused by a list of unordered findings.

#### Acceptance Criteria

1. WHEN multiple rules match, THE DiagnosisEngine SHALL select the single highest-confidence matching finding as the `Primary` finding.

2. WHEN multiple rules match at the same highest confidence level, THE DiagnosisEngine SHALL select the `Primary` finding based on a deterministic priority ordering of rules; all other same-confidence matches SHALL be listed as `AlternativeHypotheses`.

3. WHEN rules other than the primary match at a lower confidence level, THE DiagnosisEngine SHALL list those findings as `ContributingFactors`.

4. THE `ContributingFactors` list SHALL NOT contain the `Primary` finding.

5. THE `AlternativeHypotheses` list SHALL NOT contain the `Primary` finding.

6. WHEN only one rule matches, THE DiagnosisEngine SHALL produce a `DiagnosisResult` with a non-nil `Primary` and empty `ContributingFactors` and `AlternativeHypotheses` lists.

---

### Requirement 18: Diagnosis Is Idempotent

**User Story:** As a Platform Engineer, I want the diagnosis engine to produce consistent results across repeated reconciliations so that the `IncidentReport` does not oscillate between different diagnoses on the same evidence.

#### Acceptance Criteria

1. WHEN the `DiagnosisEngine` evaluates the same `EvidenceSnapshot` multiple times, THE DiagnosisEngine SHALL produce an identical `DiagnosisResult` each time (excluding `EvaluatedAt` timestamp).

2. WHEN the `PodReconciler` reconciles the same incident multiple times without an evidence refresh, THE Investigator SHALL produce the same `DiagnosisResult` and SHALL NOT change the `IncidentReport.Status.Phase` unless the evidence has changed.

3. WHEN the `EvidenceSnapshot` changes between reconciliations (for example a new event is collected), THE DiagnosisEngine SHALL re-evaluate the updated snapshot and update the `DiagnosisResult` accordingly.

4. THE DiagnosisEngine SHALL NOT accumulate state between invocations. Each call to the engine SHALL be independent of previous calls.

---

### Requirement 19: Recommendations Are Evidence-Backed and Non-Destructive

**User Story:** As an engineer, I want the `IncidentReport` to include specific, actionable recommendations tied to the finding evidence so that I know what to investigate next without needing to interpret raw evidence myself.

#### Acceptance Criteria

1. WHEN a `DiagnosisFinding` is produced, THE finding SHALL include a `Recommendation` string that references the specific evidence supporting the finding.

2. THE `Recommendation` SHALL be actionable and specific — it SHALL identify what to investigate or adjust based on the finding, not generic advice such as "check your Kubernetes configuration".

3. THE `Recommendation` SHALL NOT suggest automatic or destructive actions such as deleting Pods, scaling down workloads, or modifying production resources.

4. THE `Recommendation` SHALL be scoped to the `Primary` finding only. `ContributingFactors` and `AlternativeHypotheses` MAY include recommendations but are not required to.

5. THE `Recommendation` SHALL NOT include Secret values, environment variable values, or log content.

---

### Requirement 20: Diagnosis Engine Does Not Import Controller or Investigation Packages

**User Story:** As a Platform Engineer, I want the diagnosis package to remain independently testable without bringing in controller or investigation dependencies so that unit tests can run without a Kubernetes cluster or controller-runtime setup.

#### Acceptance Criteria

1. THE `internal/diagnosis/` package SHALL NOT import `internal/controller`.

2. THE `internal/diagnosis/` package SHALL NOT import `internal/investigation`.

3. THE `internal/diagnosis/` package SHALL NOT import `internal/evidence`.

4. THE `internal/diagnosis/` package SHALL import only `api/v1alpha1` types and standard library packages (plus any pure-Go testing libraries in test files).

5. THE `DiagnosisEngine` SHALL accept only an `*v1alpha1.EvidenceSnapshot` as input to its evaluate function, and SHALL NOT accept controller-runtime clients or context values that imply Kubernetes API access.

---

## Out of Scope

The following capabilities are explicitly outside this specification:

- Statistical or probabilistic confidence scoring
- LLM-based diagnosis or log interpretation
- Automatic remediation based on diagnosis findings
- Prometheus or external metrics integration
- Diagnosis of multi-Pod incidents beyond the primary affected container
- Diagnosis rules for trigger types not listed above (additional rules are a future enhancement)
- Cross-cluster incident correlation
- Evidence collection (covered by the evidence-collection specification)
- Reporting or timeline generation (future specification)
- External storage of `DiagnosisResult` data
- Diagnosis for incidents in `PhaseResolved` — resolved incidents are not re-diagnosed
