# Subscription Quota Reset Design

## Goal

Add subscription quota management to the existing redemption application without changing Sub2API itself.

Users can view their active Sub2API subscriptions, remaining validity, and configured daily, weekly, and monthly quota progress. Subscription tiers can grant a fixed number of manual quota resets for each redeemed validity period. A reset clears every quota window configured for that subscription group and consumes one reset from the currently active local entitlement period.

The design must prevent future stacked subscription periods from lending their reset allowance to the current period. Different Sub2API groups remain independent. Unused resets expire at the end of their period and never carry forward.

## Confirmed Product Rules

- The existing redemption application remains the first and default tab on the recharge page.
- A second `Subscription Management` tab lists all active Sub2API subscriptions for the signed-in user.
- Each Sub2API group is one displayed subscription because Sub2API merges repeated redemptions for the same user and group.
- Different groups have separate cards, periods, and reset allowances.
- Repeated redemptions for one group create ordered, non-overlapping local periods inside the single merged upstream subscription.
- Each successful redemption contributes one period with the redemption's `validity_days` and snapshotted reset allowance.
- A future period's allowance is locked until that period begins.
- An expired period's unused allowance is discarded.
- A period with zero resets still occupies its complete validity interval so later periods do not unlock early.
- One user action consumes one reset and clears all configured quota windows. Users cannot choose individual windows.
- A missing daily, weekly, or monthly limit is omitted from the UI and is not sent as a reset target.
- A group with no daily, weekly, or monthly limit is an unlimited subscription. It has no reset allowance or reset action.
- Existing subscriptions receive legacy allowances only when their entitlement can be traced to successful redemption records in this application.
- Active subscriptions acquired outside this application remain visible but receive no local reset allowance.
- Tier changes do not alter normal, already snapshotted entitlements.
- Legacy backfill is the one exception: the first positive reset configuration for a historical tier supplies the captured allowance to eligible current and future legacy periods for that tier.

## Current System Context

The application already has several relevant foundations:

- `redeem_tiers` stores subscription group and validity settings.
- `redeem_access_requests` stores immutable snapshots of selected tier settings and fulfillment status.
- access-request and redeem-code records identify the requesting upstream user and actual code user.
- `subscription_concurrency_grants` binds successful local subscription requests to merged upstream subscriptions and periodically reconciles their status.
- the tier APIs already enrich subscription tiers with upstream group names and daily, weekly, and monthly limits.

Quota reset periods have a different lifecycle from concurrency grants. A concurrency grant follows the complete merged subscription lifetime, while reset allowance belongs to one redemption interval. The reset feature therefore uses separate tables and services rather than adding period semantics to `subscription_concurrency_grants`.

## Upstream Contract

The Sub2API administrator API is the authoritative source for subscription status, expiry, quota windows, and usage.

The client will use:

- `GET /api/v1/admin/subscriptions?user_id=...&status=active` to list active subscriptions;
- `GET /api/v1/admin/subscriptions/:id/progress` to obtain computed daily, weekly, and monthly progress;
- `POST /api/v1/admin/subscriptions/:id/reset-quota` to reset quota windows.

The reset request body contains boolean `daily`, `weekly`, and `monthly` fields. The application sets a field to `true` only when that limit is configured for the group. At least one field must be true.

Sub2API resets both usage and the selected window start. The next automatic reset time therefore follows Sub2API's updated window schedule. The application displays the refreshed upstream result and does not try to preserve the previous window schedule.

All calls use the configured administrator API key on the backend. The key is never returned to the browser.

## Tier And Request Snapshots

Add `reset_count INTEGER NOT NULL DEFAULT 0` to `redeem_tiers`.

Add the same field to `redeem_access_requests`. Creating an access request copies the tier value into the request alongside the existing group, validity, limits, and concurrency snapshots.

Validation rules are:

- balance tiers must have `reset_count = 0`;
- subscription tiers may use any non-negative integer reset count;
- a subscription tier whose group has no daily, weekly, or monthly limit must have `reset_count = 0`;
- subscription tiers sharing a group may have different reset counts because each redemption period keeps its own snapshot.

Changing a tier later affects only new access requests. Disabling a tier does not alter existing requests or periods.

## Reset Period Ledger

Create `subscription_reset_periods` with one row per successfully fulfilled local subscription request.

Required fields are:

