# Project Memory Workflow

Use Serena as the durable project-memory system for this repository.

The goal is to preserve useful project and task context while keeping memory concise, branch-aware, accurate, and safe.

---

## Session start

Before substantial work:

1. Identify the current Git repository.

2. Determine the current Git branch with:

   ```bash
   git branch --show-current
   ```

3. List available Serena memories.

4. Read only memories relevant to the current request.

5. If a task memory exists for the current branch, read it before continuing that task.

6. Do not load all memories automatically.

7. Do not assume that the current branch name alone proves which task is being worked on.

Prefer selective retrieval over broad repository rescanning.

---

## Branch-aware task memory

Task-specific state must be isolated by Git branch.

Use Serena memory topic:

`tasks/<sanitized-branch-name>`

Examples:

* `fix/issue-123` → `tasks/fix-issue-123`
* `feature/auth-refresh` → `tasks/feature-auth-refresh`
* `feat/447-slow-query-log` → `tasks/feat-447-slow-query-log`

A task memory should contain only useful resumable state:

* current branch
* task/issue identifier
* task goal
* relevant files/components
* confirmed findings
* important decisions and reasons
* failed approaches worth remembering
* implementation status
* tests run and relevant results
* unresolved blockers
* next concrete steps

Never mix task state from another branch.

Do not create detailed task memory until the actual task identity has been established reliably.

---

## Validate task identity before creating memory

Never infer that existing commits on the current branch belong to the current task solely because they are reachable from `HEAD`.

A newly created branch may have been created from:

* another feature branch
* another bug-fix branch
* a stale local branch
* an unintended base commit

Before creating a new task memory:

1. Identify the current branch.
2. Determine the repository's normal base branch, such as `main` or `master`.
3. Inspect the branch base and relevant Git history.
4. Check whether existing commits may have been inherited from another branch.
5. Establish the actual task from a reliable source, such as:

   * explicit user context
   * GitHub issue
   * Jira issue
   * existing task memory
   * another authoritative project source
6. If task identity is ambiguous, do not guess or create detailed task conclusions.

Existing Git history is evidence, not proof of user intent.

---

## Ambiguity and confirmation

If there is meaningful ambiguity about:

* which issue/task is being worked on
* whether the current branch was created from the intended base branch
* whether existing commits belong to the current task
* which repository/project context applies
* whether existing memory belongs to the current task
* whether a Spec should represent one task or another
* whether a destructive or high-impact action is intended
* whether a discovered inconsistency is intentional
* any assumption that could materially change implementation or memory

do not guess.

Ask the user a concise clarification question before:

* creating task memory
* modifying source code based on the uncertain assumption
* creating a Spec
* recording a durable conclusion
* replacing existing durable memory
* taking a destructive or high-impact action

Safe read-only investigation may be used first if it can resolve the ambiguity without changing anything.

Never convert an uncertain inference into durable Serena memory.

When uncertainty remains, explicitly mark the information as unknown rather than selecting the most likely explanation.

---

## Recommendations vs confirmed decisions

Kiro must distinguish clearly between:

- confirmed user/maintainer decisions
- explicit constraints from authoritative sources
- Kiro recommendations
- proposed defaults
- unresolved design questions

Recommendations and proposed defaults are NOT decisions.

Until the user or an authoritative source confirms them:

- keep them under `Open Questions` or `Recommendations`
- do not store them under `Confirmed Constraints`
- do not store them under `Decisions`
- do not treat them as implementation requirements
- do not silently proceed using them as final choices

Example:

Bad:

`Decision: use PermJoin for promote`

when Kiro merely recommended it.

Good:

`Open Question: reuse PermJoin or introduce PermPromote?`
`Recommendation: reuse PermJoin because the existing join path already uses it.`

Only move an item into `Decisions` or `Confirmed Constraints` after it is explicitly confirmed.

---

## Reliable sources and evidence

Prefer evidence in roughly this order when determining task context:

1. explicit user instruction
2. linked/current GitHub or Jira issue
3. existing task memory confirmed for the current branch
4. repository documentation or authoritative project metadata
5. Git history and branch naming
6. inference from code

Do not treat branch names, commit messages, or inferred code behavior as stronger evidence than explicit user intent.

---

## Durable memory policy

Before writing memory, ask:

> Would rediscovering this information later cost meaningful time or context?

Store:

* architecture knowledge
* non-obvious project conventions
* useful commands
* testing workflows
* contribution workflows
* important entry points
* durable debugging discoveries
* decisions and tradeoffs
* failed approaches worth avoiding
* branch/task progress needed to resume work
* confirmed blockers
* important user corrections

Do not store:

* raw conversations
* greetings
* private chain-of-thought
* temporary speculation
* unverified assumptions
* large command output
* giant logs
* trivial facts obvious from source code
* complete source-code explanations when the source itself is sufficient
* duplicate information
* obsolete conclusions
* secrets
* passwords
* credentials
* access tokens
* AWS temporary credentials
* kubeconfig contents
* private keys
* temporary authentication values

Prefer updating an existing memory over creating a duplicate.

Keep memories compact, factual, and useful for future retrieval.

