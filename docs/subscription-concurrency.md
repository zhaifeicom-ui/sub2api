# Subscription concurrency limits

## Configuration

- Group create/edit, subscription section: **Subscription concurrency per user** (`subscription_concurrency`). 0 adds no extra limit.
- Admin subscriptions table, **Concurrency** action: set `concurrency_limit` on one user's subscription. 0 inherits the group default; positive values override it. No changes to global user concurrency.
- API: `PUT /api/v1/admin/subscriptions/:id/concurrency` with `{ "concurrency_limit": 2 }` (admin authentication required). Integer range 0–100000; missing/null/fractional/negative values rejected.
- New columns default to zero. Existing subscriptions and subscriptions fulfilled via purchase/redemption inherit the group setting. Existing quotas, expiry, balance and renewal behavior are not changed. Renewals retain individual overrides.

## Enforcement

Authentication attaches a trusted active subscription scope to the request context for standard and Google API-key routes. `ConcurrencyService.AcquireUserSlot` atomically reserves an independent Redis subscription slot, then the existing global user slot. Failure of the global reservation releases the subscription reservation. Releases are idempotent and run with a bounded background context, so cancellation does not prevent release. Existing handler waiting, 429 and stream cleanup mechanisms remain in use. Redis failure is fail-closed when a subscription limit is configured.

Keys are `concurrency:subscription:<subscription-id>`; no collision with user/account/API-key counters. Subscription rows identify a user/group pair, so multiple keys share the limit, other users and other subscriptions do not. The existing Redis Lua/clock/TTL contract is reused. Orphan slots expire after the configured regular-slot TTL; long requests have the same TTL limitation as existing user slots. On configuration changes, already-running requests are not terminated. Existing WebSocket connections retain authentication-time configuration; reconnect for changed limits.

HTTP Chat/Responses/Messages, Gemini and Responses WebSocket entry points use the common user slot service. Persistent Live/WebRTC session leases have a separate lifecycle; creating a Live session is rejected for capped subscriptions until dedicated persistent subscription leases are implemented. Uncapped groups and existing call management are unchanged.

## Persistence and deployment

Migration `244_subscription_concurrency.sql` adds checked non-null integer columns only. Ent schema and generated code, group copies, repository reads/writes, auth snapshots and DTOs carry the fields. Individual changes use a column-specific UPDATE (never rewrite quota counters or renewal state), then invalidate subscription caches. Group updates use existing auth-cache invalidation.

Production rollout backs up the database, builds externally and replaces only the application container. Existing PostgreSQL/Redis containers are not restarted.

## Customer display and balance checkout

Plan cards, purchase confirmation and My Subscriptions display concurrency. User overrides are resolved before the group default. Balance checkout at `POST /api/v1/payment/subscriptions/balance` requires JWT authentication, a listed plan, expected USD price and a 16–128-character Idempotency-Key. Price, duration and group availability are revalidated server-side. A user-row write lock serializes purchases and normal balance billing. Frozen funds are excluded using decimal arithmetic. Order creation, deduction, subscription assignment/renewal and assignment audit share one transaction. Retry keys are scoped to the user; completed requests replay without charging or extending again. The browser retains its operation key on ambiguous failures. Existing overrides and quota consumption survive active renewals.

Internal orders use payment_type=balance, provider_key=internal_balance, currency=USD and zero payment fee. They are excluded from external payment revenue statistics and receive no second affiliate rebate. No payment provider is contacted. Existing subscription refund restrictions apply. Cache invalidation happens after commit. Listed USD prices are used directly, regardless of the optional display-only currency label; checkout explicitly states USD.

No price/plan is created automatically. Operators must list a subscription plan and enable the payment entry point before selling. Atomicity and replay tests run against a real SQLite transaction; only SELECT FOR UPDATE is adapted in that test fixture because SQLite serializes writes on one connection. The production repository's PostgreSQL row lock remains unchanged.

## Verification

Service tests cover per-user/subscription isolation, multi-key sharing, global cap composition, rollback on Redis error/global-full, concurrent override enforcement, idempotent release and update field isolation. Redis adapter tests use miniredis and the actual Lua script, including release after context cancellation and orphan expiry. Admin endpoint tests cover zero/inheritance and invalid JSON values. Auth snapshot regression preserves group defaults. Frontend regression covers setting overrides and restoring inheritance; existing group copy/bulk actions and locale completeness tests retained.
