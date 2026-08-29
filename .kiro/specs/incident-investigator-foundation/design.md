# Design Document — Incident Investigator Foundation

## Overview

The Incident Investigator Foundation provides the core infrastructure for the Kubernetes Incident
Investigator: a Kubernetes-native controller that detects meaningful workload failures, creates
persistent `IncidentReport` Custom Resources, tracks affected Pods, manages the incident lifecycle,
and makes investigation state visible through standard `kubectl` tooling.

This design covers the foundation layer only. It does not include detailed evidence collection,
log analysis, root-cause diagnosis, or recommendation generation — those are addressed in
subsequent specifications.

The foundation establishes four durable guarantees:

1. **Persistence-first** — an `IncidentReport` is written to Kubernetes before investigation proceeds,
   so controller restarts never lose active incident state.
2. **Workload-centric identity** — incidents are keyed on the owning workload, not on the ephemeral
   Pod, so Pod replacement does not fragment a continuous failure into multiple incidents.
3. **Idempotent reconciliation** — running the same reconciliation loop any number of times
   produces the same logical incident state.
4. **Bounded operations** — every threshold, stability period, and in-memory structure has an
   explicit limit.

---

## Architecture

### High-Level Component Flow

```text
Kubernetes API Server
        │
        ├─── watch (Pods)
        │
        └─── watch (Events: FailedMount, Unhealthy, FailedScheduling)
             │ involvedObject.kind = Pod → enqueue Pod reconcile
        ▼
 controller-runtime cache
        │
        │  reconcile request → work queue
        ▼
  PodReconciler.Reconcile()           ← thin orchestration only
        │
        ▼
  TriggerEvaluator                    ← internal/investigation
  (is this Pod state a trigger?)
        │
        ├── not a trigger → return (no-op)
        │
        ▼
  OwnershipResolver                   ← internal/investigation
  (Pod → RS → Deployment / StatefulSet / DaemonSet / Job / CronJob)
        │
        ▼
  IncidentCorrelator                  ← internal/investigation
  (find or create IncidentReport by deterministic name)
        │
        ├── active IncidentReport found → update
        │
        └── not found → create IncidentReport first, then update
        │
        ▼
  RecoveryEvaluator                   ← internal/investigation
  (workload-type-aware health check; stability period elapsed?)
        │
        ├── not recovered → requeue after interval
        │
        └── recovered → transition to Resolved
        │
        ▼
  IncidentReport persisted in etcd
```

### Package Dependency Diagram

```text
cmd/main.go
    │
    ▼
internal/controller          (thin: orchestration + k8s API calls only)
    │
    ▼
internal/investigation       (domain logic: trigger, correlation, lifecycle)
    │
    ├──▶  api/v1alpha1        (types only — no logic)
    │
    └──▶  internal/config     (configuration + defaults)
```

> **Note:** `internal/evidence`, `internal/diagnosis`, and `internal/reporting` are created in
> future specifications and **must NOT** be created as empty placeholder directories during
> foundation implementation.

The `investigation` package contains all domain logic and must not import `controller`.
The `controller` package calls `investigation`; never the reverse.

---

## Components and Interfaces

### api/v1alpha1 — CRD Types

Holds the Go type definitions for `IncidentReport`. No business logic lives here.

Key marker annotations used:

- `+kubebuilder:object:root=true` — registers the type with controller-gen
- `+kubebuilder:subresource:status` — enables the `/status` subresource
- `+kubebuilder:resource:shortName=ir,categories=investigator` — short name for kubectl
- `+kubebuilder:printcolumn` — defines kubectl column output

### internal/controller — PodReconciler

Responsibilities (thin by design):

- Register watches with controller-runtime
- Receive reconcile requests from the work queue
- Fetch the current Pod from the cache
- Call `TriggerEvaluator` to determine relevance
- Call `OwnershipResolver` to find the workload
- Call `IncidentCorrelator` to find or create the `IncidentReport`
- Call `RecoveryEvaluator` to check if the incident should be resolved
- Write updates to Kubernetes via the client
- Return `ctrl.Result` with requeue interval or error

`Reconcile()` must not contain threshold logic, ownership traversal logic, or lifecycle
state machine logic. These live in `internal/investigation`.

### internal/investigation — Domain Logic

Contains four focused sub-concerns:

| Sub-component | Responsibility |
|---|---|
| `TriggerEvaluator` | Classify a Pod state into a `TriggerType` and determine whether it crosses the threshold |
| `OwnershipResolver` | Traverse owner references from Pod to its top-level workload |
| `IncidentCorrelator` | Find an active `IncidentReport` by deterministic name, or determine one must be created |
| `RecoveryEvaluator` | Determine if a workload is healthy (workload-type-aware) and whether the stability period has elapsed |

These are pure or near-pure functions that operate on structured inputs. The controller
passes in Kubernetes API results; the investigation package does not call the Kubernetes API
directly — **with one explicit exception**: `OwnershipResolver` is injected with a
`client.Client` because the ownership traversal chain (Pod → RS → Deployment, etc.) requires
fetching intermediate resources whose identity is only known after inspecting the Pod's owner
references. All API calls within `OwnershipResolver` are bounded, read-only, and scoped to
the resources in the ownership chain. All other investigation components (`TriggerEvaluator`,
`IncidentCorrelator`, `RecoveryEvaluator`) are pure functions operating on pre-fetched inputs.

### internal/config — Configuration

Provides the `Config` struct and defaults. Loaded once at startup, passed via dependency
injection into investigation components.

---

## Data Models

### IncidentPhase

```go
// IncidentPhase represents the current lifecycle state of an incident.
// +kubebuilder:validation:Enum=Investigating;Diagnosed;Unknown;Resolved
type IncidentPhase string

const (
    // PhaseInvestigating is set when the incident is first detected.
    // It remains the active phase until diagnosis or resolution.
    PhaseInvestigating IncidentPhase = "Investigating"

    // PhaseDiagnosed is set when a diagnosis rule matches the collected evidence.
    // Set by future diagnosis specification, not the foundation.
    PhaseDiagnosed IncidentPhase = "Diagnosed"

    // PhaseUnknown is set when investigation completes but no diagnosis rule matched.
    // Set by future diagnosis specification, not the foundation.
    PhaseUnknown IncidentPhase = "Unknown"

    // PhaseResolved is set when the workload has been healthy for the full stability period.
    PhaseResolved IncidentPhase = "Resolved"
)
```

### TriggerType

```go
// TriggerType identifies the class of failure signal that created or updated an incident.
type TriggerType string

const (
    TriggerOOMKilled                  TriggerType = "OOMKilled"
    TriggerCrashLoopBackOff           TriggerType = "CrashLoopBackOff"
    TriggerImagePullBackOff           TriggerType = "ImagePullBackOff"
    TriggerCreateContainerConfigError TriggerType = "CreateContainerConfigError"
    TriggerMountFailure               TriggerType = "MountFailure"
    TriggerReadinessProbeFailure      TriggerType = "ReadinessProbeFailure"
    TriggerLivenessProbeFailure       TriggerType = "LivenessProbeFailure"
    TriggerSchedulingFailure          TriggerType = "SchedulingFailure"
    TriggerEviction                   TriggerType = "Eviction"
)
```

