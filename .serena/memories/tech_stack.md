# Tech Stack

## Language
Go (idiomatic; standard project layout conventions)

## Kubernetes Libraries
- `controller-runtime` — primary controller framework (watches, reconciler, manager, envtest)
- `k8s.io/client-go` — Kubernetes API client where controller-runtime doesn't cover
- `k8s.io/apimachinery` — API types, meta, status conditions

## Scaffolding / Code Generation
- Kubebuilder — CRD and RBAC generation via `controller-gen` markers
- `controller-gen` — generates CRD manifests and deepcopy functions from API type annotations

## Testing
- Go `testing` package (table-driven tests)
- `controller-runtime/pkg/envtest` — integration tests with real API server (no cluster)
- Ginkgo + Gomega — for BDD-style integration/controller tests where appropriate
- Kind — end-to-end local cluster tests

## Local Kubernetes
Kind (Kubernetes in Docker) — required for local demos and e2e scenarios

## Observability
Prometheus-compatible metrics exposed by the controller itself (not a dependency for investigating workloads). Initial metrics: incidents detected, investigations completed, unknown diagnoses, reconciliation errors, evidence collection failures, investigation duration, diagnosis rule matches.

## Container
Multi-stage Docker build. Final image: minimal (only runtime binary).

## CI (planned)
GitHub Actions: format check, static analysis, unit tests, integration tests, build, container image build.

## MVP External Dependency Exclusions
No AWS, Prometheus (as mandatory dep), Grafana, Slack, PagerDuty, Redis, PostgreSQL, S3, LLM APIs.
