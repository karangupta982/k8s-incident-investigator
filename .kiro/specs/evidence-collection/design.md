# Design Document: Evidence Collection

## Overview

Evidence collection fills the `IncidentReport` with structured investigation data gathered from the Kubernetes API and container logs after the foundation layer has detected and created an incident. It is implemented as a set of focused Go collectors in `internal/evidence/`, orchestrated by a single `EvidenceOrchestrator` that is called from `PodReconciler.Reconcile()`.

The design follows five principles:

- **Adaptive**: different trigger types activate different evidence layers
- **Bounded**: every collection operation has explicit limits on bytes, lines, counts, and total duration
- **Best-effort**: individual source failures are recorded as `CollectionError` entries and do not abort the overall collection
- **Non-destructive**: all API access is read-only; no workload modifications
- **Explainable**: the absence of evidence is recorded explicitly so an engineer knows what was unavailable and why

---

## Architecture

### Dependency Direction

```
PodReconciler
      |
      v
EvidenceOrchestrator          ← orchestrates, enforces timeout, returns EvidenceSnapshot
      |
      +-- PodCollector         ← Layer 1: Pod state
      +-- EventCollector       ← Layer 2: Kubernetes Events
      +-- WorkloadCollector    ← Layer 3: Deployment/StatefulSet/DaemonSet/Job state
      +-- NodeCollector        ← Layer 4: Node conditions and capacity
      +-- LogCollector         ← Layer 6: Bounded container log excerpts
      +-- DependencyCollector  ← Layer 5: PVCs, image pull secrets, scheduling constraints
```

The orchestrator is the only component that knows about the full `EvidenceSnapshot`. Each collector knows only about its own slice.

Collectors do not import `internal/controller` or `internal/investigation`.
The controller imports `internal/evidence` — not the reverse.

### Data Flow

```
Reconcile()
   │
   ├─ [foundation steps 1–9 unchanged]
   │
   ├─ Step 9.5: Collect evidence
   │     collectCtx, cancel := context.WithTimeout(ctx, cfg.EvidenceCollectionTimeout)
   │     snapshot := orchestrator.Collect(collectCtx, report, pod)
   │     patch report.Status.Evidence = &snapshot
   │
   ├─ Step 10: Re-fetch report (existing)
   └─ Steps 11–12: Recovery evaluation (existing)
```

Evidence collection sits between the status update (step 9) and the recovery re-fetch (step 10). It uses a child context with the configured timeout so it cannot block reconciliation indefinitely.

---

## Components and Interfaces

### EvidenceCollector Interface

```go
// package internal/evidence

// EvidenceCollector is the common interface for all focused collectors.
// Each implementation is responsible for exactly one evidence layer.
//
// Collect returns the populated evidence value and a slice of CollectionErrors
// for any sub-source that could not be reached.
// A non-nil error return is reserved for programming errors (nil client, etc.).
// Kubernetes API failures are NOT returned as errors; they produce CollectionError entries.
type EvidenceCollector[T any] interface {
    Collect(ctx context.Context, input CollectorInput) (T, []v1alpha1.CollectionError)
}
```

### CollectorInput

```go
// CollectorInput is the read-only context passed to every collector.
// Collectors must not modify any field.
type CollectorInput struct {
    // Client is the controller-runtime client for Kubernetes API access.
    Client client.Client

    // Report is the current IncidentReport being investigated.
    Report *v1alpha1.IncidentReport

    // Pod is the primary affected Pod, fetched by the orchestrator before collection.
    // May be nil if the Pod was deleted before collection ran.
    Pod *corev1.Pod

    // Config holds the evidence collection limits.
    Config *config.Config
}
```

### EvidenceOrchestrator

```go
// EvidenceOrchestrator coordinates all evidence layers and returns a complete
// EvidenceSnapshot. It enforces the overall collection timeout and tolerates
// individual layer failures.
type EvidenceOrchestrator struct {
    Client client.Client
    Config *config.Config
    Log    logr.Logger
}

// Collect runs all evidence layers for the given report and returns the snapshot.
// The context should carry the configured EvidenceCollectionTimeout.
// Collect always returns a non-nil EvidenceSnapshot even when all sources fail.
func (o *EvidenceOrchestrator) Collect(
    ctx context.Context,
    report *v1alpha1.IncidentReport,
    pod *corev1.Pod,
) v1alpha1.EvidenceSnapshot
```