### TriggerSource

```go
// TriggerSource identifies how a trigger was detected — from current Pod state or from
// Kubernetes Event counts.
type TriggerSource string

const (
    // TriggerSourceStateBased means the trigger was detected from current Pod object state.
    // State-based triggers are idempotent: the same Pod state on reconciliation N and N+1
    // represents the same condition, not two distinct failures.
    TriggerSourceStateBased TriggerSource = "StateBased"

    // TriggerSourceEventBased means the trigger was detected from Kubernetes Event counts.
    // The threshold check uses event.count >= threshold.
    TriggerSourceEventBased TriggerSource = "EventBased"
)
```

### WorkloadRef

```go
// WorkloadRef identifies the Kubernetes workload associated with an incident.
type WorkloadRef struct {
    // Kind is the workload type: Deployment, StatefulSet, DaemonSet, Job, CronJob.
    Kind string `json:"kind"`

    // Name is the workload name.
    Name string `json:"name"`

    // Namespace is the workload namespace.
    Namespace string `json:"namespace"`

    // UID is the workload UID, used to detect workload replacement.
    // +optional
    UID types.UID `json:"uid,omitempty"`
}
```

### PodRef

```go
// PodRef identifies a Pod affected by an incident.
type PodRef struct {
    // Name is the Pod name.
    Name string `json:"name"`

    // Namespace is the Pod namespace.
    Namespace string `json:"namespace"`

    // UID is the Pod UID, used to distinguish replaced Pods with the same name.
    // +optional
    UID types.UID `json:"uid,omitempty"`
}
```

### TriggerInfo

```go
// TriggerInfo records the failure signal that created or most recently updated the incident.
type TriggerInfo struct {
    // Type is the classified trigger type.
    Type TriggerType `json:"type"`

    // Reason is the raw Kubernetes reason string from the Pod or Event, if available.
    // +optional
    Reason string `json:"reason,omitempty"`

    // ContainerName is the container that triggered the incident, if applicable.
    // +optional
    ContainerName string `json:"containerName,omitempty"`

    // ObservedAt is the time at which this trigger was first observed.
    // +optional
    ObservedAt *metav1.Time `json:"observedAt,omitempty"`

    // Message is a short human-readable description of the failure signal.
    // +optional
    Message string `json:"message,omitempty"`
}
```

### IncidentReportSpec

```go
// IncidentReportSpec contains the immutable identity of the incident.
// Fields in spec are set at creation and not modified during the lifecycle.
type IncidentReportSpec struct {
    // Workload is the workload associated with the incident.
    // Set when ownership can be resolved; may be empty if resolution failed.
    // +optional
    Workload *WorkloadRef `json:"workload,omitempty"`
}
// Note: The incident namespace is derived from metadata.namespace (standard Kubernetes
// convention for namespace-scoped resources). There is no spec.namespace field — storing
// the namespace in spec would create a second, potentially conflicting source of truth.
```

### IncidentReportStatus

```go
// IncidentReportStatus holds the dynamic investigation state of the incident.
// Updated throughout the incident lifecycle via the status subresource.
type IncidentReportStatus struct {
    // Phase is the current lifecycle state of the incident.
    // +optional
    Phase IncidentPhase `json:"phase,omitempty"`

    // StartedAt is the time the incident was first detected.
    // +optional
    StartedAt *metav1.Time `json:"startedAt,omitempty"`

    // ResolvedAt is the time the incident was resolved.
    // Set only when Phase = Resolved.
    // +optional
    ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

    // StabilityStartedAt records when the workload first became healthy after the incident.
    // Used to track the stability period without relying on in-memory state.
    // Reset to nil if a new failure occurs during the stability window.
    // +optional
    StabilityStartedAt *metav1.Time `json:"stabilityStartedAt,omitempty"`

    // AffectedPods is the list of Pods associated with this incident.
    // Pods are appended; existing entries are preserved even after Pod deletion.
    // +optional
    AffectedPods []PodRef `json:"affectedPods,omitempty"`

    // Trigger is the primary failure signal that created the incident.
    // +optional
    Trigger *TriggerInfo `json:"trigger,omitempty"`

    // WorkloadOwnerResolved indicates whether workload ownership was successfully determined.
    // False means the incident is tracked at Pod level (see AffectedPods).
    // +optional
    WorkloadOwnerResolved bool `json:"workloadOwnerResolved,omitempty"`

    // FailureCount tracks the number of distinct failure observations associated with this
    // incident. For state-based triggers, this increments only when a new unique failure
    // signature is observed (new container affected, or reoccurrence after recovery).
    // For event-based triggers, this is set to the current Kubernetes Event.count value.
    // +optional
    FailureCount int32 `json:"failureCount,omitempty"`

    // LastFailureAt records the time the most recent failure was observed.
    // +optional
    LastFailureAt *metav1.Time `json:"lastFailureAt,omitempty"`

    // Conditions provides standard Kubernetes condition semantics for the incident state.
    // +optional
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### IncidentReport (full type)

```go
// IncidentReport is the Schema for the incidentreports API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ir,categories=investigator
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=".spec.workload.name"
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=".spec.workload.kind"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Trigger",type=string,JSONPath=".status.trigger.type"
// +kubebuilder:printcolumn:name="Started",type=date,JSONPath=".status.startedAt"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type IncidentReport struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   IncidentReportSpec   `json:"spec,omitempty"`
    Status IncidentReportStatus `json:"status,omitempty"`
}
```

### Config

```go
// Config holds all configurable parameters for the Investigator.
// Loaded once at startup via flags or environment variables.
type Config struct {
    // WatchNamespaces is the list of namespaces to watch.
    // Empty means all namespaces.
    WatchNamespaces []string

    // StabilityPeriod is the duration a workload must remain healthy
    // before an active incident is marked Resolved.
    // Default: 5 minutes.
    StabilityPeriod time.Duration

    // CorrelationWindow defines the time-based correlation behavior.
    // When a failure arrives for a workload and NO active incident exists,
    // the system creates a new incident unconditionally.
    // When an active incident DOES exist, the window is not used as a gate;
    // failures are always associated with the active incident.
    // Default: 10 minutes.
    CorrelationWindow time.Duration

    // ReadinessProbeFailureThreshold is the number of Kubernetes Event counts for
    // readiness probe failures (event.count) required to create an incident.
    // Default: 3.
    ReadinessProbeFailureThreshold int

    // LivenessProbeFailureThreshold is the number of Kubernetes Event counts for
    // liveness probe failures (event.count) required to create an incident.
    // Default: 3.
    LivenessProbeFailureThreshold int

    // MountFailureThreshold is the number of Kubernetes Event counts for mount failures
    // (event.count) required to create an incident.
    // Default: 3.
    MountFailureThreshold int

    // SchedulingFailureThreshold is the number of Kubernetes Event counts for scheduling
    // failures (event.count) required to create an incident.
    // Default: 5.
    SchedulingFailureThreshold int

    // RequeueInterval is how often active incidents are re-evaluated.
    // Default: 30 seconds.
    RequeueInterval time.Duration
}
```

> **Go and controller-runtime version:** Use Go 1.23+ (verify current supported versions
> against the controller-runtime compatibility matrix before implementation). Check
> https://github.com/kubernetes-sigs/controller-runtime for the minimum Go version
> requirement for the current stable release.

---

## Sequence Diagram — Main Reconcile Path (OOMKilled)

```text
kubelet                API Server           controller-runtime      PodReconciler      investigation pkg       etcd
   │                        │                       │                     │                    │                  │
   │ container OOMKilled     │                       │                     │                    │                  │
   │──────────────────────▶ │                       │                     │                    │                  │
   │                        │ Pod status updated     │                     │                    │                  │
   │                        │──────────────────────▶│                     │                    │                  │
   │                        │                       │ Pod watch event      │                    │                  │
   │                        │                       │─────────────────────▶│                   │                  │
   │                        │                       │                     │ TriggerEvaluator   │                  │
   │                        │                       │                     │ (state-based check)│                  │
   │                        │                       │                     │───────────────────▶│                  │
   │                        │                       │                     │◀── TriggerOOMKilled (StateBased)      │
   │                        │                       │                     │                    │                  │
   │                        │                       │                     │ OwnershipResolver  │                  │
   │                        │                       │                     │───────────────────▶│                  │
   │                        │         get RS, Deployment                  │────────────────────────────── GET ──▶│
   │                        │◀──────────────────────────────────────────────────────────────────────────────────│
   │                        │                       │                     │◀── WorkloadRef(Deployment/payment-api)│
   │                        │                       │                     │                    │                  │
   │                        │         GET IncidentReport by name          │                    │                  │
   │                        │◀──────────────────────────────────────────────────────────────────────────────────│
   │                        │                       │                     │ IncidentCorrelator │                  │
   │                        │                       │                     │───────────────────▶│                  │
   │                        │                       │                     │◀── not found       │                  │
   │                        │                       │                     │                    │                  │
   │                        │                       │                     │ CREATE IncidentReport (deterministic name, phase=Investigating)
   │                        │                       │                     │───────────────────────────── CREATE ─▶│
   │                        │                       │                     │◀────────────────────── created ───────│
   │                        │                       │                     │                    │                  │
   │                        │                       │                     │ PATCH status       │                  │
   │                        │                       │                     │──────────────────────────── PATCH ──▶│
   │                        │                       │                     │◀─────────────────────── updated ──────│
   │                        │                       │                     │                    │                  │
   │                        │                       │                     │ RecoveryEvaluator  │                  │
   │                        │                       │                     │ (workload snapshot)│                  │
   │                        │                       │                     │───────────────────▶│                  │
   │                        │                       │                     │◀── not recovered   │                  │
   │                        │                       │                     │                    │                  │
   │                        │                       │ requeue after 30s   │                    │                  │
   │                        │                       │◀────────────────────│                    │                  │
