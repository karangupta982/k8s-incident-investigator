# Requirements Document

## Introduction

The Kubernetes Incident Investigator is a Kubernetes-native system that detects meaningful workload failures, creates persistent incident records, collects and correlates investigation data, and presents an actionable incident report to Platform Engineers, SREs, DevOps Engineers, and Kubernetes administrators.

This specification covers the foundation of the system.

The foundation is responsible for detecting meaningful Pod failure signals, creating and maintaining `IncidentReport` resources, associating incidents with their owning workloads, tracking affected Pods, and managing the basic incident lifecycle.

Detailed evidence collection, root-cause diagnosis, recommendations, advanced correlation, and external integrations will be implemented in later specifications.

The system is designed as an observation and investigation tool. It must not automatically modify or remediate the affected workload.

---

## Glossary

### Incident

A continuous period of abnormal behavior affecting a Kubernetes workload.

An incident may involve one or multiple Pods belonging to the same workload.

### IncidentReport

A Kubernetes Custom Resource representing the persistent state of an incident and its investigation.

### Trigger

A Kubernetes failure signal that may indicate abnormal workload behavior.

Examples include:

- OOMKilled
- CrashLoopBackOff
- ImagePullBackOff
- CreateContainerConfigError
- repeated FailedMount
- repeated readiness probe failures
- repeated liveness probe failures
- Pod eviction

### Threshold

The condition that determines whether a failure signal is significant enough to create or update an incident.

### Workload

The Kubernetes resource responsible for managing one or more Pods.

Examples include:

- Deployment
- StatefulSet
- DaemonSet
- Job
- CronJob

### Affected Pod

A Pod that has experienced a failure associated with an incident.

### Active Incident

An IncidentReport whose lifecycle has not reached `Resolved`.

### Stability Period

A configured period during which a workload must remain healthy before an active incident can be marked as resolved.

### Reconciliation

The process in which the Investigator examines the current Kubernetes state and determines whether its managed IncidentReport resources need to be created or updated.

### Investigator

The Kubernetes controller responsible for detecting incidents and managing IncidentReport resources.

---

## Requirements

### Requirement 1: Detect Meaningful Pod Failure Signals

**User Story:** As a Platform Engineer, I want the Investigator to detect meaningful Kubernetes failure signals so that incidents can be identified without manually watching Pods.

#### Acceptance Criteria

1. WHEN a watched Pod's container is terminated with reason `OOMKilled`, THE SYSTEM SHALL identify the event as an incident trigger.

2. WHEN a watched Pod enters `CrashLoopBackOff`, THE SYSTEM SHALL identify the state as an incident trigger.

3. WHEN a watched Pod enters `ImagePullBackOff`, THE SYSTEM SHALL identify the state as an incident trigger.

4. WHEN a watched Pod enters `CreateContainerConfigError`, THE SYSTEM SHALL identify the state as an incident trigger.

5. WHEN a Pod experiences repeated mount failures, THE SYSTEM SHALL evaluate the failures against the configured incident threshold.

6. WHEN a Pod experiences repeated readiness probe failures, THE SYSTEM SHALL evaluate the failures against the configured incident threshold.

7. WHEN a Pod experiences repeated liveness probe failures, THE SYSTEM SHALL evaluate the failures against the configured incident threshold.

8. WHEN a Pod experiences a detectable scheduling failure that crosses the configured threshold, THE SYSTEM SHALL identify it as an incident trigger.

9. WHEN a Pod is evicted, THE SYSTEM SHALL identify the eviction as an incident trigger.

10. WHEN a Pod undergoes a normal lifecycle transition that does not represent a configured failure condition, THE SYSTEM SHALL NOT create an incident. Normal lifecycle transitions that do represent a configured failure condition SHALL still trigger incident evaluation.

---

### Requirement 2: Apply Failure Thresholds

**User Story:** As a Platform Engineer, I want transient failures to be distinguished from meaningful incidents so that the Investigator does not generate unnecessary incident records.

#### Acceptance Criteria

