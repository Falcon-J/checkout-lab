# Integration and dependency decisions

Date: 2026-10-08. Selected architecture baseline; no accounts provisioned or packages installed.

## ADR 1: Stripe sandbox with hosted Checkout

Choose Stripe's official Go SDK and hosted Checkout, mode payment, card method only, automatic capture, USD. Use the SDK Client rather than the legacy package-global API pattern. No card details pass through our server. Account availability is an execution prerequisite; do not replace a blocked real integration with fake evidence.

Provider request snapshot: order UUID in client_reference_id and session metadata; the same order UUID in payment_intent_data.metadata; one line item containing immutable name, USD price and quantity; fixed configured success/cancel URLs; no client-supplied redirects. Set only documented SDK fields and pin the API/webhook version compatible with the installed SDK.

Local reservation lifetime is 15 minutes. Do not set Stripe expires_at to that deadline: the provider's configurable lifetime starts at 30 minutes. Use default provider expiry and explicitly expire an open session when the local reservation expires. Do not initiate a new logical creation for expired local orders. Recovering an already ambiguous creation may require replaying its original request/key; if that establishes a session, expire it immediately and never expose its URL. A session created concurrently with local expiry is discovered then expired or refunded if already paid. [Creation contract](https://docs.stripe.com/api/checkout/sessions/create), [Expire operation](https://docs.stripe.com/api/checkout/sessions/expire).

