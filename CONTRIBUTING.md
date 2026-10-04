# Contributing to Kubernetes Incident Investigator

Thank you for your interest in contributing. This document describes how to get started.

## Development Prerequisites

- Go 1.23+
- Docker (for building container images)
- [Kind](https://kind.sigs.k8s.io/) (for local Kubernetes testing)
- `kubectl`

## Getting Started

```bash
git clone https://github.com/karangupta982/k8s-incident-investigator
cd k8s-incident-investigator
go mod download
```

## Running Tests

```bash
# Unit tests (fast, no cluster needed)
make test-unit

# Integration tests (requires envtest binaries)
make test-integration

# All tests
make test
```

## Building

```bash
# Build manager binary
make build

# Build Docker image
make docker-build IMG=my-registry/k8s-incident-investigator:dev
```

## Code Generation

If you change API types in `api/v1alpha1/`, regenerate the generated files:

```bash
make generate   # regenerates zz_generated.deepcopy.go
make manifests  # regenerates CRD and RBAC YAML
```

## Project Structure

```
api/v1alpha1/       CRD type definitions (IncidentReport and sub-types)
cmd/                Controller manager entrypoint
internal/
  config/           Configuration and defaults
  controller/       PodReconciler — thin orchestration layer
  investigation/    Foundation domain logic (trigger, ownership, correlation, recovery)
  evidence/         Evidence collectors (Pod, Events, Logs, Node, Workload, Dependencies)
  correlation/      Evidence correlation (causal signals, log patterns)
  diagnosis/        Diagnosis rules engine (10 deterministic rules)
  reporting/        Reporting layer (Summary, Timeline, Recommendations)
  metrics/          Prometheus metrics
config/             Kubernetes manifests (CRD, RBAC, manager)
deploy/kind/        Kind cluster setup and demo scenarios
test/unit/          Unit tests and property-based tests
test/integration/   Envtest integration tests
```

## Submitting Changes

1. Fork the repository
2. Create a branch: `git checkout -b feat/your-feature`
3. Make your changes and add tests
4. Ensure all tests pass: `make test`
5. Ensure formatting is clean: `gofmt -l .` (should produce no output)
6. Open a pull request against `main`

## Adding a Diagnosis Rule

Diagnosis rules live in `internal/diagnosis/rules/`. Each rule is a small, stateless Go struct:

```go
type MyNewRule struct{}

func (r *MyNewRule) ID() string    { return "MyRuleID" }
func (r *MyNewRule) Priority() int { return 30 }

func (r *MyNewRule) Evaluate(snapshot *v1alpha1.EvidenceSnapshot) *v1alpha1.DiagnosisFinding {
    // inspect snapshot, return nil if rule does not match
    // return a DiagnosisFinding if it does
}
```

Register the rule in `internal/diagnosis/engine.go` → `NewDiagnosisEngine()`.

Rules must:
- Be stateless (safe for concurrent calls)
- Not call the Kubernetes API
- Return nil when the rule does not match (not an error)

## Code of Conduct

Be respectful. Disagreements about technical approaches are fine; personal attacks are not.
