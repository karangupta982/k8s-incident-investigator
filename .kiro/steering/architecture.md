# Kubernetes Incident Investigator — Architecture

## 1. High-Level Architecture

The system is a Kubernetes controller-based incident investigation system.

The high-level flow is:

```text
Kubernetes failure signal
        |
        v
Trigger / Threshold Evaluation
        |
        v
Incident Correlation
        |
        +---- Existing active incident?
        |          |
        |          +---- Yes -> update existing incident
        |          |
        |          +---- No  -> create IncidentReport
        |
        v
Evidence Collection
        |
        v
Evidence Correlation
        |
        v
Diagnosis Engine
        |
        +---- Known diagnosis
        |
        +---- Multiple possible causes
        |
        +---- Unknown / insufficient evidence
        |
        v
Recommendations
        |
        v
IncidentReport updated
        |
        v
Incident recovery detection
        |
        v
Incident resolved
````

---

# 2. Core Architectural Principle

The system is an evidence-first investigation pipeline.

It must NOT be designed as:

```text
Failure type -> hardcoded answer
```

Instead:

```text
Failure signal
    ->
Collect evidence
    ->
Correlate evidence
    ->
Interpret evidence
    ->
Produce diagnosis
```

This allows the system to remain useful for failures that are not explicitly known by the diagnosis engine.

---

# 3. Kubernetes Controller Model

The Investigator is implemented as a Kubernetes controller.

The controller uses `controller-runtime`.

Conceptually:

```text
Kubernetes API Server
        |
        | Watch
        v
controller-runtime
        |
        v
Work Queue
        |
        v
Reconcile()
        |
        v
Investigation Logic
```

The Kubernetes API Server does not directly call the Go `Reconcile()` function.

The controller-runtime watch receives Kubernetes resource events, places reconciliation requests into a work queue, and workers invoke the controller's `Reconcile()` method.

---

# 4. Role of Reconcile()

`Reconcile()` is the orchestration entry point.

It should NOT contain the entire investigation implementation.

Its responsibilities are:

1. Identify the resource/event requiring reconciliation.
2. Determine whether the event is relevant.
3. Find or create the corresponding active incident.
4. Trigger/update the investigation.
5. Collect or request relevant evidence.
6. Update the `IncidentReport`.
7. Determine whether another reconciliation should occur later.
8. Handle errors safely.

Complex logic must be separated into dedicated packages.

The controller should not become a large monolithic function.

---

# 5. Incident Model

An incident represents a continuous period of abnormal behavior affecting a workload.

The incident is workload-centric rather than Pod-centric.

A Pod is evidence associated with the incident.

Example:

```text
Incident #17

Workload:
Deployment/payment-api

Affected Pods:
payment-api-abc
payment-api-def
payment-api-ghi
```

This is necessary because Pods are ephemeral and may be replaced during an ongoing workload failure.

---

# 6. Incident Creation

Not every Kubernetes state change creates an incident.

Normal lifecycle changes must be ignored.

An incident is created when a failure signal crosses a meaningful trigger threshold.

Examples:

```text
OOMKilled
    -> immediate incident trigger

CrashLoopBackOff
    -> immediate incident trigger

ImagePullBackOff
    -> immediate incident trigger

CreateContainerConfigError
    -> immediate incident trigger

Repeated FailedMount
    -> incident trigger

Repeated readiness probe failures
    -> incident trigger

Repeated liveness probe failures
    -> incident trigger

Repeated scheduling failures
    -> incident trigger
```

"Immediate" means the signal is strong enough to create an incident immediately. It does not mean that the diagnosis is immediately known.

---

# 7. IncidentReport as Persistent Case File

The `IncidentReport` CR is the persistent representation of an incident and its investigation.

The object exists throughout the investigation lifecycle.

Example lifecycle:

```text
Incident detected
       |
       v
IncidentReport created
       |
       | phase = Investigating
       v
Evidence collected
       |
       v
Diagnosis available
       |
       | phase = Diagnosed
       v
Workload recovers
       |
       v
Stable for configured period
       |
       | phase = Resolved
       v
Incident complete
```

The IncidentReport should not be recreated for every related failure signal.

---

# 8. Incident Correlation

The system must determine whether a new failure signal belongs to:

* an existing active incident, or
* a new incident.

Correlation should consider:

1. Workload identity
2. Affected Pods
3. Failure signature
4. Time relationship
5. Recovery state
6. Kubernetes resource relationships
7. Causal relationships between signals

Example:

```text
10:00 OOMKilled
10:01 OOMKilled
10:02 CrashLoopBackOff
10:03 OOMKilled
```

These should normally belong to one incident.

If the workload becomes healthy for a significant stability period and later experiences a different failure:

```text
10:00 OOMKilled
...
10:30 recovered

12:00 ImagePullBackOff
```

the second failure should create a new incident.

---

# 9. Trigger Sources vs Evidence Sources

These are separate concepts.

## Primary trigger source

Pods.

## Secondary trigger sources

Potentially:

* Nodes
* PVCs
* relevant Kubernetes Events

## Evidence sources

Potentially:

* Pod
* Kubernetes Events
* Node
* Deployment
* ReplicaSet
* StatefulSet
* DaemonSet
* Job
* PVC
* PV
* StorageClass
* ConfigMap metadata
* Secret metadata
* Service
* EndpointSlice
* container logs
* Prometheus metrics in a future/optional integration

A resource does not need to be a trigger source to be an evidence source.

---

# 10. Evidence Collection

Evidence collection must be adaptive.

Do not fetch every possible Kubernetes resource for every incident.

Use:

```text
Trigger
   ->
Basic evidence
   ->
Failure category
   ->