1. WHEN an immediate-trigger failure such as `OOMKilled`, `CrashLoopBackOff`, `ImagePullBackOff`, `CreateContainerConfigError`, or Pod eviction is detected, THE SYSTEM SHALL allow incident creation without requiring repeated occurrences.

2. WHEN a single readiness probe failure occurs AND the configured readiness probe failure threshold is greater than 1, THE SYSTEM SHALL NOT create an incident solely because of that failure. WHEN the configured readiness probe failure threshold is 1, a single readiness probe failure SHALL be sufficient to cross the threshold.

3. WHEN readiness probe failures repeatedly occur and the failure count reaches or exceeds the configured threshold, THE SYSTEM SHALL create or update an incident.

4. WHEN mount failures repeatedly occur and the failure count reaches or exceeds the configured threshold, THE SYSTEM SHALL create or update an incident.

5. WHEN scheduling failures repeatedly occur and the failure count reaches or exceeds the configured threshold, THE SYSTEM SHALL create or update an incident.

6. WHEN a failure does not cross its configured threshold, THE SYSTEM SHALL NOT create a new incident solely because of that failure.

---

### Requirement 3: Create an IncidentReport

**User Story:** As an engineer investigating a production failure, I want a persistent incident record to be created so that investigation state is not lost between reconciliation attempts or controller restarts.

#### Acceptance Criteria

1. WHEN a failure crosses the configured incident threshold and no related active incident exists, THE SYSTEM SHALL create an `IncidentReport`.

2. WHEN an `IncidentReport` is created, THE SYSTEM SHALL associate it with the namespace of the affected workload. The namespace is carried by the `IncidentReport` object's own `metadata.namespace` field (standard Kubernetes convention) and SHALL NOT be duplicated as a separate field in the spec.

3. WHEN an `IncidentReport` is created, THE SYSTEM SHALL record the initial affected Pod.

4. WHEN an `IncidentReport` is created, THE SYSTEM SHALL record the identified workload when workload ownership can be resolved.

5. WHEN an `IncidentReport` is created, THE SYSTEM SHALL record the failure signal that caused the incident.

6. WHEN an `IncidentReport` is created, THE SYSTEM SHALL record the incident start time.

7. WHEN an `IncidentReport` is first created, THE SYSTEM SHALL set its lifecycle phase to `Investigating`.

8. WHEN an incident is created, THE SYSTEM SHALL persist the IncidentReport before considering the incident investigation state established. IF persistence of a new IncidentReport fails, THE SYSTEM SHALL retry the persistence operation until it succeeds before proceeding.

---

### Requirement 4: Resolve Workload Ownership

**User Story:** As an engineer, I want an incident associated with the workload rather than only an ephemeral Pod so that failures across replacement Pods can be correlated.

#### Acceptance Criteria

1. WHEN a Pod has an identifiable controller owner, THE SYSTEM SHALL resolve the owning workload. IF workload ownership resolution fails despite the Pod having a controller owner, THE SYSTEM SHALL fall back to retaining the Pod identity and recording that ownership could not be determined.

2. WHEN a Pod is owned by a ReplicaSet that is controlled by a Deployment, THE SYSTEM SHALL associate the incident with the Deployment.

3. WHEN a Pod is owned by a StatefulSet, THE SYSTEM SHALL associate the incident with the StatefulSet.

4. WHEN a Pod is owned by a DaemonSet, THE SYSTEM SHALL associate the incident with the DaemonSet.

5. WHEN a Pod is owned by a Job, THE SYSTEM SHALL associate the incident with the Job.

6. WHEN a Pod is owned by a CronJob through a Job and the CronJob ownership is successfully resolved, THE SYSTEM SHALL associate the incident with the CronJob.

7. WHEN workload ownership cannot be resolved, THE SYSTEM SHALL retain the affected Pod identity and record that workload ownership could not be determined.

---

### Requirement 5: Reuse Existing Active Incidents

**User Story:** As a Platform Engineer, I want related failures to update an existing incident rather than create duplicate incidents so that one production problem produces one coherent incident record.

#### Acceptance Criteria

