# Task Completion

When a coding task is considered done, verify all of the following:

## 1. Compile
```bash
go build ./...
```
Must produce no errors.

## 2. Tests
```bash
go test ./...
```
All tests must pass.
For unit-only work: `go test ./test/unit/...`
For controller/integration work: `go test ./test/integration/...`

## 3. Formatting
```bash
gofmt -l ./...
```
Must produce no output (all files formatted).

## 4. Vet
```bash
go vet ./...
```
Must produce no errors.

## 5. Code Generation (if API types or RBAC markers changed)
```bash
make generate
make manifests
```
Commit generated files alongside source changes.

## 6. Diff Review
```bash
git diff HEAD
```
Confirm no unintended files changed. No secrets, credentials, or sensitive values included.

## Definition of Done (per SKILL.md)
A feature requires: implementation + unit tests + integration tests + error handling + structured logging + Kubernetes manifests (if new resources) + appropriate RBAC markers + validation where applicable.

## Spec-specific
If working under `.kiro/specs/incident-investigator-foundation/`:
- Update `tasks.md` task status as tasks complete
- Update Serena task memory (`tasks/<branch>`) with current status and next steps
- Spec is source of truth for implementation plan; Serena memory tracks continuity