```

Key points illustrated:

- `Reconcile()` calls investigation sub-components sequentially and handles the Kubernetes API
  calls (GET, CREATE, PATCH); the investigation package receives already-fetched data.
- The `IncidentReport` name is deterministic, derived only from workload identity. If two
  reconcilers race to CREATE the same name, only one wins; the other receives AlreadyExists
  and fetches the existing object to continue.
- The `IncidentReport` is persisted **before** the status update. If the controller crashes
  after CREATE but before the status PATCH, the next reconciliation finds the report and
  continues. (Req 3.8, Req 11)
- After creating/updating the report, `RecoveryEvaluator` is always called to detect if the
  workload is already healthy (handles race between failure signal and rapid recovery).

---

## State Machine — Incident Lifecycle

```text
                  ┌────────────────────────────┐
                  │ Trigger crosses threshold   │
                  │ (Req 1, Req 2)              │
                  └──────────────┬─────────────┘
                                 │
                                 ▼
                     ┌───────────────────────┐
                     │     Investigating      │◀──────────────────────────────┐
                     │  (phase=Investigating) │                               │
                     └───────────┬───────────┘                               │
                                 │                                            │
          ┌──────────────────────┼──────────────────────┐                    │
          │                      │                       │                    │
          ▼                      ▼                       ▼                    │
  [diagnosis rule       [no diagnosis rule        [workload becomes           │
   matches evidence]     matches evidence]          healthy]                  │
   (future spec)         (future spec)                  │                     │
          │                      │                       │                    │
          ▼                      ▼                       │                    │
   ┌─────────────┐     ┌──────────────────┐             │                    │
   │  Diagnosed  │     │     Unknown      │             │                    │
   │             │     │                  │             │                    │
   └──────┬──────┘     └────────┬─────────┘             │                    │
          │                     │                        ▼                    │
          └─────────────────────┘              ┌──────────────────────┐      │
                                               │ Stability period     │      │
                                               │ timer starts         │      │
                                               │ (status.             │      │
                                               │ stabilityStartedAt)  │      │
                                               └──────────┬───────────┘      │
                                                          │                   │
                                             ┌────────────┴──────────────┐   │
                                             │                            │   │
                                             ▼                            ▼   │
                                  [new failure during          [no new failure │
                                   stability period]            for full period]
                                             │                            │
                                             │ reset stabilityStartedAt   │
                                             └──────────────────────┐     │
                                                                    │     ▼
                                                                    │  ┌─────────┐
                                                                    │  │Resolved │
                                                                    │  │(Req 7.4)│
                                                                    │  └─────────┘
                                                                    │
                                                                    ▼
                                                        workload healthy again?
                                                                    │
                                                                    └──────────▶ (restart stability timer)
