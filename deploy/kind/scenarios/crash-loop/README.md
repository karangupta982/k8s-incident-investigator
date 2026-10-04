# Scenario: CrashLoopBackOff (Application Error)

## What it does
Deploys a container that always exits with code 1 after printing error messages.
Kubernetes retries until CrashLoopBackOff is entered.

## Expected result
- Phase: Diagnosed
- Primary finding: `CrashLoopAppError` (Medium confidence)
- Logs contain "ERROR: fatal startup failure"