1. WHEN a failure crosses the incident threshold and a related active IncidentReport already exists for the workload, THE SYSTEM SHALL update the existing IncidentReport instead of creating another active IncidentReport. WHEN multiple related failures occur simultaneously, THE SYSTEM SHALL queue them for processing against the same incident, preventing duplicate IncidentReport creation even during processing delays.

2. WHEN a related failure is associated with an existing active incident, THE SYSTEM SHALL preserve the existing incident start time.

3. WHEN a related failure is associated with an existing active incident, THE SYSTEM SHALL preserve previously recorded incident information.

4. WHEN a related failure is associated with an existing active incident, THE SYSTEM SHALL record the new failure information without replacing the complete existing incident history.

5. WHEN no related active incident exists, THE SYSTEM SHALL create a new IncidentReport.

6. WHEN an active incident exists for a workload, THE SYSTEM SHALL record all subsequent failures for that workload against that active incident regardless of the time elapsed since the incident started. The configured correlation window does not permit creation of a second active incident for the same workload. WHEN no active incident exists for a workload, THE SYSTEM SHALL create a new incident for the next failure regardless of the correlation window.

---

### Requirement 6: Track Multiple Affected Pods

**User Story:** As an engineer, I want an incident to track multiple affected Pods so that I can understand whether a workload failure is isolated to one Pod or affects multiple replacements.

#### Acceptance Criteria

1. WHEN multiple Pods belonging to the same active workload incident experience related failures, THE SYSTEM SHALL associate those Pods with the same active IncidentReport when the incident correlation conditions are satisfied.

2. WHEN a Pod is already associated with an active IncidentReport, THE SYSTEM SHALL NOT add the same Pod identity repeatedly.

3. WHEN a workload replaces a failed Pod during an active incident, THE SYSTEM SHALL allow the replacement Pod to be associated with the existing incident.

4. WHEN a previously affected Pod is deleted, THE SYSTEM SHALL retain its identity in the historical incident information.

---

### Requirement 7: Maintain Incident Lifecycle

**User Story:** As an engineer, I want the incident lifecycle to clearly represent whether an incident is still active or resolved.

#### Acceptance Criteria

1. WHEN an IncidentReport is created, THE SYSTEM SHALL set its phase to `Investigating`.

2. WHEN an active incident has not yet recovered, THE SYSTEM SHALL keep its phase as an active investigation state.

3. WHEN the workload becomes healthy but has not yet completed the configured stability period, THE SYSTEM SHALL NOT mark the incident as `Resolved`.

4. WHEN the workload remains healthy for the configured stability period, THE SYSTEM SHALL transition the IncidentReport to `Resolved`.

5. WHEN an incident is resolved, THE SYSTEM SHALL record the resolution time.

6. THE SYSTEM SHALL support the lifecycle states `Investigating`, `Diagnosed`, `Unknown`, and `Resolved` in the IncidentReport API model.

7. THE SYSTEM SHALL NOT require diagnosis logic in this foundation specification to transition an incident into `Diagnosed` or `Unknown`.

---

### Requirement 8: Detect Recovery

**User Story:** As an engineer, I want an incident to remain active until the workload is demonstrably stable so that transient recovery does not incorrectly close an incident.

#### Acceptance Criteria

1. WHEN an active workload becomes healthy, THE SYSTEM SHALL begin evaluating the configured stability period.

2. WHEN a new relevant failure occurs during the stability period, THE SYSTEM SHALL keep the IncidentReport active.

3. WHEN the workload remains healthy for the complete stability period, THE SYSTEM SHALL mark the IncidentReport as `Resolved`. WHEN the workload becomes unhealthy during the stability period, THE SYSTEM SHALL keep the IncidentReport active and restart stability period evaluation when the workload becomes healthy again.

4. WHEN the workload does not become healthy, THE SYSTEM SHALL keep the IncidentReport active.

5. WHEN the Investigator restarts while an incident is waiting for its stability period, THE SYSTEM SHALL recover the incident state from Kubernetes resources and continue recovery evaluation.

6. WHEN a workload is undergoing a normal rolling update or scaling operation that temporarily reduces the number of ready replicas, THE SYSTEM SHALL evaluate workload health using the workload's own desired/ready/available replica counts and SHALL NOT incorrectly treat normal lifecycle operations as an unrecovered failure.

