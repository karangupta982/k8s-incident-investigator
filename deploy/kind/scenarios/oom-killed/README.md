# Scenario: OOMKilled

## What it does
Deploys a container that deliberately allocates 200MB of memory against a 64Mi limit.
Kubernetes immediately OOMKills the container. The Incident Investigator detects this,
collects evidence (exit code 137, memory limit, node conditions), and diagnoses it.

## Expected result
- IncidentReport created in namespace `demo-oom-killed`
- Phase transitions: Investigating → Diagnosed
- Primary finding: `OOMMemoryLimit` (High confidence)
- Supporting evidence: "termination reason: OOMKilled", "exit code: 137", "memory limit: 64Mi"

## Run it
```bash
./deploy/kind/demo.sh oom-killed
```
