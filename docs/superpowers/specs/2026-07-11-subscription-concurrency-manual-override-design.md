# Subscription Concurrency Manual Override Design

## Goal

Prevent the subscription concurrency monitor from overwriting concurrency values that an administrator set directly in sub2api, while retaining automatic subscription entitlement changes and expiry fallback for users still managed by this service. Also make historical fulfilled requests repairable after upgrading from a version that did not store concurrency snapshots.

## Confirmed Rules

- The subscription target remains the maximum concurrency among active grants; grants do not stack.
- When no grant is active, the computed target remains sub2api's live `default_concurrency`.
- Reconciliation still runs at startup and every 30 minutes.
- A detected manual override has higher priority than active subscriptions and expiry fallback. The monitor observes the user but does not update their upstream concurrency.
- Manual override is per upstream user and persists across restarts and subscription expiry.
- An override clears automatically when the administrator changes the upstream concurrency to the monitor's current computed target. Monitoring resumes from that value.
- On the first reconciliation after this feature is deployed, a user without local control state is treated conservatively: if upstream concurrency differs from the computed target, preserve it as a pre-existing manual override; if it matches, establish the managed baseline.
- Historical fulfilled requests whose concurrency snapshot is zero may recover concurrency from a current subscription tier with the same sub2api group ID. Existing tier validation guarantees all tiers in a group use the same concurrency.

## Architecture

Add a `subscription_concurrency_user_states` table keyed by `upstream_user_id`. It stores the last concurrency successfully applied by this service, whether the user is manually overridden, the observed override value, and timestamps. Grant records continue to represent subscription entitlements; the new table represents ownership of the resulting upstream user setting.

The reconciler computes the desired target exactly as today, then passes the target, current upstream concurrency, and persisted control state through a small decision function. Keeping this decision pure makes all state transitions explicit and testable without HTTP or database setup.

The states are:

- `untracked`: no local state exists yet.
- `managed`: `last_applied_concurrency` is the value last confirmed or written by this service.
- `manual_override`: an external value is protected from monitor writes.

## Reconciliation Decisions

For each user with concurrency grants:

1. Fetch the upstream user and subscriptions, update grant observations, and compute the current target.
2. If no control state exists and current equals target, create a managed baseline without issuing an upstream update.
3. If no control state exists and current differs from target, record a manual override using the current value and do not update upstream.
4. If manual override is active and current equals target, clear the override and establish the target as the managed baseline.
5. If manual override is active and current differs from target, preserve upstream. If the administrator changed the override from one non-target value to another, update the observed override value only.
6. If managed and current equals target, keep or refresh the managed baseline.
7. If managed, current differs from the last value applied by this service, and current differs from target, record a manual override and do not update upstream.
8. If managed, current still equals the last applied value but target changed, update upstream to the target. Persist the new last-applied value only after sub2api confirms success.

Checking `current == target` before external-change detection ensures an administrator who deliberately restores the calculated target resumes monitoring immediately.

## Historical Repair

When creating a missing grant, concurrency resolution uses this order:

1. The fulfilled access request's concurrency snapshot, when positive.
2. The exact current tier ID, when present and positive.
3. A current subscription tier with the request's `sub2api_group_id`, when positive.
4. Return the existing repair error if no reliable value exists.

The group fallback is intentionally limited to subscription tiers. It does not guess from the upstream user's current concurrency or the global default, because neither identifies the purchased entitlement.

## Monitoring Status

Extend the existing admin status payload and panel with:

- `manual_override_users`: number of users currently protected from writes.

The existing latest reconciliation error remains responsible for operational failures. Detecting a manual override is expected state, not an error. The count makes the protected population visible without exposing or editing manual values in this application.

## Failure Handling

- Failure to read or persist user control state fails that user's reconciliation and is included in the existing aggregate/latest error flow.
- Failure to update sub2api leaves `last_applied_concurrency` unchanged, allowing the next run to retry rather than misclassifying a failed write as an administrator override.
- Grant observation failures retain the existing behavior and do not create or clear override state from incomplete upstream data.
- State updates use upsert operations so startup reconciliation and approval-time reconciliation are idempotent.

## Tests

Backend tests cover:

- first-run matching value establishes managed state without an upstream write;
- first-run differing value creates a persistent override without an upstream write;
- a managed user whose upstream value changes externally enters override;
- an overridden user remains unchanged while subscriptions expire and the target falls back;
- changing an overridden user to the current target clears override;
- a managed target change is applied and recorded;
- a failed upstream update does not advance the last-applied value;
- monitor status reports the override user count;
- historical repair falls back by group and still errors when no reliable tier exists.

Existing concurrency reconciliation and HTTP tests remain regression coverage for maximum entitlement, expiry fallback, redeem activation, and status serialization.
