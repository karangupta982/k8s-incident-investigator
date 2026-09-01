# Design Document: End-to-End Demo

## Overview

This spec builds out `deploy/kind/` with six realistic failure scenarios, a guided demo script, and documentation showing expected output. It does not add any application code — it is entirely manifests, shell scripts, and documentation.

---

## Scenario Directory Structure

```
deploy/kind/
├── kind-config.yaml            (existing)
├── setup.sh                    (existing)
├── validate.sh                 (existing)
├── demo.sh                     (new — guided walkthrough)
└── scenarios/
    ├── oom-killed/
    │   ├── namespace.yaml
    │   ├── deployment.yaml     (stress tool, 64Mi limit)
    │   └── README.md
    ├── crash-loop/
    │   ├── namespace.yaml
    │   ├── deployment.yaml     (exits with code 1 after 2s)
    │   └── README.md
    ├── image-pull-backoff/
    │   ├── namespace.yaml
    │   ├── deployment.yaml     (invalid image tag)
    │   └── README.md
    ├── mount-failure/
    │   ├── namespace.yaml
    │   ├── storageclass.yaml   (non-existent provisioner so PVC stays Pending)
    │   ├── pvc.yaml
    │   ├── deployment.yaml     (mounts the PVC)
    │   └── README.md
    ├── scheduling-failure/
    │   ├── namespace.yaml
    │   ├── deployment.yaml     (requests 999 CPU — unschedulable)
    │   └── README.md
    └── readiness-probe-failure/
        ├── namespace.yaml
        ├── deployment.yaml     (nginx serving 503 on /healthz)
        └── README.md
```

---

## demo.sh Design

```bash
#!/usr/bin/env bash
# demo.sh — Guided Incident Investigator walkthrough
# Usage: ./deploy/kind/demo.sh <scenario> [--no-cleanup] [--all]

SCENARIO="${1:-oom-killed}"
SCENARIOS="oom-killed crash-loop image-pull-backoff mount-failure scheduling-failure readiness-probe-failure"

run_scenario() {
    local scenario="$1"
    local ns="demo-${scenario}"

    echo "==> Deploying scenario: ${scenario}"
    kubectl apply -f "deploy/kind/scenarios/${scenario}/"

    echo "==> Waiting for IncidentReport (up to 120s)..."
    # Poll for an IR
    for i in $(seq 1 40); do
        if kubectl get incidentreports -n "${ns}" --no-headers 2>/dev/null | grep -q ""; then
            break
        fi
        sleep 3
    done

    echo "==> Waiting for investigation to complete (phase != Investigating, up to 10 min)..."
    for i in $(seq 1 60); do
        phase=$(kubectl get incidentreports -n "${ns}" -o jsonpath='{.items[0].status.phase}' 2>/dev/null)
        [[ "$phase" != "Investigating" && -n "$phase" ]] && break
        sleep 10
    done

    echo ""
    echo "==> Final IncidentReport:"
    echo "============================================================"
    kubectl describe incidentreports -n "${ns}"
    echo "============================================================"
}
```

---

## Scenario Manifests Design

### OOMKilled

```yaml
# deployment.yaml
containers:
- name: crasher
  image: polinux/stress
  args: ["--vm", "1", "--vm-bytes", "200M", "--vm-hang", "0"]
  resources:
    limits:
      memory: "64Mi"
```

### CrashLoop

```yaml
# deployment.yaml — exits with code 1 after printing an error
containers:
- name: crasher
  image: busybox
  command: ["/bin/sh", "-c"]
  args: ["echo 'ERROR: fatal startup failure' && sleep 2 && exit 1"]
  resources:
    limits:
      memory: "64Mi"
```

### MountFailure — uses a fake StorageClass

```yaml
# storageclass.yaml
provisioner: fake.provisioner.does.not.exist/k8s
volumeBindingMode: Immediate
# PVC will stay Pending → FailedMount events
```

### SchedulingFailure

```yaml
containers:
- name: app
  image: nginx:alpine
  resources:
    requests:
      cpu: "999"    # No node can satisfy this
```

### ReadinessProbeFailure

```yaml
containers:
- name: app
  image: nginx:alpine
  readinessProbe:
    httpGet:
      path: /healthz
      port: 8080    # nginx not listening on 8080 → probe fails
    failureThreshold: 3
    periodSeconds: 5
```

---

## Documentation Design

`docs/demo/scenarios.md` contains one section per scenario with:
1. What the scenario does and why it triggers the failure
2. What the engineer should expect to see in `kubectl describe`
3. Annotated sample output showing Summary, Timeline, Diagnosis, and Recommendations

---

## Testing

The demo is not run in CI (stability periods make it too slow). The `validate.sh` (already implemented) covers the OOMKilled scenario with automated assertions. The demo scenarios are validated by the existence of their manifest files in CI via a lint check.
