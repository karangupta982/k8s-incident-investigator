# Demo Scenarios — Annotated Expected Output

This document shows what a complete `kubectl describe incidentreport` looks like
for each supported failure type. Use it to understand what the Incident Investigator
produces and what each field means.

> **Note:** Output is manually curated from known investigation results.
> Exact timestamps and pod names will differ in your environment.

---

## Running the demos

```bash
# Deploy controller first
./deploy/kind/setup.sh

# Run a specific scenario
./deploy/kind/demo.sh oom-killed

# Run all scenarios sequentially
./deploy/kind/demo.sh --all

# Keep resources after the demo for manual inspection
./deploy/kind/demo.sh oom-killed --no-cleanup
```

---

## Scenario 1: OOMKilled

**Trigger:** Container allocates 200MB against a 64Mi memory limit.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `OOMMemoryLimit` (High confidence)

### Sample `kubectl describe` output

```
Name:     oom-crasher-deployment-active
...
Status:
  Phase:   Diagnosed
  Summary: |
    Workload: Deployment/demo-oom-killed/oom-crasher
    Phase: Diagnosed (Confidence: High)
    Cause: Container exceeded its configured memory limit
    Trigger: OOMKilled
    Affected Pods: 1
    Started: 2026-01-01T14:02:00Z

  Diagnosis:
    Primary:
      Rule ID:    OOMMemoryLimit
      Confidence: High
      Cause:      Container exceeded its configured memory limit
      Explanation: Container "crasher" was terminated with OOMKilled (exit code 137).
                   The container has a memory limit of 64Mi. The node did not report
                   MemoryPressure, indicating the OOM kill was caused by the container
                   exceeding its own limit.
      Supporting Evidence:
        - termination reason: OOMKilled
        - exit code: 137
        - memory limit: 64Mi
        - node did not report MemoryPressure
      Recommendation: Container "crasher" was OOMKilled with a memory limit of 64Mi.
                      Investigate application memory consumption and consider increasing
                      the memory limit if the workload legitimately requires more memory.

  Timeline:
    14:02:00  controller  IncidentDetected  Incident detected and IncidentReport created.
    14:02:08  container   OOMKilled         Container "crasher" terminated with reason
                                            OOMKilled (exit code 137, restart count 1).
    14:02:09  kubernetes-event  OOMKilling  ...
```

---

## Scenario 2: CrashLoopBackOff (Application Error)

**Trigger:** Container always exits with code 1.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `CrashLoopAppError` (Medium confidence)

### What to look for

```
Diagnosis:
  Primary:
    Rule ID:    CrashLoopAppError
    Confidence: Medium
    Cause:      Container is crash-looping due to a non-OOM application failure
    Supporting Evidence:
      - waiting reason: CrashLoopBackOff
      - last exit code: 1
      - restart count: 3
    Recommendation: Container "crasher" has restarted 3 times with exit code 1.
                    Examine container logs for application errors immediately
                    preceding the crash.
```

The logs will contain the application error message:
```
Logs:
  Container: crasher (current)
    ERROR: fatal startup failure — missing required configuration
    Failed to connect to database: connection refused
```

---

## Scenario 3: ImagePullBackOff

**Trigger:** Image `this-image-does-not-exist-investigator-demo:v0.0.0-invalid` does not exist.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `ImagePullFailure` (High confidence)

### What to look for

```
Diagnosis:
  Primary:
    Rule ID:    ImagePullFailure
    Confidence: High
    Cause:      Container image could not be pulled
    Supporting Evidence:
      - container: app
      - image: this-image-does-not-exist-investigator-demo:v0.0.0-invalid
      - waiting reason: ImagePullBackOff
    Recommendation: Image "this-image-does-not-exist-investigator-demo:v0.0.0-invalid"
                    could not be pulled. Verify the image name, tag, and registry are
                    correct. No imagePullSecrets are configured...
```

> Note: No previous logs are collected for ImagePullBackOff — the container
> never started. `ContainerLogEvidence.UnavailableReason` will explain this.

---

## Scenario 4: MountFailure (PVC Not Bound)

**Trigger:** PVC uses a fake StorageClass with a non-existent provisioner.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `PVCNotBound` (High confidence)

### What to look for

```
Diagnosis:
  Primary:
    Rule ID:    PVCNotBound
    Confidence: High
    Cause:      PersistentVolumeClaim is not bound to a PersistentVolume
    Supporting Evidence:
      - pvc: demo-data
      - pvc phase: Pending
      - storage class: fake-storage-investigator-demo
    Recommendation: PVC "demo-data" is in phase "Pending" using StorageClass
                    "fake-storage-investigator-demo". Verify the provisioner is running
                    and healthy...

Dependencies:
  PVCs:
    Name:            demo-data
    Phase:           Pending
    Storage Class:   fake-storage-investigator-demo
    Requested:       1Gi
    Access Modes:    ReadWriteOnce
```

---

## Scenario 5: SchedulingFailure

**Trigger:** Pod requests 999 CPUs — no Kind node can satisfy this.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `SchedulingFailure` (High confidence)

### What to look for

```
Diagnosis:
  Primary:
    Rule ID:    SchedulingFailure
    Confidence: High
    Cause:      Pod could not be scheduled to any available node
    Supporting Evidence:
      - 0/1 nodes are available: 1 Insufficient cpu.
      - resource request cpu: 999
    Recommendation: Inspect node availability: verify nodes have sufficient
                    available capacity for the resource requests.

Dependencies:
  Scheduling Constraints:
    Resource Requests:
      cpu: "999"
      memory: 64Mi
```

---

## Scenario 6: ReadinessProbeFailure

**Trigger:** Readiness probe checks port 8080 but nginx serves on port 80.

**Expected phase after investigation:** `Diagnosed`

**Expected primary finding:** `ProbeFailure` (Medium confidence)

> Note: Takes ~15 seconds for the probe to fail 3 times before Kubernetes
> emits Unhealthy events and the threshold is crossed.

### What to look for

```
Diagnosis:
  Primary:
    Rule ID:    ProbeFailure
    Confidence: Medium
    Cause:      Container readiness probe is failing repeatedly
    Supporting Evidence:
      - trigger type: ReadinessProbeFailure
      - probe type: HTTPGet (failure threshold: 3)
      - probe path: /healthz:8080
      - Readiness probe failed: Get "http://....:8080/healthz": dial tcp ...
    Recommendation: Verify the application is healthy and the readiness probe
                    at /healthz:8080 is accessible. Review probe failureThreshold=3...
```

---

## Understanding Phase Transitions

```
Pod failure detected
        ↓
  Phase: Investigating   ← IncidentReport created, trigger recorded
        ↓
  Phase: Investigating   ← Evidence collected (Pod, Events, Node, Logs, Dependencies)
        ↓
  Phase: Investigated    ← Correlation step derives causal signals
        ↓
  Phase: Diagnosed       ← A diagnosis rule matched the evidence
    OR
  Phase: Unknown         ← No rule matched (evidence insufficient or unknown failure)
        ↓
  Phase: Resolved        ← Workload healthy for stability period (default 5 minutes)
```

The `Unknown` phase is not an error — it means the system collected evidence
but could not match it to a known failure pattern. The evidence and timeline
are still available for manual investigation.
