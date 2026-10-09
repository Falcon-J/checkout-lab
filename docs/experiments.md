# Engineering experiments

## Proven defects and fixes

1. A committed replay waited on the same-key advisory lock. The regression creates a reservation, holds that lock in another transaction, and attempts replay with a 500 ms context. It failed before the read fast path and passed afterward. New-key transactions still lock and recheck.
2. Confirmation could evaluate expiry before waiting for the row lock. A hold became due while confirmation waited; the regression failed because it was confirmed after its deadline. The deadline is now queried after the lock is acquired, and the regression passed.

## Serial replay baseline

Environment: Windows amd64, Go 1.25, AMD Ryzen 5 3500U; PostgreSQL 18 in local Docker. Three samples, 100 operations each; setup excluded.

Before fast path: 17,950,202; 38,568,099; 20,676,103 ns/op (approximately 18.0, 38.6, 20.7 ms). Median: 20.7 ms. These are local serial measurements with substantial variation, not capacity or production claims.

After fast path, **native PostgreSQL** (a different database environment): 2,920,719; 5,227,751; 2,947,641 ns/op (2.9, 5.2, 2.9 ms). Median: 2.95 ms. Do not calculate a speedup across these two environments. The deterministic advisory-lock regression establishes removal of unnecessary replay waiting independently of this benchmark.

## Verification ledger

- New lifecycle tests initially failed with missing endpoints; confirmation/cancellation, overdue expiry, repeated terminal actions, and competing confirm/cancel subsequently passed.
- Existing HTTP parsing, bearer authorization and create/read compatibility tests passed after the package refactor.
- Full Windows verification during Linux compilation hit timeouts in simple reads and concurrent replay. No passing-suite claim is made from these runs. Direct Linux database verification and controlled failure exercises remain required.
- Full native PostgreSQL verification passed: formatting, module verification, vet, build/typecheck, and all unit/integration tests, including 20 concurrent requests, terminal transitions, expiry/cancel competition, lock-wait cancellation, overdue confirmation, and worker startup recovery.
- Integration fixtures prewarm ten connections, matching the application pool limit. These establish transactional invariants in steady state, not cold-start latency. Initial cold/congested runs timed out and remain part of this record.
- The rebuilt worker crash/restart demo passed against a fresh native database. An earlier client timeout cancelled insertion; inspection showed no reservation and all ten stock units intact. Client timeouts now allow server cleanup, and transient retries are bounded to three with the same key.
- Fresh whole-branch review and targeted follow-ups found no Critical/Important issues. CI configuration was reviewed; hosted execution remains unverified before the first push.
- Docker CLI attempts stopped returning results. A direct engine-pipe read timed out after ten seconds. Linux race verification and the database-outage exercise remain unverified; no passing result is claimed.
- Readiness is pending those runtime checks. Source is preserved as a reviewed checkpoint, not a completed verification claim.
