# Requirements Document

## Introduction

The reporting feature completes the Kubernetes Incident Investigator investigation pipeline by transforming the evidence and diagnosis produced by previous specifications into a human-readable, actionable incident report surfaced through native Kubernetes tooling.

When an engineer runs `kubectl describe incidentreport <name>`, the report must answer the question:

> "What happened, what was affected, what evidence was found, what do we think caused it, what is uncertain, and what should I investigate next?"

The reporting layer assembles a structured `IncidentSummary` from the existing `EvidenceSnapshot` and `DiagnosisResult` fields, adds a chronological `Timeline` of observed events, and enriches each diagnosis finding with a full `Recommendation`. All output is Kubernetes-native — no external dashboards, web UIs, or notification systems are required in this specification.

---

## Glossary

### IncidentSummary

A concise, human-readable summary of the incident stored in `IncidentReport.Status.Summary`. It is generated from existing status fields and updated on each reconciliation cycle. It does not add new investigation logic — it renders existing information in a form immediately legible to an engineer.

### Timeline

A chronological, bounded list of `TimelineEvent` entries representing the significant moments observed during the incident. Stored in `IncidentReport.Status.Timeline`. Events are sourced from: Kubernetes Events collected by the evidence layer, container termination timestamps from PodEvidence, workload condition transitions from WorkloadEvidence, and lifecycle state changes recorded by the controller (incident created, stability period started, resolved).

### Recommendation

A short, actionable, evidence-backed suggestion stored inside each `DiagnosisFinding.Recommendation` field. The reporting layer does not introduce a new top-level type for recommendations; it enriches the finding already produced by the diagnosis engine. Recommendations must reference specific observed values (e.g., "memory limit: 512Mi", "restart count: 7") rather than generic advice.

### ReportingEngine

The Go component in `internal/reporting/` that assembles the `IncidentSummary` and `Timeline` from existing status fields. It is a pure function that takes an `*v1alpha1.IncidentReport` and returns the assembled output. It does not call the Kubernetes API.

---

## Requirements

### Requirement 1: Produce an IncidentSummary

**User Story:** As an engineer receiving an alert, I want a concise summary of the incident at the top of `kubectl describe incidentreport` so that I understand the scope and status without reading through raw evidence fields.

#### Acceptance Criteria

1. WHEN an `IncidentReport` is active (phase is not `Resolved`) and has a non-nil `DiagnosisResult.Primary`, THE Investigator SHALL generate an `IncidentSummary` that includes the workload identity, phase, trigger type, primary cause, confidence level, and the number of affected Pods.

2. WHEN an `IncidentReport` has phase `Unknown` (no rule matched), THE Investigator SHALL generate an `IncidentSummary` that includes the workload identity, phase, trigger type, the unknown reason from `DiagnosisResult.UnknownReason`, and the number of affected Pods.

3. WHEN an `IncidentReport` has phase `Investigating` and no `DiagnosisResult` yet (evidence or diagnosis not yet run), THE Investigator SHALL generate an `IncidentSummary` that includes the workload identity, phase, trigger type, and a note that investigation is in progress.

4. WHEN an `IncidentReport` is `Resolved`, THE Investigator SHALL generate an `IncidentSummary` that includes the workload identity, resolution timestamp, primary cause (if diagnosed before resolution), and duration of the incident.

5. THE Investigator SHALL store the `IncidentSummary` in `IncidentReport.Status.Summary` and update it on every reconciliation cycle.

6. THE `IncidentSummary` SHALL NOT exceed 2048 characters in total length.

---

### Requirement 2: Build a Chronological Timeline

**User Story:** As an engineer, I want a time-ordered list of significant events during the incident so that I can understand the sequence of what happened without correlating timestamps manually.

#### Acceptance Criteria

1. WHEN an `IncidentReport` has evidence collected, THE Investigator SHALL build a `Timeline` from the following sources and store it in `IncidentReport.Status.Timeline`:
   - Kubernetes Events from `EvidenceSnapshot.Events`, ordered by `LastTime`
   - Container termination timestamps from `EvidenceSnapshot.Pod.Containers[*].State` where state is "terminated"
   - The incident `StartedAt` timestamp as a "Incident detected" event
   - The `StabilityStartedAt` timestamp as a "Workload became healthy" event (when set)
   - The `ResolvedAt` timestamp as a "Incident resolved" event (when set)

2. WHEN building the `Timeline`, THE Investigator SHALL order all events chronologically by timestamp (ascending — earliest first).

3. WHEN two events have the same timestamp, THE Investigator SHALL order them deterministically by source type: controller lifecycle events first, then container terminations, then Kubernetes Events.

4. THE `Timeline` SHALL be bounded to a maximum of `MaxTimelineEvents` entries (default 50). WHEN the number of events exceeds this limit, THE Investigator SHALL retain the most recent events and add a note at the top of the timeline indicating entries were omitted.

5. WHEN an evidence source is unavailable (e.g., `EvidenceSnapshot.Events` is empty due to a collection error), THE Investigator SHALL build the timeline from the available sources and SHALL NOT record the absence of events as a timeline entry.

6. THE Investigator SHALL store the `Timeline` in `IncidentReport.Status.Timeline` and update it on every reconciliation cycle.

---

### Requirement 3: Generate Evidence-Backed Recommendations

