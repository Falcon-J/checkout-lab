# Active plan: reservation core

Approved scope: 2026-10-09. This supersedes the larger implementation plan for current work. Earlier payment/hosting documents remain optional reference material.

Goal: a local single-operator service with Go, PostgreSQL and Docker Compose. Two tables: inventory and reservations. One operator bearer token; no customer identity or payments. Bind the API and database to loopback.

Contracts: POST /reservations with Idempotency-Key (16-128 ASCII alphanumeric/underscore/hyphen) and JSON sku/quantity (1-10); GET /reservations/{id}; GET /health/live and /health/ready. Matching keys return the same ID and current state; conflicting payloads return 409. Unknown reservation returns 404. Expiry uses database time, releases stock once and persists the expired state. GET may show held until the expiry worker runs; expires_at is always included.

Invariant: inventory cannot be negative; concurrent duplicate requests create one row; repeated/two-worker expiry cannot release twice; unfinished expiry survives process restart. No external effect requires leases or a saga. PostgreSQL transactions and row locks are sufficient.

Tasks:
1. Write failing PostgreSQL tests for overselling, concurrent replay, conflicting key, expired-row release and two-worker recovery. Provision a disposable database; implement transactional Store.Create/Get/Expire.
2. Write failing HTTP tests for token enforcement, strict input validation, conflict/not-found/status mapping and client response loss. Implement net/http transport and bounded worker startup/shutdown.
3. Run Go formatting, vet, build, module verification, all tests, race tests and an actual process-restart demo. Review the diff and commit.

Paths: cmd/reservation/main.go; internal/reservation/store.go, http.go, worker.go and adjacent tests; migrations/001_initial.sql; compose.yaml; scripts/demo.ps1. Only pgx is an application dependency. No custom migration framework: PostgreSQL's official image applies the initial SQL to a new development volume. Schema changes later require an explicit versioned migration tool.

Ruling: use installed Go 1.25 and cached compatible pgx v5.8.0 (v5.11.0 download failed; checksum verification retained) for this local slice, rather than requiring a toolchain upgrade before a first working example. A hosted/production release needs a current supported patched toolchain.
