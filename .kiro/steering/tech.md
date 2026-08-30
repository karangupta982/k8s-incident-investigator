# Kubernetes Incident Investigator — Technical Context

## Language

Go.

The project should use idiomatic Go and standard project conventions.

Prefer:

- small packages
- explicit interfaces where useful
- dependency injection for external systems
- context propagation
- structured errors
- table-driven tests
- clear separation between Kubernetes integration and business logic

Avoid unnecessary abstractions.

---

## Kubernetes

The project targets Kubernetes.

Primary libraries:

- controller-runtime
- Kubernetes client-go APIs where required
- Kubernetes API machinery

The controller should use controller-runtime patterns rather than implementing a custom Kubernetes watch system.

---

## Operator Framework

Use Kubebuilder for project scaffolding and CRD/controller generation.

Kubebuilder is a development/scaffolding tool.

The runtime controller behavior comes from controller-runtime and Kubernetes APIs.

---

## CRD

Primary Custom Resource:

```text
IncidentReport
````

The CRD represents the persistent state of an incident investigation.

Use the Kubernetes status subresource for dynamic investigation state.

---

## Local Kubernetes Environment

Use Kind for local development and realistic Kubernetes integration testing.

The project should be demonstrable on a local Kind cluster without requiring a cloud provider.

---

## Testing

Use:

* Go testing package
* controller-runtime envtest
* Ginkgo/Gomega where appropriate
* Kind for end-to-end scenarios

Tests should cover:

* incident detection
* incident correlation
* lifecycle transitions
* evidence collection
* diagnosis rules
* unknown failures
* report updates
* controller recovery
* duplicate event handling

---

## Container

Build the controller as a container image.

Use a multi-stage Docker build.

The final image should contain only what is required to run the controller.

---

## CI

GitHub Actions should eventually run:

* formatting checks
* static analysis
* unit tests
* integration tests where practical
* build
* container image build

CI should fail on test or build errors.

---

## Observability

The Investigator itself should expose operational metrics.

Initial metrics should include concepts such as:

* incidents detected
* investigations completed
* unknown diagnoses
* reconciliation errors
* evidence collection failures
* investigation duration
* diagnosis rule matches

Use Prometheus-compatible metrics for the Investigator itself.

This does not mean Prometheus must be a dependency for investigating workloads in the MVP.

---

## Security

The controller should use least-privilege RBAC.

Prefer read-only access.

Do not grant workload mutation permissions unless a future feature explicitly requires them.

Do not expose Secret values in logs, reports, metrics, or errors.

---

## Configuration

Configuration should be explicit and bounded.

Potential configuration areas:

* watched namespaces
* incident thresholds
* stability period
* maximum log size
* maximum log lines
* investigation timeout
* evidence collection limits

Do not expose every internal implementation detail as configuration.

Start with sensible defaults.

---

## External Dependencies

MVP should not require:

* AWS
* Prometheus
* Grafana
* Slack
* PagerDuty
* Redis
* PostgreSQL
* S3
* LLM APIs

The core investigator should operate using Kubernetes APIs and container logs.

---

## Engineering Constraints

### Read-first

The Investigator primarily observes the cluster.

### Bounded

Every expensive operation must have a limit.

Examples:

* log bytes
* log lines
* investigation duration
* API calls
* retry attempts

### Idempotent

Reconciliation may happen multiple times.

Running reconciliation repeatedly must not create duplicate incidents or corrupt reports.

### Eventually consistent

The system must tolerate information arriving at different times.

### Restart-safe

The controller may restart at any point.

Investigation state must be recoverable from Kubernetes resources.

### Explainable

Diagnosis must be traceable to collected evidence.

### No fabricated certainty

If evidence is insufficient, report uncertainty.