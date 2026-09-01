# Requirements Document

## Introduction

The evidence collection feature extends the Kubernetes Incident Investigator foundation with the ability to gather structured information about a detected incident from multiple Kubernetes API sources and container logs.

When the foundation layer creates an `IncidentReport`, it records only the triggering failure signal and the affected workload identity. Evidence collection fills the rest of the report: Pod container state, workload health, node conditions, Kubernetes Events, container logs, and dependency resources (PVCs, image pull secrets, scheduling constraints). This evidence is stored in the `IncidentReport` status and forms the complete input to the diagnosis engine in the next specification.

The evidence collection system must be bounded, adaptive, best-effort, explainable, and non-destructive. No external systems, remote storage, LLMs, or Prometheus integrations are required.

---

## Glossary

### Evidence

Structured information gathered from Kubernetes APIs and container logs that describes the state of a workload at the time of an incident.

### EvidenceSnapshot

The top-level status field on an `IncidentReport` that holds all collected evidence for one incident investigation cycle.

### Evidence Layer

A logical grouping of evidence by the Kubernetes resource it is collected from. Layers are ordered from most directly related (the affected Pod) to least directly related (workload dependencies).

### EvidenceCollector

A Go interface implemented by each focused evidence-gathering component in `internal/evidence/`. Each collector is responsible for one layer.

### Adaptive Collection

The behaviour whereby the specific evidence sources consulted depend on the trigger type of the incident. For example, a `MountFailure` incident collects PVC/PV/StorageClass evidence; an `ImagePullBackOff` incident collects image pull secret metadata.

### Bounded Collection

Any evidence collection operation that enforces an explicit limit on bytes, line counts, event counts, or total API calls to prevent unbounded growth of the `IncidentReport` or unbounded Kubernetes API load.

### Collection Error

A structured record appended to `EvidenceSnapshot.CollectionErrors` when an evidence source could not be reached or returned insufficient data. The absence of evidence is itself reported and useful.

### ContainerLogEvidence

Evidence type representing a bounded excerpt of a container's stdout/stderr log, including a flag indicating whether the log was truncated and a reason string when log retrieval was not possible.

### Trigger Type

The classified failure signal that created the incident, as already recorded in `IncidentReport.Status.Trigger.Type`. Adaptive collection uses this value to select which dependency evidence to gather.

### Investigation Timeout

A configurable upper bound on total evidence collection duration per reconciliation cycle. When the timeout elapses, partial results are accepted and a collection error is recorded.

---

## Requirements

### Requirement 1: Trigger Evidence Collection After Incident Detection

**User Story:** As a Platform Engineer, I want evidence to be automatically gathered after an incident is detected so that the IncidentReport contains meaningful investigation data without manual intervention.

#### Acceptance Criteria

1. WHEN an `IncidentReport` is created or updated by the foundation layer, THE Investigator SHALL initiate evidence collection for the incident before the next reconciliation requeue.

2. WHEN evidence collection completes, THE Investigator SHALL store the resulting `EvidenceSnapshot` in the `IncidentReport` status.

3. WHEN a subsequent reconciliation updates an active `IncidentReport`, THE Investigator SHALL refresh the `EvidenceSnapshot` with newly gathered evidence, replacing the previous snapshot.

4. WHEN evidence collection is initiated, THE Investigator SHALL enforce the configured `EvidenceCollectionTimeout`. IF the timeout elapses before all sources are collected, THE Investigator SHALL store the partial results and record a collection error indicating the timeout.

5. WHEN evidence collection completes with partial results due to any source failure or timeout, THE Investigator SHALL still update the `IncidentReport` status with the available evidence rather than discarding the partial snapshot.

---

### Requirement 2: Store Evidence in the IncidentReport Status

**User Story:** As an engineer investigating an incident, I want all collected evidence to appear in the `IncidentReport` status so that I can inspect it with `kubectl describe incidentreport` without consulting external systems.

#### Acceptance Criteria

1. THE Investigator SHALL store evidence exclusively in the `IncidentReport` status subresource via the `evidence` field. THE Investigator SHALL NOT store raw evidence in external databases, object storage, or remote logging systems.

2. WHEN evidence is stored, THE Investigator SHALL record the timestamp at which collection last ran in `EvidenceSnapshot.CollectedAt`.