### Collector Implementations

| File | Type | Layer |
|------|------|-------|
| `internal/evidence/pod.go` | `PodCollector` | Layer 1 |
| `internal/evidence/events.go` | `EventCollector` | Layer 2 |
| `internal/evidence/workload.go` | `WorkloadCollector` | Layer 3 |
| `internal/evidence/node.go` | `NodeCollector` | Layer 4 |
| `internal/evidence/logs.go` | `LogCollector` | Layer 6 |
| `internal/evidence/dependencies.go` | `DependencyCollector` | Layer 5 (adaptive) |
| `internal/evidence/collector.go` | `EvidenceOrchestrator` | coordination |

---

## Data Models

### New fields on Config

```go
// MaxLogBytes is the maximum number of bytes collected per container log excerpt.
// Default: 32768 (32 KB).
MaxLogBytes int

// MaxLogLines is the maximum number of lines collected per container log excerpt.
// Default: 200.
MaxLogLines int

// MaxEventsPerIncident is the maximum number of Kubernetes Events stored in evidence.
// Default: 25.
MaxEventsPerIncident int

// EvidenceCollectionTimeout is the maximum duration for a single evidence collection cycle.
// Default: 30 seconds.
EvidenceCollectionTimeout time.Duration
```

### New API Types — api/v1alpha1/evidence_types.go

