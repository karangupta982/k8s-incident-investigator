# k8s-incident-investigator — Core

## Purpose
Kubernetes-native controller that detects workload failures, creates `IncidentReport` CRDs, collects structured evidence, correlates it, and produces explainable diagnoses. Read-only/non-mutating observer. MVP targets Kind + controller-runtime.

## Repo Root
`/Users/karan.gupta/Desktop/personal-github/kubernetes_incident_investigator/k8s-incident-investigator`

## Key Directories
- `api/v1alpha1/` — `IncidentReport` CRD type definitions (currently empty; Kubebuilder-generated)
- `cmd/` — main.go entrypoint (currently empty)
- `internal/controller/` — Kubernetes reconciler; must remain thin
- `internal/investigation/` — incident lifecycle, correlation, orchestration
- `internal/evidence/` — per-source collectors (pod, events, logs, node, workload, storage, networking)
- `internal/diagnosis/` — engine + modular rules (no direct k8s access)
- `internal/reporting/` — translates investigation results → IncidentReport status
- `internal/config/` — thresholds, namespace scope, timeouts, log limits
- `config/crd|rbac|manager|samples/` — Kubebuilder-generated manifests
- `deploy/kind/` — local Kind demo scripts/resources
- `test/unit/` — isolated business logic tests (no cluster needed)
- `test/integration/` — envtest-based controller tests
- `docs/complete_workflow.md` — end-to-end OOM example walkthrough (important reference)
- `.kiro/specs/incident-investigator-foundation/` — active spec (requirements.md full; design.md + tasks.md TBD)
- `.kiro/skills/kubernetes-incident-investigator/` — architecture patterns, investigation patterns, dev skill guide

## Current State
Project scaffolded; all source directories empty. Only one git commit (initial). Spec `incident-investigator-foundation` has requirements written, design and tasks not yet written.

## Domain References
- Investigation pipeline and patterns: `mem:architecture`
- Tech stack / library choices: `mem:tech_stack`
- Dev commands: `mem:suggested_commands`
- Coding conventions: `mem:conventions`
- Task completion checklist: `mem:task_completion`
