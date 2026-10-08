# Preparation checklist

Date: 2026-10-08

The user agreed to proceed with a separate project following the checkout design. The design is the starting baseline, not evidence of implemented behavior.

## Complete before application code

- [ ] Specify customer authentication and its test strategy without introducing a custom authentication platform.
- [ ] Select the payment provider and confirm sandbox access, hosted checkout behavior, idempotency retention, webhook signatures, and cancellation/refund semantics against official documentation.
- [ ] Define HTTP operations, request/response schemas, status codes, ownership checks, idempotency-key format, and payload/quantity limits.
- [ ] Define PostgreSQL tables, constraints, indexes, transaction boundaries, lock ordering, and migration workflow.
- [ ] Specify worker claims, lease duration, retry budgets, reconciliation cadence, operator intervention, and event retention.
- [ ] Select the minimal Go dependencies and toolchain, with version and rationale documented.
- [ ] Write and review the implementation plan for the first database-backed checkout slice.

## First implementation milestone

Persist a checkout and reserve stock in one transaction. Demonstrate that concurrent buyers cannot oversell, duplicate submissions create one order, conflicting idempotency-key reuse is rejected, and unauthorized reads disclose no order data. No payment or infrastructure extension is required for this milestone.

## Later milestones

1. Real provider sandbox integration with explicit uncertain outcomes.
2. Durable recovery and deterministic crash experiments.
3. Optional separate worker process and event-delivery experiments.
4. Optional Redis or Kubernetes learning exercises with stated hypotheses.

This checklist is a preparation roadmap, not an executable implementation plan. Do not claim completion or install dependencies from it.