- primary key `id`;
- unique `access_request_id` for idempotent creation;
- immutable `upstream_user_id`, `tier_id`, `sub2api_group_id`, `validity_days`, and `reset_limit` snapshots;
- nullable `upstream_subscription_id` until binding succeeds;
- `fulfilled_at` and a stable fulfillment ordering key;
- nullable `period_start` and `period_end` until scheduling succeeds;
- `reset_used`, initially zero;
- status: `pending_binding`, `scheduled`, `active`, `expired`, or `inactive`;
- `inferred_from_legacy` and a migration version;
- `last_synced_at`, `last_error`, `created_at`, and `updated_at`.

`reset_used` includes successful resets and currently reserved or uncertain attempts. It never exceeds `reset_limit`.

A row is created even when `reset_limit` is zero. Such a row preserves the validity interval but never enables the reset action.

Index periods by `(upstream_user_id, sub2api_group_id, period_start)` and by `upstream_subscription_id`. Only one period for a user and group may be active at a time. Service validation rejects overlapping assigned intervals.

## Reset Attempt Ledger

Create `subscription_reset_attempts` with one row per user action.

Required fields are:

- primary key `id`;
- globally unique client-generated `request_id` for request idempotency;
- `period_id`, `upstream_user_id`, and `upstream_subscription_id` snapshots;
- `reset_daily`, `reset_weekly`, and `reset_monthly` target flags;
- status: `reserved`, `succeeded`, `failed`, or `uncertain`;
- structured before and after snapshots containing usage and window-start values for every selected window;
- upstream status, error text, and diagnostic response details with secrets excluded;
- `reserved_at`, nullable `completed_at`, nullable `confirmed_at`, `created_at`, and `updated_at`.

A partial unique index on `period_id` for `reserved` and `uncertain` rows prevents more than one blocking operation for a period. Repeating the same `request_id` returns the original operation result without consuming another reset.

## Period Creation And Scheduling

### Direct charge

Before direct subscription fulfillment, the service attempts to read the user's active subscription for the target group. After `CreateAndRedeemCode` succeeds, it reads the resulting subscription again.

If an active subscription existed before fulfillment, the new local period starts at the previous upstream expiry and lasts for the snapshotted validity days. This keeps an existing external or earlier local interval ahead of the newly purchased entitlement.

If no active subscription existed, the period uses the returned upstream start and expiry. If either observation fails after successful fulfillment, the local period is still inserted as `pending_binding`; reconciliation later binds and schedules it so a successful purchase cannot lose its entitlement because of a temporary read failure.

### Redeem code

Issuing a subscription code does not activate a reset period. The existing sync flow must first confirm that:

- the code is `used`;
- `used_by_upstream_user_id` equals the requesting user;
- the user has the matching active upstream group subscription.

The service then creates or binds the period and orders it by actual `used_at`, with local record ID as a deterministic tie-breaker.

If several local codes are observed between sync runs, their known validity intervals are assigned as one ordered block ending at the observed upstream expiry. Interleaved external changes cannot always be reconstructed from the upstream current-state API; the service records an inference diagnostic instead of silently claiming exact history.

### Stable boundaries

Once a period receives start and end boundaries, normal reconciliation does not shift them merely because an external extension changes the merged upstream expiry. External extensions create no local reset allowance.

If the upstream subscription is revoked, expired early, or disappears, the service marks affected periods inactive and disables reset operations while retaining the ledger for audit. A future confirmed upstream state may reactivate a still-time-valid period, but it does not restore a period whose local end has passed.

## Period State Reconciliation

For each managed user and group, reconciliation:

1. loads the active upstream subscription and its progress;
2. binds pending local periods to the matching subscription;
3. schedules unscheduled periods in stable fulfillment order without overlap;
4. marks a scheduled period active when `period_start <= now < period_end`;
5. marks ended periods expired, permanently discarding unused allowance;
6. marks periods inactive when the required upstream subscription is not effective;
7. resolves uncertain reset attempts when before and current upstream usage/window snapshots prove the result;
8. records transient upstream errors without rewriting the last confirmed boundaries.

The existing configured sync interval drives this reconciliation. Immediate reconciliation also runs after successful direct charge, confirmed code redemption, a reset action, and a legacy backfill trigger. A failed immediate reconciliation is non-fatal and remains eligible for scheduled retry.

## Legacy Data Backfill

Migration scope is limited to subscription fulfillment history in this application. An active upstream subscription with no matching successful local history is visible but receives no reset allowance.

