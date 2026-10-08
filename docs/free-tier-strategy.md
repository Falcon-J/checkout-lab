# Free-tier strategy

Verified: 2026-10-08. Scope: personal learning and small demonstrations, not production availability.

## Decision

Use a local reliability lab as the default. There is no required recurring cloud-hosting subscription. A public hosted demo is optional and allowed to sleep. Keep the same Go/PostgreSQL business architecture in both profiles; do not replace it with platform-specific serverless functions merely to fit a free tier.

The budget is zero additional service spend. Existing computer, internet and electricity costs are outside that statement. Free-service eligibility and quotas must be checked again before provisioning. Do not upgrade plans, add paid resources, buy domains or enable overages without explicit authorization.

## Profile A: local reliability lab

| Component | Choice | Cost and reason |
| --- | --- | --- |
| Application and durable worker | Local Go executable/container | No hosted compute subscription; full control of crash timing |
| Database | Local PostgreSQL 18 in Compose | No hosted database quota; real transactions and locks |
| Container tools | Docker Personal when eligible | Suitable for eligible personal/education use; verify license eligibility |
| Customer identity | Auth0 Free, default tenant domain | No paid custom domain or advanced enterprise features needed |
| Payments | Accessible Stripe sandbox, test keys only | Real provider integration; sandbox does not process real card-network payments |
| Callback delivery | Official Stripe CLI forwarding to localhost | No paid tunnel required for attended local tests |
| CI | Standard Linux GitHub Actions runner if repository is public | Public standard runner compute is free under current GitHub policy |
| Evidence | Local logs and committed redacted experiment notes | No paid metrics, tracing or log service required |

Auth0 currently includes up to 25,000 monthly active users and permits signup without a credit card. Use basic user login and API access-token validation. Custom-domain verification and optional premium features are unnecessary. [Auth0 pricing](https://auth0.com/pricing).

Stripe sandbox transactions are isolated from live payment processing. They exercise provider objects, API calls and callbacks; they are not real money settlement. [Stripe sandbox contract](https://docs.stripe.com/sandboxes).

Forward callbacks during an attended local run:

```text
stripe listen --forward-to http://localhost:8080/api/v1/webhooks/stripe
```

Use the signing secret for that listener, stored locally and never committed. This is a later execution command, not a command run during documentation. [Stripe CLI](https://docs.stripe.com/cli).

[Docker pricing](https://www.docker.com/pricing/) defines Personal eligibility. [GitHub runner policy](https://docs.github.com/en/actions/how-tos/write-workflows/choose-where-workflows-run/choose-the-runner-for-a-job) defines free standard public-repository runners. Private repositories have included quotas; never publish a private repository merely to get free CI without the user's choice.

## Profile B: optional hosted demonstration

Use one Render Free web service for the Go application and its in-process worker, and one Neon Free PostgreSQL project. Serve the small browser from the same application. Use provider-generated domains and TLS rather than purchasing a domain. Keep Auth0 Free and payment sandbox mode.

This is a proposed deployment, not an account or resource already created.

| Service | Current relevant limits | Design consequence |
| --- | --- | --- |
| Render Free web service | Sleeps after 15 minutes without inbound traffic; 750 instance hours per workspace/month; cold start around one minute; ephemeral filesystem | PostgreSQL owns all durable state; no local SQLite/files for truth; no dedicated free worker assumption |
| Neon Free | 1 GB storage/project; 100 CU-hours/project/month; 0.25 CU can use that allowance in 400 hours | Small cohorts and short demo sessions; no continuous database polling for an unattended service |
| Auth0 Free | 25,000 monthly active users | More than sufficient for test customers; basic tenant domain |
| GitHub standard public runners | Free compute, subject to policy and storage/usage rules | Linux PR/push checks; short artifact retention |

[Render limits](https://render.com/docs/free), [Neon October 2 update](https://neon.com/blog/neon-free-plan-1-gb-per-project), [Auth0 pricing](https://auth0.com/pricing), [GitHub policy](https://docs.github.com/en/actions/concepts/billing-and-usage).

Render's free PostgreSQL expires after 30 days, so it is not selected as the persistent demonstration database. Render may bill bandwidth/build overages if a payment method is present; without one, relevant services/builds may be suspended instead. Verify current billing controls and keep paid resources disabled. [Render limits and billing behavior](https://render.com/docs/free).

Neon figures above use the newer October 2 official update, not older pages that still list 0.5 GB. Quota dashboards are authoritative for the actual account at provisioning.

## Sleeping is a recovery condition

A hosted service that sleeps does not run its expiry/reconciliation worker while asleep. A durable due_at value is not an external scheduler. Do not promise stock release, callback handling or refunds within the local worker's schedule during sleep.

On startup, resume the existing sweeps and reclaim expired leases before reporting worker readiness. Evaluate reservation expiry against current database time on every confirmation transition; never allow a late success to consume released/expired stock. GET must hide payment URLs for expired reservations, even before the expiry sweep runs.

Work survives in PostgreSQL and converges after the process resumes, subject to retry identity windows and operator escalation. A process waking after more than 23 hours must not blindly replay an unknown-ID financial mutation.

Do not add synthetic keepalive traffic to defeat sleep. Do not introduce a cron service, separate worker deployment or broker merely to keep the free demo awake. Demonstrate strict timing/crash behavior locally and label hosted results as best-effort demonstrations.

## Quota-conscious engineering

- Run integration, contention, load and crash tests against local/disposable PostgreSQL, not Neon.
- Use pgxpool with a maximum of 5 hosted connections initially; release it on shutdown. Pool size is a profile tuning value, not a correctness mechanism.
- Do not poll database health from an external uptime monitor or leave the browser polling after its order is settled.
- While an order is unresolved, browser polls at most every 5 seconds and stops when its page closes; final states stop polling.
- Current 5-second sweeps run only while the application is awake. When the host sleeps, the process and its database polling stop; durable due work remains in PostgreSQL.
- Store only required event fields; expire processed payloads according to the runbook. Keep small test catalogs/cohorts; keep event IDs for deduplication.
- Use default domains, basic logs, and short CI artifact retention. No SMS, paid email service, paid dashboard or always-on telemetry backend.

Polling prevents database inactivity; free compute is not unlimited. At an illustrative constant 0.25 CU, 24 hours/day over 30 days uses 180 CU-hours, exceeding the 100 CU-hour allowance. This is arithmetic from Neon's published allowance, not a workload measurement.

## Payment account access gate

Confirm access to sandbox keys and required callbacks/refund APIs before investing in the provider adapter. New Stripe accounts in India are invite-only; a free testing strategy cannot assume signup is available to every user. [Stripe account policy](https://support.stripe.com/questions/stripe-accounts-are-invite-only-in-india).

If Stripe is available, keep the existing adapter design. If it is unavailable, select an accessible provider sandbox and revise its specific checkout, idempotency, capture, refund and webhook contract first. Do not assume another provider shares Stripe's guarantees, currency configuration or session-expiry behavior. No provider switch is made by this document.

Local PostgreSQL milestones can proceed without a payment account. Real sandbox acceptance remains a separate gate; test doubles are useful but are not evidence that the real integration works.

## Completion and cost boundary

The first useful learning deliverable is local authenticated checkout with transaction/concurrency proof. The complete project adds actual sandbox integration and durable recovery. Hosting is optional.

Zero recurring cloud spend is achievable for the attended learning lab with eligible tools and accessible test accounts. A continuously available hosted worker with strict timing guarantees is not claimed under these free plans.

No accounts, deployment, paid resources, trial subscriptions or payment methods were created or changed by this revision.
