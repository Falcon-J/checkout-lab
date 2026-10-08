# HTTP API contract

Date: 2026-10-08. Status: implementation contract for the first version.
These are proposed application endpoints, not claims about existing AtlasPay endpoints.

## Transport and identity

Serve a versioned JSON API under /api/v1. HTTPS outside loopback development. Authentication is an RS256 access token for the configured issuer and API audience; derive the customer key from validated issuer and subject. Never accept customer identity in request JSON or a client identity header. See [integration decisions](integration-decisions.md).

Use RFC3339 UTC timestamps. Amounts are integer minor units in USD for the first version. UUIDs identify orders. Errors use application/problem+json with type, title, status, code, and request_id; omit SQL, provider payloads, tokens, and stack traces.

Generated request IDs are 32 lowercase hex characters from crypto/rand. Return X-Request-ID; do not trust inbound request IDs. Set Cache-Control: no-store on authenticated responses.

## Routes

| Method and route | Access | Success | Purpose |
| --- | --- | --- | --- |
| GET /health/live | Public | 200 | Process liveness |
| GET /health/ready | Public | 200 or 503 | Database reachable, migrations current, worker running |
| GET /api/v1/products | Authenticated | 200 | Catalog, maximum 100 entries ordered by SKU |
| POST /api/v1/checkouts | Authenticated | 202 | Durable order and reservation accepted |
| GET /api/v1/orders/{order_id} | Owner only | 200 | Order and payment setup/status |
| POST /api/v1/webhooks/stripe | Valid provider signature | 200 | Durable receipt, not business completion |

No stock mutation, refunds, worker control, or user management endpoints are public. Health output is {"status":"up"} or {"status":"not_ready"} without dependency details. Stripe reachability is a metric/operator condition, not a synchronous readiness dependency.

## Checkout request

Required Idempotency-Key: 16-128 characters, matching [A-Za-z0-9_-]+. Scope is the validated customer. Never expire checkout keys in the first version.

Content-Type must be application/json. Body limit: 4 KiB. Reject unknown fields, multiple JSON values, duplicate object keys, non-integer quantity, and invalid UTF-8. Accepted body:

```json
{"sku":"book-go","quantity":1}
```

SKU: 1-64 lowercase ASCII characters matching [a-z0-9][a-z0-9_-]*. Quantity: 1-10. Do not normalize keys, SKU case, or customer identity silently. Unit price: 50-100000 minor units; amount maximum: 1000000. Catalog prices are validated during operator seeding; multiplication uses checked int64 arithmetic.

Fingerprint the validated request as SHA-256 of the UTF-8 string v1 + newline + SKU + newline + decimal quantity. The fixed SKU alphabet makes this encoding unambiguous. Money is excluded because retries must replay the original server-side price snapshot.

## Checkout response and replay

Return 202, Location: /api/v1/orders/{id}, and a JSON object with order_id, status, amount_minor, currency, reservation_expires_at, payment_status, refund_status, payment_url, and recovery_action.

```json
{
  "order_id":"550e8400-e29b-41d4-a716-446655440000",
  "status":"pending",
  "amount_minor":2500,
  "currency":"usd",
  "reservation_expires_at":"2026-10-08T12:15:00Z",
  "payment_status":"not_started",
  "refund_status":"none",
  "payment_url":null,
  "recovery_action":"none"
}
```

The first response is persisted in the order's acceptance_response field. A matching retry returns that same 202 body and Location, even if the order has since progressed. GET provides current status. Return Idempotency-Replayed: true only on matching replay. A conflicting fingerprint returns 409 idempotency_conflict.

Reject insufficient stock with 409 out_of_stock and no order. A rolled-back/rejected request does not consume its key. After commit, losing the HTTP response does not undo acceptance.

If a duplicate key is being committed, database arbitration may wait within the 3-second transaction budget. A lock timeout returns 503 temporarily_unavailable with Retry-After: 1; the client must reuse its key.

## Order read and catalog

GET order returns the same fields as checkout, but with current state, plus sku, quantity, created_at, and updated_at. Expose payment_url only to its owner while the order is pending, the reservation is held/unexpired, and a provider session is open. recovery_action is none, reconciling, refund_pending, or operator_review. Do not expose provider IDs or internal errors.

Non-owner and nonexistent UUIDs both return 404 order_not_found. Malformed UUID returns 400 invalid_order_id after authentication. Catalog entries contain sku, name, unit_price_minor, currency, available_stock. Stock is advisory until reservation commits; do not promise the displayed quantity.

## Error mapping

| Status | Code | Condition |
| --- | --- | --- |
| 400 | invalid_request | Syntax, duplicated/unknown fields, format or bounds violation |
| 401 | unauthorized | Missing/invalid/expired token; include WWW-Authenticate: Bearer |
| 403 | insufficient_scope | Valid token lacking required checkout:read or checkout:write scope |
| 404 | product_not_found / order_not_found | Missing catalog SKU or owner-scoped order |
| 405 | method_not_allowed | Known route with unsupported method |
| 409 | out_of_stock / idempotency_conflict | Business conflict |
| 413 | payload_too_large | Request exceeds limit |
| 415 | unsupported_media_type | Checkout is not JSON |
| 503 | temporarily_unavailable | Database/JWKS infrastructure cannot serve safely |
| 500 | internal_error | Unexpected application error, logged with request ID |

Read scopes protect products/orders; write scope protects checkout. Public health and the signed webhook have their separate contracts. Return 404 for unknown routes. Do not classify known infrastructure failure as an invalid credential when token validity could not be established.

## Webhook receipt

Limit raw body to 256 KiB; verify Stripe-Signature against the raw bytes using the official SDK before parsing trusted fields. Signature tolerance: 5 minutes. Accept only test-mode events from the configured account.

A supported signed event is persisted with necessary event data and processing work in one transaction; acknowledge 200 only after commit. Duplicate event IDs return 200. Signed unsupported event types return 200 without business changes. Invalid signature/malformed event returns 400; oversized body 413; unavailable storage 503.

A callback that references a missing order is durable, then classified for bounded retry/operator review; do not fabricate an order from metadata. See [provider contract](integration-decisions.md) and [recovery](recovery-runbook.md).
