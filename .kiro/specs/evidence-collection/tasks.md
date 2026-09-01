# Implementation Plan: Evidence Collection

## Overview

This plan implements the evidence collection pipeline for the Kubernetes Incident Investigator. It adds new API types for evidence storage, a focused set of evidence collectors in `internal/evidence/`, configuration additions, wiring into `PodReconciler`, and the full test suite. Tasks build incrementally: types and interfaces first, then each collector, then integration wiring, then tests.

The implementation language is **Go**, consistent with the rest of the project.

---

## Tasks
any o
- [ ] 1. Define evidence API types and add Evidence field to IncidentReportStatus
  - Create `api/v1alpha1/evidence_types.go` with all evidence types as defined in the design: `EvidenceSnapshot`, `PodEvidence`, `ContainerEvidence`, `ProbeSummary`, `VolumeMountEvidence`, `WorkloadEvidence`, `WorkloadCondition`, `NodeEvidence`, `EventEvidence`, `ContainerLogEvidence`, `DependencyEvidence`, `PVCEvidence`, `SchedulingConstraints`, `CollectionError`
  - Add `+kubebuilder:object:generate=true` markers to all struct types
  - Add `Evidence *EvidenceSnapshot` field to `IncidentReportStatus` in `api/v1alpha1/incidentreport_types.go`
  - Ensure all fields carry appropriate `// +optional` markers per design
  - _Requirements: 2.1, 2.2, 2.3, 3.1, 4.3, 5.1, 6.1, 7.1, 8.1, 10.4, 10.5_

- [ ] 2. Run code generation and update CRD manifests
  - [ ] 2.1 Regenerate DeepCopy methods
    - Run `make generate` to regenerate `zz_generated.deepcopy.go` for the new evidence types
    - Verify no compile errors after generation
    - _Requirements: 2.1_
  - [ ] 2.2 Regenerate CRD manifests
    - Run `make manifests` to regenerate `config/crd/bases/investigation.k8s.io_incidentreports.yaml` with the new evidence fields
    - Verify the `evidence` field appears in the CRD schema under `status`
    - _Requirements: 2.1_

- [ ] 3. Add evidence collection configuration fields
  - [ ] 3.1 Extend Config struct with evidence limits
    - Add `MaxLogBytes int`, `MaxLogLines int`, `MaxEventsPerIncident int`, and `EvidenceCollectionTimeout time.Duration` to `internal/config/config.go`
    - Set defaults: `MaxLogBytes = 32768`, `MaxLogLines = 200`, `MaxEventsPerIncident = 25`, `EvidenceCollectionTimeout = 30 * time.Second`
    - Add validation in `Config.Validate()` for all four new fields (must be positive / non-zero)
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_
  - [ ]* 3.2 Write unit tests for new Config fields
    - Add test cases to `test/unit/config_test.go` covering default values, valid custom values, and each invalid value triggering a validation error
    - _Requirements: 13.1, 13.2, 13.3, 13.4, 13.5_

- [ ] 4. Implement the evidence package foundation (CollectorInput and EvidenceOrchestrator)
  - [ ] 4.1 Create `internal/evidence/collector.go`
    - Define `CollectorInput` struct (Client, KubeClient, Report, Pod, Config)
    - Define `triggerNeeds` struct and `evidenceNeedsForTrigger(TriggerType) triggerNeeds` function
    - Implement `EvidenceOrchestrator` struct with `Collect(ctx, report, pod) v1alpha1.EvidenceSnapshot`
    - Orchestrator fetches the primary Pod from the API, calls all collectors with appropriate inputs, merges `CollectionError` slices, sets `CollectedAt`, and returns the assembled snapshot
    - Orchestrator always returns a non-nil snapshot
    - _Requirements: 1.1, 1.2, 1.4, 9.1, 9.2, 9.3_
  - [ ]* 4.2 Write property test for orchestrator partial-failure behaviour
    - **Property 3: Partial Failure Produces Non-Nil Snapshot With CollectionErrors**
    - Use `pgregory.net/rapid` to generate random subsets of source failures
    - For any combination of source failures, verify: snapshot is non-nil, CollectedAt is set, CollectionErrors contains entries for every failed source
    - Tag: `Feature: evidence-collection, Property 3: partial-failure-non-nil-snapshot`
    - _Requirements: 9.1, 9.2, 9.3_
  - [ ]* 4.3 Write property test for collection idempotence
    - **Property 2: Collection Idempotence**
    - For any deterministic CollectorInput, verify that two successive calls to Collect produce snapshots with identical logical content (same evidence fields, same CollectionErrors count)
    - Tag: `Feature: evidence-collection, Property 2: collection-idempotence`
    - _Requirements: 1.3_