3. WHEN evidence sources cannot be contacted or return errors, THE Investigator SHALL record the source identifier and failure reason in `EvidenceSnapshot.CollectionErrors`.

4. THE `IncidentReport` object inclusive of all status fields and evidence SHALL remain well under the Kubernetes etcd 1 MB per-object limit. THE Investigator SHALL enforce per-source size limits that collectively keep the total object size safe.

5. WHEN evidence is updated on a subsequent reconciliation, THE Investigator SHALL replace the previous `EvidenceSnapshot` wholesale rather than attempting to merge individual fields.

---

### Requirement 3: Collect Pod Evidence (Layer 1)

**User Story:** As an engineer, I want detailed container state from the affected Pod so that I can understand the exact failure condition without running `kubectl describe pod`.

#### Acceptance Criteria

1. WHEN evidence collection runs for an incident, THE Investigator SHALL collect the primary Pod's container statuses, including container name, image, current state, restart count, exit code, and termination reason for the current and last termination.

2. WHEN evidence collection runs, THE Investigator SHALL collect the resource limits and requests (CPU and memory) configured for each container.

3. WHEN evidence collection runs, THE Investigator SHALL record whether liveness and readiness probes are configured and, when they are, include the failure threshold and a summary of the probe type (HTTP GET path and port, TCP socket port, or exec command without arguments).

4. WHEN evidence collection runs, THE Investigator SHALL record the volume mounts that are relevant to the incident trigger type.

5. WHEN the primary affected Pod no longer exists at the time of evidence collection, THE Investigator SHALL record a collection error for the Pod layer and continue collecting other evidence layers.

---

### Requirement 4: Collect Kubernetes Events (Layer 2)

**User Story:** As an engineer, I want the relevant Kubernetes Events for the affected Pod and workload so that I can understand what Kubernetes itself observed without running `kubectl get events`.

#### Acceptance Criteria

1. WHEN evidence collection runs, THE Investigator SHALL collect Kubernetes Events whose `involvedObject` is the affected Pod, bounded by the configured `MaxEventsPerIncident` limit.

2. WHEN the number of relevant Events exceeds `MaxEventsPerIncident`, THE Investigator SHALL retain the most recent Events up to the configured limit and discard older ones.

3. WHEN evidence collection runs, THE Investigator SHALL store for each Event: the reason, message (truncated to 256 characters), count, firstTime, lastTime, and involvedObject kind and name.

4. WHEN no relevant Events exist for the affected Pod, THE Investigator SHALL store an empty Events list and SHALL NOT record this as a collection error.

5. WHEN Event listing fails due to an API error, THE Investigator SHALL record a collection error for the Events layer and continue collecting other evidence layers.

---

### Requirement 5: Collect Workload Evidence (Layer 3)

**User Story:** As an engineer, I want current workload state so that I can understand whether the failure affected the entire workload or only an individual Pod.

#### Acceptance Criteria

1. WHEN evidence collection runs and the workload owner has been resolved, THE Investigator SHALL collect the workload resource's kind, name, desired replica count, ready replica count, and conditions.

2. WHEN evidence collection runs and the workload owner is a Deployment or StatefulSet, THE Investigator SHALL also record the update strategy type.

3. WHEN the workload resource cannot be fetched, THE Investigator SHALL record a collection error for the Workload layer and continue collecting other evidence layers.

4. WHEN workload ownership was not resolved by the foundation layer, THE Investigator SHALL skip Workload layer collection and SHALL NOT record this as a collection error.

---

### Requirement 6: Collect Node Evidence (Layer 4)

**User Story:** As an engineer, I want Node conditions and capacity information so that I can determine whether a node-level problem such as memory pressure contributed to the incident.

#### Acceptance Criteria

1. WHEN evidence collection runs and the affected Pod was scheduled to a Node, THE Investigator SHALL collect the Node's readiness condition status, all pressure conditions (MemoryPressure, DiskPressure, PIDPressure, NetworkUnavailable), allocatable CPU and memory, and kernel version.

2. WHEN the affected Pod was not yet scheduled to a Node (nodeName is empty), THE Investigator SHALL skip Node layer collection and SHALL NOT record this as a collection error.

3. WHEN the Node resource cannot be fetched, THE Investigator SHALL record a collection error for the Node layer and continue collecting other evidence layers.

---

### Requirement 7: Collect Container Logs (Layer 6)

