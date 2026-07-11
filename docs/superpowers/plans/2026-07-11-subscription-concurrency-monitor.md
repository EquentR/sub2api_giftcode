# Subscription Concurrency Monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the highest configured subscription-tier concurrency to approved users while an upstream sub2api subscription is active, then restore sub2api's live default concurrency after all managed subscriptions expire.

**Architecture:** Persist immutable concurrency snapshots on access requests and one idempotent grant per approved subscription request. A reconciliation service observes upstream subscriptions and users, calculates the maximum active grant, and updates upstream concurrency immediately after direct charge and every 30 minutes. The admin UI edits tier concurrency, displays upstream defaults and monitor status, while recharge views disclose concurrency before purchase.

**Tech Stack:** Go 1.23, Gin, SQLite, testify, Vue 3, TypeScript, Element Plus, pnpm/Vite.

---

### Task 1: Persist concurrency configuration and request snapshots

**Files:**
- Modify: `backend/internal/db/migrate.go`
- Modify: `backend/internal/models/models.go`
- Modify: `backend/internal/app/admin.go`
- Modify: `backend/internal/app/access.go`
- Modify: `backend/internal/httpapi/types.go`
- Test: `backend/internal/db/store_test.go`
- Test: `backend/internal/app/admin_test.go`
- Test: `backend/internal/app/redeem_test.go`

- [ ] **Step 1: Write failing migration, validation, and snapshot tests**

Assert that `redeem_tiers` and `redeem_access_requests` have `concurrency`, that `subscription_concurrency_grants` has the designed columns and unique request constraint, that balance tiers normalize concurrency to zero, that subscription tiers require positive concurrency, that same-group tiers reject different concurrency values, and that a created request stores the selected tier concurrency.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd backend; go test ./internal/db ./internal/app -run 'Test.*(Concurrency|Snapshot|Tier)' -v`

Expected: FAIL because the columns, fields, and validation do not exist.

- [ ] **Step 3: Implement minimal schema and model changes**

Add `Concurrency int` to `models.RedeemTier` and `models.AccessRequest`; add SQLite columns and the grant table; include concurrency in tier SQL, request creation SQL, selection scanners, normalization, and HTTP request mapping. Validate subscription concurrency and a `map[groupID]concurrency` invariant before writing tiers.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `cd backend; go test ./internal/db ./internal/app -run 'Test.*(Concurrency|Snapshot|Tier)' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db backend/internal/models backend/internal/app/admin.go backend/internal/app/access.go backend/internal/httpapi/types.go backend/internal/httpapi/access_handlers.go
git commit -m "feat: persist subscription concurrency configuration"
```

### Task 2: Add sub2api settings and user concurrency contracts

**Files:**
- Modify: `backend/internal/sub2api/client.go`
- Test: `backend/internal/sub2api/client_test.go`

- [ ] **Step 1: Write failing HTTP contract tests**

Cover `GET /api/v1/admin/settings` returning `default_concurrency`, `GET /api/v1/admin/users/:id` returning current `concurrency`, and `PUT /api/v1/admin/users/:id` receiving only `{ "concurrency": 12 }` with the admin API key.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend; go test ./internal/sub2api -run 'TestClient.*(Concurrency|Settings|User)' -v`

Expected: FAIL because the methods and user field do not exist.

- [ ] **Step 3: Implement minimal client methods**

Add `Concurrency int` to `User`, a minimal settings response, `GetDefaultConcurrency`, `GetUser`, and `UpdateUserConcurrency`. Reject non-positive upstream default values as invalid responses.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `cd backend; go test ./internal/sub2api -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sub2api/client.go backend/internal/sub2api/client_test.go
git commit -m "feat: add upstream concurrency client methods"
```

### Task 3: Implement the grant reconciler and approval integration

**Files:**
- Create: `backend/internal/app/concurrency.go`
- Create: `backend/internal/app/concurrency_test.go`
- Modify: `backend/internal/app/access.go`
- Modify: `backend/internal/app/redeem.go`
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Write failing reconciler tests**