```

Notes:
- `Diagnosed` and `Unknown` phases are set by the future diagnosis specification (Req 7.7).
- The foundation only transitions between `Investigating` and `Resolved`.
- `stabilityStartedAt` in the status subresource allows the stability timer to survive
  controller restarts without in-memory state (Req 8.5, Req 11).
- A new failure during the stability window resets `stabilityStartedAt` to nil (Req 8.2, Req 8.3).

---

## Trigger Detection

The `TriggerEvaluator` classifies a Pod into a trigger type by examining the Pod's current
container states and pre-fetched Kubernetes Event counts. It operates on a `TriggerContext`
struct; it does not call the Kubernetes API directly. (Req 1)

### State-Based Triggers (Req 2.1)

State-based triggers are observed from the current Pod object state and are **idempotent**.
The same Pod state on reconciliation N and N+1 represents the **same condition**, not two
distinct failures. Re-evaluating a Pod in CrashLoopBackOff on every reconciliation does not
produce multiple trigger events.

| Signal | Detection location in Pod |
|---|---|
| `OOMKilled` | `containerStatuses[*].lastTerminationState.terminated.reason == "OOMKilled"` or `containerStatuses[*].state.terminated.reason == "OOMKilled"` |
| `CrashLoopBackOff` | `containerStatuses[*].state.waiting.reason == "CrashLoopBackOff"` |
| `ImagePullBackOff` | `containerStatuses[*].state.waiting.reason == "ImagePullBackOff"` or `"ErrImagePull"` |
| `CreateContainerConfigError` | `containerStatuses[*].state.waiting.reason == "CreateContainerConfigError"` |
| Eviction | `status.reason == "Evicted"` or `status.phase == "Failed"` with eviction-related condition |

State-based triggers set `TriggerResult.Source = TriggerSourceStateBased`. No event counts
are needed; these can be determined in a quick pre-check before fetching any Events (step 2
in Reconcile outline).

### Count-Based Triggers (Req 2.3–2.6)

Count-based triggers are observed from Kubernetes Event counts and **require deduplication**.
The count source is the Kubernetes Event's `count` field (which Kubernetes aggregates
internally), **not** the number of separate Event objects. This avoids double-counting from
Event aggregation.

Threshold check: `event.count >= threshold`

**Event aggregation rule:** When multiple `Event` objects exist for the same Pod and the same reason (e.g., two `FailedMount` Event objects created at different times), use the **highest `event.count`** among all matching Event objects as the threshold input. Do not sum counts across independent Event objects — they may represent different failure episodes. Using the maximum is conservative and avoids undercounting without risking inflation from unrelated events.

| Signal | Event reason | Threshold config field |
|---|---|---|
| Mount failure | `FailedMount` | `MountFailureThreshold` |
| Readiness probe failure | `Unhealthy` (probe type = Readiness) | `ReadinessProbeFailureThreshold` |
| Liveness probe failure | `Unhealthy` (probe type = Liveness) | `LivenessProbeFailureThreshold` |
| Scheduling failure | `FailedScheduling` | `SchedulingFailureThreshold` |

Count-based triggers set `TriggerResult.Source = TriggerSourceEventBased`.

### TriggerContext and TriggerResult

```go
// TriggerContext contains all pre-fetched data needed for trigger evaluation.
// EventCounts represent the Kubernetes Event.count field values (aggregated by Kubernetes),
// not the raw number of Event objects.
type TriggerContext struct {
    Pod                          *corev1.Pod
    MountFailureEventCount       int // from Event.count for reason=FailedMount
    ReadinessProbeEventCount     int // from Event.count for reason=Unhealthy (Readiness)
    LivenessProbeEventCount      int // from Event.count for reason=Unhealthy (Liveness)
    SchedulingFailureEventCount  int // from Event.count for reason=FailedScheduling
}

// TriggerResult is the output of trigger evaluation.
type TriggerResult struct {
    // IsTrigger indicates whether this Pod state represents an incident trigger.
    IsTrigger bool

    // Type is the classified trigger type. Zero value if IsTrigger is false.
    Type TriggerType

    // Source distinguishes state-based from event-based triggers.
    Source TriggerSource

    // IsImmediate indicates the trigger does not require threshold evaluation.
    IsImmediate bool

    // ContainerName is the container that triggered the incident, if applicable.
    ContainerName string

    // Reason is the raw reason string from Kubernetes.
    Reason string
}
```

### Normal Lifecycle Filtering (Req 1.10)

The evaluator returns `IsTrigger = false` for:
- Pod `Pending` with `Scheduled` condition false but not yet at the scheduling threshold
- Pod `Succeeded` (completed Job)
- Container `Completed` exit (exit code 0)
- `ContainerCreating`, `PodInitializing` waiting reasons
- Pod under graceful termination (`deletionTimestamp` set, no failure reason)

---

## Incident Correlation

The `IncidentCorrelator` determines whether a new trigger belongs to an existing active
`IncidentReport` or requires a new one. (Req 5)

### Two-Name Model: Active and Historical IncidentReports

The system maintains exactly one possible active `IncidentReport` per workload at a time.
Active and historical incidents are distinguished by name and label:

**Active incident name** (deterministic, derived from workload identity only):
```
<workload-name>-<kind-lowercase>-active
```
Examples: `payment-api-deployment-active`, `worker-daemonset-active`

For Pod-level fallback (no workload resolved):
```
<pod-name>-pod-active
```

**Historical incident name** (unique per resolved incident):
```
<workload-name>-<kind-lowercase>-<YYYYMMDD>-<5hex>
```
Where `<5hex>` is the first 5 characters of the hex-encoded FNV-32a hash of
`namespace/workloadKind/workloadName/startedAt-unix-seconds`.
Examples: `payment-api-deployment-20260829-a3f2b`, `payment-api-deployment-20260901-d91ca`

> **Name collision avoidance:** Hash collisions between two historical names for the same workload are prevented in practice by the minimum stability period. Because the active incident must remain healthy for at least `StabilityPeriod` (default 5 minutes) before resolving, the `startedAt` unix-seconds for successive incidents from the same workload are always separated by at least that duration. Two incidents starting within the same second for the same workload are therefore not possible under normal operation.

**Active label** (set at creation, used for fast filtering):
```yaml
labels:
  investigator.k8s.io/workload-namespace: "<namespace>"
  investigator.k8s.io/workload-name: "<workload-name>"
  investigator.k8s.io/workload-kind: "<kind>"
  investigator.k8s.io/active: "true"
```
On resolution, the `investigator.k8s.io/active` label is removed (set to `"false"` or
deleted).

**Transition on resolution:**

When an active `IncidentReport` is resolved:
1. The controller issues a **single PATCH** that atomically sets `status.phase = Resolved`,
   `status.resolvedAt = now`, and removes the `investigator.k8s.io/active` label from
   `metadata.labels`. Combining these into one operation ensures that if the controller
   crashes after this step, the report is unambiguously in the Resolved state with the
   active label already cleared — the startup recovery handler can detect this without
   needing to infer partial state from two separate fields.
2. The controller creates a new historical `IncidentReport` with the unique historical name,
   copying `spec`, `status`, and relevant labels/annotations from the active report. The
   following Kubernetes server-managed metadata fields MUST be excluded from the new object:
   `metadata.uid`, `metadata.resourceVersion`, `metadata.creationTimestamp`,
   `metadata.managedFields`, `metadata.deletionTimestamp`, and `metadata.finalizers`. The
   historical object is a fresh Kubernetes resource with its own server-assigned identity.
3. The controller deletes the active-named report.
4. The active slot (`<workload-name>-<kind>-active`) is now free for the next incident.

> **Why copy-then-delete?** This preserves the historical record with a recoverable two-step transition. Kubernetes does not provide a transaction spanning separate API operations, so these are two independent requests, not an atomic operation. If the controller
> crashes after step 3 but before step 4, both the active-named and historical-named reports
> exist temporarily. On restart, the controller detects this state (both exist, active one is
> Resolved) and completes the deletion of the active-named report. No history is lost.

> **RBAC note:** This requires `delete` permission on `incidentreports`. Add:
> `// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports,verbs=get;list;watch;create;update;patch;delete`

**Five-scenario walkthrough:**

1. **First incident:**
   - No `payment-api-deployment-active` exists → CREATE it (phase=Investigating)
   - Label `active=true` set

2. **Incident resolution:**
   - PATCH `payment-api-deployment-active`: set phase=Resolved, resolvedAt=now, remove active label (single operation)
   - CREATE `payment-api-deployment-20260829-a3f2b` (copy)
   - DELETE `payment-api-deployment-active`
   - Historical record `payment-api-deployment-20260829-a3f2b` is permanent

3. **Later new incident:**
   - GET `payment-api-deployment-active` → NotFound
   - CREATE new `payment-api-deployment-active` (phase=Investigating)
   - Previous history untouched

