# Spec Decision Policy

Use this policy whenever deciding whether a task should use a Kiro Spec.

The goal is to gain structure only when it adds value. Do not create Specs mechanically for every issue.

## First principle

A Spec is planning structure, not durable memory.

- Serena memory = durable project/task context across sessions
- Kiro Spec = structured requirements/design/tasks for a particular implementation

Do not duplicate large amounts of Serena memory into Specs.

## Never create a Spec before task identity is confirmed

Before recommending or starting a Spec:

1. confirm current branch/task identity
2. resolve material branch/history conflicts
3. read the current branch task memory
4. distinguish confirmed decisions from recommendations/open questions

If task identity is ambiguous, ask the user first.

## Choose NO SPEC when

Use normal Kiro chat + Serena task memory when most of these are true:

- task is localized
- requirements are clear
- implementation path is obvious
- one or few components are involved
- design choices are minor
- work is likely to finish in one short session
- regression risk is low/moderate and straightforward to test

Do not create a Spec just because a GitHub/Jira issue exists.

## Choose BUG FIX when

Recommend Kiro's native Bug Fix workflow when:

- the task is genuinely a defect
- root cause needs structured investigation
- regression prevention matters
- the bug touches a critical path
- previous fixes failed or caused regressions
- expected and unchanged behavior need to be made explicit

Bug Fix is preferred over Feature Spec for complex bugs.

## Choose QUICK SPEC when

Recommend Quick Spec when:

- this is a feature/change rather than a bug
- requirements are already well understood
- implementation spans multiple components
- structure/tasks would help
- explicit approval gates between requirements/design/tasks are unnecessary
- speed matters more than iterative planning

Do not use Quick Spec when major design questions remain unresolved.

## Choose FEATURE SPEC — REQUIREMENTS FIRST when

Recommend standard Feature Spec, Requirements-First, when:

- desired behavior is clearer than implementation
- requirements need review or iteration
- acceptance criteria need to be explicit
- architecture is still flexible
- user/product behavior drives the change

## Choose FEATURE SPEC — DESIGN FIRST when

Recommend standard Feature Spec, Design-First, when:

- technical architecture/constraints drive the work
- an existing design already exists
- infrastructure/platform constraints dominate
- feasibility or non-functional constraints matter heavily
- the technical approach needs to shape requirements

## Open questions

A recommendation is not a decision.

If unresolved design questions materially affect the Spec:

- keep them under `Open Questions`
- do not silently choose defaults
- ask the user before treating a recommendation as confirmed
- do not put unconfirmed recommendations into `Confirmed Constraints` or `Decisions`

A Spec may document unresolved alternatives when appropriate, but must not pretend they are settled.

## Spec recommendation format

When recommending a Spec, report only:

- Spec needed: yes/no
- Recommended workflow
- Why, in 1-3 concise reasons
- Blocking open questions, if any

Avoid long generic explanations.

## After Spec creation

When official Kiro Spec artifacts exist under `.kiro/specs/`:

1. task memory should record only:
   - Spec name/path
   - important confirmed decisions
   - current implementation status
   - blockers / next steps
2. do not duplicate the full requirements/design/tasks into Serena
3. the Spec is the source of truth for implementation plan
4. Serena remains the source of truth for durable task continuity

## During implementation

If a confirmed decision changes:

1. update the relevant Spec artifact when it affects requirements/design/tasks
2. update concise Serena task memory
3. do not leave contradictory stale decisions

## Core rule

Use the lightest planning mechanism that safely supports the task:

normal chat < Quick Spec < standard Feature/Bug Fix workflow

More structure is not automatically better.
