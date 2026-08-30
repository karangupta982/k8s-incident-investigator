# Kubernetes Incident Investigator — Product Context

## Product Overview

Kubernetes Incident Investigator is a Kubernetes-native investigation system that helps Platform Engineers, SREs, DevOps Engineers, and Kubernetes administrators understand workload failures without manually collecting information from many different Kubernetes resources and tools.

The system continuously observes Kubernetes for meaningful failure signals. When an incident threshold is crossed, it creates a persistent `IncidentReport` Custom Resource and investigates the incident by collecting relevant evidence from Kubernetes resources, events, container logs, workload ownership information, nodes, storage, networking resources, and optionally observability systems.

The system correlates the collected evidence into a timeline, evaluates known failure patterns, produces an explainable diagnosis when sufficient evidence exists, and provides evidence-backed recommendations.

The primary goal is to reduce the amount of manual investigation required during Kubernetes incidents.

---

## Problem

When a Kubernetes workload fails, engineers often need to manually execute several commands and inspect different sources of information.

A typical investigation may involve:

- `kubectl get pods`
- `kubectl describe pod`
- `kubectl logs`
- `kubectl logs --previous`
- `kubectl get events`
- `kubectl describe node`
- `kubectl describe pvc`
- inspecting Deployments and ReplicaSets
- checking Services and EndpointSlices
- checking ConfigMaps and Secrets
- checking metrics in Prometheus/Grafana
- correlating timestamps between different sources

The difficult part is usually not obtaining one piece of information.

The difficult part is connecting the pieces together and understanding:

1. What happened?
2. When did it happen?
3. Which workload was affected?
4. What changed before the failure?
5. Which Kubernetes components or dependencies were involved?
6. Which evidence supports a possible cause?
7. What has been ruled out?
8. What should the engineer investigate next?

The investigator automates this evidence-gathering and correlation process.

---

## Primary Value

The product provides value at three levels.

### 1. Evidence aggregation

The engineer receives relevant information from multiple Kubernetes sources in one investigation rather than manually collecting it.

### 2. Timeline reconstruction

Related events are organized chronologically so the engineer can understand the sequence of events rather than seeing isolated facts.

### 3. Explainable diagnosis

When sufficient evidence matches a known failure pattern, the system provides:

- likely cause
- confidence level
- supporting evidence
- contributing factors
- recommendations

The system must never invent a root cause when the evidence is insufficient.

---

## What the Product Does

The investigator:

1. Watches Kubernetes resources for meaningful failure signals.
2. Applies incident trigger and threshold policies.
3. Creates an `IncidentReport` when a meaningful incident begins.
4. Associates the incident with the affected workload.
5. Tracks affected Pods throughout the incident.
6. Collects relevant Kubernetes evidence.
7. Collects bounded container log excerpts when useful.
8. Correlates evidence using time, resource relationships, and failure semantics.
9. Builds an incident timeline.
10. Evaluates deterministic diagnosis rules.
11. Identifies primary causes, contributing factors, and alternative hypotheses when appropriate.
12. Represents unknown or partially understood failures honestly.
13. Generates evidence-backed recommendations.
14. Updates the `IncidentReport` throughout the incident lifecycle.
15. Marks the incident resolved after the workload has recovered and remained stable for the configured stability period.
16. Exposes the result through Kubernetes-native resources such as `kubectl get incidentreports` and `kubectl describe incidentreport`.

---

## What the Product Does Not Do in MVP

The MVP is an investigation and reporting system, not an automatic remediation platform.

The investigator must NOT automatically:

- restart workloads
- delete Pods
- modify Deployments
- change resource limits
- modify Services
- modify NetworkPolicies
- recreate PVCs
- modify application configuration
- modify Secrets
- execute arbitrary commands inside containers
- perform production remediation based on its diagnosis

Recommendations are suggestions for engineers.

The engineer remains responsible for deciding whether and how to remediate the problem.

---

## Target Users

### Primary Users

- Platform Engineers
- Site Reliability Engineers
- DevOps Engineers
- Kubernetes Administrators

### Secondary Users

- Backend Engineers responsible for Kubernetes workloads
- Cloud Infrastructure Engineers
- Engineering teams operating microservices

---

## Primary User Story

An engineer receives an alert that a production workload is failing.

Instead of immediately executing several Kubernetes commands and manually correlating the results, the engineer can inspect the corresponding `IncidentReport`.

The report should answer, as far as the available evidence allows:

> "What happened, what was affected, what evidence do we have, what do we think caused it, what remains unknown, and what should I investigate next?"

---

## Example

A workload repeatedly crashes because its container exceeds its memory limit.

The investigator may produce:

```text
Incident: payment-api-incident-001

Workload:
Deployment/payment-api

Affected Pods:
payment-api-abc123

Status:
Resolved

Timeline:
14:02:01 - Container memory usage increased
14:02:08 - Container exceeded configured memory limit
14:02:08 - Container terminated with OOMKilled
14:02:09 - Container restarted
14:02:20 - Container terminated again
14:02:21 - Pod entered CrashLoopBackOff

Diagnosis:
Container exceeded its memory limit

Confidence:
High

Supporting Evidence:
- Termination reason: OOMKilled
- Exit code: 137
- Memory limit: 512Mi
- Node did not report MemoryPressure

Recommendation:
Investigate application memory consumption.
Consider increasing the memory limit if the workload legitimately requires more memory.
````

The system does not claim that a memory leak exists unless there is evidence supporting that conclusion.

---

## Design Philosophy

The product follows these principles:

### Evidence first

Collect evidence before making claims.

### Explainability

Every diagnosis should be explainable using collected evidence.

### Honest uncertainty

Unknown is a valid outcome.

### Kubernetes-native

Use Kubernetes resources, controllers, watches, CRDs, and reconciliation patterns rather than building an unrelated external monitoring system.

### Non-invasive

The investigator should observe and report without modifying the affected workload.

### Bounded investigation

The system must limit log size, API calls, investigation duration, and stored evidence.

### Incremental intelligence

The system should remain useful even when it encounters failures it does not yet understand.

---

## MVP Scope

The MVP should focus on:

* Kubernetes controller
* `IncidentReport` CRD
* Pod-centered failure detection
* incident lifecycle
* workload correlation
* Kubernetes evidence collection
* bounded container logs
* deterministic evidence correlation
* deterministic diagnosis rules
* unknown diagnosis handling
* recommendations
* Kubernetes-native reporting
* tests
* Kind-based demonstration
* documentation

Prometheus integration, external storage, Slack/PagerDuty, dashboards, LLM-assisted diagnosis, and automatic remediation are outside the initial MVP.
