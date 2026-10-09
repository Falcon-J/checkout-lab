# Engineering guide and folder ownership

Date: 2026-10-08. Applies to the proposed application, not to the archived/reference AtlasPay implementation.

## File responsibilities

```text
cmd/checkout/main.go                 serve, migrate, ops subcommands and composition root
internal/config/config.go            validated environment configuration
internal/database/pool.go            pgxpool creation and readiness
internal/database/migrate.go         Goose wrapper for explicit migration command
internal/checkout/model.go           order states, inputs, outputs, typed errors
internal/checkout/store.go           cross-table checkout transactions and owner-scoped reads
internal/checkout/service.go         validation and application operations
internal/inventory/reservation.go    stock/reservation operations accepting caller transaction
internal/payment/model.go            provider-independent request and observation types
internal/payment/provider.go         narrow external-I/O interface
internal/payment/stripe.go           official SDK adapter
internal/payment/webhook.go          official SDK signature verification and normalization
internal/worker/store.go             work claiming, scheduling and lease checks
internal/worker/loop.go              bounded task execution and shutdown
internal/worker/reconcile.go         payment, expiry and refund sweeps
internal/httpapi/auth.go             managed JWT/JWKS middleware adaptation
internal/httpapi/handlers.go         JSON transport, bounds and error mapping
internal/httpapi/server.go           routes, middleware and timeout configuration
migrations/00001_checkout.sql        initial tables and constraints
tests/integration/helpers_test.go    disposable PostgreSQL and identity fixtures
tests/integration/checkout_test.go   transactions, duplicate requests and contention
tests/integration/recovery_test.go   leases, restart recovery, expiry races
tests/sandbox/stripe_test.go         explicit real sandbox checks
deployments/compose/compose.yaml     local database/app runtime
web/                                minimal browser checkout added only in UI milestone
docs/                               specifications, decisions, experiments and plan
```

Create files when needed. A file may be split when its responsibilities actually diverge; this map is not permission to prebuild every abstraction.

## Dependency direction

httpapi -> checkout application operations. worker -> checkout transitions and payment provider interface. checkout coordinates inventory/payment records using one pgx transaction. inventory accepts pgx.Tx from the caller and never begins/commits a transaction itself. payment adapter knows provider transport, not order SQL. database manages pool/migrations, not domain queries. cmd wires concrete dependencies.

Cross-table persistence deliberately lives with checkout coordination. Avoid cycles: payment types contain opaque order IDs and observations, not a dependency on checkout. Worker scheduling persistence accepts pgx.Tx where atomic application state requires enqueue/finish.

## Go conventions

- Pass context explicitly as the first argument of I/O operations. Do not store request contexts in structs.
- Wrap errors with %w and use errors.Is/As for classification. Business conflicts are typed/sentinel errors; handlers map them to documented status codes.
- Keep state transitions as named operations with explicit preconditions. SQL constraints supplement them.
- Use concrete implementations internally; add interfaces only around actual external I/O, time/random sources for deterministic tests, or independently replaceable operations.
- Use table-driven tests for input boundaries and real PostgreSQL tests for isolation/locks. Do not mock SQL and claim transaction correctness.
- Defer rollback on every transaction and check commit errors. Never call external providers while holding a database transaction.
- Avoid implicit package-global clients, silent fallback modes and disabled checks in production.
- Keep domain monetary values int64; reject overflow before side effects.
- Use standard flag parsing for the single executable's subcommands; no CLI framework needed.

## Configuration contract

| Name | Requirement/default |
| --- | --- |
| APP_ENV | local or hosted; explicit |
| HTTP_ADDR | 127.0.0.1:8080 local; configured hosted address |
| DATABASE_URL | Required; TLS verify-full outside local/test |
| AUTH_ISSUER | Required exact HTTPS issuer, trailing slash preserved |
| AUTH_AUDIENCE | Required exact API audience |
| STRIPE_SECRET_KEY | Required for payment milestone; test key only |
| STRIPE_WEBHOOK_SECRET | Required for payment milestone |
| PUBLIC_BASE_URL | Absolute trusted URL; HTTPS outside loopback |
| RESERVATION_TTL | 15m |
| WORKER_CONCURRENCY | 2, allowed 1-8 |
| LOG_LEVEL | info; debug still cannot log secrets |

Local PostgreSQL credentials may live in an untracked .env; committed examples use unmistakable nonsecret examples. No configuration is provisioned by documentation. The initial database-only test milestone does not start a production server without identity config.

Validate configuration at startup, reject live Stripe mode, and expose only nonsensitive readiness. Secret key prefix checks are a guard, not proof of account mode; verify returned livemode values.

## Verification commands to implement

Use Go commands, not the generic yarn instructions copied from another stack. A PowerShell script checks every native command exit code explicitly.

```text
gofmt -l cmd internal tests
go vet ./...
go build ./...
go test ./...
go test -race ./...
go mod verify
docker compose -f deployments/compose/compose.yaml config --quiet
go test ./tests/integration -count=1 -v
go test ./tests/sandbox -count=1 -v
```

A formatting check passes only when gofmt prints no files. Integration command requires an explicitly provisioned disposable test database; sandbox command requires real test account credentials and is excluded from normal CI. The build is the Go typecheck. Add dependency vulnerability scanning using the maintained Go tool when executable code/dependencies exist.

Pin Go/library versions and container images during implementation. Do not fabricate successful commands before their scripts/files exist.

## Review checklist

Can a reviewer locate the transaction boundary, ownership check, authoritative amount calculation, mutation identity, ambiguity handling, and recovery path without tracing a generic framework? If not, simplify the code before adding features.

## Free-tier runtime profiles

Primary learning profile: local application and PostgreSQL, Auth0 Free tenant and provider sandbox. Optional hosted profile: Render Free web service plus Neon Free PostgreSQL. Serve browser assets from the Go app; no second frontend hosting service is required.

Derive pgxpool capacity from profile: 10 local connections, 5 hosted connections initially. These are tunable limits, not transaction guarantees. External health monitors and synthetic keepalive traffic are excluded. Database/recovery experiments use local/disposable PostgreSQL.

Worker schedules apply while the process is running; a sleeping hosted demo has no wall-clock recovery SLA. See [free-tier strategy](free-tier-strategy.md).