Relevant evidence
```

Example:

For `OOMKilled`:

```text
Pod
Events
Node
Resources
Logs
Potentially metrics
```

For `ImagePullBackOff`:

```text
Pod
Events
Image configuration
ImagePullSecrets metadata/existence
Node
```

For `FailedMount`:

```text
Pod
Events
PVC
PV
StorageClass
CSI-related information where available
```

---

# 11. Evidence Layers

Evidence should be collected in layers.

### Layer 1: Primary resource

Pod state, container state, restart count, exit code, termination reason, image, resources, probes, volumes, node, ownership.

### Layer 2: Events

Relevant Kubernetes events for the affected resources.

### Layer 3: Workload ownership

Pod -> ReplicaSet -> Deployment or equivalent workload owner.

### Layer 4: Node

Node readiness and pressure conditions.

### Layer 5: Dependencies

PVC, Service, EndpointSlice, ConfigMap metadata, Secret metadata, etc., only when relevant.

### Layer 6: Logs

Bounded current/previous container log excerpts.

### Layer 7: Metrics

Optional future evidence source such as Prometheus.

---

# 12. Log Handling

Raw logs must NOT be stored without limits inside the IncidentReport.

The Investigator should:

* collect current logs when useful
* collect previous logs for restart/crash investigations
* limit the number of lines and/or bytes
* identify the container
* extract relevant information
* store only a bounded excerpt in the IncidentReport

Large raw evidence should not be stored in etcd.

External evidence storage is a future enhancement.

If logs cannot be retrieved, the report should explicitly record:

```text
Container logs unavailable because ...
```

The absence of logs is itself useful evidence.

---

# 13. Evidence Correlation

Evidence correlation must initially be deterministic.

Use:

```text
Time
+
Kubernetes resource relationships
+
Failure semantics
+
Ownership relationships
+
Causal relationships
```

Do not introduce an LLM as the core correlation mechanism.

Example:

```text
PVC Pending
    ->
FailedMount
    ->
Container cannot start
```

These signals are likely related because they involve the same workload, dependency, time period, and causal chain.

---

# 14. Diagnosis Engine

Diagnosis is performed after evidence has been collected and correlated.

The MVP uses modular deterministic diagnosis rules.

Conceptually:

```text
Evidence
   |
   v
Diagnosis Engine
   |
   +--> OOM rule
   +--> CrashLoop rule
   +--> ImagePull rule
   +--> Probe rule
   +--> Mount rule
   +--> Scheduling rule
   +--> ...
```

Each rule should be independently testable.

Rules should produce structured findings rather than only strings.

A finding should contain:

* cause
* confidence
* explanation
* supporting evidence
* contributing factors
* recommendation

---

# 15. Multiple Causes

The diagnosis engine must not force a single cause when evidence does not support one.

The report may contain:

```text
Primary finding
Contributing factors
Alternative hypotheses
```

Example:

```text
Primary:
Container exceeded memory limit

Contributing factor:
Node was under memory pressure

Confidence:
High
```

Confidence must not be presented as fake statistical probability unless the system actually implements a statistical model.

Use qualitative confidence levels in MVP:

```text
High
Medium
Low
```

---

# 16. Unknown Diagnosis

Unknown is a valid result.

If no diagnosis rule matches:

```text
Diagnosis:
Unknown

Reason:
No known failure pattern matched the collected evidence.

Evidence:
...
```

The system should still provide:

* timeline
* evidence
* logs/excerpts
* affected workload
* what was ruled out
* suggested next investigation area

The system must never invent a root cause merely to produce an answer.

---

# 17. Recommendations

Recommendations must be evidence-backed and non-destructive.

Bad:

```text
Check your Kubernetes configuration.
```

Good:

```text
The container was terminated with OOMKilled and reached
its configured memory limit of 512Mi.

Investigate application memory consumption and consider
increasing the memory limit if the workload legitimately
requires additional memory.
```

Recommendations are suggestions.

The MVP does not automatically execute them.

---

# 18. CRD Storage Principles

IncidentReport must remain reasonably small.

Do not store:

* unlimited logs
* entire cluster state
* huge event histories
* Secret values
* unnecessary duplicate objects

Store:

* incident metadata
* workload identity
* affected Pods
* lifecycle state
* timeline
* diagnosis
* important evidence
* bounded log excerpts
* recommendations

---

# 19. Reliability

The Investigator must tolerate:

* API Server failures
* missing Pods
* deleted Pods
* unavailable logs
* unavailable evidence sources
* RBAC failures
* operator restarts
* temporary network failures

Evidence collection is best-effort.

One unavailable source should not automatically fail the entire investigation.

Example:

```text
Pod evidence       ✓
Events             ✓
Node evidence      ✓
Logs               ✗
```

The investigation should continue and clearly record that logs were unavailable.

---

# 20. Non-Remediation Principle

The Investigator is read-mostly.

It must not modify the application being investigated.

This makes the system safer and simplifies RBAC.

Future automatic remediation is explicitly outside MVP scope.

---

# 21. MVP Architecture

The initial system should contain these logical components:

```text
Incident Controller
        |
        v
Incident Manager
        |
        v
Evidence Collector
        |
        v
Evidence Correlator
        |
        v
Diagnosis Engine
        |
        v
Recommendation Generator
        |
        v
IncidentReport
```

Supporting components:

```text
Kubernetes API client
Controller-runtime cache/watch
Logging client
Configuration
Metrics/observability
```

---

# 22. Future Components

Do not implement these in MVP:

* Prometheus as mandatory dependency
* external evidence storage
* Slack/PagerDuty
* Web UI
* Grafana dashboard
* LLM-based diagnosis
* automatic remediation
* cross-cluster investigation