4. **Concurrent creation attempts:**
   - Controller A and B both GET `payment-api-deployment-active` → NotFound
   - Both attempt CREATE
   - One succeeds; the other receives AlreadyExists
   - AlreadyExists reconciler fetches the existing active report and continues with update path
   - No duplicate active incident

5. **Controller restart:**
   - `payment-api-deployment-active` exists in etcd with phase=Investigating
   - Startup list finds it, enqueues synthetic reconcile request
   - Controller resumes managing the active incident

### Temporal Correlation Window

The `CorrelationWindow` configuration (default 10 minutes) is stored in `Config` for future use.

In the current foundation implementation, the window does **not** gate any active-incident decision:

- When an active incident exists for a workload → all subsequent failures for that workload are recorded against it unconditionally.
- When no active incident exists → a new incident is created unconditionally.

The window is **reserved** for a future enhancement that may use it to determine whether a new failure after resolution is related to the previous incident episode. Do not implement window-based correlation logic in the foundation.

The `--correlation-window` flag remains configurable so operators can set it without a code change when the feature is activated.

### Lookup Strategy

The correlator performs a **direct GET by the active name** (`<workload-name>-<kind>-active`).
Because the active name is deterministic, the GET is sufficient and avoids list overhead.
Labels are set for `kubectl` filtering but are not the primary lookup mechanism.

### Duplicate CREATE Race

Because the active-incident name is deterministic, concurrent reconcilers attempting to
CREATE the same-named `IncidentReport` produce a Conflict/AlreadyExists error on the second
CREATE. The reconciler treats AlreadyExists as success and fetches the existing object.

---

## Workload Ownership Resolution

The `OwnershipResolver` traverses `ownerReferences` on the Pod to identify the top-level
workload. It operates on pre-fetched objects passed in from the controller. (Req 4)

### Traversal Chain

```text
Pod
 └─ ownerReferences[kind=ReplicaSet]
       └─ RS.ownerReferences[kind=Deployment]  ──▶ WorkloadRef{Kind:"Deployment"}

Pod
 └─ ownerReferences[kind=StatefulSet]           ──▶ WorkloadRef{Kind:"StatefulSet"}

Pod
 └─ ownerReferences[kind=DaemonSet]             ──▶ WorkloadRef{Kind:"DaemonSet"}

Pod
 └─ ownerReferences[kind=Job]
       └─ Job.ownerReferences[kind=CronJob]     ──▶ WorkloadRef{Kind:"CronJob"}
       └─ (no CronJob owner)                    ──▶ WorkloadRef{Kind:"Job"}

Pod
 └─ (no controller ownerReference)              ──▶ nil (retain Pod identity, Req 4.7)
```

### Failure Handling (Req 4.1, Fix F3)

Ownership resolution uses a "Pod identity fallback" strategy:

- If the direct owner (e.g., ReplicaSet) cannot be fetched, or if the direct owner is successfully fetched but its parent (e.g., Deployment) cannot be resolved, fall back to **Pod identity** and record `WorkloadOwnerResolved = false`. Do not use an intermediate resource like ReplicaSet as the workload identity — `WorkloadRef.Kind` is restricted to the supported top-level workload types defined in the Glossary (Deployment, StatefulSet, DaemonSet, Job, CronJob). Using an intermediate resource as workload identity would produce an invalid `WorkloadRef` kind that the `RecoveryEvaluator` and naming logic cannot handle.

Resolution outcomes:

| Scenario | Result |
|---|---|
| RS fetched, Deployment fetched | `WorkloadRef{Kind:"Deployment"}`, `WorkloadOwnerResolved=true` |
| RS fetched, Deployment NOT found | Pod identity fallback, `WorkloadOwnerResolved=false`, log warning |
| RS NOT found | Pod identity fallback, `WorkloadOwnerResolved=false` |
| No controller owner | Pod identity fallback, `WorkloadOwnerResolved=false` |

### Interface

```go
// OwnershipResolver resolves a Pod's owning workload.
type OwnershipResolver interface {
    Resolve(ctx context.Context, pod *corev1.Pod) (*WorkloadRef, error)
}
```

The controller provides the Kubernetes client. The resolver uses it to fetch intermediate
resources (RS, Job) as needed, with bounded retries.

---

## Recovery Detection

The `RecoveryEvaluator` determines whether an active incident should be resolved. (Req 8)

### Workload-Type-Aware Health Check (D4)

Recovery evaluation is workload-type-aware. The evaluator does not check raw Pod readiness
uniformly; it checks the workload's own status fields, which reflect the workload
controller's view of health:

| Workload Kind | Healthy condition |
|---|---|
| `Deployment` | `status.readyReplicas >= spec.replicas` AND `status.availableReplicas >= spec.replicas` |
| `StatefulSet` | `status.readyReplicas >= spec.replicas` |
| `DaemonSet` | `status.numberReady >= status.desiredNumberScheduled` |
| `Job` | `status.succeeded >= 1` (at least one successful completion) |
| `CronJob` | evaluate the most recent Job using the Job rule above |
| Pod-level fallback (no workload) | Pod `phase == Running` AND all containers `ready == true` |

### Normal Lifecycle Operations

The following Deployment/StatefulSet/DaemonSet states must NOT be treated as unrecovered
failures:

- **Rolling update in progress**: `Deployment.status.updatedReplicas < spec.replicas`
  during a rolling update. Do not reset `stabilityStartedAt` solely because
  `updatedReplicas < replicas`; use `readyReplicas >= desiredReplicas` as the health signal.
- **Scale-up in progress**: New Pods being created. `availableReplicas < spec.replicas`
  is expected during scale-up; wait until `availableReplicas >= spec.replicas`.
- **Scale-down in progress**: `replicas` decreases. A scale-down that results in healthy
  replicas is not a failure; evaluate health against the new `spec.replicas`.
- **StatefulSet rolling update**: `status.updatedReplicas < spec.replicas` during update.
  Use `status.readyReplicas >= spec.replicas` as the health signal.

The health check uses the workload's current `spec.replicas` (desired) as the target,
not a cached value from before the incident began.

### WorkloadSnapshot

The controller is responsible for populating the `WorkloadSnapshot` before calling
`RecoveryEvaluator`. Population logic:

- If `report.Spec.Workload` is non-nil and `WorkloadOwnerResolved == true`: fetch the
  resource matching `Workload.Kind` (e.g., GET the Deployment by `Workload.Namespace/Workload.Name`)
  and populate only that field. All other workload fields remain nil.
- If `report.Spec.Workload` is nil or `WorkloadOwnerResolved == false`: fall back to the
  Pod-level path. Fetch the Pods listed in `report.Status.AffectedPods` that still exist
  and populate `WorkloadSnapshot.Pods`. An empty Pod list (all deleted) is not considered
  healthy — the incident stays active until a new Pod from the same workload triggers the
  watch.

