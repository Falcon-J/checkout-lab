# Documentation review record

Date: 2026-10-08. Scope: design and implementation readiness; no application runtime claims.

## Reading order

1. [Checkout design](checkout-design.md): product, architecture and invariants.
2. [API contract](api-contract.md): identity, endpoints, payloads and errors.
3. [Data model](data-model.md): tables, constraints, locks and transactions.
4. [Integration decisions](integration-decisions.md): provider, auth and libraries.
5. [Recovery runbook](recovery-runbook.md): budgets, races and operator actions.
6. [Engineering guide](engineering-guide.md): source ownership and conventions.
7. [Implementation plan](implementation-plan.md): executable task sequence.
8. [Learning roadmap](learning-roadmap.md): project and learning completion.

## Requirement traceability

| Requirement | Contract | Planned verification |
| --- | --- | --- |
| I1 stock cannot go negative | Data: product constraint and transactional hold | Task 2 TestLastItemConcurrency |
| I2 stable checkout identity | API fingerprint and original response; data unique key | Task 2 TestConcurrentKeyReplay / TestReplayAfterStockExhaustionAndPriceChange |
| I3 stable financial identity | Integration mutation windows and immutable request | Tasks 5-6 TestStripeRequestIdentity / TestMutationWindowExpired |
| I4 correct confirmation | Data ApplyObservation | Task 6 TestExpirySuccessRace |
| I5 single release/consume | Data reservation guards | Task 6 duplicate/expiry tests |
| I6 restart durability | Data atomic enqueue and recovery claim | Tasks 4/6 TestDurableRetryBudget / TestPaymentResponseLostAndRestart |
| I7 uncertainty is explicit | Integration observation mapping | Task 6 response-loss test |
| I8 callback deduplication | API durable receipt and data event key | Task 5 TestWebhookSignatureAndReceipt |
| I9 authorization | API customer identity and owner-scoped reads | Tasks 2/3 TestOwnerScopedRead / TestJWTValidation |
| I10 exact money | API limits and data constraints | Tasks 1/2 schema and arithmetic tests |
| Actual payment integration | Stripe hosted sandbox | Task 5 sandbox check and Task 8 acceptance gate |
| Operability | Recovery commands and integrity checks | Task 7 TestOpsRetryGuards / TestOpsIntegrity |
| Small reproducible build | Engineering/Compose/CI contract | Tasks 1/8 explicit command checks |

## Self-review corrections

- Kept the local 15-minute reservation policy separate from provider session lifetime.
- Specified unknown outcomes and late-success refund recovery instead of assuming timeout means failure.
- Added refund failed/canceled states and operator review without fabricated refund success.
- Separated create and refund identity windows; a late refund does not inherit the old create deadline.
- Made checkout replay return the original accepted response while GET returns current state.
- Made stock inspection occur after idempotency arbitration for the winning new order; existing retries do not fail on exhausted stock.
- Defined consistent lock ordering, claim-token guards, transient SQL handling and per-operation retry limits.
- Distinguished retained event IDs from removable processed payloads.
- Recorded live-mode/amount/currency correlation checks and real-account acceptance prerequisites.
- Guarded confirmed/refunded terminal orders against repeated success observations and required unexpired leases even before reclamation.
- Documented that Go build/vet/format/tests replace irrelevant yarn commands for this Go project.

## Verification boundaries

Documentation checks cover file existence, local link targets, Markdown fence balance, placeholders, whitespace, invariant traceability and planned test names. Official provider and library references were consulted on the document date.

No schema execution, Go compilation, lint/typecheck, application tests, account provisioning, payment, or hosted-authentication check has occurred. Those belong to the implementation plan and must be reported with fresh evidence.

## Execution status

The documentation package is complete for review. The application is not implemented. Library patch versions and image digests will be resolved and pinned in Task 1 rather than fabricated before package resolution. External credentials are execution prerequisites, not missing application-design decisions.