**User Story:** As an engineer, I want bounded container log excerpts included in the IncidentReport so that I can see the last output of a failing container without manually running `kubectl logs`.

#### Acceptance Criteria

1. WHEN evidence collection runs and the trigger type is `OOMKilled` or `CrashLoopBackOff`, THE Investigator SHALL attempt to collect both the current logs and the previous container logs for the affected container.

2. WHEN evidence collection runs and the trigger type is not `OOMKilled` or `CrashLoopBackOff`, THE Investigator SHALL attempt to collect only the current logs for the affected container.

3. WHEN collecting logs, THE Investigator SHALL enforce both a maximum byte limit (default 32768 bytes / 32 KB) and a maximum line limit (default 200 lines). WHEN either limit is reached, THE Investigator SHALL stop collecting further log content and set `ContainerLogEvidence.Truncated` to `true`.

4. WHEN log retrieval fails for any reason including Pod deletion, eviction, or container not started, THE Investigator SHALL record the reason in `ContainerLogEvidence.UnavailableReason` and SHALL NOT treat the log unavailability as a blocking error.

5. THE Investigator SHALL NOT store raw log bytes directly in the IncidentReport. THE Investigator SHALL store the log content as an ordered slice of individual log lines, each line being a string.

6. WHEN the trigger type is `ImagePullBackOff`, `CreateContainerConfigError`, or `SchedulingFailure`, THE Investigator SHALL skip log collection because no container logs will be available, and SHALL NOT record this as a collection error.

---

### Requirement 8: Collect Dependency Evidence (Layer 5 — Adaptive)

**User Story:** As an engineer, I want evidence about the specific Kubernetes dependencies involved in the failure so that I can diagnose the root cause without manually inspecting PVCs, pull secrets, and scheduling constraints.

#### Acceptance Criteria

1. WHEN the incident trigger type is `MountFailure`, THE Investigator SHALL collect evidence for each PersistentVolumeClaim referenced by the affected Pod's volumes, including the PVC name, namespace, storage class name, requested storage capacity, access modes, and phase.

2. WHEN the incident trigger type is `MountFailure` and a PVC is bound to a PersistentVolume, THE Investigator SHALL additionally record the PV name and the reclaim policy.

3. WHEN the incident trigger type is `ImagePullBackOff`, THE Investigator SHALL collect the image name and, for each ImagePullSecret referenced by the Pod's `spec.imagePullSecrets`, record only the Secret name. THE Investigator SHALL NOT read or store Secret values.

4. WHEN the incident trigger type is `SchedulingFailure`, THE Investigator SHALL collect the Pod's node selector, tolerations, and resource requests for use in diagnosing why the Pod could not be scheduled.

5. WHEN dependency evidence collection fails for an individual resource (for example a PVC is not found), THE Investigator SHALL record a collection error for that specific resource and continue collecting remaining dependency sources.

6. WHEN the incident trigger type does not require dependency evidence (such as `OOMKilled`, `CrashLoopBackOff`, `ReadinessProbeFailure`, `LivenessProbeFailure`, or `Eviction`), THE Investigator SHALL skip dependency collection and SHALL NOT record this as a collection error.

---

### Requirement 9: Tolerate Partial Collection Failures

**User Story:** As a Platform Engineer, I want evidence collection to be resilient to unavailable sources so that one missing piece of information does not block the entire investigation.

#### Acceptance Criteria

1. WHEN any single evidence source (Pod, Events, Node, Workload, Logs, or a dependency resource) is unavailable or returns an error, THE Investigator SHALL continue collecting evidence from all remaining sources.

2. WHEN a source fails, THE Investigator SHALL record a `CollectionError` entry with the source identifier and reason.

3. WHEN all evidence sources fail, THE Investigator SHALL still update the `IncidentReport` status with an `EvidenceSnapshot` that contains only the `CollectionErrors` list and the `CollectedAt` timestamp.

4. WHEN evidence collection succeeds for at least one source, THE Investigator SHALL reflect the partial results in the IncidentReport status.

5. WHEN a Kubernetes API call returns a temporary error (for example a timeout or 503), THE Investigator SHALL treat the source as unavailable for the current collection cycle and record a collection error. THE Investigator SHALL NOT retry the individual API call within the same collection cycle.

---

### Requirement 10: Keep Evidence Bounded and Storage-Safe

