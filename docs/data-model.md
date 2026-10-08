# PostgreSQL data and transaction design

Date: 2026-10-08. PostgreSQL 18; application transactions use READ COMMITTED.
This document specifies the schema to implement; no migrations have run.

## Tables

Columns are NOT NULL unless explicitly marked nullable. All timestamps are timestamptz. Monetary fields are bigint. Use UUIDs generated in Go through crypto/rand plus RFC4122 version/variant bits; generating identifiers is not a new dependency. Text states use CHECK constraints, not custom PostgreSQL enum types, to simplify later migration.

| Table | Columns and constraints |
| --- | --- |
| products | sku text PRIMARY KEY, name text NOT NULL (1-200 chars), unit_price_minor bigint NOT NULL CHECK 50..100000, currency text NOT NULL CHECK = usd, available_stock integer NOT NULL CHECK 0..1000000, created_at and updated_at |
| orders | id uuid PRIMARY KEY; customer_issuer text NOT NULL; customer_subject text NOT NULL; checkout_key text NOT NULL; request_hash bytea NOT NULL CHECK length=32; sku REFERENCES products; quantity integer CHECK 1..10; unit_price_minor bigint CHECK 50..100000; amount_minor bigint CHECK = unit_price_minor*quantity AND <=1000000; currency CHECK = usd; status CHECK pending/confirmed/failed/recovery_required/refunded; acceptance_response jsonb NOT NULL; created_at/updated_at; UNIQUE(customer_issuer,customer_subject,checkout_key) |
| reservations | order_id uuid PRIMARY KEY REFERENCES orders; quantity integer CHECK 1..10; state CHECK held/consumed/released; expires_at, created_at, updated_at NOT NULL |
| payment_attempts | order_id uuid PRIMARY KEY REFERENCES orders; create_key text NOT NULL UNIQUE; create_request jsonb NOT NULL; session_id text UNIQUE nullable; session_url text nullable; payment_intent_id text UNIQUE nullable; payment_status CHECK not_started/pending/unknown/succeeded/failed/canceled; refund_key text NOT NULL UNIQUE; refund_id text UNIQUE nullable; refund_status CHECK none/pending/unknown/succeeded/failed/canceled; create_first_call_at/create_retry_identity_deadline nullable; refund_first_call_at/refund_retry_identity_deadline nullable; last_observed_at nullable; created_at/updated_at |
| provider_events | event_id text PRIMARY KEY; type text NOT NULL; object_id text NOT NULL; order_id uuid nullable; payload jsonb nullable only after processing and retention cleanup; state CHECK received/processed/operator_review; received_at/processed_at; last_error_code nullable |
| work_items | id uuid PRIMARY KEY; kind CHECK create_session/reconcile_payment/expire_session/create_refund/reconcile_refund/process_event; entity_key text NOT NULL; status CHECK ready/leased/done/operator_review; due_at timestamptz NOT NULL; attempts integer CHECK >=0; lease_token uuid nullable; lease_until nullable; last_error_code nullable; first_attempt_at/last_attempt_at nullable; created_at/updated_at; CHECK lease token/expiry present iff leased |

customer_issuer <=512 bytes; customer_subject <=255 bytes; checkout_key/API bounds enforced again as database length checks. Validate provider identifiers as opaque strings <=255 bytes, not assumed UUIDs. Bound provider payload storage by the webhook limit. work_items.entity_key is the order UUID string except process_event, which uses event ID; it is validated by the owning operation, not interpolated into SQL.

provider_events.order_id deliberately has no FK because an authentic external event may refer to an unknown local order. Linking it to a business transition requires a separately verified local lookup.

No cascading deletion of orders or money-related records. Do not delete checkout idempotency history in v1.

## Indexes

- reservations(expires_at,order_id) WHERE state='held'.
- orders(customer_issuer,customer_subject,created_at DESC,id).
- payment_attempts(last_observed_at,order_id) WHERE payment_status IN ('not_started','pending','unknown').
- work_items(due_at,id) WHERE status='ready'.
- work_items(lease_until,id) WHERE status='leased'.
- UNIQUE work_items(kind,entity_key) WHERE status IN ('ready','leased','operator_review').
- provider_events(received_at,event_id) WHERE state='received'.

Done work may be retained for 30 days. Inserting new reconciliation work after a previous done task is allowed; never let an existing operator_review task silently become a duplicate ready task.

