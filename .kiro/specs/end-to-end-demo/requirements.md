# Requirements Document

## Introduction

The end-to-end demo spec delivers the "Kind-based demonstration" called for in the product MVP scope. It provides realistic, repeatable demo scenarios for all major failure types the investigator can handle, a guided walkthrough that shows the full investigation flow, and annotated `kubectl describe` output showing what a complete IncidentReport looks like for each scenario.

This spec depends on all prior specs being implemented: foundation, evidence collection, evidence correlation, diagnosis engine, and reporting.

---

## Glossary

### Demo Scenario

A pre-built Kubernetes workload manifest that deliberately triggers a specific failure type, allowing an engineer to observe the Incident Investigator's full investigation pipeline from detection through diagnosis.

### Walkthrough Script

A shell script that automates deploying a demo scenario, waiting for investigation to complete, and printing the resulting IncidentReport in a readable format.

---

## Requirements

### Requirement 1: Provide Demo Scenarios for All Supported Trigger Types

#### Acceptance Criteria

1. THE demo SHALL include a scenario for each of the following trigger types:
   - OOMKilled (container exceeds memory limit)
   - CrashLoopBackOff (application exit code non-zero)
   - ImagePullBackOff (invalid image reference)
   - MountFailure (PVC not bound / storage class missing)
   - SchedulingFailure (resource request exceeds available node capacity)
   - ReadinessProbeFailure (probe always returns HTTP 500)

2. Each scenario SHALL be a self-contained Kubernetes manifest in `deploy/kind/scenarios/<trigger-type>/`.

3. Each scenario manifest SHALL include a `Namespace`, the workload `Deployment` (or `Job` where appropriate), and any required dependencies (PVC, StorageClass stub, etc.).

4. Each scenario SHALL be deployable with `kubectl apply -f deploy/kind/scenarios/<trigger-type>/`.

---

### Requirement 2: Provide a Guided Walkthrough Script

#### Acceptance Criteria

1. THE demo SHALL include `deploy/kind/demo.sh` that:
   - Accepts a scenario name as an argument (e.g., `./demo.sh oom-killed`)
   - Deploys the scenario to the Kind cluster
   - Waits for an `IncidentReport` to appear (up to 2 minutes)
   - Waits for the phase to advance beyond `Investigating` (up to the investigation timeout)
   - Prints the `kubectl describe incidentreport` output in full
   - Prints a summary of what the investigation found

2. THE script SHALL accept `--all` to run all scenarios sequentially with a cleanup between each.

3. THE script SHALL clean up its deployed resources on exit (success or failure) unless `--no-cleanup` is passed.

---

### Requirement 3: Provide Annotated Expected Output Documentation

#### Acceptance Criteria

1. THE demo SHALL include `docs/demo/scenarios.md` documenting the expected `kubectl describe` output for each scenario, with inline comments explaining what each field means and what evidence led to the diagnosis.

2. For each scenario, `scenarios.md` SHALL include: trigger type, expected phase after investigation, expected primary finding RuleID and cause, and a sample of the timeline and evidence sections.

---

### Requirement 4: Demo Works on a Fresh Kind Cluster

#### Acceptance Criteria

1. THE demo SHALL work on a cluster created with `deploy/kind/setup.sh` with no additional manual steps.

2. THE demo SHALL be runnable within 15 minutes total for the OOMKilled scenario (most of which is the stability period wait).

3. THE demo SHALL print clear progress messages at each stage so an observer knows what is happening.

---

## Out of Scope

- Production cluster deployment
- Multi-cluster demo
- Prometheus/Grafana dashboard demo
- Automated CI execution of the full demo (the stability period makes it too slow for CI)