Schema migration first adds the new columns and tables without granting any resets. Existing tier and request reset counts therefore begin at zero.

Create `subscription_reset_backfill_runs` to make administrator-triggered legacy grants durable and idempotent. A run stores:

- unique `tier_id`;
- the first positive `reset_limit` snapshot;
- status and progress counters;
- trigger, start, completion, retry, and error timestamps.

The first administrator save that changes a historical subscription tier from zero to a positive reset count inserts the unique run. Later tier edits do not change the captured backfill allowance.

The backfill worker:

1. finds successful local subscription fulfillments for users affected by the tier's group;
2. includes zero-allowance local periods from other tiers in the same group so interval placement remains correct;
3. requires matching upstream subscriptions and confirmed code ownership for redeem-code fulfillment;
4. groups history by upstream user and group;
5. orders local periods by fulfillment time;
6. infers period boundaries backwards from the current upstream `expires_at` using each historical `validity_days`;
7. treats any upstream interval outside the inferred local block as external and unentitled;
8. grants the captured allowance only to current and future legacy periods belonging to the triggering tier;
9. does not grant allowance to already ended periods;
10. marks every result with `inferred_from_legacy` and the migration version.

This inference is exact when the active merged subscription consists only of the recorded local extensions. Historical external extensions may make exact boundaries unknowable; this limitation is retained in diagnostics. Re-running a failed or interrupted backfill resumes the same run and never grants a period twice.

## User API

### List subscriptions

Add `GET /api/subscriptions` under normal session authentication.

The service derives the upstream user from the session and returns all active upstream subscriptions. Each item contains:

- upstream subscription and group identifiers;
- group name and platform;
- exact expiry and a non-negative human-readable remaining-day value;
- only configured quota windows, each with `limit_usd`, `used_usd`, `remaining_usd`, nullable `window_start`, and nullable `resets_at`;
- current local period start and end when one exists;
- current reset total, used, and remaining;
- next local period start and reset total when scheduled;
- whether the subscription is currently in an external, zero-reset, or unlimited interval;
- `can_reset`, a stable disable reason, and any blocking attempt status.

When a configured upstream window has not activated, the API returns zero usage, full remaining quota, and a null reset time. The UI labels it as starting after first use.

### Reset subscription

Add `POST /api/subscriptions/:id/reset-quota` under normal session authentication. The request contains only `request_id`.

The service does not trust a user ID, group ID, period ID, or window selection from the browser. It loads the subscription by ID, verifies ownership against the session, refreshes upstream state, and derives all reset flags from the current group configuration.

The operation is allowed only when:

- the upstream subscription is active and belongs to the signed-in user;
- a local period is currently active;
- the period has remaining resets;
- no reserved or uncertain attempt blocks the period;
- at least one configured window has non-zero usage.

The response returns the attempt status and a freshly computed subscription item.

## Reset Transaction And Error Semantics

Reset spans SQLite and Sub2API and cannot be one distributed transaction.

The service uses this sequence:

1. refresh and validate upstream subscription ownership, local period, allowance, and usage;
2. begin a SQLite write transaction;
3. insert a `reserved` attempt and increment `reset_used` by one;
4. commit the reservation;
5. call the upstream reset endpoint with every configured window selected;
6. on success, save the returned after snapshot and mark the attempt `succeeded`;
7. on a confirmed upstream rejection, mark the attempt `failed` and decrement `reset_used` in one transaction;
8. on timeout, connection loss, malformed success response, or any other unknown outcome, mark the attempt `uncertain` and keep the reservation consumed;
9. prevent another reset until reconciliation or an administrator resolves the uncertain attempt.

Failures before the upstream call do not consume allowance. Confirmed upstream failures release allowance. Unknown outcomes remain pessimistically consumed to prevent a reset that actually succeeded from being repeated for free.

Reconciliation confirms success when selected window starts and usage values prove a reset occurred after the reservation. It confirms failure only when upstream state and response evidence make failure unambiguous. Otherwise the attempt remains uncertain.

## Administrator API And Operations

The existing tier request and response objects gain `reset_count`.

Add administrator endpoints to:

- list reserved or uncertain reset attempts with user, subscription, period, before/current snapshots, and error details;
- resolve an uncertain attempt as consumed, leaving `reset_used` unchanged;
- resolve an uncertain attempt as released, atomically decrementing `reset_used` once;
- inspect legacy backfill status and errors.