- [ ] 5. Checkpoint — ensure existing tests still pass
  - Run `make test` to confirm the existing test suite compiles and passes with the new types and config fields
  - Resolve any compile errors from the new `Evidence` field on `IncidentReportStatus`

- [ ] 6. Implement PodCollector (Layer 1)
  - [ ] 6.1 Create `internal/evidence/pod.go`
    - Implement `PodCollector.Collect(ctx, input) (*v1alpha1.PodEvidence, []v1alpha1.CollectionError)`
    - Map all container statuses to `ContainerEvidence`: name, image, state string, waitingReason, restartCount, exitCode, terminationReason, lastTerminationReason
    - Map `resources.limits` and `resources.requests` to `ResourceLimits`/`ResourceRequests` as `string` maps using `resource.Quantity.String()`
    - Map liveness and readiness probes to `ProbeSummary` (type, httpPath, port, failureThreshold)
    - Map volume mounts relevant to the trigger: for MountFailure include all PVC-backed volumes; for other triggers include all volumes
    - When `input.Pod` is nil, return a `CollectionError{Source: "pod", Reason: "pod not found"}`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_
  - [ ]* 6.2 Write unit tests for PodCollector
    - Test pod with multiple containers: verify ContainerEvidence count matches
    - Test OOMKilled container: verify terminationReason = "OOMKilled", exitCode = 137
    - Test nil Pod input: verify CollectionError returned
    - Test probe mapping for HTTPGet, TCPSocket, Exec types
    - _Requirements: 3.1, 3.2, 3.3, 3.5_

- [ ] 7. Implement EventCollector (Layer 2)
  - [ ] 7.1 Create `internal/evidence/events.go`
    - Implement `EventCollector.Collect(ctx, input) ([]v1alpha1.EventEvidence, []v1alpha1.CollectionError)`
    - List Events in the Pod's namespace where `involvedObject.name == pod.Name` and `involvedObject.kind == "Pod"`
    - Sort events by `lastTimestamp` descending and retain only the most recent `MaxEventsPerIncident` events
    - Map each event to `EventEvidence`, truncating message to 256 characters via `truncateMessage()`
    - On List error, return `CollectionError{Source: "events", Reason: ...}`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 10.3, 10.5_
  - [ ]* 7.2 Write property test for event bounding
    - **Property 4: Event Count Is Bounded**
    - Use `rapid` to generate event lists of size N (0 to 100)
    - Verify: len(result) == min(N, MaxEventsPerIncident)
    - Verify: when N > MaxEventsPerIncident, retained events have the most recent LastTime values
    - Tag: `Feature: evidence-collection, Property 4: event-count-bounded`
    - _Requirements: 4.1, 4.2_
  - [ ]* 7.3 Write property test for event message truncation
    - **Property 7: Event Message Length Is Bounded**
    - Use `rapid` to generate strings of arbitrary length
    - Verify: len(truncateMessage(s)) <= 256; for s with len <= 256, truncateMessage(s) == s
    - Tag: `Feature: evidence-collection, Property 7: event-message-truncation`
    - _Requirements: 4.3, 10.5_

- [ ] 8. Implement WorkloadCollector (Layer 3)
  - [ ] 8.1 Create `internal/evidence/workload.go`
    - Implement `WorkloadCollector.Collect(ctx, input) (*v1alpha1.WorkloadEvidence, []v1alpha1.CollectionError)`
    - When `report.Spec.Workload` is nil or `WorkloadOwnerResolved` is false, return nil evidence and no error (expected state)
    - Fetch the workload resource by kind (Deployment, StatefulSet, DaemonSet, Job)
    - Map desired replicas, ready replicas, update strategy, and conditions (capped at 10) to `WorkloadEvidence`
    - On fetch error, return `CollectionError{Source: "workload", Reason: ...}`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 10.4_
  - [ ]* 8.2 Write unit tests for WorkloadCollector
    - Test Deployment with conditions: verify conditions capped at 10, updateStrategy mapped correctly
    - Test nil workload reference: verify nil evidence returned, no error
    - Test API fetch error: verify CollectionError returned
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [ ] 9. Implement NodeCollector (Layer 4)
  - [ ] 9.1 Create `internal/evidence/node.go`
    - Implement `NodeCollector.Collect(ctx, input) (*v1alpha1.NodeEvidence, []v1alpha1.CollectionError)`
    - When `input.Pod.Spec.NodeName` is empty, return nil evidence and no error (Pod not yet scheduled)
    - Fetch the Node by name
    - Map Ready, MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable conditions to their status strings
    - Map `node.Status.Allocatable["cpu"]` and `["memory"]` to strings
    - Map `node.Status.NodeInfo.KernelVersion`
    - On fetch error, return `CollectionError{Source: "node", Reason: ...}`
    - _Requirements: 6.1, 6.2, 6.3_
  - [ ]* 9.2 Write unit tests for NodeCollector
    - Test node with MemoryPressure=True: verify mapped correctly
    - Test empty nodeName: verify nil returned, no error
    - Test node fetch error: verify CollectionError returned
    - _Requirements: 6.1, 6.2, 6.3_

