# Implementation Plan: End-to-End Demo

## Overview

This plan creates the six scenario manifests, the demo walkthrough script, and the annotated documentation. All work is in `deploy/kind/` and `docs/demo/` — no application code changes. This spec should be implemented last, after all other specs are complete.

---

## Tasks

- [ ] 1. Create OOMKilled scenario
  - Create `deploy/kind/scenarios/oom-killed/namespace.yaml` with `Namespace: demo-oom-killed`
  - Create `deploy/kind/scenarios/oom-killed/deployment.yaml` — Deployment with `polinux/stress` container requesting 200M but limited to 64Mi
  - Create `deploy/kind/scenarios/oom-killed/README.md` — one paragraph describing what the scenario does and what to expect
  - _Requirements: 1.1, 1.2, 1.3, 1.4_

- [ ] 2. Create CrashLoopBackOff scenario
  - Create `deploy/kind/scenarios/crash-loop/namespace.yaml`
  - Create `deploy/kind/scenarios/crash-loop/deployment.yaml` — busybox container that prints an error and exits 1 after 2 seconds
  - Create `deploy/kind/scenarios/crash-loop/README.md`
  - _Requirements: 1.1_

- [ ] 3. Create ImagePullBackOff scenario
  - Create `deploy/kind/scenarios/image-pull-backoff/namespace.yaml`
  - Create `deploy/kind/scenarios/image-pull-backoff/deployment.yaml` — Deployment with a deliberately invalid image tag (`this-image-does-not-exist:v0.0.0`)
  - Create `deploy/kind/scenarios/image-pull-backoff/README.md`
  - _Requirements: 1.1_

- [ ] 4. Create MountFailure scenario
  - Create `deploy/kind/scenarios/mount-failure/namespace.yaml`
  - Create `deploy/kind/scenarios/mount-failure/storageclass.yaml` — StorageClass with `provisioner: fake.provisioner.does.not.exist/k8s` so PVC stays Pending
  - Create `deploy/kind/scenarios/mount-failure/pvc.yaml` — PVC requesting 1Gi using the fake StorageClass
  - Create `deploy/kind/scenarios/mount-failure/deployment.yaml` — nginx Deployment mounting the PVC
  - Create `deploy/kind/scenarios/mount-failure/README.md`
  - _Requirements: 1.1_

- [ ] 5. Create SchedulingFailure scenario
  - Create `deploy/kind/scenarios/scheduling-failure/namespace.yaml`
  - Create `deploy/kind/scenarios/scheduling-failure/deployment.yaml` — Deployment requesting 999 CPU (unschedulable on any Kind node)
  - Create `deploy/kind/scenarios/scheduling-failure/README.md`
  - _Requirements: 1.1_

- [ ] 6. Create ReadinessProbeFailure scenario
  - Create `deploy/kind/scenarios/readiness-probe-failure/namespace.yaml`
  - Create `deploy/kind/scenarios/readiness-probe-failure/deployment.yaml` — nginx:alpine with a readiness probe pointing to port 8080 (nginx serves on 80, so probe always fails)
  - Create `deploy/kind/scenarios/readiness-probe-failure/README.md`
  - _Requirements: 1.1_

- [ ] 7. Create demo.sh walkthrough script
  - Create `deploy/kind/demo.sh` (executable)
  - Implement scenario selection via argument, `--all` flag, `--no-cleanup` flag
  - Implement polling for IncidentReport creation (up to 120s)
  - Implement polling for phase != Investigating (up to 10 minutes)
  - Print `kubectl describe incidentreports` output after investigation completes
  - Print a human-readable summary of what was found (Phase, Primary Cause if any, Timeline count)
  - Clean up deployed resources on exit unless `--no-cleanup` is passed
  - Print clear progress messages at each stage
  - _Requirements: 2.1, 2.2, 2.3, 4.3_

- [ ] 8. Create annotated documentation
  - Create `docs/demo/scenarios.md`
  - For each of the 6 scenarios, document: what it triggers, what the engineer should observe, annotated sample `kubectl describe` output showing Summary, Diagnosis, Timeline sections with inline comments
  - The sample output should be realistic (hand-crafted based on the known behavior of the diagnosis rules for each trigger type)
  - _Requirements: 3.1, 3.2_

- [ ] 9. Final checkpoint
  - Run `kubectl apply --dry-run=client -f deploy/kind/scenarios/<each>/` to validate all manifests are syntactically valid
  - Verify `demo.sh` is executable (`chmod +x`)
  - Verify all scenario directories contain at minimum `namespace.yaml`, a workload manifest, and `README.md`

---

## Notes

- The `oom-killed` scenario reuses the existing `deploy/kind/test-workload/oom-crasher.yaml` pattern — the new scenario directory can be a superset of that with namespace isolation
- `ReadinessProbeFailure` needs the failure threshold in the `Config` to be reached — with the default threshold of 3 and a 5-second period, the IR should appear within ~20 seconds
- The demo script should handle the stability period gracefully — for quick demos, operators can pass `--stability-period 30s` to the controller flags in `setup.sh`
- `docs/demo/scenarios.md` will need to be updated each time the reporting output format changes — annotate this clearly in the doc header

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1", "2", "3", "4", "5", "6"] },
    { "id": 1, "tasks": ["7"] },
    { "id": 2, "tasks": ["8"] },
    { "id": 3, "tasks": ["9"] }
  ]
}
```
