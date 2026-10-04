# Kubernetes Incident Investigator Helm Chart

Kubernetes-native automated workload failure investigation controller. Detects failures, collects evidence from multiple sources, correlates the evidence, runs deterministic diagnosis rules, and produces structured `IncidentReport` custom resources viewable with kubectl.

## Prerequisites

- Kubernetes 1.28+
- Helm 3.8+
- kubectl

## Installation

```bash
helm install incident-investigator \
  oci://ghcr.io/karangupta982/charts/k8s-incident-investigator \
  --version 0.1.0 \
  --namespace incident-investigator-system \
  --create-namespace
```

Verify:

```bash
kubectl get pods -n incident-investigator-system
kubectl get incidentreports -A
```

## Configuration

Key values:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of controller replicas | `1` |
| `leaderElection.enabled` | Enable leader election for HA | `true` |
| `watchNamespaces` | Namespaces to watch (empty = all) | `""` |
| `stabilityPeriod` | How long workload must be healthy before resolving incident | `5m` |
| `evidence.maxLogBytes` | Max bytes per container log excerpt | `32768` |
| `evidence.maxLogLines` | Max lines per container log excerpt (0 disables log collection) | `200` |
| `evidence.maxEvents` | Max Kubernetes Events stored per incident | `25` |
| `resources.requests.cpu` | CPU request | `50m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |

Example with custom values:

```bash
helm install incident-investigator \
  oci://ghcr.io/karangupta982/charts/k8s-incident-investigator \
  --version 0.1.0 \
  --namespace incident-investigator-system \
  --create-namespace \
  --set evidence.maxLogLines=0 \
  --set-string 'watchNamespaces=production\,staging'
```

## Uninstallation

```bash
helm uninstall incident-investigator \
  --namespace incident-investigator-system
```

Helm intentionally retains the IncidentReport CRD during uninstall.

To completely remove the CRD and all stored IncidentReport resources:

```bash
kubectl delete crd incidentreports.investigation.k8s.io
```

Warning: Deleting the CRD permanently deletes all IncidentReport resources across all namespaces.

If the incident-investigator-system namespace was created only for this controller and contains no other resources, it can also be removed:

```bash
kubectl delete namespace incident-investigator-system
```

Do not automatically delete the namespace as part of the normal uninstall flow.

## Documentation

- [Main project README](https://github.com/karangupta982/k8s-incident-investigator)
- [Architecture](https://github.com/karangupta982/k8s-incident-investigator/blob/main/docs/architecture.html)
- [Contributing guide](https://github.com/karangupta982/k8s-incident-investigator/blob/main/CONTRIBUTING.md)
- [Security policy](https://github.com/karangupta982/k8s-incident-investigator/blob/main/SECURITY.md)

## Support

- [GitHub Issues](https://github.com/karangupta982/k8s-incident-investigator/issues)
- [Security vulnerabilities](https://github.com/karangupta982/k8s-incident-investigator/security/advisories/new)

## License

Apache 2.0 — see [LICENSE](https://github.com/karangupta982/k8s-incident-investigator/blob/main/LICENSE).