- [ ] 10. Implement LogCollector (Layer 6)
  - [ ] 10.1 Create `internal/evidence/logs.go`
    - Implement `LogCollector.Collect(ctx, input, needs triggerNeeds) ([]v1alpha1.ContainerLogEvidence, []v1alpha1.CollectionError)`
    - When `needs.currentLogs` is false and `needs.previousLogs` is false, return nil immediately (no error)
    - Determine target container name from `report.Status.Trigger.ContainerName`; fall back to iterating all non-init containers
    - Use `KubeClient.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{TailLines: &maxLines, Previous: isPrevious})` to open a streaming log reader
    - Wrap the log stream with `io.LimitReader` using `MaxLogBytes` as the byte limit
    - Use `bufio.NewScanner` to read lines; stop when lines exceed `MaxLogLines` or the byte limit fires
    - Set `Truncated = true` when either limit interrupted reading before stream EOF
    - When `GetLogs` returns an error, populate `ContainerLogEvidence.UnavailableReason` with the error string; do not add a `CollectionError` (unavailable logs are expected for some trigger types)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 10.1, 10.2_
  - [ ]* 10.2 Write property test for log line bounding
    - **Property 5: Log Line Count Is Bounded**
    - Use `rapid` to generate log content strings with arbitrary numbers of newline-separated lines
    - For any content, verify: len(Lines) <= MaxLogLines; when truncated, Truncated==true
    - Tag: `Feature: evidence-collection, Property 5: log-line-count-bounded`
    - _Requirements: 7.3, 10.2_
  - [ ]* 10.3 Write property test for log byte bounding
    - **Property 6: Log Byte Count Is Bounded**
    - Use `rapid` to generate log content of varying byte lengths
    - For any content, verify: sum of len(line)+1 for each line <= MaxLogBytes; when truncated, Truncated==true
    - Tag: `Feature: evidence-collection, Property 6: log-byte-count-bounded`
    - _Requirements: 7.3, 10.1_
  - [ ]* 10.4 Write unit tests for LogCollector unavailability
    - Test log fetch error (simulated 404): verify UnavailableReason is set, Lines is nil
    - Test ImagePullBackOff trigger: verify skip (no logs attempted, no error)
    - _Requirements: 7.4, 7.6_

- [ ] 11. Implement DependencyCollector (Layer 5 — adaptive)
  - [ ] 11.1 Create `internal/evidence/dependencies.go`
    - Implement `DependencyCollector.Collect(ctx, input, needs triggerNeeds) (*v1alpha1.DependencyEvidence, []v1alpha1.CollectionError)`
    - When none of `needs.pvcEvidence`, `needs.imagePullEvidence`, or `needs.schedulingEvidence` are true, return nil immediately (no error)
    - For `pvcEvidence`: iterate Pod volumes, find PVC-backed volumes, fetch each PVC, optionally fetch bound PV; map to `PVCEvidence`; on per-resource fetch error add `CollectionError{Source: "pvc/<name>", ...}`
    - For `imagePullEvidence`: read `pod.Spec.ImagePullSecrets[].Name` strings directly from the Pod spec — no Secret API calls
    - For `schedulingEvidence`: map `pod.Spec.NodeSelector`, `pod.Spec.Tolerations` (as "key=value:effect" strings), and aggregated `resources.requests` across all containers
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 11.1, 11.2, 12.3_
  - [ ]* 11.2 Write property test for secret value exclusion
    - **Property 8: Secret Values Are Never Stored in DependencyEvidence**
    - Use `rapid` to generate Pods with arbitrary numbers of `imagePullSecrets` entries
    - Verify: DependencyEvidence.ImagePullSecretNames contains exactly the Secret names (strings); no struct types; length equals len(pod.Spec.ImagePullSecrets)
    - Tag: `Feature: evidence-collection, Property 8: secret-values-excluded`
    - _Requirements: 11.1, 11.2_
  - [ ]* 11.3 Write unit tests for DependencyCollector
    - Test MountFailure: pod with two PVC volumes, both PVCs fetched, bound PV recorded
    - Test MountFailure with missing PVC: CollectionError added for missing PVC, other PVC still collected
    - Test OOMKilled trigger: DependencyCollector returns nil without errors
    - Test SchedulingFailure: nodeSelector, tolerations, resource requests all mapped
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

