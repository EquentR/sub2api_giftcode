# Subscription Concurrency Monitor Design

## Goal

Allow subscription tiers to define a user concurrency limit, apply that limit after the subscription becomes active in sub2api, and reconcile managed users every 30 minutes. A user with multiple effective subscription grants receives the highest configured concurrency, never a sum. When no managed grant remains effective, the user returns to sub2api's current global `default_concurrency`.

## Confirmed Rules

- Only subscription tiers have a positive concurrency value. Balance tiers store zero.
- Multiple effective grants use the maximum concurrency value and never add together.
- Tiers that reference the same `sub2api_group_id` must use the same concurrency value because sub2api extends one subscription for repeated redemptions in the same group.
- Direct-charge approvals attempt concurrency reconciliation immediately after the upstream subscription is redeemed.
- Redeem-code approvals remain pending until the code has been redeemed by the requesting upstream user and a matching upstream subscription is active.
- sub2api is the only source of truth for subscription status and expiry.
- sub2api's global `default_concurrency` is the only fallback source. This application does not store a duplicate fallback value.
- Reconciliation runs once at service startup and every 30 minutes afterward.
- Upstream read or update failures do not turn a successful subscription approval into a failure. They are recorded and retried.

## Architecture

The feature adds an entitlement ledger beside the existing access-request and redeem-code records. Each approved subscription request creates one concurrency grant containing immutable snapshots of the requesting user, subscription group, and tier concurrency. The grant later binds to the matching sub2api subscription and follows that subscription's upstream status and expiry.

A single reconciliation service handles both immediate post-approval work and scheduled monitoring. It loads managed grants by user, queries sub2api once per user for current user data and active subscriptions, updates grant observations, calculates the highest effective concurrency, and changes the upstream user only when the current value differs.

The existing redeem-code synchronization remains responsible for observing code redemption. The concurrency reconciler uses that local observation to prevent a redeem-code grant from activating before its specific code was redeemed by the requesting user.

## Data Model

### Tier and request snapshots

Add `concurrency INTEGER NOT NULL DEFAULT 0` to `redeem_tiers`. The field is returned by tier APIs and accepted by the admin tier update API.

Add the same snapshot field to `redeem_access_requests`. Creating an access request copies the selected tier concurrency into the request. Later tier edits or deletion therefore do not alter an existing request or grant.

Tier validation requires:

- balance tier concurrency equals zero;
- subscription tier concurrency is greater than zero;
- all subscription tiers with the same positive group ID have the same concurrency.

### Subscription concurrency grants

Create `subscription_concurrency_grants` with:

- `id` primary key;
- `access_request_id` unique foreign reference to the logical source request;
- `upstream_user_id`, `tier_id`, `sub2api_group_id`, and `desired_concurrency` snapshots;
- nullable `upstream_subscription_id` once a matching subscription is observed;
- `status` with `pending`, `active`, or `inactive`;
- nullable `upstream_expires_at`;
- `last_synced_at`, `last_error`, `created_at`, and `updated_at`.

`last_error` is diagnostic and does not replace the grant status. A temporarily failed check leaves the last confirmed status intact.

Use a unique constraint on `access_request_id` so repeated or resumed approvals cannot duplicate a grant. Index grants by `upstream_user_id` and status for reconciliation.

## Upstream Client Contract

Extend the sub2api client with:

- `User.Concurrency` from admin user responses;
- `GetUser(userID)` using `GET /api/v1/admin/users/:id`;
- `UpdateUserConcurrency(userID, concurrency)` using `PUT /api/v1/admin/users/:id` with only the `concurrency` field;
- `GetDefaultConcurrency()` using `GET /api/v1/admin/settings` and its `default_concurrency` field.

Existing `ListActiveUserSubscriptions` remains the subscription source. Subscription records also expose their update timestamp when returned upstream, but matching is based primarily on user and group, then persisted subscription ID.

## Approval Flow

### Direct charge

After `CreateAndRedeemCode` succeeds for a subscription and the local fulfillment records are persisted:

