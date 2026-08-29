# Kubernetes Incident Investigator — Project Structure

## General Principle

The repository is organized by responsibility.

Kubernetes controller code, investigation orchestration, evidence collection, diagnosis logic, reporting, and tests should remain separated.

Do not place all functionality inside the controller.

---

## Root Structure

```text
.
├── .kiro/
├── api/
├── cmd/
├── internal/
├── config/
├── test/
├── docs/
├── deploy/
├── Dockerfile
├── Makefile
├── README.md
├── .gitignore
└── .kiroignore
````

---

## `.kiro/`

Contains Kiro-specific project context and workflows.

```text
.kiro/
├── steering/
├── skills/
├── specs/
├── hooks/
└── settings/
```

Steering documents describe persistent project context.

Skills describe development behavior and domain knowledge.

Specs contain structured implementation workflows.

Hooks contain development automation.

---

## `api/`

Contains Kubernetes API type definitions.

```text
api/
└── v1alpha1/
```

The `IncidentReport` API type belongs here.

Do not put investigation business logic in API type files.

---

## `cmd/`

Contains application entry points.

```text
cmd/
└── main.go
```

The main function should initialize and start the controller manager.

Do not place investigation logic here.

---

## `internal/controller/`

Contains Kubernetes controller/reconciler code.

Responsibilities:

* watch resources
* receive reconciliation requests
* coordinate investigation lifecycle
* call investigation services
* update Kubernetes resources
* handle reconciliation errors

The controller should remain thin.

Business logic should be delegated to internal packages.

---

## `internal/investigation/`

Contains incident lifecycle and investigation orchestration.

Responsibilities:

* determine whether an active incident exists
* create or reuse incidents
* manage incident lifecycle
* correlate events into incidents
* manage investigation state
* determine when investigation should continue or complete

This package represents the central investigation domain.

---

## `internal/evidence/`

Contains evidence collectors.

Example structure:

```text
internal/evidence/
├── collector.go
├── pod.go
├── events.go
├── node.go
├── workload.go
├── storage.go
├── networking.go
└── logs.go
```

Each collector should have a focused responsibility.

Evidence collectors should return structured evidence rather than diagnosis strings.

---

## `internal/diagnosis/`

Contains diagnosis logic.

```text
internal/diagnosis/
├── engine.go
├── rule.go
└── rules/
```

Each rule should be independently testable.

Rules interpret evidence.

Rules should not directly access the Kubernetes API.

The diagnosis layer should operate on structured evidence.

---

## `internal/reporting/`

Responsible for transforming investigation results into the representation required by the IncidentReport status.

It should not contain Kubernetes watch logic.

---

## `internal/config/`

Contains application configuration and defaults.

Examples:

* thresholds
* namespace scope
* investigation timeout
* log limits
* stability period

---

## `config/`

Contains Kubernetes manifests generated or maintained for deployment.

Typical directories:

```text
config/
├── crd/
├── rbac/
├── manager/
└── samples/
```

Kubebuilder-generated files should remain here unless there is a strong reason to restructure them.

---

## `test/unit/`

Tests isolated business logic.

Examples:

* correlation
* incident lifecycle
* diagnosis rules
* evidence transformation

These tests should not require a Kubernetes cluster unless necessary.

---

## `test/integration/`

Tests interactions with Kubernetes APIs and controller behavior.

Use envtest where appropriate.

---

## `deploy/kind/`

Contains resources or scripts required for local Kind-based demonstrations.

Do not put application source code here.

---

## `docs/`

Contains human-facing documentation.

```text
docs/
├── architecture/
├── design-decisions/
└── demo/
```

### architecture/

Architecture diagrams and explanations.

### design-decisions/

Important Architecture Decision Records or design decisions.

### demo/

Demo scenarios and screenshots/video documentation.

---

## Dependency Direction

Prefer this dependency direction:

```text
Controller
    |
    v
Investigation
    |
    +--> Evidence
    |
    +--> Diagnosis
    |
    +--> Reporting
```

Lower-level components should not depend on the controller.

In particular:

```text
Diagnosis
    X--> Kubernetes API

Diagnosis
    X--> Controller

Evidence
    X--> Diagnosis
```

Instead:

```text
Kubernetes API
    |
    v
Evidence
    |
    v
Diagnosis
```

This keeps diagnosis logic easy to unit test.

---

## Naming

Use idiomatic Go naming.

Prefer clear names such as:

```text
Incident
IncidentReport
Evidence
EvidenceCollector
Diagnosis
DiagnosisRule
CorrelationResult
```

Avoid vague names such as:

```text
Manager
Helper
Utils
Processor
Handler
```

unless the responsibility is genuinely clear.

---

## Controller Rule

`Reconcile()` should not become a large function.

If a piece of logic can be independently understood and tested, move it into the appropriate package.

---

## Generated Files

Do not manually edit generated Kubernetes code unless necessary.

Use Kubebuilder/controller-gen generation commands when API types or RBAC markers change.