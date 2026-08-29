# Context Capture and Memory Routing

Use this policy during normal Kiro conversations in this repository.

The user should not need to say "save this to memory."

## Core rule

When the user provides new information, first determine whether it is durable and useful for future work.

If it is durable, route it to the smallest correct Serena memory.

If it is temporary, trivial, uncertain, duplicated, or easily rediscovered, do not store it.

## Routing

### Current branch/task memory

Store in `tasks/<sanitized-current-branch>` when the information applies mainly to the current issue/task, including:

- issue/Jira requirements
- maintainer or reviewer clarification
- task-specific constraints
- confirmed design decisions
- rejected approaches worth remembering
- blockers
- implementation progress
- test results that affect task state
- next steps

Before updating task memory:

1. identify the current Git branch
2. verify the existing task memory matches the current task
3. if task identity is ambiguous, ask the user
4. update the existing task memory instead of creating duplicates

### Project-wide memory

Store in an appropriate project memory only when the information is reusable across future tasks in this repository, including:

- architecture facts
- project-wide conventions
- recurring commands/workflows
- contribution rules
- durable repository gotchas
- reusable debugging knowledge

If it is unclear whether information is task-specific or project-wide, prefer task-specific storage or ask the user before promoting it to project-wide memory.

### Do not store

Do not persist:

- raw chat
- greetings
- temporary ideas
- speculative conclusions
- private reasoning
- large logs
- complete code explanations that are easy to re-read
- duplicate information
- stale information
- secrets, credentials, tokens, keys, or sensitive authentication data

## Recommendations are not decisions

Keep these separate:

- `Confirmed Constraints`
- `Decisions`
- `Recommendations`
- `Open Questions`

A Kiro recommendation or proposed default remains a recommendation until explicitly confirmed by the user or an authoritative source.

Do not silently promote a recommendation into a decision.

## User corrections

If the user corrects previously stored information:

1. treat the correction as higher-priority evidence of intent
2. update or remove the stale memory
3. do not preserve contradictory obsolete conclusions unless historical context is genuinely useful

## Normal-chat behavior

When durable information is supplied during ordinary conversation:

1. answer the user's request normally
2. update the appropriate Serena memory when the destination is clear
3. keep the memory update concise
4. do not interrupt the user merely to ask permission to store clearly task-specific durable information
5. ask only when the correct destination or meaning is materially ambiguous

Do not announce every memory write unless it is useful to the user.

## Information from links/issues

When the user provides a GitHub issue, Jira issue, documentation link, maintainer comment, or other authoritative task information:

- extract only durable conclusions relevant to the task
- update the current branch task memory
- preserve important source identifiers or links when useful for later verification
- do not copy entire issue/comment bodies into memory

## Memory quality check

Before writing, ask:

> Will this materially reduce future rediscovery effort?

Before project-wide storage, also ask:

> Is this likely to matter outside the current task?

If no, keep it task-specific or do not store it.

## Source of truth

Authoritative current sources and verified repository state outrank Serena memory.

If memory conflicts with authoritative information, correct the memory.