Create key: checkout/create/{order_uuid}/v1. Refund key: checkout/refund/{order_uuid}/v1. Save the full original request before the first call and reuse it exactly. Record a separate first_call_at and retry deadline for session creation and refund creation before each operation's first network call. Permit retries only before that operation's first_call_at + 23 hours. This is our conservative policy against provider key pruning, not a provider guarantee. After that point use known-ID reads or operator review; never retry unknown-ID mutation blindly. [Provider idempotency contract](https://docs.stripe.com/api/idempotent_requests).

SDK automatic retries are disabled for mutating calls so the durable worker owns budgets and identities. A read may be retried under the same worker budget. Successful HTTP response is not necessarily successful financial outcome.

### Observation mapping

- Create session establishes session ID, hosted URL and pending payment.
- Retrieve session, expanding payment_intent if necessary. Bind the intent ID only after correlation validation.
- paid plus a succeeded intent with the expected amount/currency establishes payment success.
- open/unpaid or an intent requiring a payment method is still pending, including after a declined card; the customer may retry within the same hosted session.
- expired/unpaid establishes local cancellation after confirming no succeeded payment exists.
- Transport error is unknown; ambiguous expire/refund operations are reconciled.
- No-charge/zero-total sessions are impossible by catalog constraints and should trigger operator review if observed.
- Conflicting identity, live-mode, amount, or currency enters operator review without confirming fulfillment.

Relevant events: checkout.session.completed, checkout.session.expired, payment_intent.succeeded, payment_intent.payment_failed, refund.created, refund.updated, refund.failed. Events trigger current-object retrieval/normalization; do not assume delivery order or fulfill from a redirect. A payment_failed event is not automatically a terminal order failure. Verify raw bytes with the SDK webhook helper. [Webhook documentation](https://docs.stripe.com/webhooks).

Recovery issues one full refund for the established PaymentIntent, tagged with order metadata. Refund pending or ambiguous stays recovery_required; only provider-confirmed succeeded becomes refunded. Failed/canceled refunds enter operator review; do not silently create a replacement refund. A known existing full refund must be recognized by retrieval before mutation. [Refund contract](https://docs.stripe.com/api/refunds/create).

## ADR 2: Managed Auth0 access tokens

Use a managed issuer and Auth0's JWT middleware with cached JWKS. Allow only RS256; validate exact issuer, API audience, exp, nbf with 30 seconds maximum clock tolerance, nonempty sub, and operation scope. Accept access tokens intended for this API, not arbitrary OIDC ID tokens. Customer identity is (issuer,subject), not email.

No custom registration/login/token issuance endpoints. Tests use a local JWKS fixture with generated keys and explicit test issuer/audience; production mode cannot enable an identity bypass. Unknown key/invalid signature rejects the request; an infrastructure fetch failure without sufficient cached keys is 503, never anonymous access.

The first milestone is API-driven with test fixtures. The later small browser uses an official Auth0 client integration with Authorization Code + PKCE; in-memory token storage. Do not manually implement browser OAuth. Keep the API same-origin where possible; if browser origin differs, allow an exact configured origin and no wildcard credentials. [Access token validation](https://auth0.com/docs/secure/tokens/access-tokens/validate-access-tokens), [Official Go middleware](https://github.com/auth0/go-jwt-middleware).

## ADR 3: Minimal Go dependency set

| Capability | Choice | Why |
| --- | --- | --- |
| HTTP, JSON, logs, contexts, tests | net/http, encoding/json, log/slog, context, testing | Standard library is sufficient |
| PostgreSQL runtime | github.com/jackc/pgx/v5 and pgxpool | Native transactions and driver-owned connection pool |
| SQL migrations | github.com/pressly/goose/v3 | Existing ordered migration machinery |
| Provider | github.com/stripe/stripe-go/v87 | Official transport, errors and signature helper |
| Token validation | github.com/auth0/go-jwt-middleware/v3 | Existing JWT/JWKS validation and middleware |
| Browser identity, later | Official Auth0 SPA SDK | Existing PKCE and token lifecycle implementation |

Major paths reflect official docs checked on this date. Resolve compatible stable patch versions at the first task, record them in go.mod/go.sum (including tool dependencies), and record image digests. Never use floating latest in committed runtime/CI configuration. No ORM, application framework, generic repository library, mocking framework, Redis or broker client initially.

Go minimum 1.27, initial toolchain target 1.27.1, PostgreSQL major 18. Verify current patched versions and SDK compatibility at implementation. [Go releases](https://go.dev/doc/devel/release), [pgx](https://github.com/jackc/pgx), [Goose](https://github.com/pressly/goose), [Stripe SDK](https://github.com/stripe/stripe-go).

Use official examples as small references, not wholesale starter applications. Record source and license when adapting substantial snippets. The business state machine remains our application code.


### Verified initial dependency candidates

Use pgx v5.11.0, Goose v3.28.0, stripe-go v87.0.0 and go-jwt-middleware v3.3.0 as the initial pins. Their tagged releases were verified on the documentation date; compatibility and checksums still require the Task 1 build/module verification. Browser SDK version is selected in the UI task, not installed prematurely.

Release references: [pgx](https://github.com/jackc/pgx/releases/tag/v5.11.0), [Goose](https://github.com/pressly/goose/releases/tag/v3.28.0), [Stripe](https://github.com/stripe/stripe-go/releases/tag/v87.0.0), [JWT middleware](https://github.com/auth0/go-jwt-middleware/releases/tag/v3.3.0).
## Execution prerequisites, not design gaps

Before sandbox acceptance, supply an available Stripe test account/API key/webhook signing secret and an Auth0 issuer/audience with a test user. Keep credentials outside Git and logs. No paid subscriptions, real transactions, or external account provisioning are included in the documentation task.

First milestone tests need neither external account. Real provider and identity checks are separate and cannot be claimed from local fixtures.

## ADR 4: Local reliability lab and optional free hosted demo

The primary runtime is local Go plus PostgreSQL in Compose, with Auth0 Free and accessible Stripe sandbox keys. No recurring hosted compute/database subscription is required. Use the official Stripe CLI for local callback delivery.

Optional public demonstration uses one Render Free web service and Neon Free PostgreSQL. Sleeping suspends in-process worker execution; recovery resumes after wake and strict timing is proved locally. No always-on free-worker guarantee, paid upgrade or keepalive service is implied.

Verify sandbox access before provider-adapter implementation; Stripe's India signup policy may block new accounts. A provider switch requires a revised provider-specific contract, not renamed SDK calls. See [free-tier strategy](free-tier-strategy.md) for current official limits, profiles and cost boundaries.
