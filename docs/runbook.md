# Local operation and failure exercises

All commands run from the repository root in PowerShell 7. These exercises deliberately stop the project database. Do not run them alongside tests or other users of this Compose instance.

## Migrations

Fresh Compose volumes apply `001_initial.sql`, then `002_reservation_lifecycle.sql`.
For an existing development volume that has only migration 001:

```powershell
Get-Content -Raw migrations/002_reservation_lifecycle.sql | docker compose exec -T postgres psql -U reservation -d checkout_test -v ON_ERROR_STOP=1
```

Apply migration 002 once; repeated application is not required. It changes only the status constraint, preserves rows, and runs in a transaction. Existing databases with the old constraint are rejected by application startup.

## Worker crash/restart

Create a new disposable demo database and apply both migrations. Do not reuse one whose inventory has been consumed.

```powershell
docker compose exec -T postgres createdb -U reservation checkout_demo_worker
Get-Content -Raw migrations/001_initial.sql | docker compose exec -T postgres psql -U reservation -d checkout_demo_worker -v ON_ERROR_STOP=1
Get-Content -Raw migrations/002_reservation_lifecycle.sql | docker compose exec -T postgres psql -U reservation -d checkout_demo_worker -v ON_ERROR_STOP=1
go build -o bin/reservation.exe ./cmd/reservation
./scripts/demo.ps1 -DatabaseURL 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_demo_worker?sslmode=disable'
```

The script uses five-second client timeouts (allowing the three-second server operation and bounded cleanup) and at most three same-key retries for transport failures or 503. Its demo hold lasts fifteen seconds. It owns and terminates only its API/worker processes. It reserves ten units, repeats the key, kills the worker, verifies overdue state persists, restarts the worker, and reserves all ten units again.

## Database outage/recovery

Repeat the database initialization commands above with a new name `checkout_demo_outage`, then:

```powershell
./scripts/failure-demo.ps1 -DatabaseURL 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_demo_outage?sslmode=disable'
```

This script stops only Compose service `postgres`, expects readiness and reservation reads to return 503, keeps PostgreSQL unavailable past the thirty-second hold deadline, restarts it, checks overdue expiry recovery, and verifies stock can be reserved again. Its finally block attempts database restoration even on failure. Logs live in ignored `.tmp/`.

## Replay benchmark

```powershell
$env:TEST_DATABASE_URL = 'postgres://reservation:local-test-only@127.0.0.1:54329/checkout_test?sslmode=disable'
go test ./internal/postgres -run '^$' -bench BenchmarkCommittedReplay -benchtime=100x -count=3
```

Setup is excluded. `ns/op` measures serial committed-replay latency, not production throughput or end-to-end percentiles. The benchmark checks that only one reservation exists and stock was consumed once. Keep CPU load and database configuration comparable; retain raw samples.

## Stop local resources

`docker compose stop postgres` stops the database without deleting data. `docker volume ls` lists volumes. Keep the PostgreSQL data volume and Go build cache until you intentionally decide their data is no longer needed.
