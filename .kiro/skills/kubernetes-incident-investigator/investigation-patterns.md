# Kubernetes Investigation Patterns

This document contains known Kubernetes failure investigation patterns.

The patterns describe:

1. Trigger
2. Evidence to collect
3. Evidence relationships
4. Possible diagnosis
5. Confidence
6. Recommendation
7. Conditions that prevent a diagnosis

The diagnosis engine must use evidence rather than matching only on a single error string.

---

# OOMKilled

## Trigger

Container termination reason:

```text
OOMKilled
````

## Important Evidence

Collect:

* container termination reason
* exit code
* restart count
* memory limit
* memory request
* Pod events
* Node memory pressure
* previous logs
* memory metrics when available

## Possible Diagnosis

```text
Container exceeded its configured memory limit.
```

## Supporting Evidence

Strong evidence includes:

* `OOMKilled`
* exit code 137
* memory usage approaching the configured limit
* no evidence that the failure was caused by another infrastructure problem

## Important Distinction

Do not automatically claim:

```text
Memory leak
```

OOMKilled proves that the container was terminated due to memory exhaustion, but it does not by itself prove a memory leak.

## Recommendation

Investigate application memory consumption.

Consider increasing the memory limit if the workload legitimately requires more memory.

---

# CrashLoopBackOff

## Trigger

Container enters:

```text
CrashLoopBackOff
```

## Important Evidence

Collect:

* restart count
* current container state
* previous termination state
* exit code
* termination reason
* previous logs
* Pod events
* startup/liveness/readiness probes
* configuration references
* image information

## Important Principle

`CrashLoopBackOff` describes repeated container startup failure and backoff.

It is not itself the root cause.

The actual cause may be:

* application crash
* configuration failure
* missing dependency
* probe failure
* resource issue
* permission problem
* other application-level failure

Diagnosis should use additional evidence.

---

# ImagePullBackOff

## Trigger

Container enters:

```text
ImagePullBackOff
```

## Important Evidence

Collect:

* image name
* image tag/digest
* Pod events
* imagePullSecrets metadata/existence
* registry-related error messages
* node information

## Possible Causes

Examples:

* image does not exist
* incorrect tag
* registry authentication failure
* registry unavailable
* imagePullSecret missing
* registry/network failure

Do not automatically select one cause without supporting evidence.

---

# FailedMount

## Trigger

Repeated mount-related failure.

## Important Evidence

Collect:

* Pod volume configuration
* PVC
* PV
* StorageClass
* Pod events
* relevant CSI information when available

## Possible Causes

Examples:

* PVC does not exist
* PVC is Pending
* storage provisioning failure
* volume attachment failure
* permission/configuration issue

A single transient mount error should not necessarily create an incident.

---

# Probe Failures

## Readiness Probe

A single readiness failure should normally not create an incident.

Repeated failures may indicate:

* application unavailable
* dependency unavailable
* incorrect probe configuration
* resource pressure
* network/dependency problem

Collect:

* probe configuration
* event history
* container state
* application logs
* restart behavior
* service/endpoints where relevant

---

## Liveness Probe

Repeated liveness failures are more serious because they can cause Kubernetes to restart the container.

Collect:

* probe configuration
* failure timestamps
* restart count
* termination state
* logs
* events
* resource information

Do not assume the probe itself is misconfigured without evidence.

---

# Unknown Failure

If no known pattern matches:

```text
Diagnosis:
Unknown
```

The report should still contain:

* incident timeline
* collected evidence
* available logs
* affected workload
* relevant Kubernetes resources
* what was ruled out
* suggested manual investigation area

Never fabricate a root cause.