**User Story:** As a Platform Engineer, I want the IncidentReport to remain a reasonable size so that etcd is not burdened by large evidence objects.

#### Acceptance Criteria

1. THE Investigator SHALL enforce a maximum of `MaxLogBytes` bytes (default 32768) per container log excerpt. Logs exceeding this limit SHALL be truncated to the limit.

2. THE Investigator SHALL enforce a maximum of `MaxLogLines` lines (default 200) per container log excerpt. Log excerpts exceeding this limit SHALL be truncated to the limit.

3. THE Investigator SHALL enforce a maximum of `MaxEventsPerIncident` events (default 25) in the Events layer. When the Event list exceeds this limit, the most recent events SHALL be retained.

4. WHEN collecting workload evidence for a Deployment or StatefulSet, THE Investigator SHALL limit stored conditions to the most recent 10 conditions.

5. WHEN storing Kubernetes Event messages in evidence, THE Investigator SHALL truncate each message to a maximum of 256 characters.

6. THE Investigator SHALL enforce a total collection timeout of `EvidenceCollectionTimeout` (default 30 seconds) across all evidence layers. WHEN the timeout is exceeded, partial evidence SHALL be stored and a timeout collection error SHALL be recorded.

---

### Requirement 11: Never Store Secret Values

**User Story:** As a Platform Engineer, I want to guarantee that sensitive Kubernetes Secret values are never included in an IncidentReport so that investigation records do not become a security liability.

#### Acceptance Criteria

1. THE Investigator SHALL NOT read Kubernetes Secret values from the API during evidence collection.

2. WHEN evidence references a Kubernetes Secret (for example an ImagePullSecret), THE Investigator SHALL store only the Secret's name, not its data fields.

3. THE Investigator SHALL NOT store environment variable values from containers in evidence, as environment variables may contain secret values injected via `secretKeyRef` or `configMapKeyRef`.

4. THE Investigator SHALL NOT store volume content, configmap data values, or secret data values in evidence.

---

### Requirement 12: Operate with Least-Privilege RBAC

**User Story:** As a Platform Engineer, I want evidence collection to require only read-only access to the Kubernetes resources it consults so that the controller does not accumulate dangerous permissions.

#### Acceptance Criteria

1. THE Investigator SHALL require only `get`, `list`, and `watch` verbs for all Kubernetes resource types accessed during evidence collection.

2. THE Investigator SHALL require the `create` verb on the `pods/log` subresource to stream container logs. THE Investigator SHALL NOT require any other write permissions for log collection.

3. THE Investigator SHALL NOT require `get` or `list` access to `Secret` objects (only the Pod's `spec.imagePullSecrets` names are used, which are already on the Pod object).

4. THE Investigator SHALL require `get` and `list` access to `PersistentVolumeClaims` and `PersistentVolumes` to support dependency evidence collection.

5. THE Investigator SHALL require `get` and `list` access to `StorageClasses` to support mount failure dependency evidence.

---

### Requirement 13: Provide Configuration for Collection Limits

**User Story:** As a Platform Engineer, I want to tune evidence collection limits to match my cluster's constraints so that the defaults can be overridden when needed.

#### Acceptance Criteria

1. THE Investigator SHALL expose `MaxLogBytes` as a configurable integer with a default of 32768 (32 KB). Values of zero or less SHALL be rejected at startup.

2. THE Investigator SHALL expose `MaxLogLines` as a configurable integer with a default of 200. Values of zero or less SHALL be rejected at startup.

3. THE Investigator SHALL expose `MaxEventsPerIncident` as a configurable integer with a default of 25. Values of zero or less SHALL be rejected at startup.

4. THE Investigator SHALL expose `EvidenceCollectionTimeout` as a configurable duration with a default of 30 seconds. Values of zero or less SHALL be rejected at startup.

5. WHEN any collection limit configuration value is invalid, THE Investigator SHALL reject startup with a descriptive error message identifying the invalid field and its value.

---

## Out of Scope

The following capabilities are explicitly outside this specification:

- Diagnosis logic or confidence scoring (next specification)
- Recommendation generation (reporting specification)
- Prometheus or metrics-based evidence
- External evidence storage (future enhancement)
- LLM-based log analysis or correlation
- Automatic remediation triggered by evidence findings
- Evidence from multi-cluster resources
- Evidence from resources in namespaces not watched by the controller
- Collection of Secret data values
- Web UI for evidence browsing
