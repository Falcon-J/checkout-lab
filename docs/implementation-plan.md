> Frozen reference plan as of 2026-10-09. Current work follows reservation-plan.md; do not execute the payment/cloud scope automatically.

# Checkout Lab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for native execution or superpowers:subagent-driven-development if the user explicitly selects delegation. Steps use checkbox syntax.

**Goal:** Implement the small authenticated checkout product with real sandbox payments and durable recovery, without Redis, Kafka or Kubernetes.

**Architecture:** One Go executable serves HTTP and runs bounded PostgreSQL-backed workers. Local state transitions are transactional; Stripe operations occur outside transactions with persisted identities and reconciliation.

**Tech Stack:** Go 1.27, PostgreSQL 18, pgx/v5, Goose/v3, Stripe Go/v87, Auth0 JWT middleware/v3, Docker Compose.

**Spec:** [Checkout design](checkout-design.md), [API](api-contract.md), [data](data-model.md), [integration decisions](integration-decisions.md), [recovery](recovery-runbook.md), [engineering guide](engineering-guide.md).

## Global constraints

- One store, USD, one SKU per order, quantity 1-10.
- Reservation lifetime 15 minutes; mutation identity window less than 23 hours.
- Database transaction budget 3 seconds; provider request deadline 10 seconds; worker concurrency 2; leases 30 seconds.
- No client-supplied identity, price, redirect URL, or provider mutation identity.
- No network I/O inside database transactions; no application auth bypass.
- Use test-mode provider only and generated JWKS fixtures for ordinary CI.
- Primary execution is local and subscription-free. Optional free hosted demos may sleep; no strict hosted timing guarantee or paid upgrade. Follow free-tier-strategy.md.
- Use the verified initial dependency candidates in integration-decisions.md; resolve/check compatibility and checksums, and pin toolchain/image digests before execution. Do not commit floating latest.
- Initial live sandbox checks require user-supplied account access and are not proved by fake-provider tests.

## Review focus

1. A matching retry after stock runs out must return its original accepted response: Task 2.
2. A committed order whose HTTP response was lost must survive client cancellation: Task 3.
3. A stale worker returning after lease replacement must not overwrite newer state: Tasks 4 and 6.
4. A card decline inside an open Checkout session must not release stock prematurely: Tasks 5 and 6.
5. A refund with uncertain or failed outcome must not mark the order refunded or create a new identity: Task 6.

## File structure

Use the exact responsibilities and paths in [engineering guide](engineering-guide.md). Unit tests sit beside implementation; crossing-boundary/database tests live under tests/integration. Read free-tier-strategy.md before any deployment. Add files only in their owning task. No pre-generated service framework.

## Task 1: Reproducible database foundation

**Files:** go.mod, go.sum, cmd/checkout/main.go, internal/config/config.go, internal/database/pool.go, internal/database/migrate.go, migrations/00001_checkout.sql, deployments/compose/compose.yaml, tests/integration/helpers_test.go, tests/integration/database_test.go, .env.example.

**Consumes:** Data-model tables, indexes and constraints; engineering configuration.
**Produces:**
- config.Load() (Config, error).
- Config fields: AppEnv, HTTPAddr, DatabaseURL, AuthIssuer, AuthAudience, StripeSecretKey, StripeWebhookSecret, PublicBaseURL strings; ReservationTTL time.Duration; WorkerConcurrency int; LogLevel slog.Level.
- database.Open(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error).
- database.Migrate(ctx context.Context, databaseURL, migrationDir string) error.
- Integration helper newTestDB(t *testing.T) *pgxpool.Pool, restricted to database names prefixed checkout_test_; fixtures run explicit migrations and clean only that database.

