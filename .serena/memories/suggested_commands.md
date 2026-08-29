# Suggested Commands

## Go Module
```bash
go mod tidy
go mod download
```

## Build
```bash
go build ./...
```
(Makefile currently empty; actual build targets TBD when scaffolding is complete)

## Tests
```bash
# Unit tests (no cluster required)
go test ./test/unit/... -v

# Integration tests (requires envtest binaries)
go test ./test/integration/... -v

# All tests
go test ./...
```

## Code Generation (Kubebuilder)
```bash
# After changing API types or RBAC markers:
make generate    # runs controller-gen deepcopy
make manifests   # regenerates CRD + RBAC manifests
```
(These Makefile targets are standard Kubebuilder scaffold; will be populated when project is initialized)

## Linting / Formatting
```bash
gofmt -w ./...
go vet ./...
```
Static analysis with `golangci-lint` is planned for CI.

## Kind Cluster
```bash
kind create cluster --name incident-investigator
kind delete cluster --name incident-investigator
```

## Deploy to Kind
```bash
# Build and load image into Kind:
docker build -t incident-investigator:dev .
kind load docker-image incident-investigator:dev --name incident-investigator

# Apply manifests:
kubectl apply -f config/crd/
kubectl apply -f config/rbac/
kubectl apply -f config/manager/
```

## Inspect Incidents
```bash
kubectl get incidentreports
kubectl describe incidentreport <name>
```

## Git
```bash
git branch --show-current
git log --oneline -10
git diff HEAD
git status --short
```
