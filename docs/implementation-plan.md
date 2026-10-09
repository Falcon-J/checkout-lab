# Reservation backend implementation plan

> For agentic workers: use superpowers:executing-plans inline. One fresh whole-branch review at the end.

Goal: make the existing reservation backend understandable and demonstrably correct.
Architecture: domain service consumes a narrow storage interface; SQL implements atomic operations; HTTP accepts the service and readiness callback.
Tech stack: Go 1.25, pgx v5.8.0, PostgreSQL 18, local Docker Compose.
Spec: architecture.md

## Global constraints
Preserve loopback and bearer authorization, SKU/create/read contracts, three-second operation bound, one direct dependency, and database test guard. No remote publish. Preserve old documents in docs/archive.

## Review focus
Concurrent confirm/cancel/expiry must produce one terminal outcome and at most one stock restoration. Overdue confirmation must not sell expired stock. Replay after a lost response must use the same row. Context cancellation must remain bounded. Tests sharing a database must not run in parallel across packages.

## Tasks
- [x] Add HTTP lifecycle tests and a committed replay under held advisory lock test; run and observe missing endpoint/timeout failures.
- [x] Separate model/service, PostgreSQL, and HTTP. Adapt existing integration tests into the PostgreSQL package; retain HTTP unit tests beside handlers. Keep worker integration tests with SQL fixture.
- [x] Implement lifecycle transitions and committed replay read; apply explicit status migration to disposable databases. Run all existing and new tests.
- [ ] Add reproducible contention benchmark and controlled database outage recovery evidence. Run formatting, vet, build, module verification, ordinary and Linux race tests. Preserve Linux build cache between runs.
- [ ] Consolidate documentation, document actual measurements and resume bullets, conduct final fresh review, resolve important findings, commit and report readiness.

## Execution ledger
Ruling: continue on existing codex/reservation-core branch in dedicated local checkout; user approved this redesign and inline implementation. No additional project or platform scope.

Ruling: concurrency fixtures prewarm ten connections (matching application MaxConns) before measuring transactional behavior. This excludes cold SCRAM/pool creation from the invariant check; it does not establish cold-start latency. Outage and explicit cancelled-lock tests exercise bounded failures separately.

Checkpoint: native full suite and rebuilt worker crash/restart passed; code and CI reviews clear. Docker engine response timeout blocks Linux race verification and the database-outage demo. No publication performed.

Publication update — 2026-10-09: the user explicitly authorized public publication, superseding the earlier no-publish constraint. Published as https://github.com/Falcon-J/checkout-lab on main. Hosted CI run 37944659606 passed, including the full Linux race suite and PostgreSQL integration tests without skips. Only the controlled database-outage exercise remains unverified; final readiness is still gated on that exercise.