- [ ] Write TestMigrationsApplyOnce and TestSchemaRejectsInvalidMoneyAndStock: two migration runs succeed, version count unchanged, negative stock and invalid quantity/currency rejected.
- [ ] Write TestConfigRejectsUnsafeHostedSettings: missing issuer/audience, live secret key, non-HTTPS hosted URL/database settings fail.
- [ ] Run go test ./internal/config ./tests/integration -run 'TestConfig|TestMigrations|TestSchema' -count=1 -v; confirm tests fail for the missing behavior.
- [ ] Implement pinned module/tool dependencies, explicit migrate subcommand, pool, migration and disposable PostgreSQL fixture. Bind local DB port to loopback and use a separate test DB.
- [ ] Run the same tests and Compose config validation; expect all selected tests PASS and config exit zero.
- [ ] Commit only Task 1 files with message build: establish reproducible PostgreSQL foundation.

## Task 2: Atomic checkout and idempotency

**Files:** internal/checkout/model.go, service.go, store.go; internal/inventory/reservation.go; tests/integration/checkout_test.go; internal/checkout/service_test.go.

**Consumes:** Task 1 pool and fixtures.
**Produces:**
- checkout.Customer { Issuer, Subject string }.
- checkout.PlaceInput { SKU string; Quantity int32; Key string }.
- checkout.Order { ID, SKU, Currency string; Quantity int32; UnitPriceMinor, AmountMinor int64; Status, PaymentStatus, RefundStatus, RecoveryAction string; PaymentURL *string; CreatedAt, UpdatedAt, ReservationExpiresAt time.Time }.
- checkout.Acceptance { OrderID string; Body json.RawMessage; Replayed bool }.
- checkout.NewService(pool *pgxpool.Pool) *Service; Service.Place(ctx context.Context, customer Customer, input PlaceInput) (Acceptance, error); Service.Get(ctx context.Context, customer Customer, orderID string) (Order, error).
- Sentinel errors ErrInvalidRequest, ErrProductNotFound, ErrOutOfStock, ErrIdempotencyConflict, ErrOrderNotFound.
- inventory.Hold(ctx context.Context, tx pgx.Tx, orderID, sku string, quantity int32, expiresAt time.Time) error; inventory.Release(ctx context.Context, tx pgx.Tx, orderID string) (bool,error); inventory.Consume(ctx context.Context, tx pgx.Tx, orderID string) (bool,error).
- Persist provider create/refund keys and immutable request snapshot, but do not execute payments yet.

- [ ] Write TestLastItemConcurrency: seed stock 1, run 20 distinct-key buyers, assert 1 accepted order, 1 held reservation, stock 0 and 19 out-of-stock results.
- [ ] Write TestConcurrentKeyReplay: seed stock 5, run 20 matching-key requests, assert 1 order/reservation/task and identical acceptance bodies; then reuse key with quantity 2 and assert conflict.
- [ ] Write TestReplayAfterStockExhaustionAndPriceChange: accepted response is unchanged after stock reaches 0 and price changes.
- [ ] Write TestNoPartialCheckoutOnFailure and TestOwnerScopedRead: failure leaves no key/order/task; another customer cannot read the order.
- [ ] Write table tests for quantity 0/11, invalid SKU/key, wrong currency/invalid catalog amount and checked total arithmetic.
- [ ] Run go test ./internal/checkout ./tests/integration -run 'TestLastItem|TestConcurrentKey|TestReplay|TestNoPartial|TestOwner|TestPlace' -count=1 -v; confirm the new tests fail.
- [ ] Implement the exact arbitration/locking sequence in data-model.md and atomic initial durable work. Use parameterized SQL and database clock.
- [ ] Run selected tests repeatedly for concurrency (count=10), then go test -race ./internal/checkout ./tests/integration; expect PASS without race reports.
- [ ] Commit with message feat: reserve checkout stock atomically with idempotency.

## Task 3: Authenticated HTTP contract

**Files:** internal/httpapi/auth.go, handlers.go, server.go and adjacent tests; tests/integration/http_test.go; update cmd/checkout/main.go.