```go
// EvidenceSnapshot holds all collected investigation evidence for one incident cycle.
//
// +kubebuilder:object:generate=true
type EvidenceSnapshot struct {
    // CollectedAt is the time evidence collection last completed (or was interrupted).
    // +optional
    CollectedAt *metav1.Time `json:"collectedAt,omitempty"`

    // Pod holds evidence from the primary affected Pod.
    // +optional
    Pod *PodEvidence `json:"pod,omitempty"`

    // Workload holds evidence from the owning workload resource.
    // +optional
    Workload *WorkloadEvidence `json:"workload,omitempty"`

    // Node holds evidence from the node the affected Pod ran on.
    // +optional
    Node *NodeEvidence `json:"node,omitempty"`

    // Events is a bounded list of relevant Kubernetes Events.
    // +optional
    Events []EventEvidence `json:"events,omitempty"`

    // Logs holds bounded container log excerpts.
    // +optional
    Logs []ContainerLogEvidence `json:"logs,omitempty"`

    // Dependencies holds evidence about workload dependencies relevant to the trigger type.
    // +optional
    Dependencies *DependencyEvidence `json:"dependencies,omitempty"`

    // CollectionErrors records sources that could not be collected and why.
    // +optional
    CollectionErrors []CollectionError `json:"collectionErrors,omitempty"`
}

// PodEvidence holds evidence extracted from the affected Pod resource.
//
// +kubebuilder:object:generate=true
type PodEvidence struct {
    // Name is the Pod name.
    Name string `json:"name"`

    // Namespace is the Pod namespace.
    Namespace string `json:"namespace"`

    // Phase is the Pod phase at collection time.
    // +optional
    Phase string `json:"phase,omitempty"`

    // NodeName is the node the Pod was scheduled on.
    // +optional
    NodeName string `json:"nodeName,omitempty"`

    // Containers holds per-container state evidence.
    Containers []ContainerEvidence `json:"containers,omitempty"`

    // InitContainers holds per-init-container state evidence.
    // +optional
    InitContainers []ContainerEvidence `json:"initContainers,omitempty"`

    // VolumeMounts holds volumes relevant to the trigger type.
    // +optional
    VolumeMounts []VolumeMountEvidence `json:"volumeMounts,omitempty"`
}

// ContainerEvidence holds state evidence for one container.
//
// +kubebuilder:object:generate=true
type ContainerEvidence struct {
    // Name is the container name.
    Name string `json:"name"`

    // Image is the full image reference.
    Image string `json:"image"`

    // State is the current container state: running, waiting, or terminated.
    State string `json:"state"`

    // WaitingReason is the Waiting.Reason when state is "waiting".
    // +optional
    WaitingReason string `json:"waitingReason,omitempty"`

    // RestartCount is the number of times the container has been restarted.
    RestartCount int32 `json:"restartCount"`

    // ExitCode is the exit code from the most recent termination.
    // Zero when the container has not yet terminated.
    // +optional
    ExitCode int32 `json:"exitCode,omitempty"`

    // TerminationReason is the reason for the most recent termination (e.g., OOMKilled, Error).
    // +optional
    TerminationReason string `json:"terminationReason,omitempty"`

    // LastTerminationReason is the reason from the previous termination, if any.
    // +optional
    LastTerminationReason string `json:"lastTerminationReason,omitempty"`

    // ResourceLimits holds the configured resource limits for this container.
    // +optional
    ResourceLimits map[string]string `json:"resourceLimits,omitempty"`

    // ResourceRequests holds the configured resource requests for this container.
    // +optional
    ResourceRequests map[string]string `json:"resourceRequests,omitempty"`

    // LivenessProbe summarises the liveness probe configuration.
    // +optional
    LivenessProbe *ProbeSummary `json:"livenessProbe,omitempty"`

    // ReadinessProbe summarises the readiness probe configuration.
    // +optional
    ReadinessProbe *ProbeSummary `json:"readinessProbe,omitempty"`
}

// ProbeSummary holds enough probe configuration to understand the failure context.
//
// +kubebuilder:object:generate=true
type ProbeSummary struct {
    // Type is "HTTPGet", "TCPSocket", or "Exec".
    Type string `json:"type"`

    // HTTPPath is the HTTP GET path when Type is HTTPGet.
    // +optional
    HTTPPath string `json:"httpPath,omitempty"`

    // Port is the numeric port for HTTPGet or TCPSocket probes.
    // +optional
    Port int32 `json:"port,omitempty"`

    // FailureThreshold is the number of consecutive failures before the probe fails.
    FailureThreshold int32 `json:"failureThreshold"`
}

// VolumeMountEvidence captures a volume mount relevant to the incident.
//
// +kubebuilder:object:generate=true
type VolumeMountEvidence struct {
    // Name is the volume name.
    Name string `json:"name"`

    // MountPath is the path inside the container.
    MountPath string `json:"mountPath"`

    // VolumeType is the backing volume type (PVC, ConfigMap, Secret, EmptyDir, etc.).
    VolumeType string `json:"volumeType"`

    // ClaimName is the PVC name when VolumeType is PVC.
    // +optional
    ClaimName string `json:"claimName,omitempty"`
}

// WorkloadEvidence holds evidence from the owning workload resource.
//
// +kubebuilder:object:generate=true
type WorkloadEvidence struct {
    // Kind is the workload type.
    Kind string `json:"kind"`

    // Name is the workload name.
    Name string `json:"name"`

    // Namespace is the workload namespace.
    Namespace string `json:"namespace"`

    // DesiredReplicas is the configured replica count.
    // +optional
    DesiredReplicas int32 `json:"desiredReplicas,omitempty"`

    // ReadyReplicas is the current ready replica count.
    // +optional
    ReadyReplicas int32 `json:"readyReplicas,omitempty"`

    // UpdateStrategy is the update strategy type (e.g., RollingUpdate, Recreate, OnDelete).
    // +optional
    UpdateStrategy string `json:"updateStrategy,omitempty"`

    // Conditions holds the workload's status conditions (capped at 10).
    // +optional
    Conditions []WorkloadCondition `json:"conditions,omitempty"`
}

// WorkloadCondition captures one condition from the workload's status.
//
// +kubebuilder:object:generate=true
type WorkloadCondition struct {
    // Type is the condition type.
    Type string `json:"type"`

    // Status is "True", "False", or "Unknown".
    Status string `json:"status"`

    // Reason is the machine-readable reason string.
    // +optional
    Reason string `json:"reason,omitempty"`

    // Message is the human-readable condition message.
    // +optional
    Message string `json:"message,omitempty"`

    // LastTransitionTime is when the condition last changed.
    // +optional
    LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

// NodeEvidence holds evidence from the node the affected Pod ran on.
//
// +kubebuilder:object:generate=true
type NodeEvidence struct {
    // Name is the node name.
    Name string `json:"name"`

    // Ready is the node's Ready condition status: "True", "False", or "Unknown".
    Ready string `json:"ready"`

    // MemoryPressure is the MemoryPressure condition status.
    MemoryPressure string `json:"memoryPressure"`

    // DiskPressure is the DiskPressure condition status.
    DiskPressure string `json:"diskPressure"`

    // PIDPressure is the PIDPressure condition status.
    PIDPressure string `json:"pidPressure"`

    // NetworkUnavailable is the NetworkUnavailable condition status.
    // +optional
    NetworkUnavailable string `json:"networkUnavailable,omitempty"`

    // AllocatableCPU is the node's allocatable CPU in millicores as a string.
    // +optional
    AllocatableCPU string `json:"allocatableCPU,omitempty"`

    // AllocatableMemory is the node's allocatable memory as a human-readable string.
    // +optional
    AllocatableMemory string `json:"allocatableMemory,omitempty"`

    // KernelVersion is the node's kernel version string.
    // +optional
    KernelVersion string `json:"kernelVersion,omitempty"`
}

// EventEvidence holds evidence from a single Kubernetes Event.
//
// +kubebuilder:object:generate=true
type EventEvidence struct {
    // Reason is the short machine-readable reason string (e.g., OOMKilled, FailedMount).
    Reason string `json:"reason"`

    // Message is the human-readable event message, truncated to 256 characters.
    Message string `json:"message"`

    // Count is the number of times this event has occurred.
    Count int32 `json:"count"`

    // FirstTime is when the event was first observed.
    // +optional
    FirstTime *metav1.Time `json:"firstTime,omitempty"`

    // LastTime is when the event was most recently observed.
    // +optional
    LastTime *metav1.Time `json:"lastTime,omitempty"`

    // InvolvedObjectKind is the kind of the object this event refers to.
    InvolvedObjectKind string `json:"involvedObjectKind"`

    // InvolvedObjectName is the name of the object this event refers to.
    InvolvedObjectName string `json:"involvedObjectName"`
}

// ContainerLogEvidence holds a bounded excerpt of a container's logs.
//
// +kubebuilder:object:generate=true
type ContainerLogEvidence struct {
    // ContainerName is the container whose logs were collected.
    ContainerName string `json:"containerName"`

    // IsPrevious indicates whether these are logs from the previous container instance.
    IsPrevious bool `json:"isPrevious"`

    // Lines holds the individual log lines collected.
    // +optional
    Lines []string `json:"lines,omitempty"`

    // Truncated is true when the log was cut short due to byte or line limits.
    Truncated bool `json:"truncated"`

    // UnavailableReason describes why logs could not be retrieved.
    // Empty when logs were retrieved successfully.
    // +optional
    UnavailableReason string `json:"unavailableReason,omitempty"`
}

// DependencyEvidence holds evidence about workload dependencies relevant to the trigger type.
//
// +kubebuilder:object:generate=true
type DependencyEvidence struct {
    // PVCs holds evidence for PersistentVolumeClaims referenced by the affected Pod.
    // Populated for MountFailure triggers.
    // +optional
    PVCs []PVCEvidence `json:"pvcs,omitempty"`

    // ImagePullSecretNames holds the names of ImagePullSecrets referenced by the Pod.
    // Values (secret data) are never stored.
    // Populated for ImagePullBackOff triggers.
    // +optional
    ImagePullSecretNames []string `json:"imagePullSecretNames,omitempty"`

    // SchedulingConstraints holds scheduling-relevant Pod spec fields.
    // Populated for SchedulingFailure triggers.
    // +optional
    SchedulingConstraints *SchedulingConstraints `json:"schedulingConstraints,omitempty"`
}

// PVCEvidence holds evidence about a PersistentVolumeClaim.
//
// +kubebuilder:object:generate=true
type PVCEvidence struct {
    // Name is the PVC name.
    Name string `json:"name"`

    // Namespace is the PVC namespace.
    Namespace string `json:"namespace"`

    // StorageClassName is the storage class used by the PVC.
    // +optional
    StorageClassName string `json:"storageClassName,omitempty"`

    // RequestedStorage is the storage capacity requested by the PVC.
    // +optional
    RequestedStorage string `json:"requestedStorage,omitempty"`

    // Phase is the PVC phase: Pending, Bound, Lost.
    Phase string `json:"phase"`

    // AccessModes is the list of access modes requested by the PVC.
    // +optional
    AccessModes []string `json:"accessModes,omitempty"`

    // BoundPVName is the name of the PersistentVolume the PVC is bound to.
    // +optional
    BoundPVName string `json:"boundPVName,omitempty"`

    // PVReclaimPolicy is the reclaim policy of the bound PersistentVolume.
    // +optional
    PVReclaimPolicy string `json:"pvReclaimPolicy,omitempty"`
}

// SchedulingConstraints holds Pod scheduling fields relevant to scheduling failure diagnosis.
//
// +kubebuilder:object:generate=true
type SchedulingConstraints struct {
    // NodeSelector holds the Pod's nodeSelector labels.
    // +optional
    NodeSelector map[string]string `json:"nodeSelector,omitempty"`

    // Tolerations holds the Pod's tolerations as strings (key=value:effect).
    // +optional
    Tolerations []string `json:"tolerations,omitempty"`

    // ResourceRequests holds aggregated resource requests across all containers.
    // +optional
    ResourceRequests map[string]string `json:"resourceRequests,omitempty"`
}

// CollectionError records a source that could not be collected and why.
//
// +kubebuilder:object:generate=true
type CollectionError struct {
    // Source identifies which evidence source failed
    // (e.g., "pod", "events", "node", "workload", "logs/my-container", "pvc/my-pvc").
    Source string `json:"source"`

    // Reason is a human-readable description of the failure.
    Reason string `json:"reason"`
}
```

