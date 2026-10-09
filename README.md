# Reservation backend

A focused Go/PostgreSQL backend for reserving limited inventory, such as workshop seats. One executable runs the HTTP API and an expiry worker, together or separately. This is a local engineering project; there are no real customers, payments, or production capacity claims.

## What it demonstrates

- Atomic inventory reservation without overselling.
- Same-key replay returns the original reservation; conflicting reuse is rejected.
- Confirmation retains stock; cancellation and expiry restore it exactly once.
- Competing terminal transitions serialize on the reservation row.
- Expiry resumes from PostgreSQL after a worker or database restart.
- Deadline evaluation occurs after row-lock acquisition.

## Structure

```text
cmd/reservation/        configuration, dependency wiring, shutdown
internal/reservation/  model, validation, service policy, expiry worker
internal/postgres/     SQL transactions, integration tests, replay benchmark
internal/httpapi/      HTTP parsing, authorization, responses, unit tests
migrations/            ordered SQL migrations
scripts/               verification and controlled failure exercises
docs/                  architecture, experiments, runbook
docs/archive/          superseded designs and implementation history
```

The domain consumes a narrow repository interface. HTTP consumes the domain service and a readiness callback. Database-resetting integration tests reside in one package so Go's package parallelism cannot reset their shared database concurrently. pgx is the only direct application dependency.

## Run locally

Requires Go 1.25+, Docker with Linux containers, and PowerShell 7 for scripts. Run from this directory.

```powershell
docker compose up -d --wait postgres
$env:DATABASE_URL = 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_test?sslmode=disable'
$env:API_TOKEN = [Convert]::ToHexString([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
go run ./cmd/reservation
```

Both database publishing and HTTP bind to loopback. The database password is a local development credential. One bearer token grants access to all reservations; customer identity and multi-tenant authorization are outside this scope. Modes `-mode api` and `-mode worker` run components separately. Default lifetime: 15 minutes; expiry sweep: 5 seconds and immediately on worker startup.

New database volumes apply all migrations in numeric order. Existing volumes need explicit migration `002_reservation_lifecycle.sql`; see the [runbook](docs/runbook.md). Never remove a volume to apply a migration.

```powershell
$headers = @{ Authorization = "Bearer $env:API_TOKEN"; 'Idempotency-Key' = [guid]::NewGuid().ToString('N') }
$row = Invoke-RestMethod -NoProxy -TimeoutSec 5 http://127.0.0.1:8080/reservations -Method Post -Headers $headers -ContentType application/json -Body '{"sku":"book-go","quantity":1}'
Invoke-RestMethod -NoProxy -TimeoutSec 5 "http://127.0.0.1:8080/reservations/$($row.id)/confirm" -Method Post -Headers $headers
```

`book-go` is the retained demonstration SKU with ten units. New reservation: 201; matching replay: 200 and original ID/current status. Read: `GET /reservations/{id}`. Confirm/cancel: empty-body `POST /reservations/{id}/confirm` or `/cancel`; repeated same terminal action: 200; conflicting action: 409. Invalid input: 400, oversized create body: 413, unsupported create media type: 415, missing record: 404, unavailable database: 503. Health endpoints are public locally.

Held reservations can remain visible past their deadline until expiry commits. Confirming a due hold atomically expires it and returns 409. Clients receiving a timeout or 503 can retry creation with the same key; a failed acknowledgement does not prove the transaction failed.

## Verify

**Tests and benchmarks reset data. Use only a disposable database named `checkout_test` or `checkout_test_*`.** Without `TEST_DATABASE_URL`, database tests skip.

```powershell
./scripts/verify.ps1 -DatabaseURL 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_test?sslmode=disable'
```

Windows requires a 64-bit C compiler for the race detector. With this machine's 32-bit compiler, use `-SkipRace` for Windows checks, then run the required race check in Linux. Run suites sequentially against the shared test database.

```powershell
$projectDirectory = (Get-Location).Path
$moduleDirectory = go env GOMODCACHE
docker run --rm --network reservation-lab_default --mount "type=bind,source=$projectDirectory,target=/src,readonly" --mount "type=bind,source=$moduleDirectory,target=/go/pkg/mod,readonly" --mount 'type=volume,source=reservation-lab-go-build,target=/root/.cache/go-build' -w /src -e GOPROXY=off -e 'TEST_DATABASE_URL=postgres://reservation:local-test-only@postgres:5432/checkout_test?sslmode=disable' golang:1.25.0 go test -race ./... -count=1 -v
```

The first Linux build can take several minutes; the named volume retains its build cache. Populate the module cache with `go mod download` if needed. Repeatable process exercises and benchmarking commands are in the [runbook](docs/runbook.md). Actual verification and measurement outcomes are recorded in [experiments](docs/experiments.md).

The GitHub workflow runs formatting, module verification, vet, build, ordered migrations, and the race suite on a fresh PostgreSQL service. Hosted execution is pending the first push. It uses the official [checkout](https://github.com/actions/checkout) and [setup-go](https://github.com/actions/setup-go) actions.

## Resume entry

Use these bullets only after the recorded readiness checks pass:

**Reservation Backend | Go, PostgreSQL, Docker**
- Built a Go/PostgreSQL reservation backend with idempotent requests, atomic confirmation/cancellation, and persisted expiry recovery.
- Tested oversell prevention and single stock restoration with real PostgreSQL concurrency tests and a controlled worker crash/restart exercise.
- Investigated replay contention and row-lock timing; removed unnecessary locking from committed replays and checked expiration after lock acquisition.

The architecture and experiments explain the trade-offs. This project provides engineering discussion material alongside interview preparation and work experience.