**Consumes:** Task 2 Service and errors, configuration.
**Produces:**
- httpapi.NewAuth(cfg config.Config) (func(http.Handler) http.Handler, error), backed by official RS256/JWKS middleware.
- httpapi.NewServer(cfg config.Config, service *checkout.Service, webhook http.Handler) (*http.Server,error).
- Private JSON decoder validates duplicate/unknown keys, trailing values, 4 KiB body and strict numeric bounds; no custom general JSON framework.
- Validated customer in request context derived solely from verified issuer/sub.

- [ ] Write TestHTTPContract for valid checkout 202 + Location, replay marker/body, 409 conflict/out-of-stock, 404 non-owner, 413 body, 415 content type, 405 method.
- [ ] Write TestJWTValidation with local JWKS: wrong issuer/audience/algorithm, expired/missing-exp token, missing subject and insufficient scope reject; valid token succeeds; JWKS outage without cached key returns 503.
- [ ] Write TestResponseLossAfterCommit: cancel/drop client response after acceptance commit and assert retry returns same order with one reservation.
- [ ] Write TestStrictJSONBoundary for duplicate fields, fractional quantity, unknown fields, invalid UTF-8 and trailing JSON.
- [ ] Run go test ./internal/httpapi ./tests/integration -run 'TestHTTP|TestJWT|TestResponseLoss|TestStrictJSON' -count=1 -v; confirm failures.
- [ ] Implement routes/timeouts, safe problem responses, auth integration and ownership; webhook returns explicit not-configured/unavailable until Task 5.
- [ ] Run selected tests and go vet ./...; expect PASS and vet exit zero.
- [ ] Commit with message feat: expose owner-scoped authenticated checkout API.

## Task 4: Durable bounded worker

**Files:** internal/worker/store.go, loop.go and unit tests; tests/integration/worker_test.go; update cmd/checkout/main.go.

**Consumes:** Database work-item model and Task 1 config.
**Produces:**
- worker.Task { ID, Kind, EntityKey, Token string; Attempts int; LeaseUntil time.Time }.
- worker.Store.Claim(ctx context.Context, limit int) ([]Task,error); Reclaim(ctx context.Context) (int,error).
- worker.Store.Finish(ctx context.Context, tx pgx.Tx, task Task) error; Retry(ctx context.Context, tx pgx.Tx, task Task, dueAt time.Time, code string) error; Escalate(ctx context.Context, tx pgx.Tx, task Task, code string) error.
- worker.Run(ctx context.Context, store *Store, concurrency int, execute func(context.Context, Task) error) error.
- worker.NewStore(pool *pgxpool.Pool) *Store and ErrStaleClaim. Store functions verify and lock current token; they never rely on in-memory claims alone.

- [ ] Write TestClaimExclusiveAndReclaim with two competing claimers, verify one token per task and expired lease recovery.
- [ ] Write TestStaleTokenCannotFinish using an old token after reclaim; assert ErrStaleClaim and replacement lease unchanged. Also write TestExpiredTokenBeforeReclaim: an expired lease cannot finish even before the reclaimer replaces its token.
- [ ] Write TestDurableRetryBudget: restart worker/store between attempts; attempts persist and fifth retryable failure escalates.
- [ ] Write TestWakeResumesOverdueWork: restart after simulated sleep and verify due work is claimed and expiry evaluated from current DB time. Write TestShutdownStopsClaims and TestJitterBounds: bounded goroutines exit within 20 seconds; jitter stays within documented ceilings.
- [ ] Run go test ./internal/worker ./tests/integration -run 'TestClaim|TestStale|TestExpiredToken|TestDurable|TestWake|TestShutdown|TestJitter' -count=1 -v; confirm failures.
- [ ] Implement database claiming, token checks, scheduling and worker loop; record lease outcome; no busy polling or unbounded goroutine per task.
- [ ] Run selected tests and race detector; expect PASS.
- [ ] Commit with message feat: persist bounded background work and claims.

