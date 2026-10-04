<div align="center">

# ⚡ Kubernetes Incident Investigator

**Automated workload failure detection, evidence collection, and diagnosis — entirely Kubernetes-native.**

[![CI](https://github.com/karangupta982/k8s-incident-investigator/actions/workflows/ci.yml/badge.svg)](https://github.com/karangupta982/k8s-incident-investigator/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8.svg)](go.mod)

</div>

---

When a Kubernetes workload fails, engineers typically run a dozen commands to figure out what happened — `kubectl describe pod`, `kubectl logs`, `kubectl get events`, `kubectl describe node`, and so on. Connecting the pieces together is the hard part.

The Incident Investigator automates that entire process. When a failure is detected, it creates a persistent `IncidentReport` custom resource, collects relevant evidence from multiple Kubernetes sources, correlates the evidence, runs deterministic diagnosis rules, and presents a structured investigation report you can inspect with standard kubectl commands.

```bash
kubectl get incidentreports -A
kubectl describe incidentreport payment-api-deployment-active -n production
```

No dashboards. No agents. No external services. It installs into your cluster as a single controller and runs entirely within Kubernetes.

---

## Install

> **Helm chart coming in v0.1.0.** Until then, use the Kustomize manifests below.

```bash
# Install CRDs
kubectl apply -f https://raw.githubusercontent.com/karangupta982/k8s-incident-investigator/main/config/crd/bases/investigation.k8s.io_incidentreports.yaml

# Install controller
kubectl apply -k https://github.com/karangupta982/k8s-incident-investigator/config/default
```

Or clone and install locally:

```bash
git clone https://github.com/karangupta982/k8s-incident-investigator
cd k8s-incident-investigator
kubectl apply -k config/default
```

**Uninstall:**

```bash
kubectl delete -k config/default
kubectl delete -f config/crd/bases/investigation.k8s.io_incidentreports.yaml
```

---

## What it detects

| Trigger | Detection method | Example |
|---------|-----------------|---------|
| OOMKilled | Pod container status (immediate) | Container hit memory limit |
| CrashLoopBackOff | Pod container status (immediate) | App crashing on startup |
| ImagePullBackOff | Pod container status (immediate) | Invalid image tag or missing pull secret |
| CreateContainerConfigError | Pod container status (immediate) | Missing ConfigMap or Secret |
| Eviction | Pod status (immediate) | Node evicted the Pod |
| Readiness probe failures | Kubernetes Event.count ≥ threshold | App health check failing |
| Liveness probe failures | Kubernetes Event.count ≥ threshold | App unresponsive |
| Volume mount failures | Kubernetes Event.count ≥ threshold | PVC not bound, CSI error |
| Scheduling failures | Kubernetes Event.count ≥ threshold | Insufficient CPU/memory, no matching node |

---

## What you get

After a failure is detected and investigated, `kubectl describe incidentreport` shows:

```
Phase:   Diagnosed

Summary:
  Workload: Deployment/production/payment-api
  Phase: Diagnosed (Confidence: High)
  Cause: Container exceeded its configured memory limit
  Trigger: OOMKilled
  Affected Pods: 1
  Started: 2026-01-01T14:02:00Z

Diagnosis:
  Primary:
    Rule ID:    OOMMemoryLimit
    Confidence: High
    Cause:      Container exceeded its configured memory limit
    Explanation: Container "app" was terminated with OOMKilled (exit code 137).
                 The container has a memory limit of 512Mi configured.
                 The node did not report MemoryPressure, indicating the OOM kill
                 was caused by the container exceeding its own limit.
    Supporting Evidence:
      - termination reason: OOMKilled
      - exit code: 137
      - memory limit: 512Mi
      - node did not report MemoryPressure
    Recommendation:
      Container "app" was OOMKilled with a memory limit of 512Mi.
      Investigate application memory consumption and consider increasing
      the memory limit if the workload legitimately requires more memory.

Timeline:
  14:02:00  IncidentDetected  Incident detected and IncidentReport created.
  14:02:08  OOMKilled         Container "app" terminated (exit 137, restart 1).
  14:02:09  Unhealthy         Readiness probe failed after restart.

Affected Pods:
  payment-api-abc123 (default)

Evidence:
  Pod:
    Container: app
      State:             waiting (CrashLoopBackOff)
      Last Termination:  OOMKilled (exit 137)
      Restart Count:     3
      Memory Limit:      512Mi
  Node:
    Name:            ip-10-0-1-23
    Memory Pressure: False
    Allocatable Mem: 7640Mi
```

---

## Investigation pipeline

The controller runs each reconcile cycle through a sequential pipeline:

```
Failure signal (Pod state or Event count)
    ↓
Trigger evaluation
    ↓
Workload ownership resolution (Pod → RS → Deployment / StatefulSet / DaemonSet / Job / CronJob)
    ↓
Incident correlation (deterministic naming, one active incident per workload)
    ↓
Evidence collection (Pod, Events, Logs, Node, Workload, Dependencies)
    ↓
Evidence correlation (causal signals, log patterns, chain patterns)
    ↓
Diagnosis engine (10 rules, High/Medium/Low confidence)
    ↓
Reporting (Summary, Timeline, enriched Recommendations)
    ↓
Recovery detection (workload-type-aware stability period)
    ↓
IncidentReport → Resolved → Historical record preserved
```

See [docs/architecture.html](docs/architecture.html) for an interactive architecture diagram.

---

## Diagnosis rules

The engine evaluates 10 deterministic rules. No LLM required — every finding is traceable to specific evidence.

| Rule | Confidence | Fires when |
|------|------------|------------|
| `OOMMemoryLimit` | High | OOMKilled + exit 137 + memory limit + no node pressure |
| `NodeMemoryPressure` | Medium | Node MemoryPressure=True + OOM/eviction signal |
| `CrashLoopOOMExit` | High | CrashLoopBackOff + exit 137 |
| `CrashLoopAppError` | Medium | CrashLoopBackOff + non-OOM exit code |
| `ImagePullFailure` | High | ImagePullBackOff or ErrImagePull waiting state |
| `MissingConfigReference` | High | CreateContainerConfigError waiting state |
| `PVCNotBound` | High | MountFailure + PVC phase ≠ Bound |
| `PVCMountError` | Medium | MountFailure + PVC Bound + FailedMount events |
| `SchedulingFailure` | High | SchedulingFailure + FailedScheduling events |
| `ProbeFailure` | Medium | Readiness/Liveness failure + Unhealthy events |

When no rule matches, the phase becomes `Unknown` with an explanation — the system never fabricates a root cause.

---

## Configuration

All flags are optional with sensible defaults.

| Flag | Default | Description |
|------|---------|-------------|
| `--stability-period` | `5m` | How long a workload must remain healthy before resolving an incident |
| `--requeue-interval` | `30s` | How often active incidents are re-evaluated |
| `--readiness-threshold` | `3` | Event.count threshold for readiness probe failures |
| `--liveness-threshold` | `3` | Event.count threshold for liveness probe failures |
| `--mount-threshold` | `3` | Event.count threshold for mount failures |
| `--scheduling-threshold` | `5` | Event.count threshold for scheduling failures |
| `--max-log-bytes` | `32768` | Maximum bytes per container log excerpt |
| `--max-log-lines` | `200` | Maximum lines per container log excerpt |
| `--max-events` | `25` | Maximum Kubernetes Events stored per incident |
| `--timeline-events` | `50` | Maximum events in the incident timeline |
| `--watch-namespaces` | `""` | Comma-separated namespaces to watch (empty = all) |
| `--leader-elect` | `false` | Enable leader election for HA deployments |

---

## Prerequisites

- Kubernetes 1.28+
- `kubectl`

The controller requires read-only access to: Pods, Events, Nodes, ReplicaSets, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, PersistentVolumeClaims, PersistentVolumes, StorageClasses, and `pods/log`. It requires read-write access to `IncidentReport` custom resources only. It never modifies any workload resource.

---

## Local development with Kind

```bash
# Create a Kind cluster and deploy the controller
./deploy/kind/setup.sh

# Run a demo scenario (OOMKilled)
./deploy/kind/demo.sh oom-killed

# Run all 6 demo scenarios
./deploy/kind/demo.sh --all
```

Available scenarios: `oom-killed`, `crash-loop`, `image-pull-backoff`, `mount-failure`, `scheduling-failure`, `readiness-probe-failure`.

See [deploy/kind/scenarios/](deploy/kind/scenarios/) for details on each scenario and [docs/demo/scenarios.md](docs/demo/scenarios.md) for annotated expected output.

---

## Development

```bash
# Run unit tests
make test-unit

# Run integration tests (requires envtest)
make test-integration

# Regenerate CRD and RBAC manifests after API changes
make generate manifests

# Build binary
make build

# Build Docker image
make docker-build IMG=my-registry/k8s-incident-investigator:dev
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for full development guide.

---

## Observability

The controller exposes 10 Prometheus metrics at `/metrics`:

- `investigator_incidents_detected_total`
- `investigator_investigations_completed_total`
- `investigator_diagnosed_total`
- `investigator_unknown_diagnoses_total`
- `investigator_active_incidents`
- `investigator_reconciliation_errors_total`
- `investigator_evidence_collection_failures_total`
- `investigator_investigation_duration_seconds`
- `investigator_evidence_collection_duration_seconds`
- `investigator_diagnosis_rule_matches_total`

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
