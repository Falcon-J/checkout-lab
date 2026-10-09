# Repository decision: preserve AtlasPay, build a focused successor

Date: 2026-10-08
Status: recommendation; repository creation is not performed.

## Context

The learning objective has changed from demonstrating a multi-service technology stack to understanding a small checkout system's correctness and recovery. Published AtlasPay documents a four-process topology, Kafka workers, shared infrastructure, smoke workflows, deployment manifests, and historical evidence. Existing work is useful reference material and should remain reproducible.

The local workspace inspected for this task contains Git metadata but no application checkout and no configured remote. Documentation is saved here; this workspace is not evidence that published application files were removed.

## Options

| Option | Benefit | Cost |
| --- | --- | --- |
| Fresh repository, preserve AtlasPay | Clear scope and coherent build history; old evidence remains intact | Reimplement a small core; selectively adapt verified ideas |
| Simplify AtlasPay in place | Preserve history and potentially reuse working behavior | Audit dependencies, service boundaries, scripts and docs; risk breaking evidence while removing complexity |
| Long-lived rewrite branch inside AtlasPay | Easy comparison with old code | Two divergent architectures under one project; eventual migration and naming ambiguity |

## Recommendation

Use a fresh repository after reviewing the design. Do not delete, overwrite, or formally archive AtlasPay now. Keep it as a reference and optionally add a README link when the successor has a working milestone. Do not maintain two active implementations indefinitely.

The justification is the changed learning objective, not a blanket preference for rewrites. Simplification in place would be preferable if backward compatibility, live users, or existing deployment continuity were requirements. None has been established for this learning rebuild.

## Reuse policy

Reuse understanding, failure cases, and test ideas first. Adapt implementation only after inspecting its assumptions and verifying compatibility with the new state model. Do not copy the old orchestration framework, infrastructure, or dependency set wholesale.

Useful references include idempotency concurrency checks, outbox recovery, saga crash replay, health contracts, and failure evidence. Historical logs remain historical proof; they cannot prove the successor works.

## Safe transition

1. Review `checkout-design.md` and resolve product defaults.
2. Complete contracts and engineering decisions; approve an implementation plan.
3. Choose a repository name and create the new repository explicitly.
4. Build one working slice at a time with fresh evidence.
5. Link the projects once the successor has a reproducible milestone.

No new GitHub repository, code deletion, remote mutation, or application implementation is authorized by this recommendation alone.
