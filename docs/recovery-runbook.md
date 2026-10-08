# Recovery and operating contract

Date: 2026-10-08. These settings are initial learning baselines, not production SLOs.

## Fixed budgets

| Setting | Initial value |
| --- | --- |
| Reservation lifetime | 15 minutes from database clock |
| Database transaction budget | 3 seconds |
| Provider request deadline | 10 seconds |
| HTTP read-header / idle / write timeout | 5 / 60 / 15 seconds |
| Worker concurrency | 2 |
| Claim batch | 2 |
| Lease duration | 30 seconds |
| Lease reclaim scan | Every 5 seconds |
| Reservation expiry scan | Every 5 seconds, 100 IDs per scan |
| Provider mutation retries | 5 attempts including the initial call |
| Retry delay after failure n | Uniform jitter between 0 and min(60 seconds, 2^(n-1) seconds) |
| Pending/unknown payment sweep | Every 60 seconds, 100 IDs |
| Financial reconciliation budget | 24 observations, 5 minutes apart after immediate retry budget |
| Automatic mutation identity window | Less than 23 hours from the operation's own first_call_at |
| Graceful shutdown | 20 seconds |
| Processed event payload retention | 7 days |
| Event deduplication tombstones | Indefinite in v1 |
| Done task retention | 30 days |

The jitter source is injected in unit tests. Sweeps iterate with stable cursors so large backlogs do not starve older orders. A leased provider call must fit its 10-second deadline; no lease heartbeat is needed initially. Stale responses cannot commit through an expired/replaced token.

## Error decisions

Classify errors rather than treating all failures alike:

- HTTP 429, provider 5xx, DNS/transport timeout: retryable or ambiguous; persist uncertainty when the external effect may have happened.
- Provider validation error: no blind retry; mark setup failed only when rejection establishes no external operation. Otherwise reconcile.
- Missing provider ID after an ambiguous create: replay the identical request and same key inside the mutation identity window.
- Known provider ID: retrieve current state; prefer observation over recreating anything.
- Local identity/amount/currency mismatch: operator_review; no automatic confirmation/refund of an unbound object.
- Refund failed/canceled: operator_review with order recovery_required.
- Database error: roll back; task lease expiry/reconciliation recovers. Do not acknowledge a webhook before durable storage.
- Automatic budget exhausted: operator_review. Retain last_error_code and first/last attempt timestamps; expose a safe recovery_action to the order owner.

Deadlocks (40P01), serialization conflicts (40001), and lock timeouts roll back the whole local transaction and return/requeue a classified temporary failure within the transaction budget; never retry only a suffix of its SQL. Provider retries and task attempts are durable. SDK transport retries are disabled for mutations to avoid unaccounted call multiplication.

## Task lifecycle

ready -> leased -> done, or leased -> ready with a new due time, or leased -> operator_review. Expired leased work becomes ready with a cleared token and a recorded lease_expired outcome.

Result application locks the work row and validates the token and unexpired lease, then locks order -> reservation -> payment_attempt -> product. The same transaction applies business state and finalizes or reschedules work. A recurring reconcile task reschedules itself while pending; it does not enqueue a duplicate of its own leased row. Marking it done and inserting a later same-kind task, when needed, happens in the same transaction.

An operator retry moves the existing operator_review task to ready; it cannot create a replacement financial identity. It preserves history and counts and requires an explicit reason.

## Missing callbacks and late success

Creation, expiry, event processing, and the independent payment sweep can all discover provider success. They use the same guarded ApplyObservation transition.

When a reservation expires, release stock once locally, mark order failed, suppress hosted link, and persist session-expiry/reconciliation work. This does not establish provider failure. A known open session is expired; already complete/expired errors trigger retrieval. An unknown session ID is recovered using the original request/key within the permitted window, then expired if open.

A late success cannot consume released stock. Record succeeded payment, set recovery_required, and schedule a full refund. Do not notify a paid customer that the matter is complete until the refund is established. Failed orders with unresolved payment state remain in reconciliation until settled or escalated.

## Operator commands to implement

All commands execute locally against the configured database; no public operator API:

```text
checkout ops list --status operator_review --limit 100
checkout ops inspect --work-id <uuid>
checkout ops retry --work-id <uuid> --reason "<reason>"
checkout ops integrity
```

list returns work ID, operation, order ID, last_error_code, due time and attempts. inspect includes safe provider object IDs and operation identities, not credentials or card data. retry rejects an unknown-ID mutation outside its identity window and any task still leased; the operator must reconcile through the provider dashboard before attaching a verified result. Manual financial correction is intentionally not an arbitrary SQL or force-confirm command.

integrity verifies confirmed orders have succeeded payments and consumed reservations, and refunded orders have succeeded refunds. Nonzero discrepancy count exits nonzero.

## Observability and readiness

Use log/slog structured JSON. Correlate request IDs, order IDs and task IDs in logs without tokens, provider payloads, payment URLs, or raw customer identifiers. Log unexpected errors once at the boundary with classified code.

Initial observations: HTTP latency/status by route template, checkout acceptance/completion latency, oldest ready/unknown work age, reservation expiry count, mutation retries, operator-review backlog, stale lease results, recovery-required count. Expose a local operator summary first; a metrics backend is optional later. Never use raw order/customer IDs as metric dimensions.

Database/migration failure makes readiness 503. Provider outage leaves process/database readiness available while payment work queues; alert on age/backlog. Liveness remains process-only. Shutdown stops accepting new requests and claims, drains bounded work, then cancels remaining calls; persisted leases recover unfinished work.

## CI and evidence

Unit tests have no account credentials. Integration tests use real disposable PostgreSQL and generated JWKS fixtures. Sandbox smoke requires explicitly supplied test credentials and verifies a real session, signed callback, persisted confirmation, and expired-order recovery refund.

Each experiment records commit, dependency versions, environment, command, elapsed time, order cohort, final DB state, and limitations. Measure completed orders separately from accepted HTTP requests. Do not run destructive reset commands against an unspecified database.

## Sources

Provider ambiguity, duplicate callbacks and retrieval behavior are based on the official contracts linked in [integration decisions](integration-decisions.md). All schedules, retry budgets and expiry/refund policies above are application design choices.
