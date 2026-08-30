# 1. Everything starts normally

Suppose the cluster has:

```text
Deployment: payment-api
Pod: payment-api-7d8f9
Container: payment-api
Memory limit: 512Mi
```

The Investigator is already running in the cluster.

Its controller has registered watches with the Kubernetes API through `controller-runtime`.

Conceptually:

```text
Kubernetes API Server
        ↑
        │ watch
        │
Incident Investigator
```

The Investigator is **not continuously asking Kubernetes**:

> "Anything changed?"

Instead, it establishes watches and Kubernetes sends events when watched resources change.

---

# 2. Application consumes too much memory

The application starts using more memory:

```text
300Mi
400Mi
500Mi
512Mi
```

Eventually it exceeds its allowed memory.

The container is terminated by the kernel/cgroup mechanism.

Kubernetes then observes the container termination and updates the Pod's state.

For example, the Pod may now contain information equivalent to:

```text
containerStatuses:
  restartCount: 1

  lastState:
    terminated:
      reason: OOMKilled
      exitCode: 137
```

This is important:

**Kubernetes isn't calling our Investigator and saying:**

> "Hey, this is OOMKilled, investigate it."

Instead, Kubernetes updates the Pod object in the API.

That change is what our controller observes.

---

# 3. Kubernetes API Server receives the updated Pod state

The kubelet communicates the container state to the Kubernetes control plane.

Eventually the Pod object stored by the API Server reflects the new state.

Conceptually:

```text
Container dies
      ↓
kubelet observes it
      ↓
Pod status updated
      ↓
API Server
      ↓
Pod object changes
```

Now the Investigator's watch becomes relevant.

---

# 4. Our controller receives the watch event

The controller-runtime watch sees that the Pod changed.

It doesn't directly execute our investigation at this exact moment.

Instead, it puts a reconciliation request into its work queue.

Conceptually:

```text
Pod changed
   ↓
Watch handler
   ↓
Work Queue
   ↓
Reconcile request
```

This distinction is important.

There isn't a magical:

```text
Kubernetes → call Reconcile()
```

relationship.

It's more like:

```text
Kubernetes event
      ↓
controller-runtime observes it
      ↓
request enters queue
      ↓
worker takes request
      ↓
Reconcile()
```

---

# 5. `Reconcile()` starts

Our controller receives something like:

```text
Namespace: production
Name: payment-api-7d8f9
```

The first thing it should do is **not start diagnosing OOMKilled**.

It first asks:

> "Is this event relevant to my investigator?"

It retrieves the current Pod state from the Kubernetes API/cache.

It sees:

```text
restartCount increased
lastState.reason = OOMKilled
```

Now the trigger policy says:

```text
OOMKilled
→ immediate incident trigger
```

So this is worthy of investigation.

---

# 6. Does an active IncidentReport already exist?

This is where our incident manager comes in.

It asks:

> "Does `Deployment/payment-api` already have an active incident?"

### Case A — No

Then we create:

```text
IncidentReport/payment-api-20260810-001
```

Something conceptually like:

```yaml
kind: IncidentReport

metadata:
  name: payment-api-20260810-001

spec:
  workload:
    kind: Deployment
    name: payment-api

status:
  phase: Investigating

  startedAt: ...

  affectedPods:
    - payment-api-7d8f9
```

At this point, **we have not finished investigating**.

We've simply created the persistent case file.

---

# 7. Why create the report first?

Because the investigation may take time and the controller may restart.

Suppose we instead did:

```text
detect failure
   ↓
investigate everything
   ↓
create report
```

and the controller crashes halfway through.

We could lose our investigation state.

With our design:

```text
detect failure
   ↓
create IncidentReport
   ↓
investigate
```

the Kubernetes API stores:

```text
IncidentReport
phase: Investigating
```

If our controller crashes:

```text
Controller crashes
      ↓
Controller restarts
      ↓
Finds IncidentReport
      ↓
Continues investigation
```

That's a major reason for this design.

---

# 8. Now investigation begins

The investigation manager knows:

```text
Incident:
payment-api-20260810-001

Workload:
Deployment/payment-api

Trigger:
OOMKilled

Affected Pod:
payment-api-7d8f9
```

It now asks the evidence system:

> "What information do we need to understand this incident?"

Because this is an OOM-related incident, we don't blindly collect everything.

We collect relevant evidence.

---

# 9. Pod evidence is collected