---

### Requirement 9: Preserve Historical Incidents

**User Story:** As an engineer, I want resolved incidents to remain available so that I can investigate previous failures and identify recurring problems.

#### Acceptance Criteria

1. WHEN an IncidentReport reaches `Resolved`, THE SYSTEM SHALL retain the IncidentReport in Kubernetes.

2. WHEN the same workload experiences a new failure after the previous incident has been resolved, THE SYSTEM SHALL create a new IncidentReport. IF the previous incident was resolved and the workload subsequently experienced no new trigger signals (the failure condition cleared), a new IncidentReport SHALL be created when the next trigger is observed. The system SHALL NOT reopen a resolved IncidentReport.

3. WHEN a new incident is created for a workload with previous resolved incidents, THE SYSTEM SHALL NOT overwrite the previous resolved IncidentReport.

4. THE SYSTEM SHALL allow users to retrieve historical IncidentReports through standard Kubernetes commands.

---

### Requirement 10: Preserve Active Incident State

**User Story:** As an engineer, I want new reconciliation attempts to preserve existing incident information so that repeated Kubernetes events do not corrupt the incident record.

#### Acceptance Criteria

1. WHEN the Investigator reconciles an existing active IncidentReport, THE SYSTEM SHALL preserve previously recorded incident start time and workload identity.

2. WHEN new related failure information is received, THE SYSTEM SHALL add or update the relevant incident state without unnecessarily replacing unrelated existing information.

3. WHEN the same Kubernetes event causes multiple reconciliation attempts, THE SYSTEM SHALL produce the same logical incident state as a single reconciliation attempt. WHEN rapid successive reconciliation attempts are triggered before the current one completes, THE SYSTEM SHALL queue subsequent attempts rather than allowing concurrent processing of the same incident.

4. THE SYSTEM SHALL prevent duplicate IncidentReports for the same active workload incident.

---

### Requirement 11: Support Controller Restart

**User Story:** As a Platform Engineer, I want incident state to survive Investigator restarts so that an operator crash does not erase an active investigation.

#### Acceptance Criteria

1. WHEN the Investigator controller stops while an IncidentReport is active, THE SYSTEM SHALL preserve the IncidentReport in Kubernetes.

2. WHEN the Investigator controller starts again, THE SYSTEM SHALL attempt to discover existing active IncidentReports. IF active IncidentReport discovery fails on startup, THE SYSTEM SHALL continue running, log the failure, and remain operational for new incidents.

3. WHEN an active IncidentReport is discovered after restart, THE SYSTEM SHALL continue managing that incident.

4. THE SYSTEM SHALL NOT depend exclusively on in-memory state for active incident tracking.

---

### Requirement 12: Handle Ephemeral Pods

**User Story:** As an engineer, I want an incident to continue even when Kubernetes replaces the affected Pod because Pod identities are ephemeral.

#### Acceptance Criteria

1. WHEN an affected Pod is deleted while its workload incident remains active, THE SYSTEM SHALL keep the IncidentReport active unless the workload has recovered.

2. WHEN a replacement Pod is created for the same workload and experiences a related failure, THE SYSTEM SHALL be able to associate the replacement Pod with the existing active incident.

3. WHEN an affected Pod no longer exists, THE SYSTEM SHALL use the persisted IncidentReport state rather than requiring the original Pod to still exist.

---

### Requirement 13: Provide Kubernetes-Native Reporting

**User Story:** As an engineer, I want to inspect incidents using standard Kubernetes tooling so that I do not need a separate UI to understand the foundation system.

#### Acceptance Criteria

1. WHEN a user executes `kubectl get incidentreports`, THE SYSTEM SHALL expose active and resolved IncidentReports as Kubernetes resources.

2. WHEN a user executes `kubectl describe incidentreport <name>`, THE SYSTEM SHALL expose the incident's available status and identifying information.

3. THE SYSTEM SHALL expose sufficient fields in the IncidentReport status to understand the current lifecycle phase.

