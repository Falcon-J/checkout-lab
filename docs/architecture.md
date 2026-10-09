# Reservation backend design

Purpose: reserve limited workshop seats and demonstrate correctness, contention, and recovery with Go and PostgreSQL. Local single-operator API; no claim of real customers or production availability.

Approved structure: cmd/reservation wires internal/reservation (model, service, worker), internal/postgres (transactions), and internal/httpapi (transport). pgx remains the only direct dependency. Existing create/read API and bearer contract remain compatible; SKU is the event identifier.

Lifecycle: held -> confirmed retains stock; held -> cancelled returns stock once; due held -> expired returns stock once. Repeating the same terminal action returns the current state. Conflicting terminal actions return 409. Confirming an overdue hold expires it atomically and returns 409. Confirm/cancel serialize on the reservation row; expiry shares those locks.

Create retains conditional stock decrement and transaction-scoped key locking for new keys. A committed-key read can return a snapshot without taking that lock. Recheck inside the transaction remains necessary for racing initial requests. Service validates inputs and bounds operations to three seconds. PostgreSQL owns atomicity. HTTP consumes operations and a separate readiness function.

Evidence: real PostgreSQL integration tests, concurrency tests, race detector, process restart exercise, and a reproducible contention benchmark. Tests and benchmarking use disposable databases only. Publish truthful README and resume bullets after checks and final review pass. No push is authorized by this readiness request.