### IncidentReportStatus Addition

Add the following field to `IncidentReportStatus` in `api/v1alpha1/incidentreport_types.go`:

```go
// Evidence holds the collected investigation evidence.
// Populated after initial trigger detection; refreshed on subsequent reconciliations.
// +optional
Evidence *EvidenceSnapshot `json:"evidence,omitempty"`
```

---

## Adaptive Collection Strategy

The orchestrator uses the `TriggerType` from `report.Status.Trigger` to decide which optional layers to enable. This avoids fetching PVC state for OOMKilled incidents or log content for ImagePullBackOff incidents.

```go
// triggerNeeds maps TriggerType to the optional layers it activates.
type triggerNeeds struct {
    previousLogs    bool // collect previous (terminated) container logs
    currentLogs     bool // collect current container logs
    pvcEvidence     bool // collect PVC/PV/StorageClass evidence
    imagePullEvidence bool // collect image pull secret names
    schedulingEvidence bool // collect node selector, tolerations, requests
}

func evidenceNeedsForTrigger(t v1alpha1.TriggerType) triggerNeeds {
    switch t {
    case v1alpha1.TriggerOOMKilled:
        return triggerNeeds{previousLogs: true, currentLogs: true}
    case v1alpha1.TriggerCrashLoopBackOff:
        return triggerNeeds{previousLogs: true, currentLogs: true}
    case v1alpha1.TriggerEviction:
        return triggerNeeds{previousLogs: true, currentLogs: true}
    case v1alpha1.TriggerMountFailure:
        return triggerNeeds{currentLogs: true, pvcEvidence: true}
    case v1alpha1.TriggerImagePullBackOff:
        return triggerNeeds{imagePullEvidence: true}
    case v1alpha1.TriggerCreateContainerConfigError:
        return triggerNeeds{} // no logs; container never started
    case v1alpha1.TriggerSchedulingFailure:
        return triggerNeeds{schedulingEvidence: true} // container never ran
    case v1alpha1.TriggerReadinessProbeFailure,
         v1alpha1.TriggerLivenessProbeFailure:
        return triggerNeeds{currentLogs: true}
    default:
        return triggerNeeds{currentLogs: true} // safe default
    }
}
```