4. THE SYSTEM SHALL expose the affected workload when workload ownership is known.

5. THE SYSTEM SHALL expose the affected Pod or Pods associated with the incident.

6. THE SYSTEM SHALL expose the triggering failure signal.

---

### Requirement 14: Handle Repeated Reconciliation Safely

**User Story:** As a Platform Engineer, I want reconciliation to be idempotent so that Kubernetes watch events and retries do not create inconsistent incident state.

#### Acceptance Criteria

1. WHEN the same reconciliation request is processed multiple times, THE SYSTEM SHALL NOT create duplicate active IncidentReports.

2. WHEN the same Pod is processed multiple times for the same incident, THE SYSTEM SHALL NOT duplicate its association with the incident.

3. WHEN the same failure signal is observed repeatedly, THE SYSTEM SHALL maintain a consistent incident state.

4. WHEN a reconciliation operation fails temporarily, THE SYSTEM SHALL allow Kubernetes controller retry behavior to continue processing the incident.

5. WHEN the same Kubernetes Event is observed in multiple successive reconciliations, THE SYSTEM SHALL NOT count the same Event occurrence multiple times toward an incident threshold.

---

### Requirement 15: Handle Missing or Changing Kubernetes Resources

**User Story:** As an engineer, I want the Investigator to tolerate normal Kubernetes resource changes so that investigation state is not lost when Pods or related resources disappear.

#### Acceptance Criteria

1. WHEN an affected Pod is deleted, THE SYSTEM SHALL retain the incident state already persisted in the IncidentReport.

2. WHEN a related Kubernetes resource temporarily cannot be retrieved, THE SYSTEM SHALL not delete the active IncidentReport solely because that resource is unavailable.

3. WHEN a Kubernetes API operation temporarily fails, THE SYSTEM SHALL return a retry error that allows the reconciliation process to retry. IncidentReports are not automatically protected from deletion during transient API failures.

4. WHEN partial resource information is available, THE SYSTEM SHALL preserve the available incident information rather than failing the entire incident state.

---

### Requirement 16: Use Least-Privilege Access

**User Story:** As a Platform Engineer, I want the Investigator to have only the permissions required for investigation so that the controller does not become an unnecessary production security risk.

#### Acceptance Criteria

1. THE SYSTEM SHALL use Kubernetes RBAC permissions limited to resources required by the foundation functionality. THE SYSTEM SHALL simultaneously satisfy both minimal permission scope and the absence of all dangerous permissions listed in this requirement.

2. THE SYSTEM SHALL NOT require permission to delete Pods.

3. THE SYSTEM SHALL NOT require permission to modify Deployments.

4. THE SYSTEM SHALL NOT require permission to modify StatefulSets.

5. THE SYSTEM SHALL NOT require permission to modify DaemonSets.

6. THE SYSTEM SHALL NOT require permission to modify Services.

7. THE SYSTEM SHALL NOT require permission to modify application configuration.

8. THE SYSTEM SHALL NOT expose Secret values through IncidentReports or controller logs.

---

### Requirement 17: Remain Non-Destructive

**User Story:** As a Platform Engineer, I want the Investigator to observe and report failures without automatically changing workloads so that the investigation tool cannot accidentally make a production incident worse.

#### Acceptance Criteria

1. WHEN the Investigator detects an incident, THE SYSTEM SHALL collect and report information without automatically modifying the affected workload. IF incident detection succeeds but information collection or reporting fails, THE SYSTEM SHALL retry the failed step or escalate the failure.

2. THE SYSTEM SHALL NOT automatically restart Pods.

3. THE SYSTEM SHALL NOT automatically modify Deployment replica counts.

4. THE SYSTEM SHALL NOT automatically modify container resource limits.

5. THE SYSTEM SHALL NOT automatically modify application configuration.

6. THE SYSTEM SHALL NOT automatically delete Kubernetes resources.

---

### Requirement 18: Keep Foundation Processing Bounded

**User Story:** As a Platform Engineer, I want the Investigator to avoid unnecessary work on normal cluster activity so that the controller does not become a source of additional cluster load.

#### Acceptance Criteria