```go
// WorkloadSnapshot provides the evaluator with the current state of the workload.
// Only the field corresponding to the workload kind is populated.
type WorkloadSnapshot struct {
    Ref         *v1alpha1.WorkloadRef

    // For Deployment:
    Deployment  *appsv1.Deployment

    // For StatefulSet:
    StatefulSet *appsv1.StatefulSet

    // For DaemonSet:
    DaemonSet   *appsv1.DaemonSet

    // For Job:
    Job         *batchv1.Job

    // For CronJob: most recent Job owned by the CronJob
    LatestJob   *batchv1.Job

    // For Pod-level fallback (no workload resolved):
    Pods        []corev1.Pod
}
```

### Stability Period (Req 8.1–8.5)

```text
1. Workload becomes healthy (workload-type-aware check passes)
2. Controller sets status.stabilityStartedAt = now (if not already set)
3. On subsequent reconciliations:
   a. If new failure detected → clear stabilityStartedAt, keep Investigating
   b. If now - stabilityStartedAt >= config.StabilityPeriod → transition to Resolved
   c. If workload becomes unhealthy → clear stabilityStartedAt
4. When Resolved:
   - Set status.resolvedAt = now
   - Set status.phase = Resolved
```

`stabilityStartedAt` is stored in the `IncidentReport` status, not in memory, so it
survives controller restarts. (Req 8.5, Req 11)

### Interface

```go
// RecoveryEvaluator assesses incident recovery state.
type RecoveryEvaluator interface {
    Evaluate(ctx context.Context, report *v1alpha1.IncidentReport, workload WorkloadSnapshot, cfg *config.Config) RecoveryResult
}

type RecoveryResult struct {
    // WorkloadHealthy indicates the workload is in a healthy state per its type-specific check.
    WorkloadHealthy bool

    // StabilityPeriodElapsed indicates the stability period has been met.
    StabilityPeriodElapsed bool

    // ShouldResolve indicates the incident should transition to Resolved.
    ShouldResolve bool

    // ShouldResetStabilityTimer indicates a new failure occurred and stabilityStartedAt should be cleared.
    ShouldResetStabilityTimer bool
}
```

---

## Controller Structure

### PodReconciler

```go
type PodReconciler struct {
    client.Client
    Scheme            *runtime.Scheme
    Config            *config.Config
    TriggerEvaluator  investigation.TriggerEvaluatorInterface
    OwnershipResolver investigation.OwnershipResolverInterface
    Correlator        investigation.IncidentCorrelatorInterface
    RecoveryEvaluator investigation.RecoveryEvaluatorInterface
    Log               logr.Logger
}
```

All investigation dependencies are injected at construction time (in `cmd/main.go`).
This enables unit testing with mock implementations.

### Watch Registration

```go
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&corev1.Pod{}).
        WithEventFilter(relevancePredicate()).
        Complete(r)
}
```

`relevancePredicate()` filters out Pod events that cannot possibly be incident triggers
(e.g., Pod deletion with `Succeeded` phase) to reduce unnecessary reconciliation calls.
(Req 18.1)

There is **no second watch** for `IncidentReport` resources. The Pod watch handles ongoing
management. Active incident requeue on startup is handled explicitly in `cmd/main.go` (see
Startup Recovery below).

### Event Watch → Pod Reconcile Mapping

Threshold-based triggers (FailedMount, Unhealthy, FailedScheduling) rely on Kubernetes Event
`event.count` values. A Pod-only watch does not guarantee reconciliation when these Events
change without a corresponding Pod object change. The controller therefore also watches
`corev1.Event` resources.

The event handler maps each relevant Event to its involved Pod and enqueues a reconcile
request for that Pod:

```go
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&corev1.Pod{}).
        Watches(
            &corev1.Event{},
            handler.EnqueueRequestsFromMapFunc(r.eventToPodRequests),
            builder.WithPredicates(relevantEventPredicate()),
        ).
        WithEventFilter(relevancePredicate()).
        Complete(r)
}

// eventToPodRequests maps a relevant Event to a reconcile request for the involved Pod.
func (r *PodReconciler) eventToPodRequests(ctx context.Context, obj client.Object) []reconcile.Request {
    event, ok := obj.(*corev1.Event)
    if !ok || event.InvolvedObject.Kind != "Pod" {
        return nil
    }
    return []reconcile.Request{{
        NamespacedName: types.NamespacedName{
            Namespace: event.InvolvedObject.Namespace,
            Name:      event.InvolvedObject.Name,
        },
    }}
}
```

`relevantEventPredicate()` filters Events to the three relevant reasons only:
`FailedMount`, `Unhealthy`, `FailedScheduling`.

This ensures the reconciler is triggered when threshold event counts change, even when the
Pod object itself has not changed. When the reconciler runs, it fetches the current
`Event.count` values as usual via step 3 of the reconcile outline.

### Reconcile() Outline

```go
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch Pod from cache; if not found, check for active IncidentReport for this key;
    //    return if neither exists.
    // 2. Quick state check: is this a state-based trigger? (no API calls needed)
    //    If yes, call TriggerEvaluator.Evaluate() with zero event counts to produce a
    //    fully-populated TriggerResult (Type, ContainerName, Source=StateBased, etc.),
    //    then proceed directly to step 5 using that result — do NOT skip the evaluator.
    // 3. If not a state-based trigger: fetch Events for this Pod
    //    (for count-based threshold evaluation using Event.count fields).
    // 4. Evaluate trigger with full TriggerContext (state + event counts).
    //    If not a trigger, return (no-op).
    // 5. Resolve workload ownership.
    // 6. Build deterministic IncidentReport name from workload identity
    //    (or Pod name for fallback).
    // 7. GET the IncidentReport by name.
    //    If it exists and is active → update path.
    // 8. If not found: CREATE IncidentReport (phase=Investigating).
    //    On AlreadyExists → fetch existing object and continue with update path.
    // 9. PATCH status: add Pod to AffectedPods (deduplicate by UID), set Trigger if first,
    //    update FailureCount per trigger source semantics, set LastFailureAt.
    // 10. Fetch workload snapshot for recovery evaluation.
    // 11. Evaluate recovery (workload-type-aware).
    //     Apply stabilityStartedAt / resolve transitions.
    // 12. Return ctrl.Result{RequeueAfter: cfg.RequeueInterval} for active incidents;
    //     empty result for resolved.
}
```

### Startup Recovery (Option B)

On startup, `cmd/main.go` explicitly handles active incidents and mid-transition crash states
to ensure no active investigation is silently abandoned after a controller restart.
(Req 11.2, Req 11.3)

Startup sequence:

1. Register schemes and set up the controller manager.
2. List **all** `IncidentReport` objects across all watched namespaces — both active and Resolved.
3. For each `IncidentReport` whose name ends with `-active` (active-slot objects):
   a. If `status.phase != Resolved` → genuine active incident.
      - Enqueue a reconcile request for each Pod in `status.affectedPods` that still exists.
      - If **no affected Pods exist** (all deleted — e.g., Deployment scaled to 0, Job
        completed, or Pods replaced before the controller restarted): do **not** skip the
        incident. Instead, enqueue a synthetic reconcile for the **workload** (using
        `report.Spec.Workload.Namespace/Name` as the reconcile key if resolved, or the
        first entry in `status.affectedPods` otherwise) so the controller can evaluate
        workload health and potentially resolve the incident. Without this, an active
        incident with no surviving Pods would be orphaned indefinitely — it would never
        be picked up again unless a new Pod happens to appear.
   b. If `status.phase == Resolved` → this is a crash mid-transition (step 1 completed but
      step 3 DELETE did not). Check whether a corresponding historical IncidentReport already
      exists (match by `status.startedAt` using `GenerateHistoricalName`). If historical
      exists → DELETE the active-named report to complete the transition. If historical does
      not exist → re-attempt the historical CREATE, then DELETE the active-named report.
