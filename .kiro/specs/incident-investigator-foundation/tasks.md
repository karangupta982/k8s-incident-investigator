# Implementation Plan: Incident Investigator Foundation

## Overview

This plan converts the Incident Investigator Foundation design into discrete, ordered coding tasks.
Each task builds on the previous ones, wiring everything together at the end.
The implementation language is Go, using controller-runtime, Kubebuilder markers, and the `rapid`
library for property-based tests.

Tasks are grouped by component. Tests are sub-tasks placed immediately after the code they cover.
Optional test sub-tasks are marked with `*`.

---

## Tasks

- [ ] 1. Project bootstrap and Go module setup
  - [ ] 1.1 Initialise the Go module and root directory structure
    - Create `go.mod` with module path `github.com/k8s-incident-investigator/k8s-incident-investigator` and Go 1.23+ (verify current supported versions against the controller-runtime compatibility matrix at https://github.com/kubernetes-sigs/controller-runtime before pinning)
    - Add direct dependencies: `sigs.k8s.io/controller-runtime`, `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, `sigs.k8s.io/controller-tools` (controller-gen), `pgregory.net/rapid`
    - Run `go mod tidy` to pin all transitive dependencies with exact versions
    - Create the empty package directories: `api/v1alpha1`, `cmd`, `internal/config`, `internal/investigation`, `internal/controller`
    - Create `test/unit/` and `test/integration/` directories as needed when their first test files are added
    - _Requirements: Req 20 (demonstrable on Kind)_

  - [ ] 1.2 Add the multi-stage Dockerfile
    - Stage 1 (`builder`): use `golang:1.23-alpine or the current LTS alpine image matching verified Go version`, copy source, run `go build -o manager ./cmd`
    - Stage 2 (`runtime`): use `gcr.io/distroless/static:nonroot`, copy the compiled binary only
    - Set `USER 65532:65532` in the runtime stage
    - _Requirements: Req 16 (minimal image surface)_

  - [ ] 1.3 Add the base Makefile with standard Kubebuilder targets
    - Targets: `generate`, `manifests`, `fmt`, `vet`, `test`, `build`, `docker-build`, `deploy`, `undeploy`, `install`, `uninstall`
    - Wire `generate` to invoke `controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."``
    - Wire `manifests` to invoke `controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases output:rbac:artifacts:config=config/rbac`
    - Add a `hack/boilerplate.go.txt` file with the project license header
    - _Requirements: Req 20_

- [ ] 2. CRD API types (`api/v1alpha1`)
  - [ ] 2.1 Define core type constants: `IncidentPhase` and `TriggerType`
    - Create `api/v1alpha1/types.go`
    - Define `IncidentPhase` string type with constants `PhaseInvestigating`, `PhaseDiagnosed`, `PhaseUnknown`, `PhaseResolved` exactly as in the design
    - Add `+kubebuilder:validation:Enum` marker on `IncidentPhase`
    - Define `TriggerType` string type with all nine constants: `TriggerOOMKilled`, `TriggerCrashLoopBackOff`, `TriggerImagePullBackOff`, `TriggerCreateContainerConfigError`, `TriggerMountFailure`, `TriggerReadinessProbeFailure`, `TriggerLivenessProbeFailure`, `TriggerSchedulingFailure`, `TriggerEviction`
    - Define `TriggerSource` string type with constants `TriggerSourceStateBased = "StateBased"` and `TriggerSourceEventBased = "EventBased"`
    - _Requirements: Req 1, Req 7.6_

  - [ ] 2.2 Define `WorkloadRef`, `PodRef`, and `TriggerInfo` structs
    - Create `api/v1alpha1/shared_types.go`
    - Implement `WorkloadRef` with `Kind`, `Name`, `Namespace`, `UID` fields and correct JSON tags
    - Implement `PodRef` with `Name`, `Namespace`, `UID` fields and correct JSON tags
    - Implement `TriggerInfo` with `Type`, `Reason`, `ContainerName`, `ObservedAt`, `Message` fields and correct JSON tags; mark optional fields with `// +optional`
    - _Requirements: Req 3, Req 4, Req 13_

  - [ ] 2.3 Define `IncidentReportSpec` and `IncidentReportStatus`
    - Create `api/v1alpha1/incidentreport_types.go`
    - Implement `IncidentReportSpec` with `Workload *WorkloadRef` (optional) only — no `Namespace` field. The incident namespace is obtained from `metadata.namespace` (the object's own namespace). Do not add a `Namespace string` field to the spec.
    - Implement `IncidentReportStatus` with all fields from the design: `Phase`, `StartedAt`, `ResolvedAt`, `StabilityStartedAt`, `AffectedPods []PodRef`, `Trigger *TriggerInfo`, `WorkloadOwnerResolved bool`, `FailureCount int32`, `LastFailureAt *metav1.Time`, `Conditions []metav1.Condition`
    - Apply `+listType=map` and `+listMapKey=type` markers to `Conditions`
    - Note: The `IncidentReport` name encodes whether it is the active incident (`-active` suffix) or a historical record (date+hash suffix). No additional status field is needed to distinguish them.
    - _Requirements: Req 3, Req 6, Req 7, Req 8_

  - [ ] 2.4 Define the `IncidentReport` root type with Kubebuilder markers and printer columns
    - In `api/v1alpha1/incidentreport_types.go`, define the `IncidentReport` struct embedding `metav1.TypeMeta` and `metav1.ObjectMeta`
    - Apply all markers from the design: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:resource:shortName=ir,categories=investigator`
    - Apply all six `+kubebuilder:printcolumn` markers: `Workload`, `Kind`, `Phase`, `Trigger`, `Started`, `Age`
    - Define `IncidentReportList` with `+kubebuilder:object:root=true`
    - Create `api/v1alpha1/groupversion_info.go` with `SchemeBuilder`, `GroupVersion`, `AddToScheme`
    - _Requirements: Req 13_

  - [ ] 2.5 Run code generation to produce `zz_generated.deepcopy.go`
    - Run `make generate` to invoke controller-gen and produce the `DeepCopyObject` implementations
    - Verify the file is created and compiles cleanly with `go build ./api/...`
    - _Requirements: Req 3_

- [ ] 3. Configuration package (`internal/config`)
  - [ ] 3.1 Implement the `Config` struct with all fields and defaults
    - Create `internal/config/config.go`
    - Define the `Config` struct with all fields from the design: `WatchNamespaces []string`, `StabilityPeriod time.Duration`, `CorrelationWindow time.Duration`, `ReadinessProbeFailureThreshold int`, `LivenessProbeFailureThreshold int`, `MountFailureThreshold int`, `SchedulingFailureThreshold int`, `RequeueInterval time.Duration`
    - Implement `DefaultConfig() *Config` returning the defaults from the design table: `StabilityPeriod=5m`, `CorrelationWindow=10m`, `ReadinessProbeFailureThreshold=3`, `LivenessProbeFailureThreshold=3`, `MountFailureThreshold=3`, `SchedulingFailureThreshold=5`, `RequeueInterval=30s`
    - Add `Validate() error` that rejects zero or negative thresholds and zero durations
    - _Requirements: Req 18 (bounded operations)_

  - [ ]* 3.2 Write unit tests for the config package
    - Test `DefaultConfig()` returns expected values for all fields
    - Test `Validate()` rejects invalid configurations and accepts valid ones
    - _Requirements: Req 18_

- [ ] 4. Trigger evaluation (`internal/investigation/trigger.go`)
  - [ ] 4.1 Define `TriggerContext`, `TriggerResult`, and the `TriggerEvaluatorInterface`
    - Create `internal/investigation/trigger.go`
    - Define `TriggerContext` struct with `Pod *corev1.Pod`, `MountFailureEventCount int`, `ReadinessProbeEventCount int`, `LivenessProbeEventCount int`, `SchedulingFailureEventCount int` — event count fields use Kubernetes Event.count (aggregated by Kubernetes), not raw event object count
    - Define `TriggerResult` struct with `IsTrigger bool`, `Type TriggerType`, `Source TriggerSource`, `IsImmediate bool`, `ContainerName string`, `Reason string`
    - Define `TriggerEvaluatorInterface` with `Evaluate(ctx context.Context, tc TriggerContext, cfg *config.Config) TriggerResult`
    - _Requirements: Req 1, Req 2_

  - [ ] 4.2 Implement immediate trigger detection
    - Implement `TriggerEvaluator` struct implementing `TriggerEvaluatorInterface`
    - Detect `OOMKilled` from `containerStatuses[*].lastTerminationState.terminated.reason` and `containerStatuses[*].state.terminated.reason`
    - Detect `CrashLoopBackOff` from `containerStatuses[*].state.waiting.reason`
    - Detect `ImagePullBackOff` from waiting reason `"ImagePullBackOff"` or `"ErrImagePull"`
    - Detect `CreateContainerConfigError` from waiting reason `"CreateContainerConfigError"`
    - Detect eviction from `status.reason == "Evicted"` or `status.phase == Failed` with eviction condition
    - Return `IsImmediate = true` for all immediate triggers
    - Note: these are state-based triggers (`TriggerSourceStateBased`). The same Pod state on reconciliation N and N+1 represents the SAME condition, not two distinct failures; set `TriggerResult.Source = TriggerSourceStateBased`
    - _Requirements: Req 1.1–1.4, Req 1.9, Req 2.1_

  - [ ] 4.3 Implement threshold-based trigger detection and normal lifecycle exclusions
    - Add threshold detection for readiness probe failures using Kubernetes Event.count field values (Kubernetes-aggregated), not raw event object counts. Threshold check: `ReadinessProbeEventCount >= cfg.ReadinessProbeFailureThreshold`; set `TriggerResult.Source = TriggerSourceEventBased`
    - Add threshold detection for liveness probe failures: `LivenessProbeEventCount >= cfg.LivenessProbeFailureThreshold`; set `TriggerResult.Source = TriggerSourceEventBased`
    - Both readiness and liveness probe failures share Kubernetes Event reason `Unhealthy`. Distinguish them by `event.message`: messages with prefix `"Readiness probe"` count toward `ReadinessProbeEventCount`; messages with prefix `"Liveness probe"` count toward `LivenessProbeEventCount`. Events with an unrecognised message prefix are ignored for threshold purposes.
    - Add threshold detection for mount failures: `MountFailureEventCount >= cfg.MountFailureThreshold`; set `TriggerResult.Source = TriggerSourceEventBased`
    - Add threshold detection for scheduling failures: `SchedulingFailureEventCount >= cfg.SchedulingFailureThreshold`; set `TriggerResult.Source = TriggerSourceEventBased`
    - Return `IsTrigger = false` for normal lifecycle states: `Pending` not yet at threshold, `Succeeded`, container `Completed` (exit code 0), `ContainerCreating`, `PodInitializing`, graceful termination (`deletionTimestamp` set without failure reason)
    - WHEN multiple Event objects exist for the same Pod and reason, use the highest `event.count` value among all matching Events as the threshold input. Do not sum counts across independent Event objects.
    - _Requirements: Req 1.5–1.10, Req 2.2–2.6_

  - [ ]* 4.4 Write table-driven unit tests for trigger evaluation
    - Create `test/unit/trigger_test.go`
    - Cover all immediate trigger types with fabricated `corev1.Pod` objects
    - Cover threshold boundary conditions (count = threshold-1, count = threshold, count = threshold+1) for all threshold types
    - Cover all normal lifecycle exclusion cases
    - Use table-driven test structure with named test cases
    - _Requirements: Req 1, Req 2_

  - [ ]* 4.5 Write property-based tests for trigger evaluation (Properties 1–4)
    - In `test/unit/trigger_test.go`, add property tests using `pgregory.net/rapid`
    - **Property 1**: For any Pod with OOMKilled/CrashLoopBackOff/ImagePullBackOff/CreateContainerConfigError/Evicted signal, `IsTrigger = true` — tag: `// Feature: incident-investigator-foundation, Property 1`
    - **Property 2**: For any failure count C and threshold T, `IsTrigger = (C >= T)` — tag: `// Feature: incident-investigator-foundation, Property 2`
    - **Property 3**: For any Pod in normal lifecycle state, `IsTrigger = false` — tag: `// Feature: incident-investigator-foundation, Property 3`
    - **Property 4**: For identical `TriggerContext` inputs, `Evaluate` returns identical `TriggerResult` — tag: `// Feature: incident-investigator-foundation, Property 4`
    - Run minimum 100 iterations per property
    - _Requirements: Req 1, Req 2, Req 14.3_

- [ ] 5. Workload ownership resolution (`internal/investigation/ownership.go`)
  - [ ] 5.1 Define the `OwnershipResolverInterface` and `OwnershipResolver` struct
    - Create `internal/investigation/ownership.go`
    - Define `OwnershipResolverInterface` with `Resolve(ctx context.Context, pod *corev1.Pod) (*v1alpha1.WorkloadRef, error)`
    - Define `OwnershipResolver` struct that accepts a `client.Client` via dependency injection
    - _Requirements: Req 4_

  - [ ] 5.2 Implement the full ownership traversal chain
    - Traverse `Pod → ReplicaSet → Deployment`: fetch the RS by ownerRef, then check the RS's ownerRefs for a Deployment
    - Traverse `Pod → StatefulSet`: direct owner kind match
    - Traverse `Pod → DaemonSet`: direct owner kind match
    - Traverse `Pod → Job → CronJob`: fetch the Job by ownerRef, then check Job's ownerRefs for a CronJob; fall back to `WorkloadRef{Kind:"Job"}` if no CronJob found
    - Return `nil` workload with `WorkloadOwnerResolved = false` when no controller ownerReference exists
    - On any intermediate lookup failure (e.g., RS not found, or RS found but Deployment not found), fall back to Pod identity and record `WorkloadOwnerResolved = false`. Do NOT use an intermediate resource (e.g., ReplicaSet) as the workload identity — `WorkloadRef.Kind` must be one of: Deployment, StatefulSet, DaemonSet, Job, CronJob. Log a warning when the traversal fails.
    - _Requirements: Req 4.1–4.7_

  - [ ]* 5.3 Write unit tests for ownership resolution
    - Create `test/unit/ownership_test.go`
    - Test Pod→RS→Deployment chain with a mock client returning pre-populated objects
    - Test Pod→StatefulSet, Pod→DaemonSet, Pod→Job, Pod→Job→CronJob chains
    - Test missing intermediate resource (RS not found) → falls back to Pod identity with `WorkloadOwnerResolved=false`
    - Test RS found but Deployment not found → falls back to Pod identity with `WorkloadOwnerResolved=false` (NOT ReplicaSet as workload)
    - Test Pod with no controller owner returns nil workload
    - _Requirements: Req 4_

  - [ ]* 5.4 Write property-based test for ownership resolution determinism (Property 5)
    - In `test/unit/ownership_test.go`, add a rapid property test
    - **Property 5**: For any Pod with a fixed owner reference graph, `Resolve` returns the same `WorkloadRef` on every call — tag: `// Feature: incident-investigator-foundation, Property 5`
    - _Requirements: Req 4.1_

- [ ] 6. Incident correlation (`internal/investigation/correlator.go`)
  - [ ] 6.1 Define `IncidentCorrelatorInterface`, `CorrelationResult`, and label constants
    - Create `internal/investigation/correlator.go`
    - Define label constants: `LabelWorkloadNamespace = "investigator.k8s.io/workload-namespace"`, `LabelWorkloadName = "investigator.k8s.io/workload-name"`, `LabelWorkloadKind = "investigator.k8s.io/workload-kind"`, `LabelActive = "investigator.k8s.io/active"` — set to `"true"` on active incidents, removed/cleared on resolution
    - Define `CorrelationResult` with `ExistingReport *v1alpha1.IncidentReport` and `ShouldCreate bool`
    - Define `IncidentCorrelatorInterface` with `Correlate(ctx context.Context, workload *v1alpha1.WorkloadRef, trigger TriggerResult) (CorrelationResult, error)`
    - Primary lookup is a deterministic GET by active name (`<workload-name>-<kind>-active`), not a label selector scan
    - _Requirements: Req 5_

  - [ ] 6.2 Implement active-name GET and correlation decision
    - Implement `IncidentCorrelator` struct accepting a `client.Client`
    - GET `IncidentReport` by its active name (from `GenerateActiveName`)
    - If found AND `status.phase != Resolved` → active incident exists; return it with `ShouldCreate = false`
    - If found AND `status.phase == Resolved` → stale active slot (post-crash mid-transition); trigger cleanup and return `ShouldCreate = true` for a new incident
    - If NotFound → return `ShouldCreate = true`
    - On AlreadyExists during CREATE → fetch existing object by active name and continue with update path
    - _Requirements: Req 5.1–5.5, Req 14.1, Req 21_

  - [ ] 6.3 Implement IncidentReport name generation
    - Create `internal/investigation/naming.go`
    - Implement `GenerateActiveName(workloadName string, workloadKind string) string`
      - Pattern: `<workload-name>-<kind-lowercase>-active`
      - Examples: `payment-api-deployment-active`, `worker-daemonset-active`
      - For Pod-level fallback: `<pod-name>-pod-active`
      - Truncate workload name so total length ≤ 63 characters
    - Implement `GenerateHistoricalName(workloadName string, workloadKind string, namespace string, startedAt time.Time) string`
      - Pattern: `<workload-name>-<kind-lowercase>-<YYYYMMDD>-<5hex>`
      - `<5hex>` = first 5 chars of hex-encoded FNV-32a hash of `namespace/workloadKind/workloadName/startedAt-unix-seconds`
      - Truncate to 63 characters
    - Define a package-level named constant `const maxNameLength = 63` in `naming.go` and use it for all truncation logic. Do not use the magic number 63 inline.
    - _Requirements: Req 3, Req 21_

  - [ ]* 6.4 Write unit tests for incident correlation
    - Create `test/unit/correlator_test.go`
    - Test: active report exists (phase=Investigating) → `ShouldCreate = false`, existing report returned
    - Test: no active report → `ShouldCreate = true`
    - Test: active-named report found with phase=Resolved (stale) → `ShouldCreate = true`
    - Test `GenerateActiveName` returns correct `-active` suffix for various workload kinds
    - Test `GenerateHistoricalName` returns deterministic name for same inputs
    - Test `GenerateHistoricalName` returns different names for different `startedAt` values
    - Test `GenerateActiveName` for name length capping (long workload name truncation)
    - _Requirements: Req 5, Req 3, Req 21_

  - [ ]* 6.5 Write property-based test for correlation (Property 6)
    - In `test/unit/correlator_test.go`, add a rapid property test
    - **Property 6**: For any workload identity `(namespace, kind, name)`, the deterministic active incident name `<workload-name>-<kind-lowercase>-active` SHALL be computed identically regardless of which reconciler instance computes it or how many times it is computed — tag: `// Feature: incident-investigator-foundation, Property 6`
    - _Requirements: Req 5.1–5.4, Req 21.3, Req 21.4_

  - [ ] 6.6 Implement incident resolution transition (active → historical)
    - Create `internal/investigation/resolution.go`
    - Implement `ResolutionTransitioner` that performs the copy-then-delete when resolving an active incident:
      1. Issue a **single PATCH** to the active IncidentReport that atomically sets `status.phase = Resolved`, `status.resolvedAt = now`, and removes the `investigator.k8s.io/active` label from `metadata.labels`. This is one API call, not two separate PATCHes. Combining phase, resolvedAt, and label removal into one operation ensures the object is unambiguously in the Resolved+inactive state after this step, making crash recovery deterministic.
      2. Generate historical name using `GenerateHistoricalName`
      3. CREATE a new IncidentReport with the historical name. Construct a new IncidentReport object with the historical name. Copy `spec`, `status`, and relevant labels/annotations from the active report. Explicitly exclude all Kubernetes server-managed metadata: `metadata.uid`, `metadata.resourceVersion`, `metadata.creationTimestamp`, `metadata.managedFields`, `metadata.deletionTimestamp`, `metadata.finalizers`. The new object must be a fresh Kubernetes resource — do not attempt to set server-assigned fields.
      4. DELETE the active-named IncidentReport
    - On crash between steps 3 and 4: the restart handler (task 9.1 startup recovery) must detect that both active-named AND historical-named reports exist for the same workload (active one is Resolved) and complete the DELETE of the active-named report
    - _Requirements: Req 7.4, Req 9.1, Req 21.1–21.5_

- [ ] 7. Recovery evaluation (`internal/investigation/recovery.go`)
  - [ ] 7.1 Define `RecoveryEvaluatorInterface`, `RecoveryResult`, and the `RecoveryEvaluator` struct
    - Create `internal/investigation/recovery.go`
    - Define `RecoveryResult` with `WorkloadHealthy bool`, `StabilityPeriodElapsed bool`, `ShouldResolve bool`, `ShouldResetStabilityTimer bool`
    - Define `WorkloadSnapshot` struct with fields: `Ref *v1alpha1.WorkloadRef`, `Deployment *appsv1.Deployment`, `StatefulSet *appsv1.StatefulSet`, `DaemonSet *appsv1.DaemonSet`, `Job *batchv1.Job`, `LatestJob *batchv1.Job` (for CronJob — most recent Job owned by the CronJob), `Pods []corev1.Pod` (for Pod-level fallback when no workload is resolved); only the field corresponding to the workload kind is populated
    - Define `RecoveryEvaluatorInterface` with `Evaluate(ctx context.Context, report *v1alpha1.IncidentReport, workload WorkloadSnapshot, cfg *config.Config) RecoveryResult`
    - _Requirements: Req 8_

  - [ ] 7.2 Implement workload health check and stability period logic
    - Implement `RecoveryEvaluator` struct
    - Workload-type-aware health checks:
      - `Deployment`: `status.readyReplicas >= status.replicas AND status.availableReplicas >= spec.replicas`
      - `StatefulSet`: `status.readyReplicas >= spec.replicas`
      - `DaemonSet`: `status.numberReady >= status.desiredNumberScheduled`
      - `Job`: `status.succeeded >= 1` (at least one successful completion)
      - `CronJob`: evaluate `LatestJob` using the Job rule above
      - Pod-level fallback (no workload resolved): `phase == Running AND all containers ready == true` in `containerStatuses`
    - When healthy and `stabilityStartedAt` is nil: set `ShouldResetStabilityTimer = false`, signal that stabilityStartedAt should be set to now
    - When healthy and `stabilityStartedAt` is set: check if `now - stabilityStartedAt >= cfg.StabilityPeriod`; if so, set `ShouldResolve = true` and `StabilityPeriodElapsed = true`
    - When a new failure trigger is detected during the stability window: set `ShouldResetStabilityTimer = true`
    - When workload is unhealthy: set `WorkloadHealthy = false`, set `ShouldResetStabilityTimer = true` if stabilityStartedAt is set
    - WHEN workload ownership is resolved: recovery SHALL be determined from the workload-specific status fields (readyReplicas, availableReplicas, etc.). Pod absence alone SHALL NOT determine workload recovery. WHEN ownership cannot be resolved: Pod-level health is used as the fallback, and an empty Pod list does not count as healthy.
    - Note on state-based failures and the stability timer: A workload with any Pod in a state-based failure condition (e.g., CrashLoopBackOff, OOMKilled) will not satisfy the workload health check above (readyReplicas will be below the desired count). Therefore `stabilityStartedAt` will never be set while a state-based failure persists — the explicit `ShouldResetStabilityTimer` path is primarily relevant for event-based triggers where the Pod object may transiently appear healthy before Event.count clears.
    - _Requirements: Req 7.3, Req 7.4, Req 8.1–8.5_

  - [ ]* 7.3 Write unit tests for recovery evaluation
    - Create `test/unit/recovery_test.go`
    - Test: Deployment with `readyReplicas >= replicas` AND `availableReplicas >= spec.replicas` → `WorkloadHealthy = true`
    - Test: Deployment with `readyReplicas < replicas` → `WorkloadHealthy = false`
    - Test: StatefulSet with `readyReplicas >= spec.replicas` → `WorkloadHealthy = true`
    - Test: DaemonSet with `numberReady >= desiredNumberScheduled` → `WorkloadHealthy = true`
    - Test: Job with `succeeded >= 1` → `WorkloadHealthy = true`
    - Test: all Pods ready (Pod-level fallback) → `WorkloadHealthy = true`
    - Test: one Pod not ready (Pod-level fallback) → `WorkloadHealthy = false`
    - Test: healthy with stabilityStartedAt set and period elapsed → `ShouldResolve = true`
    - Test: healthy with stabilityStartedAt set but period not elapsed → `ShouldResolve = false`
    - Test: new failure during stability window → `ShouldResetStabilityTimer = true`
    - Test: empty Pod list → not healthy
    - _Requirements: Req 8_

  - [ ]* 7.4 Write property-based tests for recovery evaluation (Properties 8 and 10)
    - In `test/unit/recovery_test.go`, add rapid property tests
    - **Property 8**: For any `IncidentReport` with non-nil `stabilityStartedAt` and any new failure trigger, `ShouldResetStabilityTimer = true` — tag: `// Feature: incident-investigator-foundation, Property 8`
    - **Property 10**: For any threshold T and counts C1 ≤ C2, if trigger fires for C1 it also fires for C2 — tag: `// Feature: incident-investigator-foundation, Property 10`
    - _Requirements: Req 8.2, Req 8.3, Req 2.3–2.5_

- [ ] 8. Pod reconciler controller (`internal/controller/pod_reconciler.go`)
  - [ ] 8.1 Define the `PodReconciler` struct with dependency injection
    - Create `internal/controller/pod_reconciler.go`
    - Define `PodReconciler` struct with fields: `client.Client`, `Scheme *runtime.Scheme`, `Config *config.Config`, `TriggerEvaluator investigation.TriggerEvaluatorInterface`, `OwnershipResolver investigation.OwnershipResolverInterface`, `Correlator investigation.IncidentCorrelatorInterface`, `RecoveryEvaluator investigation.RecoveryEvaluatorInterface`, `Log logr.Logger`
    - Add all RBAC marker annotations from the design exactly (pods, events, nodes, replicasets, deployments, statefulsets, daemonsets, jobs, cronjobs, incidentreports, incidentreports/status, incidentreports/finalizers)
    - Change the incidentreports RBAC line to include `delete`:
      `// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports,verbs=get;list;watch;create;update;patch;delete`
    - _Requirements: Req 16_

  - [ ] 8.2 Implement `SetupWithManager` with the relevance predicate
    - Implement `SetupWithManager(mgr ctrl.Manager) error`
    - Register the controller to watch `corev1.Pod` resources
    - Implement `relevancePredicate()` that filters out `Succeeded` phase events, graceful termination events, and events where the Pod is not in a namespace covered by `Config.WatchNamespaces` (empty = all namespaces)
    - Apply the predicate via `WithEventFilter`
    - Add a `Watches()` call for `corev1.Event` with a `handler.EnqueueRequestsFromMapFunc` that maps each Event's `involvedObject` (when `kind = Pod`) to a reconcile request for that Pod
    - Implement `relevantEventPredicate()` filtering Events by reason to `FailedMount`, `Unhealthy`, `FailedScheduling` only
    - This ensures the reconciler is triggered when Event.count values change without a corresponding Pod object change
    - _Requirements: Req 18.1_

  - [ ] 8.3 Implement the 10-step `Reconcile()` function
    - Step 1: Fetch Pod from cache; if not found, check for an active IncidentReport for this key; return nil if neither exists (Pod deleted, nothing to do)
    - Step 2: Quick state-based trigger check — inspect current Pod object only, no API calls. If a state-based trigger is detected, call `TriggerEvaluator.Evaluate()` immediately with a `TriggerContext{Pod: pod}` (zero event counts) to produce a fully-populated `TriggerResult` (including `Type`, `ContainerName`, `Source=StateBased`, `IsImmediate=true`), then proceed directly to step 5 using that result. Do not skip the evaluator — the evaluator produces the `TriggerResult` that subsequent steps depend on.
    - Step 3: If not a state-based trigger, fetch Kubernetes Events for this Pod and extract Event.count values for mount, readiness, liveness, and scheduling failure events
    - Step 4: Run full `TriggerEvaluator` with the complete `TriggerContext` (Pod + Event.count values); if `IsTrigger = false`, return (no-op, no writes)
    - Step 5: Resolve workload ownership via `OwnershipResolver`
    - Step 6: Build the deterministic `IncidentReport` name from workload identity (or Pod name for fallback)
    - Step 7: GET the `IncidentReport` by deterministic name; if it exists and is active → proceed to update path
    - Step 8: If not found: CREATE `IncidentReport` (phase=Investigating); on AlreadyExists → fetch existing object by name and continue with update path
    - Step 9: PATCH status additively — add Pod to `AffectedPods` (dedup by UID before appending); set `Trigger` only if currently nil (preserve original trigger); update `FailureCount` per semantics (state-based: increment only when a new unique failure signature is observed — different container or reoccurrence after stability reset; event-based: set from current Event.count value); set `LastFailureAt` to now
    - Step 10: Fetch `WorkloadSnapshot` for recovery evaluation. Population logic: if `report.Spec.Workload` is non-nil and `status.workloadOwnerResolved == true`, GET the resource matching `Workload.Kind` (e.g., GET the Deployment by namespace/name from `Workload`) and populate only that field — all other workload fields remain nil. If `report.Spec.Workload` is nil or `status.workloadOwnerResolved == false`, fall back to the Pod-level path: fetch the Pods listed in `status.affectedPods` that still exist and populate `WorkloadSnapshot.Pods`. An empty Pod list is not considered healthy.
    - Step 11: Evaluate recovery (workload-type-aware).
      If `ShouldResolve=true`:
        a. PATCH active IncidentReport status (phase=Resolved, resolvedAt=now, clear stabilityStartedAt)
        b. CREATE historical IncidentReport with `GenerateHistoricalName`
        c. DELETE active-named IncidentReport
        d. Return empty result (no requeue needed)
      If `ShouldResetStabilityTimer=true`: PATCH status (clear stabilityStartedAt)
      If workload newly healthy: PATCH status (set stabilityStartedAt=now)
    - Step 12: Return `ctrl.Result{RequeueAfter: cfg.RequeueInterval}` for active incidents; return empty result for resolved or non-trigger paths
    - _Requirements: Req 3, Req 5, Req 6, Req 7, Req 8, Req 10, Req 14_

  - [ ] 8.4 Implement structured logging throughout `Reconcile()`
    - Log at `Info` level: incident created (include name, workload, trigger), incident reused (include existing report name), phase transition (include from/to phases), incident resolved
    - Log at `Debug` level: trigger evaluation result, ownership resolution steps, recovery evaluation result
    - Log at `Error` level: Kubernetes API failures with context (pod, workload, report names)
    - Never include Secret values, raw log contents, or full event messages in log output
    - _Requirements: Req 19_

  - [ ] 8.5 Implement status condition management
    - Set `Active` condition: `True` when phase is not `Resolved`, `False` otherwise
    - Set `WorkloadOwnerResolved` condition: `True` when ownership was resolved, `False` with descriptive reason when not
    - Set `StabilityPeriodStarted` condition: `True` when `stabilityStartedAt` is non-nil, `False` otherwise
    - Use `meta.SetStatusCondition` from `k8s.io/apimachinery/pkg/api/meta`
    - _Requirements: Req 7, Req 8_

- [ ] 9. Controller manager entrypoint (`cmd/main.go`)
  - [ ] 9.1 Implement `cmd/main.go` with manager setup, scheme registration, and dependency wiring
    - Register `corev1` and `v1alpha1` schemes with the manager's scheme
    - Parse configuration from flags: `--stability-period`, `--correlation-window`, `--readiness-threshold`, `--liveness-threshold`, `--mount-threshold`, `--scheduling-threshold`, `--requeue-interval`, `--watch-namespaces`, `--leader-elect`
    - Call `config.Validate()` on startup; exit non-zero if invalid
    - Construct concrete implementations: `investigation.NewTriggerEvaluator()`, `investigation.NewOwnershipResolver(mgr.GetClient())`, `investigation.NewIncidentCorrelator(mgr.GetClient())`, `investigation.NewRecoveryEvaluator()`
    - Inject all dependencies into `PodReconciler` and call `SetupWithManager`
    - Startup recovery: list ALL IncidentReport objects (both active and Resolved). For each IncidentReport whose name ends with `-active`: (a) if `phase != Resolved` → genuine active incident; enqueue a reconcile request for each Pod in `status.affectedPods` that still exists; IF no affected Pods exist (all deleted — e.g., Deployment scaled to 0, Job completed, or Pods replaced before restart): enqueue a synthetic reconcile request using the workload reference (`report.Spec.Workload.Namespace/Name` if resolved, or the last Pod entry in `status.affectedPods` otherwise) so the controller can evaluate workload health — do NOT silently skip these incidents, as they would be orphaned forever with no watch event to trigger them; (b) if `phase == Resolved` → crash mid-transition detected; check whether the corresponding historical IncidentReport exists using `GenerateHistoricalName` with `status.startedAt`; if historical exists → DELETE the active-named report; if historical does not exist → CREATE historical report then DELETE active-named report. If the list operation fails, log the error and continue without blocking startup.
    - Start the manager with leader election support
    - _Requirements: Req 11.2 (startup discovery), Req 18 (bounded configuration)_

- [ ] 10. Kubernetes manifests and code generation
  - [ ] 10.1 Run `make manifests` to generate CRD and RBAC manifests
    - Execute `make manifests` to produce `config/crd/bases/investigation.k8s.io_incidentreports.yaml` from the Kubebuilder markers on the API types
    - Execute `make manifests` to produce `config/rbac/role.yaml` from the RBAC markers on `PodReconciler`
    - Verify the CRD YAML includes all printer columns and the status subresource
    - Verify the RBAC role contains no delete/update/patch permissions for Pods, Deployments, StatefulSets, DaemonSets, or Services
    - Verify the RBAC role contains `delete` permission for `incidentreports` (required for resolution transition)
    - _Requirements: Req 13, Req 16_

  - [ ] 10.2 Create static Kubernetes manager manifests
    - Create `config/manager/manager.yaml` with a `Deployment` for the controller manager
    - Set `securityContext.runAsNonRoot: true`, `readOnlyRootFilesystem: true`
    - Reference the controller image placeholder `controller:latest`
    - Create `config/rbac/service_account.yaml` and `config/rbac/role_binding.yaml`
    - Create `config/rbac/leader_election_role.yaml` and `config/rbac/leader_election_role_binding.yaml`
    - _Requirements: Req 16_

  - [ ] 10.3 Create a sample `IncidentReport` manifest and namespace-scoped kustomization
    - Create `config/samples/investigation_v1alpha1_incidentreport.yaml` as a minimal example
    - Create `config/default/kustomization.yaml` composing CRD, RBAC, and manager resources
    - _Requirements: Req 13, Req 20_

- [ ] 11. Unit tests — idempotency and Pod deduplication
  - [ ] 11.1 Write idempotency tests for incident status update logic (Property 9)
    - Create `test/unit/idempotency_test.go`
    - Test: applying the same `TriggerResult` + `WorkloadRef` to an existing `IncidentReport` twice produces the same `StartedAt`, `AffectedPods`, and `FailureCount` as applying it once
    - Verify `AffectedPods` deduplication: adding the same Pod UID twice yields exactly one entry (Property 7)
    - Test: state-based trigger on the same Pod state does NOT increment `FailureCount` on second evaluation (same container, no recovery between evaluations)
    - Test: event-based trigger sets `FailureCount` to the Event.count value (not independently incremented)
    - **Property 9**: tag: `// Feature: incident-investigator-foundation, Property 9`
    - **Property 7**: tag: `// Feature: incident-investigator-foundation, Property 7`
    - Use rapid to generate random reconciliation input sequences
    - _Requirements: Req 10, Req 14_

  - [ ] 11.2 Checkpoint — run `go test ./test/unit/...` and confirm all unit tests pass
    - Ensure all tests pass, ask the user if questions arise.

- [ ] 12. Integration tests (`test/integration`)
  - [ ] 12.1 Set up the envtest integration test suite
    - Create `test/integration/suite_test.go` using Ginkgo/Gomega
    - Configure `envtest.Environment` with the CRD path pointing to `config/crd/bases`
    - Register schemes (`corev1`, `v1alpha1`)
    - Start the controller manager with default `Config` in a `BeforeSuite` block
    - _Requirements: Req 20_

  - [ ] 12.2 Write integration test: OOMKilled Pod creates an IncidentReport
    - Create `test/integration/reconciler_test.go`
    - Deploy a fake Pod with OOMKilled container status via the envtest client
    - Trigger reconciliation
    - Assert that an `IncidentReport` is created with `phase = Investigating`, correct workload reference, the Pod in `AffectedPods`, and name matching the deterministic pattern `<workload-name>-deployment`
    - Assert that a second reconciliation for the same Pod does not create a duplicate
    - _Requirements: Req 1.1, Req 3, Req 14.1, Req 20.1–20.4_

  - [ ] 12.3 Write integration test: Pod ownership resolution through RS and Deployment
    - Create a `ReplicaSet` and `Deployment` in envtest
    - Create a Pod with ownerRef pointing to the RS
    - Trigger reconciliation
    - Assert the `IncidentReport` spec references the `Deployment`, not the RS or Pod
    - _Requirements: Req 4.2_

  - [ ] 12.4 Write integration test: recovery and stability period resolution
    - Create an `IncidentReport` in the `Investigating` phase
    - Create a healthy Pod for the same workload
    - Set `Config.StabilityPeriod` to a short value (e.g., 2 seconds) for test speed
    - Wait for the stability period to elapse
    - Trigger reconciliation
    - Assert the `IncidentReport` transitions to `Resolved` with a non-nil `resolvedAt`
    - _Requirements: Req 7.4, Req 8, Req 20.5_

  - [ ] 12.5 Write integration test: controller restart recovers active incidents
    - Create an active `IncidentReport` directly via the envtest client (simulating pre-existing state)
    - Restart the controller manager within the test
    - Assert the controller discovers the existing `IncidentReport` and continues managing it without creating a duplicate
    - _Requirements: Req 11, Req 8.5_

  - [ ] 12.6 Write integration test: missing Pod during reconciliation
    - Delete a Pod that is referenced in an `IncidentReport`'s `AffectedPods`
    - Trigger reconciliation for the now-deleted Pod name
    - Assert the controller returns without error and the `IncidentReport` is preserved with the historical Pod reference intact
    - _Requirements: Req 12, Req 15.1_

  - [ ] 12.7 Write integration test: duplicate CREATE race
    - Start two goroutines both reconciling the same OOMKilled Pod simultaneously
    - Assert exactly one `IncidentReport` is created (deterministic name prevents duplicate)
    - Assert both reconcilers converge to the same `IncidentReport` state
    - _Requirements: Req 5.1, Req 14.1_

  - [ ] 12.9 Write integration tests for temporal correlation and lifecycle transitions
    - Test: failure at t=0, new failure at t=5min → same active IncidentReport updated (within correlation window)
    - Test: incident resolved at t=30min, new failure at t=60min → new active IncidentReport created, historical preserved
    - Test: active incident resolved → historical IncidentReport created with unique name, active slot freed
    - Test: crash mid-resolution (historical created, active not deleted) → restart detects and completes cleanup
    - Test: Deployment scaling up (availableReplicas < spec.replicas) → incident NOT resolved prematurely
    - Test: Deployment rolling update in progress → incident NOT resolved prematurely
    - Test: zero failure count → threshold-based trigger NOT fired (Event.count = 0 < threshold)
    - Test: failure count == threshold → trigger fired (Event.count >= threshold)
    - Test: Pod deletion during active incident → incident remains active
    - Test: replacement Pod during active incident → associated with existing incident
    - Test: replacement Pod after incident resolved → new incident created (not old incident reopened)
    - Test: Pod in steady Running state, FailedMount Event.count reaches threshold → reconciliation triggered by Event watch, incident created
    - Test: Multiple FailedMount Event objects for same Pod → highest event.count used for threshold, not sum
    - _Requirements: Req 5.6, Req 7, Req 8, Req 9, Req 12, Req 21_

  - [ ] 12.10 Checkpoint — run `go test ./test/integration/...` and confirm all integration tests pass
    - Ensure all tests pass, ask the user if questions arise.

- [ ] 13. Kind e2e scenario (`deploy/kind`)
  - [ ] 13.1 Create the Kind cluster configuration and setup script
    - Create `deploy/kind/kind-config.yaml` defining a single-node Kind cluster
    - Create `deploy/kind/setup.sh` that: creates the Kind cluster, builds the controller image with `make docker-build`, loads the image into Kind with `kind load docker-image`, applies CRDs and RBAC with `kubectl apply -k config/default`, and deploys the manager
    - _Requirements: Req 20_

  - [ ] 13.2 Create the e2e test workload manifests
    - Create `deploy/kind/test-workload/oom-crasher.yaml` — a Deployment with a container that deliberately exceeds its memory limit (e.g., using `stress` or a small Go binary)
    - Create `deploy/kind/test-workload/image-pull-fail.yaml` — a Deployment referencing a deliberately invalid image name to exercise `ImagePullBackOff`
    - _Requirements: Req 20.1_

  - [ ] 13.3 Create the e2e validation script
    - Create `deploy/kind/validate.sh` that:
      1. Applies the OOM crasher workload
      2. Waits up to 60 seconds for an `IncidentReport` to appear via `kubectl get incidentreports`
      3. Asserts `phase = Investigating`
      4. Triggers a second OOM event and asserts no duplicate `IncidentReport` is created
      5. Patches the workload to use a healthy image, waits for the stability period, asserts `phase = Resolved`
      6. Verifies the historical resolved `IncidentReport` is preserved after a new incident is created
    - Exit non-zero on any assertion failure
    - _Requirements: Req 20_

- [ ] 14. Build, CI, and documentation
  - [ ] 14.1 Add a GitHub Actions CI workflow
    - Create `.github/workflows/ci.yml`
    - Jobs:
      - `fmt`: run `gofmt -l .` and fail if output is non-empty
      - `vet`: run `go vet ./...`
      - `lint`: run `golangci-lint run` (add `.golangci.yml` with sensible defaults)
      - `unit-test`: run `go test ./test/unit/... -v -count=1`
      - `integration-test`: run `go test ./test/integration/... -v -count=1`
      - `build`: run `make build`
      - `docker-build`: run `make docker-build`
    - All jobs run on `ubuntu-latest` with Go 1.23+ (verify version against controller-runtime compatibility)
    - CI fails on any test or build error
    - _Requirements: Req 20 (demonstrable)_

  - [ ] 14.2 Write the README with usage and development instructions
    - Create or update `README.md` with sections:
      - Project overview (one paragraph)
      - Prerequisites (Go 1.23+, kubectl, Kind, Docker)
      - Quick start: Kind cluster setup using `deploy/kind/setup.sh`
      - How to inspect incidents: `kubectl get incidentreports` and `kubectl describe incidentreport <name>`
      - Configuration reference: all flags and their defaults
      - Development: `make generate`, `make manifests`, `make test`, `make build`
      - Running integration tests: `go test ./test/integration/...`
    - _Requirements: Req 13, Req 20_

  - [ ] 14.3 Final checkpoint — full build and test suite
    - Run `make generate && make manifests && make build && go test ./...`
    - Ensure all tests pass, ask the user if questions arise.

---

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP pass; all non-starred tasks must be implemented
- Property tests reference their design property number via comment tag `// Feature: incident-investigator-foundation, Property N`
- Each property test uses `pgregory.net/rapid` and runs a minimum of 100 iterations
- The design's Correctness Properties section defines 10 properties — all are covered by tasks 4.5, 5.4, 6.5, 7.4, and 11.1
- Status updates use strategic merge semantics; immutable fields (`StartedAt`, initial `Trigger`) are only written when currently zero/nil
- `stabilityStartedAt` is stored in the `IncidentReport` status (not in memory) to survive controller restarts
- The correlator uses deterministic active-name GET (`<workload-name>-<kind>-active`). On resolution, the active report is copied to a unique historical name and deleted. No additional state store is required. Delete permission on incidentreports is required for this transition.
- All API calls are wrapped with context-aware errors; transient failures return a retry error compatible with controller-runtime's exponential backoff

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2", "1.3"] },
    { "id": 1, "tasks": ["2.1", "2.2"] },
    { "id": 2, "tasks": ["2.3"] },
    { "id": 3, "tasks": ["2.4"] },
    { "id": 4, "tasks": ["2.5", "3.1"] },
    { "id": 5, "tasks": ["3.2", "4.1"] },
    { "id": 6, "tasks": ["4.2"] },
    { "id": 7, "tasks": ["4.3"] },
    { "id": 8, "tasks": ["4.4", "4.5", "5.1"] },
    { "id": 9, "tasks": ["5.2"] },
    { "id": 10, "tasks": ["5.3", "5.4", "6.1"] },
    { "id": 11, "tasks": ["6.2", "6.3"] },
    { "id": 12, "tasks": ["6.4", "6.5", "6.6", "7.1"] },
    { "id": 13, "tasks": ["7.2"] },
    { "id": 14, "tasks": ["7.3", "7.4", "8.1"] },
    { "id": 15, "tasks": ["8.2", "8.3"] },
    { "id": 16, "tasks": ["8.4", "8.5"] },
    { "id": 17, "tasks": ["9.1"] },
    { "id": 18, "tasks": ["10.1"] },
    { "id": 19, "tasks": ["10.2", "10.3"] },
    { "id": 20, "tasks": ["11.1"] },
    { "id": 21, "tasks": ["11.2"] },
    { "id": 22, "tasks": ["12.1"] },
    { "id": 23, "tasks": ["12.2", "12.3"] },
    { "id": 24, "tasks": ["12.4", "12.5", "12.6"] },
    { "id": 25, "tasks": ["12.7", "12.9"] },
    { "id": 26, "tasks": ["13.1"] },
    { "id": 27, "tasks": ["13.2"] },
    { "id": 28, "tasks": ["13.3", "14.1", "14.2"] },
    { "id": 29, "tasks": ["14.3"] }
  ]
}
```