## Local transaction boundaries

### Accept checkout

1. Begin transaction. Read existing order by customer/key; if present, compare fingerprints and return its original response or conflict without inspecting current stock.
2. For a new key, SELECT the product FOR UPDATE. Read its authoritative price/name. If absent, return product_not_found and roll back.
3. Construct candidate order UUID, stable create/refund keys, amount, expiry and acceptance response. INSERT order with ON CONFLICT DO NOTHING on customer/key.
4. If another request won arbitration, read that committed order and compare fingerprints. Return its response or conflict; no stock has been deducted. Do not reject a matching replay because the current stock is exhausted.
5. If this request inserted the new order, check available stock and UPDATE it conditionally. Insufficient stock rolls back the inserted order and leaves its key unused.
6. Insert held reservation, not_started payment attempt with immutable provider request snapshot, and create_session task.
7. Commit. All accepted records and the original response become visible together; no provisional incomplete response is committed.

A concurrently winning request with a different SKU can cause a brief wait on the unique key; transaction budgets bound this. Different-SKU concurrent requests whose SKU does not exist may return product_not_found before the other transaction commits; after a key is established, all conflicting reuses return idempotency_conflict. Clients retry only infrastructure errors, not business conflicts. Locking the product is intentionally simple for the small initial store; benchmark contention before optimizing it.

### Apply payment observation

Lock order, reservation, then payment_attempt, in that order. Verify observation matches the stored provider IDs or immutable create key/order metadata before binding an initially unknown ID. Verify amount, currency, test mode, and account.

Persist provider success as monotonic. An already confirmed or refunded order is terminal in this scope: a matching repeated success is a no-op for order/reservation/refund state. An order already recovery_required keeps that state and its existing refund identity; do not enqueue a second logical refund. Only then evaluate a newly established success. If reservation held and database clock_timestamp() < expires_at and order pending: mark reservation consumed and order confirmed atomically. Otherwise release a held reservation, restoring product stock once, mark recovery_required, and persist create_refund work. A released reservation is never recreated.

Nonterminal provider statuses cannot downgrade success. A definitive unsuccessful terminal outcome releases a held reservation and fails a pending order. New stock adds back only through the inventory release operation in the same transaction.

### Expire inventory

Select candidate IDs without locking, then for each lock order -> reservation -> payment_attempt. Recheck expiry with clock_timestamp(), state, and order status. If eligible, release once, mark failed, suppress unpaid links, and persist expire_session plus reconcile_payment work. Provider I/O happens after this transaction, and its result must use ApplyObservation.

Do not use transaction start time as a deadline check after waiting on a lock. Deadline policy is based on the time at which the locked transition is evaluated.

### Claim work

A short transaction selects due ready tasks with FOR UPDATE SKIP LOCKED, increments attempts, sets a fresh token and lease_until, then commits. A separate sweep reclaims expired leases. Business writes and marking the associated work done must occur in one transaction after locking/verifying the work's current token and unexpired lease. Stale claim results cannot update local state; they remain discoverable by reconciliation.

Avoid lock inversion: task execution locks its own work row, then order -> reservation -> payment_attempt -> product. Sweeps touching work only do not lock business rows. Business operations enqueue work with INSERT ON CONFLICT DO NOTHING and never wait on a leased task to update its status. Unique-index conflicts remain bounded by the transaction timeout.

## Integrity checks and migrations

Cross-table states cannot all be enforced by CHECK constraints. Guarded application transactions plus integration tests enforce confirmation/payment/reservation relationships. Add a diagnostic query that reports confirmed orders without succeeded payments and consumed reservations; expected count is zero.

Goose SQL migrations are versioned and applied by an explicit migrate command before starting the app. Startup verifies the schema version; it does not execute DDL. Use a dedicated migration credential in hosted environments; runtime does not need CREATE privileges. Applied migrations are immutable; corrective migrations move forward.

Tests use an isolated disposable database and reset fixtures per test; never reset a user database. Serialize migration application through the migration tool's documented locking support.

## References

Locking and transaction choices are based on [PostgreSQL explicit locking](https://www.postgresql.org/docs/18/explicit-locking.html) and [SELECT locking clauses](https://www.postgresql.org/docs/18/sql-select.html). SKIP LOCKED is for work claiming, not ordinary catalog correctness.
