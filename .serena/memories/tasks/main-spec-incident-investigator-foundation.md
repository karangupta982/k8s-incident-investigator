# Task: incident-investigator-foundation spec

## Branch
`main`

## Goal
Finalize the spec for the Kubernetes Incident Investigator foundation before implementation begins. The spec is now complete and approved.

## Spec Location
`.kiro/specs/incident-investigator-foundation/`
- `requirements.md` — 21 requirements, fully detailed with EARS-format acceptance criteria
- `design.md` — complete technical design: CRD types, Two-Name Model, trigger detection, correlation, workload-aware recovery, RBAC, idempotency, correctness properties
- `tasks.md` — 14 task groups, 48 sub-tasks, 31-wave dependency graph

## Status
**Spec finalized after full review cycle with ChatGPT. Ready for implementation.**

## Final Round Changes Applied
- Event watch → Pod mapping (FailedMount/Unhealthy/FailedScheduling trigger on Event.count change)
- Startup recovery now lists ALL IncidentReports (not just phase!=Resolved) to detect Resolved active-named crash states
- Historical copy explicitly excludes server-managed metadata (uid, resourceVersion, etc.)
- CorrelationWindow marked reserved/no-op in foundation
- Event aggregation: use highest event.count among matching Events, not sum
- RS-found-Deployment-missing now falls back to Pod identity (not RS as workload)
- Startup enqueue uses affected Pod names, not workload keys

## Key Confirmed Decisions

### Incident Identity (Two-Name Model)
- Active slot name: `<workload-name>-<kind-lowercase>-active` (e.g., `payment-api-deployment-active`)
- Historical name: `<workload-name>-<kind-lowercase>-<YYYYMMDD>-<5hex>` (FNV-32a hash of namespace/kind/name/startedAt-unix)
- Pod fallback: `<pod-name>-pod-active`
- Resolution: copy-then-delete (two-step recoverable transition, NOT atomic)
- On crash mid-transition: startup recovery detects both active+historical exist, completes the DELETE
- Requires `delete` RBAC on `incidentreports`

### Incident Correlation
- One active incident per workload at all times
- Active incident exists → all subsequent failures for that workload always update it (window irrelevant)
- No active incident → create new incident unconditionally
- `CorrelationWindow` (default 10m) is stored in Config but does NOT gate association with an already-active incident

### Trigger Detection
- State-based (idempotent, from Pod object): OOMKilled, CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError, Eviction
- Event-based (from `event.count` field, not raw event object count): FailedMount, Unhealthy (probe), FailedScheduling
- Threshold check: `event.count >= threshold`
- Same Pod state on reconciliation N and N+1 = same condition, NOT two failures

### FailureCount semantics
- State-based: increments only on new unique failure signature (new container, or reoccurrence after stability reset). NOT incremented on re-observation of same state.
- Event-based: SET to current `event.count` value (not independently incremented)

### Recovery (workload-type-aware)
- Deployment: `readyReplicas >= replicas AND availableReplicas >= spec.replicas`
- StatefulSet: `readyReplicas >= spec.replicas`
- DaemonSet: `numberReady >= desiredNumberScheduled`
- Job: `succeeded >= 1`
- CronJob: evaluate LatestJob
- Pod fallback: `phase == Running AND all containers ready`
- Rolling updates and scale ops must NOT be misidentified as unrecovered failures

### Startup Recovery (Option B)
- On startup: list active IncidentReports, detect stale mid-transition pairs, enqueue synthetic reconcile requests
- Non-blocking on list failure
- No second IncidentReport watch

### RBAC
- Read-only on all application resources
- get/list/watch/create/update/patch/delete on incidentreports only
- No delete on Pods, Deployments, StatefulSets, DaemonSets, Services

## Implementation Entry Point
Start with Task 1 (bootstrap), then Task 2 (CRD types), following wave order in the dependency graph.

## Next Steps
Begin implementation from tasks.md, starting at wave 0 (Task 1.1, 1.2, 1.3).