1. WHEN a Pod event does not represent a configured incident trigger, THE SYSTEM SHALL avoid performing expensive incident processing.

2. WHEN an incident trigger is detected, THE SYSTEM SHALL process only the information required by the foundation functionality. THE SYSTEM MAY skip foundation processing if it can quickly determine a detected trigger is a false positive, without performing expensive operations.

3. THE SYSTEM SHALL avoid unbounded in-memory state for active incidents.

4. THE SYSTEM SHALL persist authoritative incident state in Kubernetes resources rather than relying exclusively on process memory.

---

### Requirement 19: Provide Controller Observability

**User Story:** As a Platform Engineer, I want to understand what the Investigator itself is doing so that I can troubleshoot the Investigator when it encounters problems.

#### Acceptance Criteria

1. WHEN the controller begins reconciling an incident-related resource, THE SYSTEM SHALL produce structured logs sufficient to identify the reconciliation context.

2. WHEN a new IncidentReport is created, THE SYSTEM SHALL produce a structured log describing the incident creation.

3. WHEN an existing IncidentReport is reused, THE SYSTEM SHALL produce a structured log indicating that the existing incident was reused.

4. WHEN an IncidentReport changes lifecycle phase, THE SYSTEM SHALL produce a structured log describing the transition.

5. WHEN a reconciliation operation fails, THE SYSTEM SHALL produce an error containing sufficient context to investigate the failure without exposing sensitive values.

---

### Requirement 20: Foundation Success Scenario

**User Story:** As a Platform Engineer, I want the complete foundation flow to work against a real Kubernetes cluster so that the controller's core behavior can be validated before advanced investigation capabilities are added.

#### Acceptance Criteria

1. WHEN a test workload is deployed to a Kind cluster and experiences an immediate-trigger failure, THE SYSTEM SHALL detect the failure.

2. WHEN the failure crosses the incident threshold, THE SYSTEM SHALL create an IncidentReport.

3. WHEN the user executes `kubectl get incidentreports`, THE SYSTEM SHALL display the created incident.

4. WHEN the same workload experiences another related failure while the incident is active, THE SYSTEM SHALL update the existing IncidentReport rather than creating a duplicate.

5. WHEN the workload becomes healthy and remains healthy for the configured stability period, THE SYSTEM SHALL transition the IncidentReport to `Resolved`.

6. WHEN the same workload experiences a new failure after the previous incident has been resolved, THE SYSTEM SHALL create a new IncidentReport while preserving the historical incident. IF creation of the new IncidentReport fails, THE SYSTEM SHALL retry or escalate the failure until the new IncidentReport is successfully created.

---

### Requirement 21: Active-to-Historical Incident Transition

**User Story:** As an engineer, I want resolved incidents to be preserved as historical records while the system remains ready to create a new incident for the same workload in the future.

#### Acceptance Criteria

1. WHEN an active IncidentReport transitions to `Resolved`, THE SYSTEM SHALL preserve the resolved IncidentReport as a permanent historical record.

2. WHEN a new incident is required for the same workload after a previous incident has been resolved, THE SYSTEM SHALL create a new independent IncidentReport.

3. THE SYSTEM SHALL be capable of creating a new active incident for a workload regardless of how many resolved historical incidents exist for that workload.

4. THE SYSTEM SHALL NOT require deletion of any historical (resolved) IncidentReport to create a new incident. The system MAY delete the active-named IncidentReport slot as part of the resolution transition when copying the active incident to a permanent historical record.

5. THE SYSTEM SHALL distinguish the current active incident from historical resolved incidents through well-defined resource identity or labeling.

---

## Out of Scope

The following capabilities are explicitly outside this specification:

- detailed evidence collection
- container log analysis
- Kubernetes event correlation beyond what is required for trigger detection
- root-cause diagnosis
- diagnosis confidence
- recommendation generation
- advanced cross-resource correlation
- Prometheus workload metrics integration
- Grafana integration
- Slack integration
- PagerDuty integration
- LLM-based diagnosis
- automatic remediation
- web dashboard
- external evidence storage
- multi-cluster investigation