## Task 5: Official Stripe sandbox adapter and durable callbacks

**Files:** internal/payment/model.go, provider.go, stripe.go, webhook.go and tests; tests/sandbox/stripe_test.go; update HTTP webhook wiring.

**Consumes:** Immutable request identities/snapshots and integration contract.
**Produces:**
- payment.CreateInput { OrderID, Key, Name, Currency, SuccessURL, CancelURL string; UnitPriceMinor int64; Quantity int32 }.
- payment.Observation { OrderID, SessionID, PaymentIntentID, PaymentState, RefundID, RefundState, Currency string; AmountMinor int64; SessionOpen, LiveMode bool; URL *string }.
- payment.Provider interface: Create(ctx context.Context,input CreateInput)(Observation,error); Get(ctx context.Context,sessionID string)(Observation,error); Expire(ctx context.Context,sessionID string)(Observation,error); Refund(ctx context.Context,intentID,key string,amount int64)(Observation,error); GetRefund(ctx context.Context,refundID string)(Observation,error).
- payment.Event { ID, Type, ObjectID, OrderID string; Raw json.RawMessage }.
- payment.VerifyWebhook(body []byte, signature, secret string) (Event,error).
- payment.NewStripeProvider(cfg config.Config) (Provider,error).
- Classified errors preserving whether the remote operation may have happened; never depend on message-text matching.

- [ ] Write TestStripeRequestIdentity using SDK-supported test backend: exact key/request preserved on retry, no SDK mutation retry multiplication, fixed redirect and server price.
- [ ] Write TestObservationMapping for paid success, open declined/pending, expired unpaid, live mode, currency/amount mismatch.
- [ ] Write TestWebhookSignatureAndReceipt: valid signature creates one event/task, repeated event creates no duplicate, invalid signature writes nothing, storage failure does not acknowledge 200.
- [ ] Run go test ./internal/payment ./internal/httpapi -run 'TestStripe|TestObservation|TestWebhook' -count=1 -v; confirm failures.
- [ ] Confirm real sandbox account access first; do not assume new-account eligibility. Implement using the official SDK Client/signature helper; bind test account and version; subscribe only documented event types.
- [ ] Run unit/integration checks; with explicitly available sandbox credentials run TestSandboxHostedCheckout and capture actual provider IDs safely in local evidence. Without credentials, record this gate as not verified.
- [ ] Commit with message feat: integrate sandbox hosted checkout and signed callbacks.

## Task 6: Correct financial transitions and recovery

**Files:** internal/checkout/transitions.go, internal/worker/reconcile.go, payment test fake under tests/integration; tests/integration/recovery_test.go.

**Consumes:** Tasks 2, 4 and 5.
**Produces:**
- checkout.ApplyObservation(ctx context.Context, tx pgx.Tx, orderID string, observation payment.Observation) error.
- checkout.ExpireReservation(ctx context.Context, tx pgx.Tx, orderID string) (bool,error).
- worker.Executor { pool *pgxpool.Pool; provider payment.Provider; store *Store }; Executor.Execute(ctx context.Context,task Task) error.
- worker.Sweep(ctx context.Context,pool *pgxpool.Pool) error for bounded payment/expiry scans.
- Persistent refund failed/canceled states and operator-review handling.

