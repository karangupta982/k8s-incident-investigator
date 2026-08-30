# Kubernetes Incident Investigator Development Skill

## Purpose

This skill defines how development work should be performed on the Kubernetes Incident Investigator project.

The project is an evidence-first Kubernetes incident investigation controller.

Before implementing a feature, preserve the existing architectural decisions documented in `.kiro/steering/architecture.md`.

---

## Development Rules

### 1. Understand Before Implementing

Before writing code for a new feature:

1. Identify the architectural area involved.
2. Check existing design decisions.
3. Identify affected components.
4. Identify failure cases.
5. Prefer extending existing abstractions over introducing parallel systems.

Do not immediately write code from a short feature description.

---

### 2. Keep the Controller Thin

Do not put investigation, correlation, diagnosis, or large evidence-collection logic directly inside `Reconcile()`.

Prefer:

```text
Reconcile()
    ->
Incident Manager
    ->
Evidence
    ->
Correlation
    ->
Diagnosis
    ->
Report
````

---

### 3. Treat Reconciliation as Idempotent

The same Kubernetes event may result in multiple reconciliation calls.

Code must safely handle:

* duplicate events
* retries
* controller restarts
* partially completed investigations
* already-existing IncidentReports

Never assume a reconciliation request is processed exactly once.

---

### 4. Do Not Assume Event Ordering

Kubernetes information may arrive at different times.

For example:

```text
Pod state changes
Event appears
Logs become available
Node condition changes
```

The system must tolerate partial information.

Do not rely on a strict event ordering unless Kubernetes guarantees it.

---

### 5. Evidence Before Diagnosis

Diagnosis must consume structured evidence.

Do not implement:

```text
if event message contains "OOMKilled":
    return "memory leak"
```

Instead:

```text
Kubernetes data
    ->
structured evidence
    ->
diagnosis rule
```

---

### 6. No Fabricated Root Cause

A diagnosis must be supported by evidence.

If evidence is insufficient:

```text
Unknown
```

is preferable to an unsupported root-cause claim.

---

### 7. Preserve Evidence Provenance

Where practical, diagnosis findings should be traceable to the evidence that produced them.

For example:

```text
Cause:
OOMKilled

Evidence:
Pod container termination reason
Event
Exit code
Resource limit
```

---

### 8. Avoid Huge Kubernetes Objects

Never store unlimited logs or raw cluster data in IncidentReport.

All collected data must have bounded size.

---

### 9. Protect Secrets

Never:

* print Secret values
* store Secret values in IncidentReport
* include Secret values in errors
* expose Secret values through metrics

Only retrieve Secret contents if a future feature explicitly requires it and the security design permits it.

---

### 10. Prefer Read-Only Operations

The Investigator is an observation and reporting system.

Do not add mutation behavior unless explicitly required by an approved design.

---

### 11. Keep Diagnosis Rules Independent

Diagnosis rules should:

* receive structured evidence
* determine whether the evidence matches
* return structured findings

They should not access Kubernetes directly.

This allows diagnosis rules to be unit tested without a cluster.

---

### 12. Test Failure Scenarios

Every diagnosis rule should have:

* positive match tests
* negative match tests
* incomplete evidence tests
* conflicting evidence tests where applicable

---

### 13. Handle Unknowns

Every evidence collector should tolerate unavailable data.

Example:

```text
Pod data: available
Events: available
Logs: unavailable
Node: unavailable
```

The investigation should continue where possible and record missing evidence.

---

### 14. Avoid Premature Complexity

Do not introduce:

* LLMs
* Redis
* PostgreSQL
* Kafka
* external storage
* mandatory Prometheus
* web dashboards
* Slack
* automatic remediation

unless the architecture explicitly evolves to require them.

---

## Before Modifying Architecture

If an implementation requirement conflicts with an existing architecture decision:

1. Identify the conflict.
2. Explain the tradeoff.
3. Propose alternatives.
4. Do not silently change the architecture.

---

## Definition of Done

A feature is not considered complete merely because it compiles.

Where applicable it should include:

* implementation
* unit tests
* integration tests
* error handling
* documentation
* metrics/logging
* Kubernetes manifests
* appropriate RBAC
* validation

---

## Code Quality

Prefer simple, readable Go over clever abstractions.

Avoid:

* unnecessary interfaces
* giant functions
* global mutable state
* hidden side effects
* magic constants
* duplicated Kubernetes API logic

Use context-aware operations.

Return meaningful errors.

Use structured logging.

---

## Security

Assume the Investigator runs in a production Kubernetes cluster.

Review:

* RBAC
* Secret handling
* API permissions
* resource consumption
* log exposure
* denial-of-service possibilities

before adding a feature that increases cluster access.