---

## Log Collection Implementation

The `LogCollector` streams logs from the Kubernetes API and enforces both byte and line limits.

```go
// LogCollector collects bounded container log excerpts.
type LogCollector struct{}

func (c *LogCollector) Collect(ctx context.Context, input CollectorInput, needs triggerNeeds) ([]v1alpha1.ContainerLogEvidence, []v1alpha1.CollectionError)
```

Implementation approach for byte + line bounding:

1. Determine which containers to collect logs from using the trigger's `ContainerName` field. If empty, iterate all non-init containers.
2. For each target container, use `client.CoreV1().Pods(ns).GetLogs(podName, opts)` with `TailLines` set to `MaxLogLines` to limit via the API before reading bytes.
3. Open a `LimitedReader` wrapping the log stream with `MaxLogBytes` as the byte limit.
4. Read lines using `bufio.Scanner` until the stream is exhausted or the byte limit fires.
5. If the stream was not fully consumed (byte limit hit OR tail truncated), set `Truncated = true`.
6. Collect both current and previous logs when `needs.previousLogs` is true.

The `LogCollector` uses `client.Client` indirectly. Because `controller-runtime`'s `client.Client` does not expose `GetLogs`, the collector must accept a `kubernetes.Interface` (the raw client-go client). The `PodReconciler` will pass this in addition to the controller-runtime client.