4. Start the manager normally.

If the list operation fails on startup, log the error and continue — do **not** block
startup. New Pod and Event watches will trigger reconciliation normally. (Req 11.2)

**Why list all IncidentReports (not just phase != Resolved):** After step 1 of the resolution
transition the active-named report has `phase = Resolved`. A filter of `phase != Resolved`
would skip it, leaving the active slot permanently occupied and blocking future incidents.
The startup scan must inspect all active-named objects regardless of phase.

### IncidentReport Naming

Active incident names are deterministic: `<workload-name>-<kind-lowercase>-active`
(e.g., `payment-api-deployment-active`). Truncated to 63 characters.

Historical incident names include a date and short hash derived from the incident's
`startedAt` time: `<workload-name>-<kind-lowercase>-<YYYYMMDD>-<5hex>`.

See [Incident Correlation — Two-Name Model](#two-name-model-active-and-historical-incidentreports).

---

## Idempotency Mechanism

Idempotency is the most critical correctness property of the controller. (Req 10, Req 14)

### Duplicate Prevention

1. **Deterministic GET before CREATE** — the correlator performs a GET by the deterministic
   name before creating. If a report already exists, it is reused. (Req 5.1, Req 14.1)
2. **AlreadyExists on concurrent CREATE** — because names are deterministic, a concurrent
   CREATE returns AlreadyExists. The reconciler treats AlreadyExists as success and fetches
   the existing object to continue. (D1)
3. **Kubernetes optimistic concurrency** — `resourceVersion` is included in all PATCH and
   UPDATE calls. Concurrent updates cause a conflict error, which triggers a retry. The retry
   re-fetches the current state and applies logic again.
4. **Pod deduplication in AffectedPods** — when adding a Pod to `status.affectedPods`, the
   controller checks whether the Pod UID is already present. (Req 6.2, Req 14.2)
5. **Additive status updates** — status patches use strategic merge semantics. Fields that
   represent accumulated history (`AffectedPods`, `FailureCount`) are updated additively;
   immutable fields (`StartedAt`, initial `Trigger`) are only set if currently zero. (Req 10.2)
6. **Early exit on non-trigger** — the reconciler returns immediately after the quick
   state-based check and full `TriggerEvaluator` both return `IsTrigger = false`, performing
   no Kubernetes writes. (Req 18.1, Req 18.2)

### FailureCount Semantics

- **State-based triggers:** `FailureCount` increments by 1 only when a NEW unique failure
  signature is observed. "New" means: a different container has the trigger state
  (different `ContainerName`), or the failure reoccurs after the workload was healthy
  (`stabilityStartedAt` was set then cleared). Re-evaluating a Pod still in the same
  CrashLoopBackOff state does **not** increment `FailureCount`.
- **Event-based triggers:** `FailureCount` is set to the current `Event.count` value (not
  independently incremented). It reflects the Kubernetes event aggregation count.

### Concurrent Reconciliation

controller-runtime's work queue prevents concurrent processing of the same namespace/name
pair by default. Rapid successive events are coalesced in the queue. (Req 10.3, Req 5.1)

---

## RBAC

All RBAC permissions are declared via Kubebuilder `+kubebuilder:rbac` markers on the
`PodReconciler` type. (Req 16)

```go
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=investigation.k8s.io,resources=incidentreports/finalizers,verbs=update
```

No `delete` permissions are required for any resource except `incidentreports`. Delete permission is required to transition a resolved active incident to its historical name. No other resource deletion permission is granted. No `update` or `patch` permissions
are required for Pods, Deployments, StatefulSets, DaemonSets, or Services. (Req 16.2–16.7)

Secret values are never read in the foundation layer. (Req 16.8)

---

## Configuration Defaults

| Parameter | Default | Rationale |
|---|---|---|
| `StabilityPeriod` | 5 minutes | Balances responsiveness with avoiding false resolution |
| `CorrelationWindow` | 10 minutes | Prevents unrelated failures from reopening a recently-resolved incident |
| `ReadinessProbeFailureThreshold` | 3 | Typical probe retry budget matches Kubernetes default |
| `LivenessProbeFailureThreshold` | 3 | Same as readiness |
| `MountFailureThreshold` | 3 | Three mount retries typically indicates a real failure |
| `SchedulingFailureThreshold` | 5 | Scheduler may need a few cycles to find a node |
| `RequeueInterval` | 30 seconds | Regular recovery check without excessive API load |
| `WatchNamespaces` | `[]` (all) | Operator can scope to specific namespaces at deploy time |

---

## Structured Logging

Every significant operation emits a structured log entry using `logr`. (Req 19)

Standard fields included in all log entries:

| Field | Value |
|---|---|
| `pod` | `namespace/name` |
| `workload` | `kind/namespace/name` (when resolved) |
| `incidentReport` | `namespace/name` (when known) |
| `triggerType` | `TriggerType` string |
| `phase` | current `IncidentPhase` |

Log levels:
- `Info` — incident created, incident reused, phase transition, incident resolved
- `Debug` — trigger evaluation result, ownership resolution steps, recovery evaluation
- `Error` — Kubernetes API failures, ownership resolution failures, status update failures

Secret values, full log contents, and raw event messages are never included in log output.
(Req 19.5, Req 16.8)

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of
a system — essentially, a formal statement about what the system should do. Properties serve as
the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: Immediate triggers fire at count one

*For any* Pod state that contains an OOMKilled, CrashLoopBackOff, ImagePullBackOff,
CreateContainerConfigError reason or an eviction indication, the `TriggerEvaluator`
SHALL return `IsTrigger = true` with `Source = TriggerSourceStateBased` regardless of how
many times the evaluation is performed.

**Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.9, 2.1**

### Property 2: Threshold triggers respect the configured threshold

*For any* Kubernetes Event count value and configured threshold, the `TriggerEvaluator`
SHALL return `IsTrigger = true` if and only if `event.count >= threshold`, and
`IsTrigger = false` if `event.count < threshold`.

**Validates: Requirements 1.5, 1.6, 1.7, 1.8, 2.2, 2.3, 2.4, 2.5, 2.6**

### Property 3: Normal lifecycle states do not trigger incidents

*For any* Pod state representing a normal lifecycle transition (Pending/Scheduled,
Succeeded, graceful termination), the `TriggerEvaluator` SHALL return `IsTrigger = false`.

**Validates: Requirements 1.10**

### Property 4: Trigger evaluation is a pure function

*For any* identical `TriggerContext` input, the `TriggerEvaluator` SHALL return the same
`TriggerResult` output regardless of how many times it is called or in what order calls
are made.

**Validates: Requirements 14.3**

### Property 5: Ownership resolution is deterministic

*For any* Pod with a given set of owner references, the `OwnershipResolver` SHALL return
the same `WorkloadRef` for any number of resolution attempts with the same owner graph.

**Validates: Requirements 4.1, 4.2, 4.3, 4.4, 4.5, 4.6**

### Property 6: Active incident lookup by deterministic name

*For any* workload identity `(namespace, kind, name)`, the deterministic active incident
name `<workload-name>-<kind-lowercase>-active` SHALL be computed identically regardless of
which reconciler instance computes it or how many times it is computed.

**Validates: Requirements 5.1, 5.2, 5.3, 5.4, 21.3, 21.4**

### Property 7: Pod association is deduplicated

*For any* `IncidentReport` and any Pod UID, adding the same Pod UID to `AffectedPods`
multiple times SHALL produce an `AffectedPods` list containing that UID exactly once.

**Validates: Requirements 6.2, 14.2**

### Property 8: Stability period reset on new failure

*For any* `IncidentReport` with a non-nil `stabilityStartedAt` and any new failure trigger
received during the stability window, the `RecoveryEvaluator` SHALL set
`ShouldResetStabilityTimer = true`, preventing resolution until the workload is healthy again.

**Validates: Requirements 8.2, 8.3**

### Property 9: Reconciliation idempotency

*For any* `IncidentReport` and **identical cluster state input** (same Pod object, same
Event counts, same workload status), applying the reconciliation logic N times SHALL produce
the same logical `IncidentReport` state as applying it once. This property applies to a
fixed input state snapshot; different cluster states legitimately produce different outputs.

**Validates: Requirements 10.1, 10.2, 10.3, 14.1, 14.3**

### Property 10: Threshold monotonicity

*For any* threshold T and event counts C1 ≤ C2, if the trigger fires for count C1, it
SHALL also fire for count C2. The trigger function is monotonic in failure count.

**Validates: Requirements 2.3, 2.4, 2.5**

---

## Error Handling

### Kubernetes API Failures

All API calls are wrapped with context-aware errors. Transient failures return a retry
error that allows controller-runtime's exponential backoff to retry the reconciliation.
(Req 15.3)

Permanent failures (e.g., RBAC denial on an optional evidence source) are logged with
the failure reason recorded in the `IncidentReport` status condition, but do not
prevent the rest of the reconciliation from completing. (Req 19.5)

### Missing Resources

- **Pod not found** during reconciliation → check for active IncidentReport for this key; if
  none exists, return nil (Pod was deleted, nothing to do)
- **ReplicaSet not found** during ownership resolution → fall back to Pod identity; record
  `WorkloadOwnerResolved = false`
- **ReplicaSet found, Deployment not found** → fall back to Pod identity; record `WorkloadOwnerResolved = false`, log warning
- **IncidentReport not found** on GET → proceed to CREATE with deterministic active name
- **AlreadyExists on CREATE** → fetch existing object by active name and continue with update path
- **Active incident found Resolved during correlation** → skip to CREATE new active incident
- **Historical CREATE fails during resolution transition** → retry; if crash between historical CREATE and active DELETE, on restart detect both objects exist and complete the cleanup

### Partial Updates

If the `IncidentReport` CREATE succeeds but the status PATCH fails, the next reconciliation
will find the existing report (via deterministic name GET) and re-apply the status update.
This is safe because status updates are idempotent. (Req 3.8)

### Status Conditions

The foundation layer sets the following standard conditions on `IncidentReport.status.conditions`:

| Condition Type | Meaning |
|---|---|
| `Active` | True when the incident is not Resolved |
| `WorkloadOwnerResolved` | True when workload ownership was successfully determined |
| `StabilityPeriodStarted` | True when stabilityStartedAt is set; False otherwise |

---

## Testing Strategy

### Unit Tests (test/unit/)

Unit tests verify business logic in isolation without a Kubernetes cluster.

Focus areas:

- `TriggerEvaluator` — all trigger types, threshold boundary conditions, normal lifecycle
  exclusions, state-based vs event-based source classification. Use table-driven tests with
  fabricated `corev1.Pod` objects and `TriggerContext` event counts.
- `OwnershipResolver` — each ownership chain (Pod→RS→Deployment, Pod→StatefulSet, etc.),
  missing intermediate resources (RS missing → Pod fallback, Deployment missing → Pod fallback),
  no owner. Use mock API clients.
- `IncidentCorrelator` — active incident found by deterministic name, not found, AlreadyExists
  handling. Use mock GET with pre-populated `IncidentReport` objects.
- `RecoveryEvaluator` — workload-type-aware health checks (Deployment readyReplicas,
  StatefulSet, DaemonSet, Job), stability period elapsed, new failure during stability
  window. Use fabricated workload snapshot objects.
- Idempotency of incident status update logic — apply same inputs twice, compare output.
- `FailureCount` semantics — state-based trigger does not increment on re-evaluation of same
  state; event-based trigger sets count from Event.count.

Property-based tests use [rapid](https://github.com/flyingmutant/rapid) for Go:

- **Property 1–4**: Generate random `corev1.ContainerState` and `corev1.PodStatus` values;
  verify trigger evaluation properties.
- **Property 5**: Generate random owner reference graphs; verify deterministic resolution.
- **Property 6**: Generate random `IncidentReport` object states; verify correlator
  finds or does not find active incidents correctly.
- **Property 7**: Generate random sequences of `PodRef` additions; verify deduplication.
- **Property 8**: Generate random `stabilityStartedAt` values and failure events; verify
  reset behavior.
- **Property 10**: Generate random threshold and event count pairs; verify monotonicity.

Each property test runs a minimum of 100 iterations via rapid's default configuration.
Tests reference their design property using a comment tag:

```go
// Feature: incident-investigator-foundation, Property 2: Threshold triggers respect configured threshold
```

### Integration Tests (test/integration/)

Integration tests use `controller-runtime/envtest` to run against a real API server (without
a full cluster). Focus areas:

- Controller reconciles a Pod with an OOMKilled status → `IncidentReport` created with
  deterministic name
- Second OOMKilled event → existing `IncidentReport` updated, not duplicated
- **Duplicate CREATE race**: two concurrent reconcilers for the same workload → exactly one
  `IncidentReport` created (verify via name collision/AlreadyExists handling)
- **Mixed failure types on same workload within correlation window** → single `IncidentReport`
  updated, not a second report created
- Pod ownership resolution through envtest-created RS and Deployment objects
- **Workload-type-aware recovery**: Deployment `readyReplicas` check, StatefulSet check,
  DaemonSet `numberReady` check
- **Pod replacement during active incident** → replacement Pod associated with existing incident
- Stability period → `IncidentReport` transitions to Resolved after configured period
- **Startup requeue**: pre-create active `IncidentReport`, restart controller, verify incident
  is managed and continues stability evaluation without requiring a new Pod event
- Missing Pod during reconciliation → no crash, graceful handling

### End-to-End Tests (deploy/kind/)

Kind-based scenarios validating Req 20:
- Deploy a workload → trigger OOMKilled → observe `kubectl get incidentreports`
- Confirm deduplication across multiple OOM events
- Confirm resolution after workload recovery
- Confirm historical incident preserved after new incident created
