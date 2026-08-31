# Kubernetes Incident Investigator

A Kubernetes-native controller that automatically detects workload failures, creates persistent `IncidentReport` resources, and provides engineers with structured incident records — without manual `kubectl` investigation.

When a workload fails, instead of running a dozen commands to figure out what happened, you inspect one resource:

```
kubectl get incidentreports -A
kubectl describe incidentreport payment-api-deployment-active
```

---

## How it works

The controller watches Pods and Kubernetes Events for meaningful failure signals. When a threshold is crossed it creates an `IncidentReport` CR, tracks affected Pods, evaluates workload health, and marks the incident resolved after the configured stability period elapses.

Detected failure signals:

| Signal | Detection method |
|--------|-----------------|
| OOMKilled | Pod container status (immediate) |
| CrashLoopBackOff | Pod container status (immediate) |
| ImagePullBackOff / ErrImagePull | Pod container status (immediate) |
| CreateContainerConfigError | Pod container status (immediate) |
| Pod eviction | Pod status (immediate) |
| Readiness probe failures | Kubernetes Event.count ≥ threshold |
| Liveness probe failures | Kubernetes Event.count ≥ threshold |
| Mount failures | Kubernetes Event.count ≥ threshold |
| Scheduling failures | Kubernetes Event.count ≥ threshold |

---

## Prerequisites

- Go 1.23+
- kubectl
- Docker
- [Kind](https://kind.sigs.k8s.io/) (for local demo)
- [controller-gen](https://book.kubebuilder.io/reference/controller-gen) (installed automatically by `make`)

---

## Quick start — local Kind cluster

```bash
# 1. Clone and enter the repo
git clone https://github.com/k8s-incident-investigator/k8s-incident-investigator
cd k8s-incident-investigator

# 2. Create the Kind cluster, build the image, and deploy the controller
./deploy/kind/setup.sh

# 3. Deploy a workload that will OOMKill
kubectl apply -f deploy/kind/test-workload/oom-crasher.yaml

# 4. Watch for the IncidentReport to appear (takes ~30s)
kubectl get incidentreports -n demo -w

# 5. Inspect the incident
kubectl describe incidentreport -n demo $(kubectl get ir -n demo -o jsonpath='{.items[0].metadata.name}')
```

To run the full e2e validation (takes ~7 minutes due to the stability period):

```bash
./deploy/kind/validate.sh
```

---

## Inspecting incidents

```bash
# List all incidents across all namespaces
kubectl get incidentreports -A

# List incidents for a specific namespace
kubectl get incidentreports -n production

# Describe a specific incident
kubectl describe incidentreport payment-api-deployment-active -n production

# List only active incidents (not yet resolved)
kubectl get incidentreports -A -l investigator.k8s.io/active=true

# List resolved historical incidents
kubectl get incidentreports -A | grep -v active
```

Example output:

```
NAME                                    WORKLOAD       KIND         PHASE          TRIGGER        STARTED
payment-api-deployment-active           payment-api    Deployment   Investigating  OOMKilled      5m ago
worker-deployment-20260829-a3f2b        worker         Deployment   Resolved       MountFailure   2d ago
```

---

## Configuration reference

All flags have sensible defaults and are optional.

| Flag | Default | Description |
|------|---------|-------------|
| `--stability-period` | `5m` | How long a workload must remain healthy before resolving an incident |
| `--correlation-window` | `10m` | Reserved for future cross-incident correlation |
| `--readiness-threshold` | `3` | Readiness probe failure Event.count to trigger incident |
| `--liveness-threshold` | `3` | Liveness probe failure Event.count to trigger incident |
| `--mount-threshold` | `3` | Mount failure Event.count to trigger incident |
| `--scheduling-threshold` | `5` | Scheduling failure Event.count to trigger incident |
| `--requeue-interval` | `30s` | How often active incidents are re-evaluated |
| `--watch-namespaces` | `""` | Comma-separated namespaces to watch (empty = all) |
| `--leader-elect` | `false` | Enable leader election for HA deployments |
| `--metrics-bind-address` | `:8080` | Metrics endpoint address |
| `--health-probe-bind-address` | `:8081` | Health probe endpoint address |

---

## Development

### Running tests

```bash
# Unit tests (fast, no cluster needed)
make test-unit

# Integration tests (requires envtest binaries)
make test-integration

# All tests
make test
```

### Code generation

Run after changing API types in `api/v1alpha1/`:

```bash
# Regenerate DeepCopy methods
make generate

# Regenerate CRD manifests and RBAC from markers
make manifests
```

### Building

```bash
# Build the manager binary
make build

# Build the Docker image
make docker-build IMG=my-registry/incident-investigator:latest

# Push the Docker image
make docker-push IMG=my-registry/incident-investigator:latest
```

### Project structure

```
api/v1alpha1/           CRD type definitions (IncidentReport)
cmd/                    Controller manager entrypoint
internal/
  config/               Configuration and defaults
  controller/           PodReconciler — thin orchestration layer
  investigation/        Domain logic: trigger, ownership, correlation, recovery
config/
  crd/                  Generated CRD manifests
  rbac/                 Generated and static RBAC manifests
  manager/              Deployment manifests
  samples/              Example IncidentReport
deploy/kind/            Local Kind cluster setup and e2e validation
test/
  unit/                 Unit tests (table-driven + property-based)
  integration/          Envtest integration tests
```

---

## Design decisions

The investigator is read-only by design — it observes and reports but never modifies the workload being investigated. RBAC is scoped to the minimum required for investigation.

Key design choices:

- **Deterministic naming**: active incidents use `<workload>-<kind>-active` names, preventing duplicates even under concurrent reconciliation
- **Two-name model**: resolved incidents are copied to a historical name before the active slot is freed, preserving history while allowing new incidents for the same workload
- **Workload-aware recovery**: uses Deployment/StatefulSet/DaemonSet replica counts rather than raw Pod readiness, handling rolling updates and scale operations correctly
- **Evidence-first**: the foundation gathers structured incident state; diagnosis and recommendations are added in subsequent specifications

For architecture details see the spec documents in `.kiro/specs/incident-investigator-foundation/`.
