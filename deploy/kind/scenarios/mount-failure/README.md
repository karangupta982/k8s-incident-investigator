# Scenario: MountFailure (PVC Not Bound)

## What it does
Creates a PVC using a non-existent StorageClass provisioner, so the PVC stays
Pending. The workload cannot start because the volume cannot be mounted.

## Expected result
- Phase: Diagnosed
- Primary finding: `PVCNotBound` (High confidence)
- Evidence: PVC phase=Pending, StorageClass=fake-storage-investigator-demo