- [ ] Write TestPaymentResponseLostAndRestart: provider records success then transport fails; restart; assert one provider operation, confirmed order, one consumed reservation.
- [ ] Write TestDuplicateAndOutOfOrderEvents and TestOpenSessionDecline: no downgrade/repeated effects, including paid events after confirmed and refunded terminal states; decline leaves pending stock until expiry.
- [ ] Write TestExpirySuccessRace with both lock orderings; assert consumed+confirmed OR released+recovery/refund, never oversell or paid untracked failure.
- [ ] Write TestRefundResponseLostAndFailure: unknown refund reconciles same identity; failed refund stays recovery_required/operator_review.
- [ ] Write TestStaleWorkerResult, TestMutationWindowExpired, TestProviderOutageBudget and TestWebhookBeforeCreateResponse: token and identity/time checks hold, current-object reconciliation converges.
- [ ] Run go test ./tests/integration -run 'TestPayment|TestDuplicate|TestOpenSession|TestExpiry|TestRefund|TestStaleWorker|TestMutation|TestProviderOutage|TestWebhookBefore' -count=1 -v; confirm failures.
- [ ] Implement guarded transitions, budgets and sweeps exactly as data-model/recovery docs, keeping external I/O outside transactions.
- [ ] Repeat race tests count=20, run full integration suite and integrity queries; expect PASS and zero discrepancies.
- [ ] Commit with message feat: reconcile uncertain payments and recover expired stock safely.

## Task 7: Minimal browser and operator workflow

**Files:** web/index.html, web/app.js, internal/httpapi/static.go, cmd/checkout/ops.go; tests/integration/ops_test.go; docs/experiments/browser-walkthrough.md.

**Consumes:** Authenticated API, official managed-auth client, recovery commands.
**Produces:** One catalog/checkout/status page with official PKCE client; executable ops commands from recovery-runbook.md.

- [ ] Write TestOpsRetryGuards: rejects leased tasks and unknown-ID mutations outside the window; approved retry preserves financial identity/history. TestOpsIntegrity exits nonzero on an injected discrepancy.
- [ ] Run tests to confirm failure.
- [ ] Implement operator commands with standard flag parsing and a small browser client using an official Auth0 example; keep tokens in memory and URLs server-controlled. No frontend build framework.
- [ ] Run operator tests, then browser-check authentication, insufficient stock, successful sandbox checkout and expiry/refund status. Verify redirect alone does not confirm an order and unauthorized reads disclose nothing.
- [ ] Record actual walkthrough results and unresolved sandbox prerequisites, never simulated live success.
- [ ] Commit with message feat: add minimal checkout and operator workflows.

## Task 8: Reproducibility, reliability checks and evidence

**Files:** Dockerfile, .github/workflows/ci.yml, scripts/verify.ps1, scripts/verify.sh; docs/experiments/acceptance-matrix.md; update README.md.

**Consumes:** All prior deliverables.
**Produces:** One local runtime, CI without external secrets, explicit optional sandbox checks, fresh evidence tied to commit.

- [ ] Write script checks proving failed native commands produce nonzero overall exit and integration tests cannot point at a non-test database.
- [ ] Run them to confirm failure.
- [ ] Implement pinned multistage image/nonroot app, Compose healthchecks, explicit migrate-before-serve workflow, and CI formatting/vet/build/unit/race/PostgreSQL gates. Use standard public Linux runners only if repository publication is authorized; otherwise remain within private free quotas or run locally. Optional Render/Neon deployment follows the free-tier profile without keepalive or paid workers.
- [ ] Run formatting check, go vet ./..., go build ./..., go test ./..., go test -race ./..., go mod verify and Compose config. Run disposable database integration tests; all must pass.
- [ ] Run sandbox acceptance when credentials are available; record provider account mode and final database state. If not available, report application integration gate incomplete.
- [ ] Record completion latency separately from HTTP acceptance using a small known cohort; no production throughput/SLO claims.
- [ ] Commit with message ci: verify checkout invariants and recovery end to end.

## Handoff and completion

Proposed execution method: native, one task at a time, because the tasks share tight transactional contracts and the project should stay small. Delegation is not authorized merely by this plan.

Application completion requires Tasks 1-8 and the real sandbox acceptance gate. Optional Redis/Kafka/Kubernetes milestones remain outside the plan. Review the written contracts before executing this plan; do not confuse completed documentation with implemented software.
