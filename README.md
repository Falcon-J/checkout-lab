# Checkout Lab

A focused checkout project for learning correctness, concurrency, and recovery with Go, PostgreSQL, and Docker Compose.

## Current status

Project foundation created on 2026-10-08. The architecture and state model are accepted as the starting design. This repository contains documentation; it does not yet contain a runnable application.

## Product

One store, one currency, one SKU per order. Reserve stock, integrate a payment-provider sandbox, and recover safely from duplicate requests, uncertain payment outcomes, and process crashes.

## Design documents

- [Checkout design](docs/checkout-design.md): scope, invariants, ownership, states, failure experiments, and planned folder structure.
- [Repository decision](docs/repository-decision.md): why the successor is separate and what to reuse from AtlasPay.
- [Preparation checklist](docs/preparation-checklist.md): remaining contracts and decisions before application implementation.

## Initial architecture

Customer browser -> Go application (HTTP API and worker) -> PostgreSQL.
The Go application also integrates with an external payment-provider sandbox and receives signed webhooks.

PostgreSQL is authoritative for local state. Provider timeouts leave an unknown outcome that must be reconciled. Inventory expiry and late payment success have an explicit refund recovery policy.

## Planned source structure

```text
cmd/checkout/          application startup
internal/checkout/    order lifecycle and coordination
internal/inventory/   stock and reservations
internal/payment/     payment lifecycle and provider adapter
internal/httpapi/     HTTP transport and ownership checks
internal/worker/      durable work and reconciliation
internal/database/    connections and migration execution
migrations/           versioned database changes
tests/integration/    cross-module scenarios
deployments/compose/  reproducible local runtime
docs/                 design and evidence
```

Source directories are created only when their first implementation slice needs them.

## Engineering approach

Use official documentation and small maintained examples for established mechanisms. Keep business rules explicit. Prefer the Go standard library where sufficient, real PostgreSQL for transactional tests, and deterministic fault injection alongside real sandbox integration.

Redis, Kafka, and Kubernetes require a specific measured problem or learning experiment before introduction.

## Running and verification

There are no build, lint, typecheck, or test commands yet because no application exists. The first implementation plan must define reproducible Go formatting, vet, build, test, and database-integration commands.

## Relationship to AtlasPay

[AtlasPay](https://github.com/Falcon-J/AtlasPay) remains a reference implementation. Historical AtlasPay results do not verify this project. New behavior must have fresh evidence.
