# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | ✅ Active  |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Use [GitHub private vulnerability reporting](https://github.com/karangupta982/k8s-incident-investigator/security/advisories/new) to report issues confidentially. I aim to acknowledge security reports within 72 hours.

Please include a description of the vulnerability, reproduction steps, potential impact, and any suggested fixes.

---

## Security Design

### RBAC

The controller uses two separate RBAC roles:

**ClusterRole** (cluster-wide):
- **Read-only** access to: Pods, Events, Nodes, ReplicaSets, Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, PersistentVolumeClaims, PersistentVolumes, StorageClasses
- **Read** access to `pods/log` — `get` only, required to stream container logs
- **Read + Write** on `IncidentReport` custom resources — the controller creates, updates, patches, and deletes these
- **No** access to Secrets — Secret values are never read, stored, or logged. Only Secret names referenced in Pod `spec.imagePullSecrets` are recorded (already visible on the Pod object)
- **No** write permissions on any workload resource (Pods, Deployments, Services, etc.)

**Role** (namespace-scoped, for leader election):
- **Read + Write** on `coordination.k8s.io/leases` — this controller uses controller-runtime's default Lease-based leader election
- **Create + Patch** on Kubernetes Events — required for leader election status reporting
- **No** ConfigMap permissions — this controller uses controller-runtime's default Lease-based leader election

> Note: When `watchNamespaces` is configured, the controller only watches those namespaces. However, the current ClusterRole still grants cluster-wide read access. A future release will support per-namespace Roles for tighter scoping.

### Container

- Runs as UID 65532 (nonroot) — non-root by default
- Uses `gcr.io/distroless/static:nonroot` as the runtime image — no shell, no package manager
- Helm chart enables `readOnlyRootFilesystem: true` and drops all Linux capabilities

### Network

- Communicates only with the Kubernetes API server
- The controller makes no outbound calls to external services or third-party APIs
- No webhook server

---

## Known Security Considerations

### Container logs are persisted in etcd

Collected container log excerpts are stored in the `IncidentReport` status field, which is persisted in etcd. If container logs contain sensitive data (credentials, tokens, PII), you should either:

- Set `maxLogLines: 0` in `values.yaml` (or `--max-log-lines 0`) to disable log collection entirely — no `pods/log` API call will be made
- Restrict RBAC access to `IncidentReport` objects to authorised operators only

### IncidentReport objects contain investigation evidence

`IncidentReport` objects may contain:
- Container states, exit codes, resource configuration
- Kubernetes Events and their messages
- Node conditions and capacity
- PVC/PV metadata and storage class names
- Container log excerpts (if not disabled)

None of these contain Secret values. However, operators should review `IncidentReport` access controls for their environment.

### Cluster-wide read access

The ClusterRole grants cluster-wide read access to several resource types. Operators who require stricter isolation should use `watchNamespaces` to limit the namespaces the controller observes, and consider scoping RBAC manually once per-namespace Role support is added.
