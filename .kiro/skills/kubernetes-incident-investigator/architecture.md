# Investigation Architecture Patterns

## Core Pipeline

The investigation pipeline is:

```text
Trigger
    ->
Incident Decision
    ->
Incident Correlation
    ->
Evidence Collection
    ->
Evidence Correlation
    ->
Diagnosis
    ->
Recommendations
    ->
IncidentReport
````

---

## Trigger

A trigger is a Kubernetes signal that may indicate abnormal workload behavior.

Examples:

* OOMKilled
* CrashLoopBackOff
* ImagePullBackOff
* CreateContainerConfigError
* repeated FailedMount
* repeated probe failures
* repeated scheduling failures
* eviction
* relevant Node failure

A trigger does not automatically mean that diagnosis is known.

---

## Incident Decision

The trigger policy determines whether a signal crosses the threshold for incident creation.

Examples:

```text
OOMKilled
    -> immediate incident

CrashLoopBackOff
    -> immediate incident

Single readiness failure
    -> ignore

Repeated readiness failures
    -> incident
```

Thresholds should eventually be configurable but should begin with sensible defaults.

---

## Incident Correlation

Determine:

```text
Existing active incident?
```

Correlation considers:

* workload
* Pods
* time
* failure signature
* recovery
* Kubernetes ownership
* causal relationships

Related signals should update the existing incident rather than create duplicates.

---

## Evidence Collection

Evidence collectors retrieve structured information.

Collectors should not diagnose.

Example:

```text
PodCollector
    -> PodEvidence

EventCollector
    -> EventEvidence

NodeCollector
    -> NodeEvidence

LogCollector
    -> LogEvidence
```

---

## Evidence Correlation

Evidence is grouped based on:

* timestamps
* resource relationships
* ownership
* dependency relationships
* failure semantics
* causal relationships

The output is a coherent investigation context.

---

## Diagnosis

Diagnosis rules interpret correlated evidence.

Example:

```text
OOMRule

Inputs:
- termination reason
- exit code
- resource limit
- events

Output:
Diagnosis finding
```

Rules must not claim more than the evidence supports.

---

## Unknown

If no diagnosis rule matches:

```text
Diagnosis = Unknown
```

The system still reports collected evidence.

Unknown is a successful investigation outcome when evidence is insufficient for automatic diagnosis.

---

## Recovery

An incident is resolved only after:

1. The workload returns to a healthy state.
2. Relevant failure signals stop.
3. The workload remains stable for the configured stability period.

A diagnosis does not imply resolution.

---

## Pod Replacement

Pod identity is ephemeral.

The workload is the primary incident identity.

Affected Pods are tracked as part of incident evidence.

Example:

```text
Deployment/payment-api

Affected Pods:
payment-api-abc
payment-api-def
```

---

## Bounded Investigation

Every investigation should have limits:

* maximum duration
* maximum log size
* maximum evidence size
* retry limits
* API call limits
* bounded requeue frequency

The investigator must not become a source of cluster load during an incident.