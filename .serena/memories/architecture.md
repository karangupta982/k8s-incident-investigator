# Architecture

## Investigation Pipeline (ordered)
```
Trigger → Incident Decision → Incident Correlation → Evidence Collection
       → Evidence Correlation → Diagnosis → Recommendations → IncidentReport
```

## Primary Custom Resource
`IncidentReport` — persistent case file; created at incident detection, updated throughout, retained after resolution.

## Lifecycle Phases
`Investigating` → `Diagnosed` | `Unknown` → `Resolved`
- Created at trigger detection (before investigation completes)
- `Diagnosed`/`Unknown` transition requires diagnosis layer (out of scope for foundation spec)
- `Resolved` only after workload healthy for configurable stability period

## Incident Identity
**Workload-centric**, not Pod-centric. Pods are ephemeral evidence.
- One active `IncidentReport` per workload at a time
- Multiple affected Pods tracked as part of evidence
- On recovery + new failure after resolution → new `IncidentReport` (old preserved)

## Dependency Direction (strictly enforced)
```
Controller → Investigation → Evidence → (Diagnosis consumes evidence)
                          → Diagnosis
                          → Reporting
```
- Diagnosis MUST NOT access Kubernetes API directly
- Evidence MUST NOT call Diagnosis
- Reporting MUST NOT contain watch logic

## Trigger Policy
Immediate triggers (no threshold needed): `OOMKilled`, `CrashLoopBackOff`, `ImagePullBackOff`, `CreateContainerConfigError`, Pod eviction.
Threshold-based: repeated readiness/liveness probe failures, FailedMount, scheduling failures. Single probe failure → no incident.

## Evidence Layers (collected adaptively based on trigger type)
1. Primary Pod state (phase, container state, restart count, exit code, termination reason, resources, probes, ownership)
2. Kubernetes Events for affected resources
3. Workload ownership chain (Pod → RS → Deployment or StatefulSet/DaemonSet/Job)
4. Node readiness + pressure conditions
5. Dependencies (PVC/PV/StorageClass, Service, EndpointSlice, ConfigMap/Secret metadata — only when relevant)
6. Bounded container logs (current + previous; byte/line limits)
7. Metrics (future; not MVP)

## Log Handling
- Collect `--previous` logs on crash investigations
- Hard byte and line limits enforced
- Store only excerpt in IncidentReport (never raw unlimited logs in etcd)
- Record "logs unavailable because X" explicitly when fetch fails

## Idempotency Invariants
- Same reconciliation request multiple times → same logical incident state
- No duplicate IncidentReports for active workload incident
- Controller restart → rediscover active reports, continue managing

## Bounded Investigation
Every expensive op must have a limit: log bytes, log lines, investigation duration, API calls, retry attempts, requeue frequency.

## Non-Remediation
Investigator MUST NOT modify affected workloads. RBAC: read-only on application resources. No Secret value exposure.

## Known Investigation Patterns
- OOMKilled: collect exit code 137, memory limit, node memory pressure; diagnose "exceeded memory limit", NOT "memory leak"
- CrashLoopBackOff: CrashLoopBackOff is symptom not cause; use additional evidence for root cause
- ImagePullBackOff: check image name/tag, imagePullSecrets existence, registry events
- FailedMount: check PVC status, PV, StorageClass, CSI events
- Probe failures: collect probe config, event history, container state, restart behavior
- Unknown: still report timeline + evidence + what was ruled out
