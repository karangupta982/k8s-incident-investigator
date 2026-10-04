# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-04

### Added

**Foundation**
- `IncidentReport` CRD with active/historical two-name model
- `PodReconciler` controller using controller-runtime
- Workload ownership resolution: Pod → RS → Deployment / StatefulSet / DaemonSet / Job / CronJob
- Deterministic incident correlation (one active incident per workload)
- Workload-type-aware recovery detection with configurable stability period
- Leader election support
- Kubernetes-native reporting via `kubectl get incidentreports`

**Evidence Collection**
- Pod evidence: container states, exit codes, resource limits, probe configuration
- Kubernetes Events: bounded (max 25), sorted by recency, message truncated to 256 chars
- Container logs: bounded (max 32KB / 200 lines), previous logs for OOM/crash incidents
- Node evidence: conditions (MemoryPressure, DiskPressure, PIDPressure), allocatable resources
- Workload evidence: replica counts, conditions, update strategy
- Dependency evidence: PVC/PV for MountFailure, ImagePullSecret names for ImagePullBackOff, scheduling constraints

**Evidence Correlation**
- Causal signal derivation: NodeMemoryPressureCoincident, ContainerHitConfiguredLimit, PVCIsUnbound, OOMKillCausedCrashLoop, MountFailureLinkedToPVC, SchedulingConstraintsPresent
- Log pattern matching: OOMString, ConnectionRefused, PanicOrFatal, PermissionDenied
- Chain patterns: OOMToCrashLoop, PVCToBoundToMount, ScheduleToFail

**Diagnosis Engine**
- 10 deterministic rules: OOMMemoryLimit, NodeMemoryPressure, CrashLoopOOMExit, CrashLoopAppError, ImagePullFailure, MissingConfigReference, PVCNotBound, PVCMountError, SchedulingFailure, ProbeFailure
- Primary / ContributingFactors / AlternativeHypotheses structure
- Phase transitions: Investigating → Diagnosed / Unknown → Resolved
- Evidence-backed recommendations per rule
- Honest Unknown outcome when no rule matches

**Reporting**
- `status.summary`: concise phase-specific summary
- `status.timeline`: chronological event list (max 50, from controller + container + Kubernetes Events)
- Evidence-specific recommendation enrichment
- `Cause` printer column in `kubectl get -o wide`

**Observability**
- 10 Prometheus metrics on `/metrics` endpoint
- `investigator_incidents_detected_total`, `investigations_completed_total`, `unknown_diagnoses_total`, `diagnosed_total`, `active_incidents`, `reconciliation_errors_total`, `evidence_collection_failures_total`, `investigation_duration_seconds`, `evidence_collection_duration_seconds`, `diagnosis_rule_matches_total`

**Distribution**
- Multi-architecture Docker image (`linux/amd64`, `linux/arm64`)
- Helm chart for one-command installation
- GHCR container registry publication
- Kind-based local development and demo scenarios

**Demo**
- 6 realistic demo scenarios: OOMKilled, CrashLoopBackOff, ImagePullBackOff, MountFailure, SchedulingFailure, ReadinessProbeFailure
- `deploy/kind/demo.sh` guided walkthrough script
- Annotated expected output documentation

**Testing**
- 100+ unit tests with table-driven and property-based testing (pgregory.net/rapid)
- 13 envtest integration tests
- GitHub Actions CI: fmt, vet, lint, unit tests, integration tests, build, docker-build

[0.1.0]: https://github.com/karangupta982/k8s-incident-investigator/releases/tag/v0.1.0
