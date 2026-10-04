# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| v0.1.x  | ✅ Active  |

## Reporting a Vulnerability

If you discover a security vulnerability, please **do not** open a public GitHub issue.

Send an email to: **security@karangupta.dev** (or open a [GitHub Security Advisory](https://github.com/karangupta982/k8s-incident-investigator/security/advisories/new))

Include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes

You can expect an acknowledgment within 72 hours and a resolution timeline within 14 days for critical issues.

## Security Design

The controller is designed with security as a first-class concern:

- **Read-only RBAC**: The controller only requires `get`, `list`, and `watch` on all resources except `incidentreports` (which it manages) and `pods/log` (streaming only).
- **No Secret access**: Secret values are never read, stored, or logged. Only Secret names referenced in Pod specs are recorded.
- **Non-root container**: The controller image runs as UID 65532 with a read-only root filesystem.
- **No network egress**: The controller only communicates with the Kubernetes API server. No external calls are made.
- **Distroless runtime image**: The final image contains only the static binary — no shell, package manager, or OS utilities.
- **No workload modification**: The controller holds no write permissions on workload resources (Pods, Deployments, Services, etc.).

## Known Limitations

- Container logs are collected and stored in the `IncidentReport` status (in etcd). If container logs contain sensitive data, operators should configure `--max-log-lines 0` to disable log collection.
- The `IncidentReport` CRD stores evidence in etcd. In clusters with strict etcd access controls, consider scoping RBAC access to `incidentreports` appropriately.
