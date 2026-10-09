# Reservation Lab

A small local backend for learning concurrency and database recovery with Go, PostgreSQL and Docker Compose.

## What works

- Reserve limited inventory and read reservation status.
- Repeat a request safely using an idempotency key.
- Reject conflicting key reuse and insufficient stock.
- Expire reservations and return stock exactly once.
- Restart the worker and recover overdue reservations from PostgreSQL.

One executable, two tables, one application dependency (pgx). No payments, customer-account platform, Redis, Kafka, Kubernetes or cloud hosting in the current scope. Earlier documents remain optional reference material; [the reservation plan](docs/reservation-plan.md) is active.

## Run locally

Requires Go 1.25+, PostgreSQL 18 (Docker or an existing local installation), and PowerShell 7 for the scripts.

```powershell
docker compose up -d --wait postgres
$env:DATABASE_URL = 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_test?sslmode=disable'
$env:API_TOKEN = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
go run ./cmd/reservation
```

The Compose password is an explicitly local development credential. Both published database port and HTTP listener bind to loopback. The initial SQL runs only on a new Docker volume. For native PostgreSQL, create a fresh database and apply migrations/001_initial.sql with psql; the application never applies DDL automatically.

The API is intentionally single-operator: one bearer token authorizes all reservations. It is not customer authentication or a public/multi-tenant deployment. Keep the token local. No token bypass or production-readiness claim.

```powershell
$headers = @{
  Authorization = "Bearer $env:API_TOKEN"
  'Idempotency-Key' = [guid]::NewGuid().ToString('N')
}
$r = Invoke-RestMethod -NoProxy -TimeoutSec 3 http://127.0.0.1:8080/reservations -Method Post -Headers $headers -ContentType application/json -Body '{"sku":"book-go","quantity":1}'
Invoke-RestMethod -NoProxy -TimeoutSec 3 "http://127.0.0.1:8080/reservations/$($r.id)" -Headers $headers
```

New reservation: 201. Matching replay: 200 and the same ID/current state. Conflicting key or exhausted stock: 409. Missing row: 404. Invalid/duplicate/unknown JSON fields: 400; payload >4 KiB: 413; wrong media type: 415.

Default lifetime is 15 minutes; expiry sweep runs every 5 seconds and on worker startup. API-only and worker-only processes use -mode api and -mode worker. A due reservation remains held until a worker commits expiry; expires_at makes its deadline explicit.

## Verify

**Tests reset inventory and reservations. Use a disposable database named checkout_test or checkout_test_*; never point them at a database whose contents you need.** Plain go test without TEST_DATABASE_URL runs unit checks and skips PostgreSQL tests.

```powershell
./scripts/verify.ps1 -DatabaseURL 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_test?sslmode=disable'
go build -o bin/reservation.exe ./cmd/reservation
./scripts/demo.ps1 -DatabaseURL '<separate disposable demo database URL>'
```

The demo expects the initial inventory of ten book-go units. It kills only the API/worker processes it started and demonstrates same-key replay, worker crash/restart, and stock restoration. Use a fresh demo database; running it repeatedly requires waiting for its last reservation to expire.

On Windows without a 64-bit C compiler, run the regular checks with `-SkipRace`, then run the race suite in Linux Docker from this repository. The read-only mounts use the existing source and downloaded Go modules; tests use the same disposable Compose database.

```powershell
$projectDirectory = (Get-Location).Path
$moduleDirectory = go env GOMODCACHE
docker run --rm --network reservation-lab_default --mount "type=bind,source=$projectDirectory,target=/src,readonly" --mount "type=bind,source=$moduleDirectory,target=/go/pkg/mod,readonly" -w /src -e GOPROXY=off -e 'TEST_DATABASE_URL=postgres://reservation:local-test-only@postgres:5432/checkout_test?sslmode=disable' golang:1.25.0 go test -race ./... -count=1 -v
```

This disposable runner compiles a fresh race-enabled standard library and can take several minutes. Avoid running two test suites against the same database at once.

## Why the transactions work

Create takes a transaction-scoped advisory lock for the request key, checks replay, and conditionally decrements inventory. Reservation and stock commit together. The unique key constraint remains a durable guard; hash collisions serialize requests without conflating their identities.

Expiry locks due reservation rows with FOR UPDATE SKIP LOCKED. Stock restoration and expired status commit together. A crash before commit rolls back; a crash after commit leaves persisted completed state. Because this operation has no external effects, a lease framework or saga would add complexity without improving its guarantee.

## Structure

```text
cmd/reservation/       startup, loopback checks and shutdown
internal/reservation/  transactional store, HTTP boundary, expiry worker, tests
migrations/            initial development schema
scripts/               verification and controlled restart demo
compose.yaml           local PostgreSQL
docs/                  active small plan and historical design references
```

[AtlasPay](https://github.com/Falcon-J/AtlasPay) remains a reference. This project does not claim production throughput, high availability, real payment integration, or a big-tech offer.

## Verification recorded on 2026-10-09

Real PostgreSQL 18 tests passed for last-item contention, concurrent key replay, conflicting reuse, rollback, expiry across workers, and startup recovery. Formatting, Go vet, build/typecheck and module checks passed. The separate-process demo killed/restarted its worker and proved expiry recovery and all ten units becoming reservable again. One read-only review found no critical or important issues.

Docker retry: Compose started PostgreSQL successfully and it became healthy. Formatting, module verification, vet, build/typecheck, and the full non-race suite passed against that container. The separate-process recovery demo also passed after earlier connection/request timeouts during the image download.

The race suite compiled and ran in Linux Docker with Go 1.25.0. It failed TestConcurrentReplay: two requests exceeded the store's three-second deadline during concurrent replay. No data-race warning was reported, but the suite is **not passing**. This timeout remains an investigation item; no application deadline was increased to make the check pass. Windows still has a 32-bit-only C compiler; the Docker command above avoids that compiler limitation. scripts/verify.ps1 keeps race checks required by default, and -SkipRace explicitly reports the omitted check.