Resolution endpoints are idempotent and retain the attempt audit record. They never call the upstream reset endpoint again.

Tier saving commits the tier configuration first. Legacy backfill then runs asynchronously and retries independently; upstream failure cannot roll back a valid tier edit.

## User Interface

Wrap the existing `RechargeRequestView` content in tabs:

- `Redemption Request` is the first and default tab and retains the current workflow;
- `Subscription Management` is the second tab.

The management tab uses responsive repeated subscription cards. Each card shows:

- group name, remaining days, and exact expiry;
- current reset remaining and total when the current period grants resets;
- one progress row for each configured quota window;
- current usage, limit, remaining amount, and next automatic reset time;
- one compact line for the next scheduled local period and its unlock allowance;
- an explicit external-period, zero-reset, pending-confirmation, or exhausted state where applicable;
- the reset button only when the API returns `can_reset = true`.

An unlimited subscription uses the same responsive width and stable minimum height as a limited card. Its quota area contains a large equal-height `Unlimited subscription` placeholder and no reset count or action. Individual missing limits are simply omitted.

Active external subscriptions remain visible with their upstream quota and expiry information. Their reset button stays disabled until a scheduled local period begins.

Before resetting, a confirmation dialog lists all windows and current usage that will be cleared and states that one allowance will be consumed. A successful action refreshes the card. An uncertain action shows `Confirming` and remains disabled.

The layout is one column on narrow screens and a stable responsive grid on desktop. Card quota and footer regions use stable dimensions so limited, partially limited, and unlimited cards align without text or button overlap.

## Administrator Interface

Add a `Per-period reset count` numeric input to the subscription tier editor.

- balance rows show a disabled placeholder and submit zero;
- subscription rows with at least one configured quota limit accept a non-negative integer;
- selecting an unlimited group immediately clears the value to zero and disables the input;
- changing a limited row to an unlimited group does the same before save.

The tier settings view also includes compact sections for legacy backfill status and uncertain reset attempts. An attempt detail shows before/current quota snapshots and offers explicit `Confirm consumed` and `Release allowance` actions with confirmation dialogs.

## Security And Privacy

- Every user subscription request derives identity from the authenticated local session.
- Reset ownership is checked again against the upstream subscription before reservation.
- The backend chooses reset windows; browser input cannot widen the operation.
- Administrator API keys and upstream administrator-only fields never reach the browser.
- Attempt diagnostics exclude authorization headers, credentials, and other secrets.
- Administrator resolution routes retain the existing admin authorization middleware.

## Testing And Verification

Backend migration tests cover old databases, new columns and indexes, repeated migration, and interrupted backfill resumption.

Backend service and HTTP tests cover:

- tier validation and request snapshots;
- unlimited groups forcing zero resets;
- direct-charge and redeem-code period creation gates;
- zero-reset periods occupying time;
- stacked same-group periods with equal and different allowances;
- independent groups;
- external intervals before local periods;
- expired allowance not carrying forward;
- legacy backwards inference and one-time grants;
- non-owner and forged subscription reset attempts;
- configured-window request flags;
- all-zero usage disabling reset;
- reservation, success, confirmed failure, uncertain outcome, reconciliation, and manual resolution;
- duplicate request IDs and concurrent clicks consuming at most one allowance;
- upstream read failures preserving last confirmed ledger state.

Sub2API client contract tests cover subscription list/progress decoding and the exact administrator reset method, path, headers, body, and response handling.

Frontend tests cover:

- redemption remaining the default tab;
- limited, partially limited, unlimited, external, scheduled, exhausted, and uncertain card states;
- omission of unconfigured windows;
- disabled reset reasons and confirmation dialog content;
- admin reset-count enable and disable behavior;
- legacy backfill and uncertain-attempt administrator states.

Run Go tests, frontend unit tests, TypeScript compilation, and the production build. Use browser screenshots at desktop and mobile widths to verify equal card sizing, responsive grid behavior, dialog content, progress rendering, and absence of overlap or horizontal clipping.

## Non-Goals

- Modifying Sub2API upstream behavior or schema.
- Resetting API-key total quota, rate-limit usage, or provider-account quota.
- Allowing users to choose individual quota windows.
- Granting reset allowance for subscriptions without successful local fulfillment history.
- Carrying unused allowance into another period.
- Combining allowance across Sub2API groups.
- Reconstructing unknowable historical external extension ordering with false precision.