Follow Serena's `memory_maintenance` memory whenever creating or modifying Serena memories.

---

## Memory compression

Memory should preserve conclusions, not reproduce the entire implementation.

Prefer:

```text
Swap now preserves the original DB until replacement succeeds.
Failure restores the previous DB.
Regression tests were added.
```

Instead of storing a long line-by-line description of code that can easily be read from the repository.

Store enough information to restore context quickly, but rely on source code as the source of truth for implementation details.

---

## Task-memory lifecycle

### When starting a new task

1. Check the current branch.
2. Check whether a task memory already exists.
3. Establish the task identity.
4. Verify that the branch history is consistent with the intended task.
5. If anything important is ambiguous, ask the user.
6. Only then create the task memory.

Initial task memory should usually be small.

Example:

```md
# Task: fix/issue-123

## Branch

`fix/issue-123`

## Goal

Fix issue #123: <confirmed task summary>.

## Status

Investigation not started.

## Next Steps

- Read issue.
- Inspect relevant code.
- Determine root cause.
- Decide whether a Spec is warranted.
```

Do not pre-populate implementation conclusions that have not yet been established.

---

### While working

Update task memory when meaningful durable state changes, for example:

* root cause confirmed
* important design decision made
* approach rejected for a useful reason
* implementation substantially progressed
* blocker discovered
* relevant tests completed

Do not update memory after every small tool call or conversational message.

---

### When switching branches

Before continuing work:

1. detect the new branch
2. stop using the previous branch's task memory
3. look for the new branch's corresponding memory
4. load it only if relevant

Never carry unresolved task-specific assumptions automatically across branches.

---

## Before finishing meaningful work

If durable knowledge changed:

1. Update the relevant project memory.
2. Update the current branch's task memory.
3. Remove or correct stale conclusions.
4. Record unresolved blockers.
5. Record the next concrete steps.
6. Keep updates concise.

If nothing durable changed, do not write memory.

Do not create memory merely to record that a command was run unless its result matters for future work.

---

## Specs

Do not create a Kiro Spec automatically just because a task or issue exists.

Use a Spec when the work is sufficiently complex, such as:

* unclear requirements
* substantial investigation
* multiple interacting components
* important architectural decisions
* work expected to span multiple sessions
* implementation requiring a reliable task breakdown

For trivial or well-understood changes, normal task memory may be enough.

If it is unclear whether the task warrants a Spec, evaluate the complexity first.

If the task itself is ambiguous, clarify the task before creating the Spec.

---

## Read-only investigation

When context is unclear, Kiro may safely perform limited read-only investigation before asking the user.

Examples:

* inspect current branch
* inspect `git status`
* inspect Git history
* inspect existing Serena memory names
* inspect repository documentation
* inspect issue metadata if available
* search code
* inspect diffs

Read-only investigation should be targeted and should not become an excuse for broad unnecessary rescanning.

If read-only investigation still leaves meaningful ambiguity, ask the user.

---

## Git safety

Kiro may:

* inspect Git status
* inspect Git diff
* inspect branches
* inspect Git history
* inspect remotes
* compare branches
* create and edit working-tree files

Kiro must never:

* run `git add`
* run `git commit`
* run `git push`
* force push
* merge branches automatically
* create or submit pull requests automatically

The user performs staging, commits, pushes, merges, and PR creation manually.

Do not work around this restriction using another Git command, API, integration, or tool.

---

## Infrastructure safety

For infrastructure-related repositories, prefer read-only and validation operations.

Allowed when appropriate:

* inspect Terraform
* `terraform fmt`
* `terraform validate`
* `terraform plan`
* inspect Kubernetes resources
* inspect ArgoCD configuration
* inspect Jenkins configuration
* inspect deployment manifests

Do not perform mutating or destructive infrastructure operations unless the user explicitly requests and approves them.

Examples requiring explicit approval:

* `terraform apply`
* `terraform destroy`
* `kubectl apply`
* `kubectl delete`
* ArgoCD sync operations
* infrastructure resource deletion or replacement

---

## Secrets and sensitive data

Never write secrets into Serena memory, steering files, Specs, generated documentation, or task notes.

This includes:

* passwords
* API keys
* GitHub tokens
* AWS credentials
* temporary AWS SSO credentials
* kubeconfig credentials
* certificates/private keys
* session tokens
* database passwords
* internal secrets exposed in terminal output

If sensitive data appears during investigation, do not copy it into memory.

Record only non-sensitive conclusions when necessary.

---

## Source of truth

Use this hierarchy:

```text
Source code / authoritative project systems
        ↓
current verified repository state
        ↓
Serena memory
```

Memory is a compact navigation and continuity layer, not a replacement for the actual repository or issue tracker.

If memory conflicts with current verified project state:

1. verify the current state
2. treat current authoritative state as correct
3. update or remove stale memory

---

## Core principle

The system should optimize for:

> accurate context with the minimum necessary retrieval.

Therefore:

* do not guess
* do not load everything
* do not remember everything
* do not mix branches
* do not preserve stale conclusions
* do not turn uncertain assumptions into memory
* ask the user when intent matters
* preserve only knowledge that meaningfully reduces future rediscovery