```go
// CollectorInput extended for log collection
type CollectorInput struct {
    Client     client.Client
    KubeClient kubernetes.Interface   // for streaming logs
    Report     *v1alpha1.IncidentReport
    Pod        *corev1.Pod
    Config     *config.Config
}
```

---

## Integration Point with PodReconciler

### Step 9.5 Insertion

The orchestrator is called after `updateIncidentStatus` (step 9) and before the recovery re-fetch (step 10). The controller holds a reference to the orchestrator injected at construction time.

```go
// PodReconciler addition
type PodReconciler struct {
    // ... existing fields ...
    EvidenceOrchestrator *evidence.EvidenceOrchestrator
}
```

Inside `Reconcile()`, between the existing step 9 and step 10:

```go
// ---- Step 9.5: Collect evidence ----
collectCtx, cancel := context.WithTimeout(ctx, r.Config.EvidenceCollectionTimeout)
defer cancel()

snapshot := r.EvidenceOrchestrator.Collect(collectCtx, report, &pod)

base := report.DeepCopy()
report.Status.Evidence = &snapshot
if patchErr := r.Status().Patch(ctx, report, client.MergeFrom(base)); patchErr != nil {
    // Non-fatal: the next reconciliation will attempt evidence collection again.
    log.Error(patchErr, "failed to patch evidence snapshot, will retry on next reconciliation")
}
```

The patch failure is intentionally non-fatal. If the patch fails, the next reconciliation will collect and store evidence again.

### RBAC Annotations

Add these to `PodReconciler`:

```go
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/log,verbs=get
```

Note: Secret `get`/`list` is deliberately NOT added. The collector uses only `spec.imagePullSecrets[].name` which is already on the Pod object.

---

## Error Handling

### Collector Error Pattern

Every collector returns `(T, []v1alpha1.CollectionError)`. The orchestrator merges all `CollectionError` slices into `EvidenceSnapshot.CollectionErrors`.

```go
// Example: NodeCollector
func (c *NodeCollector) Collect(ctx context.Context, input CollectorInput) (*v1alpha1.NodeEvidence, []v1alpha1.CollectionError) {
    if input.Pod == nil || input.Pod.Spec.NodeName == "" {
        return nil, nil // not an error — Pod not scheduled yet
    }
    var node corev1.Node
    if err := input.Client.Get(ctx, types.NamespacedName{Name: input.Pod.Spec.NodeName}, &node); err != nil {
        return nil, []v1alpha1.CollectionError{{
            Source: "node",
            Reason: fmt.Sprintf("could not fetch node %s: %v", input.Pod.Spec.NodeName, err),
        }}
    }
    return buildNodeEvidence(&node), nil
}
```

### Timeout Handling

The orchestrator's context carries the `EvidenceCollectionTimeout` deadline. Collectors propagate this context to every Kubernetes API call. When the deadline elapses:

1. All in-flight API calls return `context.DeadlineExceeded`.
2. Collectors return partial results with a collection error.
3. The orchestrator sets `CollectedAt` and patches the snapshot.
4. A `CollectionError` with `Source: "collection"` and `Reason: "evidence collection timeout"` is appended.

### Event Message Truncation

```go
func truncateMessage(msg string) string {
    const maxLen = 256
    if len(msg) <= maxLen {
        return msg
    }
    return msg[:maxLen]
}
```

---

## Testing Strategy

### Unit Tests

