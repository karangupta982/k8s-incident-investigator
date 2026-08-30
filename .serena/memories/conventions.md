# Conventions

## Go Style
- Idiomatic Go; no unnecessary abstractions
- Small, focused packages organized by responsibility
- Explicit interfaces where useful (dependency injection for external systems)
- Context propagation on all operations touching Kubernetes or I/O
- Structured errors (wrap with context, not bare strings)
- Table-driven tests (standard Go testing pattern)

## Naming
Prefer clear domain names: `Incident`, `IncidentReport`, `Evidence`, `EvidenceCollector`, `Diagnosis`, `DiagnosisRule`, `CorrelationResult`.
Avoid: `Manager`, `Helper`, `Utils`, `Processor`, `Handler` unless responsibility is unambiguous.

## Controller Pattern
- `Reconcile()` must remain thin — orchestration only, no business logic
- All investigation, correlation, diagnosis logic lives in `internal/` packages
- Reconcile delegates: Incident Manager → Evidence → Correlation → Diagnosis → Report

## Evidence Collectors
- Return structured evidence types, not diagnosis strings
- Must tolerate unavailable data (best-effort; record "unavailable because X")
- Must NOT call diagnosis rules directly

## Diagnosis Rules
- Independent, independently testable
- Input: structured evidence only (no direct Kubernetes API calls)
- Output: structured finding with cause, confidence (High/Medium/Low), explanation, supporting evidence refs, recommendation
- Must not claim certainty beyond what evidence supports
- Unknown is a valid, acceptable output

## Security Invariants
- Never log, store, or expose Secret values — only metadata/existence
- RBAC: read-only on application resources; write only on `IncidentReport`
- All log collection bounded (bytes + lines)
- IncidentReport must never grow unbounded (no unlimited raw data in etcd)

## Idempotency
Every reconciliation path must be safe to run multiple times with the same inputs. No side effects that compound on repeated calls.

## Error Handling
Return errors that allow controller-runtime retry. Never swallow errors that affect correctness. Use `fmt.Errorf("context: %w", err)` wrapping.

## Logging
Structured logging (controller-runtime logger / zap). Include reconcile context (namespace, name, workload). Never include sensitive values.