1. Upsert the grant for the access request.
2. Run reconciliation for the requesting user immediately.
3. Return the successful approval regardless of reconciliation failure.
4. Persist any reconciliation error on the grant for the scheduled retry.

The existing approval idempotency path returns the existing code and upserts the same grant if an older successful request does not yet have one.

### Redeem code

After a subscription code is issued:

1. Upsert a `pending` grant for the access request.
2. Do not change concurrency during approval.
3. During reconciliation, require the related local redeem code to have status `used` and `used_by_upstream_user_id` equal to the requesting user.
4. Bind the grant only when that user also has an active upstream subscription in the snapshotted group.

If another account redeems the code, the requesting account's grant does not activate.

## Reconciliation

The server invokes the reconciler at startup, then on a dedicated 30-minute ticker. Immediate approval calls use the same per-user reconciliation method. An in-process mutex prevents scheduled and immediate full runs from overlapping.

For each managed user:

1. Fetch the upstream user and active subscriptions.
2. Evaluate every local grant against its redemption gate and a subscription with the same group.
3. Reuse a stored subscription ID when present; otherwise bind the matching active subscription.
4. Store the upstream expiry and mark confirmed matches active. Mark confirmed missing or invalid subscriptions inactive.
5. Calculate the maximum `desired_concurrency` across active grants.
6. If there are no active grants, fetch sub2api's current `default_concurrency` as the target.
7. Update the upstream user only when its current concurrency differs from the target.
8. Store synchronization timestamps and clear or record errors.

Because sub2api merges repeated redemptions for the same user and group into one extended subscription, grants sharing that group follow the merged subscription lifetime. The tier validation rule ensures those grants also share one concurrency value.

If fetching the user or subscriptions fails, the reconciler does not change statuses or concurrency for that user. If the fallback setting cannot be fetched, a user requiring fallback keeps the current concurrency until the next successful run. If the concurrency update fails, confirmed grant states remain and the update is retried later.

## Admin And User Interface

The tier editor shows a numeric concurrency input for subscription rows and a dash for balance rows. It includes a read-only sub2api default-concurrency value with a clear upstream-source label. Saving tiers surfaces same-group conflicts from backend validation.

The tier settings view also shows a compact monitor summary:

- last reconciliation time;
- active grants;
- pending grants;
- grants with synchronization errors;
- latest error text when present.

Every subscription tier option on the recharge/application UI includes secondary text showing its concurrency, for example `Concurrent requests: 10`. Balance tiers do not show this hint. Apply this consistently to the current recharge page and any retained access-request tier selector that remains routable.

## API Surface

Existing tier request and response objects gain `concurrency`.

Add an admin-only concurrency monitor status endpoint returning:

- upstream `default_concurrency`;
- last reconciliation timestamp;
- active, pending, inactive, and error counts;
- latest error text and timestamp.

The endpoint reports an upstream-default read error separately instead of substituting a local value.

## Testing

Backend tests cover:

- migration of tier/request concurrency columns and the grant table;
- tier validation, including the same-group invariant;
- request snapshot persistence;
- sub2api settings, user-read, user-update, and subscription HTTP contracts;
- direct-charge grant creation and non-fatal immediate synchronization errors;
- redeem-code grants remaining pending until the requesting user redeems the code;
- maximum concurrency selection without addition;
- merged same-group subscriptions;
- fallback to the live upstream default;
- no downgrade on upstream read failure;
- retry after user update failure;
- idempotent repeated approval;
- monitor status aggregation.

Frontend verification covers TypeScript compilation and the production build. Manual browser verification checks the tier concurrency editor, upstream default display, monitor summary, validation errors, and recharge-page concurrency hints on desktop and mobile widths.

## Out Of Scope

- Changing sub2api's global `default_concurrency` from this application.
- Managing concurrency for subscriptions created outside approved requests in this application.
- Adding or stacking concurrency values.
- Restoring a user's pre-grant manually customized concurrency; fallback always uses the current upstream global default as confirmed.