Each collector in `test/unit/evidence/` has its own test file. Tests use fake `client.Client` implementations via `sigs.k8s.io/controller-runtime/pkg/client/fake`.

- `pod_collector_test.go` — pod evidence completeness, missing Pod produces CollectionError
- `events_collector_test.go` — event bounding, message truncation
- `node_collector_test.go` — node condition mapping, missing node
- `workload_collector_test.go` — deployment/statefulset conditions, absent workload
- `logs_collector_test.go` — byte and line limit enforcement, truncation flag, unavailable reason
- `dependencies_collector_test.go` — PVC evidence for MountFailure, image pull names for ImagePullBackOff, no collection for OOMKilled
- `orchestrator_test.go` — partial failure tolerance, CollectionErrors aggregation, CollectedAt always set

### Property-Based Tests

This feature involves data transformations (log bounding, event truncation, evidence field mapping) that benefit from property-based testing. The Go PBT library is [`pgregory.net/rapid`](https://github.com/flyingmutant/rapid), which integrates with the standard `testing` package using `rapid.Check`.

Each property test runs a minimum of 100 iterations.

### Integration Test

`test/integration/evidence_test.go` — uses `envtest` to create a Pod that triggers an `OOMKilled` incident and verifies that after reconciliation the `IncidentReport.Status.Evidence` is populated with non-nil `Pod`, `Events`, and `Node` fields.

---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

### Property 1: EvidenceSnapshot Always Has a CollectedAt Timestamp

*For any* valid `CollectorInput`, the `EvidenceSnapshot` returned by `EvidenceOrchestrator.Collect` SHALL have a non-nil `CollectedAt` field, regardless of how many individual source collectors succeed or fail.

**Validates: Requirements 2.2**

### Property 2: Collection Idempotence

*For any* `CollectorInput`, calling `EvidenceOrchestrator.Collect` twice with identical inputs produces snapshots with logically equivalent content. The second call replaces the first snapshot wholesale — it does not accumulate or duplicate entries.

**Validates: Requirements 1.3, 1.5**

### Property 3: Partial Failure Produces Non-Nil Snapshot With CollectionErrors

*For any* combination of source failures (Pod unavailable, Node unavailable, Events unavailable, etc.), the returned `EvidenceSnapshot` SHALL be non-nil, `CollectedAt` SHALL be set, and `CollectionErrors` SHALL contain an entry for each failed source.

**Validates: Requirements 9.1, 9.2, 9.3**

### Property 4: Event Count Is Bounded

*For any* list of N Kubernetes Events associated with an incident, the number of `EventEvidence` entries in the resulting `EvidenceSnapshot` SHALL be `min(N, MaxEventsPerIncident)`. When N exceeds `MaxEventsPerIncident`, the retained entries SHALL be the most recently observed events by `LastTime`.

**Validates: Requirements 4.1, 4.2, 10.3**

### Property 5: Log Line Count Is Bounded

*For any* container log stream of arbitrary content and length, the number of lines stored in `ContainerLogEvidence.Lines` SHALL not exceed `MaxLogLines`. When the line limit was reached before the stream ended, `ContainerLogEvidence.Truncated` SHALL be `true`.

**Validates: Requirements 7.3, 10.2**

### Property 6: Log Byte Count Is Bounded

*For any* container log stream of arbitrary content and length, the total byte count of all strings in `ContainerLogEvidence.Lines` (including newlines) SHALL not exceed `MaxLogBytes`. When the byte limit was reached before the stream ended, `ContainerLogEvidence.Truncated` SHALL be `true`.

**Validates: Requirements 7.3, 10.1**

### Property 7: Event Message Length Is Bounded

*For any* Kubernetes Event with a message of arbitrary length, the stored `EventEvidence.Message` SHALL have a length of at most 256 characters. For messages of length ≤ 256, the stored message SHALL equal the original message exactly.

**Validates: Requirements 4.3, 10.5**

### Property 8: Secret Values Are Never Stored in DependencyEvidence

*For any* Pod spec that references `imagePullSecrets`, the `DependencyEvidence.ImagePullSecretNames` field SHALL contain only the name strings of the referenced Secrets and SHALL contain no key-value pairs, binary data, or other Secret fields.

**Validates: Requirements 11.1, 11.2**