**User Story:** As an engineer, I want actionable recommendations tied to specific observed values so that I know exactly what to investigate next without reading through the raw evidence.

#### Acceptance Criteria

1. WHEN the `DiagnosisResult.Primary` finding is non-nil, THE Investigator SHALL populate `DiagnosisResult.Primary.Recommendation` with a recommendation that references specific evidence values from the `EvidenceSnapshot`.

2. WHEN the `DiagnosisResult.ContributingFactors` list contains findings, THE Investigator SHALL populate the `Recommendation` field of each contributing factor with an evidence-backed suggestion.

3. WHEN the phase is `Unknown`, THE Investigator SHALL populate `IncidentReport.Status.Summary` with at least one suggested manual investigation step derived from the available evidence (e.g., "Check container logs for application errors" when logs are available).

4. Recommendations SHALL be specific and actionable. Recommendations SHALL reference observed values where available (memory limit, restart count, PVC name, image name).

5. Recommendations SHALL NOT suggest automatic remediation. Recommendations SHALL NOT contain imperative commands that would modify the workload (e.g., "restart the deployment", "increase replica count").

6. Recommendations SHALL NOT exceed 512 characters per finding.

---

### Requirement 4: Populate kubectl Print Columns

**User Story:** As an engineer, I want `kubectl get incidentreports` to show meaningful columns so that I can triage multiple incidents at a glance.

#### Acceptance Criteria

1. THE `kubectl get incidentreports` output SHALL display the following columns: Workload name, workload Kind, Phase, Trigger type, primary Cause (short string, max 60 chars), and Age.

2. WHEN `DiagnosisResult.Primary` is non-nil, the Cause column SHALL display `DiagnosisResult.Primary.Cause` truncated to 60 characters.

3. WHEN `DiagnosisResult` is nil or `Primary` is nil, the Cause column SHALL display the trigger type as a fallback.

4. THE existing Workload, Kind, Phase, Trigger, Started, and Age columns defined in the foundation spec SHALL be preserved unchanged.

---

### Requirement 5: Render a Readable kubectl describe Output

**User Story:** As an engineer running `kubectl describe incidentreport`, I want a well-organized view of the incident with evidence, diagnosis, timeline, and recommendations clearly separated so that the report is legible without parsing raw YAML.

#### Acceptance Criteria

1. WHEN the `IncidentReport` is described via `kubectl describe`, THE status fields SHALL be rendered in the following logical order: Summary, Lifecycle metadata (StartedAt, ResolvedAt, FailureCount), Trigger, Workload, AffectedPods, Diagnosis (Primary finding + ContributingFactors), Timeline, Evidence summary (collection errors if any).

2. THE ordering of status fields SHALL be controlled by JSON field ordering in the CRD schema such that `kubectl describe` renders them in the order specified above.

3. THE `IncidentSummary` SHALL appear as the first user-visible status field when the report is described.

---

### Requirement 6: Maintain Storage Safety

**User Story:** As a Platform Engineer, I want the reporting additions to keep the IncidentReport well within Kubernetes etcd size limits so that reporting does not introduce object bloat.

#### Acceptance Criteria

1. THE `IncidentSummary` field SHALL NOT exceed 2048 characters.

2. Each `TimelineEvent.Message` SHALL NOT exceed 256 characters.

3. THE `Timeline` SHALL be bounded to `MaxTimelineEvents` entries (default 50, configurable).

4. THE `Recommendation` field in each `DiagnosisFinding` SHALL NOT exceed 512 characters.

5. WHEN any generated string exceeds its configured maximum length, THE Investigator SHALL truncate the string and append "…" to indicate truncation.

---

### Requirement 7: Keep Reporting Pure and Non-Destructive

**User Story:** As a Platform Engineer, I want the reporting layer to be a pure transformation of existing status data so that it adds no additional Kubernetes API calls, permissions, or failure modes.

#### Acceptance Criteria

1. THE `ReportingEngine` SHALL NOT call the Kubernetes API. It SHALL operate exclusively on the data already present in `IncidentReport.Status`.

2. THE `ReportingEngine` SHALL NOT require any new RBAC permissions beyond those already granted by the foundation and evidence-collection specifications.

3. WHEN `ReportingEngine` produces output that requires updating the `IncidentReport` status, THE controller SHALL write the update via a single status PATCH — the same mechanism used by all other status updates.

4. WHEN the `ReportingEngine` encounters an unexpected nil field that should have been populated, it SHALL produce a graceful partial output rather than panicking or returning an error.

---

### Requirement 8: Configuration for Reporting Limits

**User Story:** As a Platform Engineer, I want to tune the timeline event limit to match my operational needs.

#### Acceptance Criteria

1. THE Investigator SHALL expose `MaxTimelineEvents` as a configurable integer with a default of 50. Values of zero or less SHALL be rejected at startup.

2. WHEN `MaxTimelineEvents` is invalid, THE Investigator SHALL reject startup with a descriptive error.

---

## Out of Scope

The following are explicitly outside this specification:

- Slack, PagerDuty, or webhook notifications
- Web UI or Grafana dashboard integration
- Email or ticket system integration
- External reporting storage
- LLM-generated narrative summaries
- Automatic remediation
- Cross-incident trend analysis
- Historical incident comparison
