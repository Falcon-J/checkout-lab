# Learning roadmap and completion definition

Date: 2026-10-08.

## Milestones

| Milestone | Working deliverable | Explanation you must be able to give | Evidence |
| --- | --- | --- | --- |
| 1. Correct local checkout | Authenticated API and atomic stock reservation | Transactions, constraints, duplicate-key arbitration, owner-scoped reads | Real PostgreSQL concurrency and authorization tests |
| 2. Actual provider integration | Real sandbox hosted payment and signed webhook receipt | Why remote effects cannot join a local DB transaction; why a decline may be nonterminal | Sandbox checkout confirmation with correlated records |
| 3. Durable recovery | Claims, retry budgets, reconciliation, stock expiry, recovery refunds | Unknown outcomes, at-least-once execution, stale workers and compensation | Deterministic crash/expiry-race tests plus sandbox recovery |
| 4. Small usable product | Minimal authenticated checkout page and operator commands | Why redirects are not payment truth; how an operator resolves uncertain work | Browser walkthrough and integrity report |
| 5. Optional distribution lab | Separate API/worker processes; Kafka only for a stated experiment | Broker acknowledgement, duplicate effects, replay and ordering scope | Experiment contrasting baseline and broker behavior |
| 6. Optional operations lab | Redis or Kubernetes introduced individually | Cache staleness or rollout/probe/failure tradeoffs | Measured before/after or controlled deployment exercise |

Milestones 1-4 complete the first project. Milestones 5-6 are extensions, not prerequisites or resume claims.

## What done means

Documentation is done when scope, ownership, API responses, schema/transactions, state transitions, error classification, retry/recovery budgets, dependency choices, and implementation tasks are mutually consistent and traceable.

The application is done only after the runnable system meets the acceptance experiments in the design, including real sandbox integration. Local fake-provider results cannot close that gate.

Neither definition promises production uptime, multi-node guarantees, or interview offers. Demonstrating depth includes explaining the boundaries of evidence.

## Working method

For each slice: predict the behavior, write a meaningful failing test, implement the smallest behavior, run required checks, inspect persisted state, write a short experiment note, and commit. A learning note should explain the wrong approach and the failure that disproved it, not repeat library documentation.

Use a smaller official example for unfamiliar plumbing. Learn its contract before adapting it. Build new infrastructure only when an experiment needs it.

## Interview material to retain

For each serious failure, record five sentences: what could go wrong, which invariant it violates, how it was reproduced, why the chosen fix works, and what remains outside its guarantee. This is more useful than a list of technologies.