- [ ] 12. Wire EvidenceOrchestrator into PodReconciler
  - [ ] 12.1 Add EvidenceOrchestrator to PodReconciler and inject in cmd/main.go
    - Add `EvidenceOrchestrator *evidence.EvidenceOrchestrator` field to `PodReconciler` struct
    - Construct `EvidenceOrchestrator{Client: mgr.GetClient(), KubeClient: kubeClient, Config: cfg, Log: ...}` in `cmd/main.go`
    - Pass the orchestrator when constructing `PodReconciler`
    - Build `kubernetes.Interface` from `rest.Config` (`kubernetes.NewForConfig(mgr.GetConfig())`)
    - _Requirements: 1.1, 1.2_
  - [ ] 12.2 Insert evidence collection step (Step 9.5) into Reconcile()
    - Add step 9.5 comment block between the existing step 9 (status patch) and step 10 (re-fetch)
    - Use `context.WithTimeout(ctx, r.Config.EvidenceCollectionTimeout)` for the collection context
    - Call `r.EvidenceOrchestrator.Collect(collectCtx, report, &pod)` and patch `report.Status.Evidence`
    - Treat patch failure as non-fatal — log the error and continue to recovery evaluation
    - _Requirements: 1.1, 1.2, 1.4, 1.5_
  - [ ] 12.3 Add new RBAC markers to PodReconciler
    - Add `// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch`
    - Add `// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch`
    - Add `// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch`
    - Add `// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get`
    - Run `make manifests` to regenerate RBAC role manifests
    - _Requirements: 12.1, 12.2, 12.4, 12.5_

- [ ] 13. Checkpoint — run full test suite
  - Run `make test` to verify all unit tests and property tests pass
  - Fix any compilation or test failures before proceeding

- [ ] 14. Write integration test for evidence population
  - [ ] 14.1 Add evidence integration test
    - Create `test/integration/evidence_collection_test.go` using `envtest`
    - Simulate a Pod with an OOMKilled container using a fake Pod status object
    - Reconcile the controller and verify `IncidentReport.Status.Evidence` is non-nil
    - Verify `Evidence.Pod` is non-nil with at least one container entry
    - Verify `Evidence.CollectedAt` is set
    - Verify `Evidence.CollectionErrors` does not contain a "pod" layer error (Pod exists in the test)
    - _Requirements: 1.1, 1.2, 3.1_
  - [ ]* 14.2 Write property test for EvidenceSnapshot CollectedAt invariant
    - **Property 1: EvidenceSnapshot Always Has a CollectedAt Timestamp**
    - Use `rapid` to generate `CollectorInput` variants with different trigger types and nil/non-nil Pod combinations
    - Verify: `EvidenceOrchestrator.Collect(...)` always returns a snapshot where `CollectedAt != nil`
    - Tag: `Feature: evidence-collection, Property 1: collected-at-always-set`
    - _Requirements: 2.2_

- [ ] 15. Final checkpoint — ensure all tests pass and manifests are current
  - Run `make generate manifests test` to confirm code generation, CRD manifests, and tests are all in a consistent state
  - Verify `config/crd/bases/investigation.k8s.io_incidentreports.yaml` contains the `evidence` field in the status schema
  - Verify `config/rbac/role.yaml` contains the new PVC/PV/StorageClass and pods/log permissions

---

## Notes

- Tasks marked with `*` are optional sub-tasks (property tests and unit tests) that can be skipped for a faster MVP
- All property tests use `pgregory.net/rapid` and run a minimum of 100 iterations
- Each property test includes a Tag comment referencing the design property number
- The `LogCollector` requires `kubernetes.Interface` in addition to `client.Client` because `controller-runtime`'s cached client does not expose the log streaming API
- `DependencyCollector` never reads Secret data — it only uses Secret names from the Pod spec's `imagePullSecrets` field
- Evidence collection is non-fatal to the reconciliation loop: patch failures are logged and retried on the next reconciliation cycle
- All new API types require `+kubebuilder:object:generate=true` markers to get DeepCopy generated

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1"] },
    { "id": 1, "tasks": ["2.1", "2.2", "3.1"] },
    { "id": 2, "tasks": ["3.2", "4.1"] },
    { "id": 3, "tasks": ["4.2", "4.3", "6.1", "7.1", "8.1", "9.1", "10.1", "11.1"] },
    { "id": 4, "tasks": ["6.2", "7.2", "7.3", "8.2", "9.2", "10.2", "10.3", "10.4", "11.2", "11.3"] },
    { "id": 5, "tasks": ["12.1"] },
    { "id": 6, "tasks": ["12.2", "12.3"] },
    { "id": 7, "tasks": ["14.1"] },
    { "id": 8, "tasks": ["14.2"] }
  ]
}
```