Create real SQLite and `httptest.Server` cases for: maximum rather than sum; fallback to upstream default; no downgrade on subscription read failure; update failure retained for retry; direct-charge grant upsert; redeem-code grant pending until the requesting user redeems; same-group grants following one upstream subscription; repeated approval keeping one grant.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend; go test ./internal/app -run 'Test.*Concurrency' -v`

Expected: FAIL because the grant service does not exist.

- [ ] **Step 3: Implement grant persistence and reconciliation**

Define `SubscriptionConcurrencyGrant`, idempotent `upsertSubscriptionConcurrencyGrant`, grant listing/scanning, `ReconcileSubscriptionConcurrency`, and per-user reconciliation. Preserve confirmed state on upstream read failure, use the highest active desired value, fetch the live default only for fallback, skip identical updates, and record retryable errors.

- [ ] **Step 4: Integrate both fulfillment modes**

After subscription direct charge persists successfully, upsert and immediately reconcile without failing approval on reconciliation errors. After subscription redeem-code issuance, upsert a pending grant. On idempotent approval paths, ensure a missing historical grant is repaired.

- [ ] **Step 5: Run tests and verify GREEN**

Run: `cd backend; go test ./internal/app -run 'Test.*Concurrency' -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/app/concurrency.go backend/internal/app/concurrency_test.go backend/internal/app/access.go backend/internal/app/redeem.go backend/internal/models/models.go
git commit -m "feat: reconcile subscription concurrency grants"
```

### Task 4: Expose monitor status and schedule reconciliation

**Files:**
- Modify: `backend/internal/app/concurrency.go`
- Modify: `backend/internal/httpapi/admin_handlers.go`
- Modify: `backend/internal/httpapi/router.go`
- Modify: `backend/cmd/server/main.go`
- Test: `backend/internal/httpapi/access_handlers_test.go`
- Test: `backend/internal/app/concurrency_test.go`

- [ ] **Step 1: Write failing monitor-status API tests**

Assert an admin response contains the live default, last run time, active/pending/inactive/error counts, and latest error. Assert upstream-default failure is returned explicitly and never replaced with a local constant.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend; go test ./internal/httpapi ./internal/app -run 'Test.*ConcurrencyMonitor' -v`

Expected: FAIL because no status endpoint exists.

- [ ] **Step 3: Implement endpoint and ticker**

Add `GET /api/v1/admin/subscription-concurrency/status`, aggregate grant status in the service, and start one immediate reconciliation followed by a dedicated 30-minute ticker. Log failures and stop cleanly with the server context.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `cd backend; go test ./internal/httpapi ./internal/app -run 'Test.*ConcurrencyMonitor' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/app/concurrency.go backend/internal/httpapi backend/cmd/server/main.go
git commit -m "feat: monitor subscription concurrency grants"
```

### Task 5: Add tier controls, monitor summary, and purchase hints

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/admin.ts`
- Modify: `frontend/src/components/TierEditor.vue`
- Modify: `frontend/src/views/AdminTiersView.vue`
- Modify: `frontend/src/views/RechargeRequestView.vue`
- Modify: `frontend/src/views/AccessRequestView.vue`

- [ ] **Step 1: Extend frontend contracts**

Add `concurrency` to tier and request types, define the monitor-status response, and add an admin API method for the status endpoint.

- [ ] **Step 2: Build and verify RED**

Run: `cd frontend; pnpm build`

Expected: FAIL while templates reference the not-yet-defined fields or API.

- [ ] **Step 3: Implement admin controls and user hints**

Add a subscription-only numeric concurrency control to the tier editor; normalize balance tiers to zero. Show the upstream default and monitor counters in the admin tier view. Add small secondary `并发数 N` text to subscription cards/options and selected subscription summaries in both recharge/application views.

- [ ] **Step 4: Build and verify GREEN**

Run: `cd frontend; pnpm build`

Expected: PASS with `vue-tsc` and Vite completing successfully.

- [ ] **Step 5: Commit**

```bash
git add frontend/src
git commit -m "feat: show subscription concurrency controls"
```

### Task 6: Full verification and visual QA

**Files:**
- Modify only files needed to fix verification findings.

- [ ] **Step 1: Format and run backend suite**

Run: `cd backend; gofmt -w cmd internal; go test ./...`

Expected: PASS with zero failing packages.

- [ ] **Step 2: Run frontend production build**

Run: `cd frontend; pnpm build`

Expected: PASS.

- [ ] **Step 3: Run repository checks**

Run: `git diff --check`

Expected: no output and exit code 0.

- [ ] **Step 4: Visually verify responsive pages**

Start the configured local server, then inspect desktop and mobile widths for the tier editor, monitor summary, recharge tier cards, and access-request selector. Confirm no overlap, truncation, or missing concurrency hint.

- [ ] **Step 5: Request final review and commit fixes**

Review the complete diff against `docs/superpowers/specs/2026-07-11-subscription-concurrency-monitor-design.md`, fix all critical or important findings, rerun the full commands, and commit any final corrections.