The Pod collector examines:

```text
Pod phase
Container state
Restart count
Termination reason
Exit code
Memory request
Memory limit
Node
Probes
Volumes
OwnerReferences
```

We might get:

```text
Container:
payment-api

Restart count:
1

Termination reason:
OOMKilled

Exit code:
137

Memory limit:
512Mi

Node:
worker-2
```

This becomes structured evidence.

Not:

```text
"something something OOM"
```

but actual structured data that diagnosis rules can consume.

---

# 10. Events are collected

Next, we look at relevant Kubernetes Events.

For example:

```text
14:02:08
Container payment-api was terminated

14:02:09
Container payment-api restarted
```

Events provide additional context and timestamps.

Again, they become evidence.

---

# 11. Workload ownership is resolved

The Pod itself isn't our ultimate incident identity.

We follow its ownership:

```text
Pod
 ↓
ReplicaSet
 ↓
Deployment
```

So we establish:

```text
Affected workload:
Deployment/payment-api
```

This matters because the Pod might disappear and be replaced.

---

# 12. Node evidence is collected

The Pod was running on:

```text
worker-2
```

We inspect relevant node information.

Suppose:

```text
Ready: True
MemoryPressure: False
DiskPressure: False
PIDPressure: False
```

This is useful.

Why?

Because now we can distinguish:

```text
Container exceeded its own memory limit
```

from:

```text
Entire node was experiencing memory pressure
```

Both are possible scenarios.

---

# 13. Previous logs are collected

Because the container crashed, the Investigator attempts to retrieve previous logs.

Conceptually this is the same information an engineer would obtain with:

```bash
kubectl logs payment-api-7d8f9 --previous
```

Suppose we get:

```text
Processing batch 18291
Loading customer records
Processing batch 18292
...
```

Maybe there is no useful error.

That's okay.

The absence of a useful application error is itself relevant.

We store only a bounded excerpt, not the entire log stream.

---

# 14. Evidence is now assembled

We might have:

```text
Pod
 ├── OOMKilled
 ├── Exit code 137
 ├── Memory limit 512Mi
 └── Restart count 1

Events
 └── Container terminated

Workload
 └── Deployment/payment-api

Node
 ├── Ready
 └── MemoryPressure=False

Logs
 └── Previous container logs available
```

Now we have enough information to start reasoning.

---

# 15. Evidence correlation

The correlation layer asks:

> "Which pieces of evidence belong together?"

In this example:

```text
OOMKilled
+
exit code 137
+
memory limit 512Mi
+
container restart
+
same Pod
+
same timestamp
```

all clearly belong to the same failure.

The node information is also relevant because it tells us the node itself wasn't reporting memory pressure.

So our correlated incident context becomes:

```text
Container exceeded its configured memory limit
```

with supporting evidence.

---

# 16. Diagnosis engine runs

Now the diagnosis engine receives **structured evidence**.

For example:

```text
terminationReason = OOMKilled
exitCode = 137
memoryLimit = 512Mi
nodeMemoryPressure = false
```

The OOM diagnosis rule matches.

It produces something like:

```text
Cause:
Container exceeded memory limit

Confidence:
High

Evidence:
- OOMKilled
- Exit code 137
- Memory limit 512Mi
- Node MemoryPressure=False
```

Notice what it **doesn't** say:

```text
Cause:
Memory leak
```

We don't have enough evidence to claim that.

---

# 17. Recommendation is generated

Based on the diagnosis:

```text
Investigate application memory consumption.

Consider increasing the memory limit if the workload
legitimately requires additional memory.
```

Again, the Investigator doesn't change the Deployment.

It only reports.

---

# 18. IncidentReport gets updated

Now the previously-created report gets richer.

Conceptually:

```yaml
status:
  phase: Diagnosed

  affectedPods:
    - payment-api-7d8f9

  diagnosis:
    cause: Container exceeded memory limit
    confidence: High

  evidence:
    terminationReason: OOMKilled
    exitCode: 137
    memoryLimit: 512Mi
    nodeMemoryPressure: false

  recommendations:
    - Investigate application memory consumption
```

Now an engineer can run:

```bash
kubectl get incidentreports
```

and see something like:

```text
NAME                       WORKLOAD       STATUS      CAUSE
payment-api-20260810-001   payment-api    Diagnosed   OOMKilled
```

And:

```bash
kubectl describe incidentreport payment-api-20260810-001
```

can show the detailed investigation.

---

# 19. But is the incident resolved?

No.

This is another important distinction.

We diagnosed the problem:

```text
Cause identified
```

but the workload might still be unhealthy.

For example, the container might crash again.

So the IncidentReport remains active.

---

# 20. Another OOMKilled happens

Suppose:

```text
14:03
OOMKilled

14:04
OOMKilled

14:05
OOMKilled
```

The controller sees the Pod changes again.

Reconciliation happens again.

It checks:

> "Is there already an active incident for `Deployment/payment-api`?"

Yes.

Therefore:

```text
DO NOT create IncidentReport #2
```

Instead:

```text
Update IncidentReport #1
```

Add the new evidence/timeline information.

---

# 21. Pod gets replaced

Eventually the Deployment creates another Pod:

```text
payment-api-abc
```

The old Pod:

```text
payment-api-7d8f9
```

may disappear.

The new Pod becomes:

```text
payment-api-abc
```

If it continues suffering the same failure:

```text
Deployment/payment-api
 ├── Pod A → OOMKilled
 └── Pod B → OOMKilled
```

the same incident continues.

The report can show:

```text
Affected Pods:
payment-api-7d8f9
payment-api-abc
```

This is why our incident identity is the **workload**, not simply the Pod name.

---

# 22. Eventually the workload becomes healthy

Suppose the engineer changes the memory limit.

Now:

```text
Pod Running
Ready=True
restart count stable
no new failure signals
```

The Investigator notices this during reconciliation.

But it doesn't immediately say:

```text
Resolved!
```

We want a stability period.

For example:

```text
Healthy
   ↓
wait 5 minutes
   ↓
still healthy
   ↓
resolve incident
```

The exact period will be configurable.

---

# 23. Incident becomes Resolved

The report finally becomes:

```yaml
status:
  phase: Resolved

  startedAt: 14:02
  resolvedAt: 14:15

  diagnosis:
    cause: Container exceeded memory limit
    confidence: High
```

Now:

```bash
kubectl get incidentreports
```

could show:

```text
NAME                       WORKLOAD       STATUS      CAUSE
payment-api-20260810-001   payment-api    Resolved    OOMKilled
```

---

# 24. What happens if the same workload fails tomorrow?

Suppose:

```text
Today:
OOMKilled
→ Incident #1
→ Resolved

Tomorrow:
ImagePullBackOff
```

The Investigator sees:

```text
No active incident
```

and creates:

```text
Incident #2
```

So we get:

```text
Incident #1
OOMKilled
Resolved

Incident #2
ImagePullBackOff
Investigating
```

We **never overwrite historical incidents**.

That's important for debugging, auditing, and understanding recurring problems.

---

# Complete flow in one picture

```text
                    APPLICATION
                         │
                         ▼
                  Container fails
                    OOMKilled
                         │
                         ▼
                      kubelet
                         │
                         ▼
                 Pod status changes
                         │
                         ▼
                  Kubernetes API
                         │
                         │ watch event
                         ▼
               controller-runtime
                         │
                         ▼
                    Work Queue
                         │
                         ▼
                    Reconcile()
                         │
                         ▼
              Is signal meaningful?
                    │          │
                   No         Yes
                    │          │
                  return       ▼
                       Find active incident
                              │
                    ┌─────────┴─────────┐
                    │                   │
                  Exists             Doesn't exist
                    │                   │
                    ▼                   ▼
               Reuse it           Create report
                    │                   │
                    └─────────┬─────────┘
                              ▼
                    Evidence Collection
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
             Pod            Events           Node
              │               │               │
              ├───────────────┼───────────────┤
              ▼               ▼               ▼
          Workload          Logs          Dependencies
                              │
                              ▼
                     Evidence Correlation
                              │
                              ▼
                      Diagnosis Engine
                              │
                    ┌─────────┼─────────┐
                    ▼         ▼         ▼
                  Known     Partial   Unknown
                    │         │         │
                    └─────────┼─────────┘
                              ▼
                       Recommendations
                              │
                              ▼
                    Update IncidentReport
                              │
                              ▼
                       Monitor recovery
                              │
                    ┌─────────┴─────────┐
                    │                   │
                 Still failing        Healthy
                    │                   │
                    ▼                   ▼
               Reconcile again      Stability period
                                        │
                                        ▼
                                    Resolved
```
