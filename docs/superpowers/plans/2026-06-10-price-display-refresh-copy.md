# Price Display, Auto Refresh, and Copy UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split tier pricing into "arrival amount" in USD and "paid amount" in CNY, show both amounts in the user-facing views, fix the tier sort input plus button, and make the admin approval queue auto-refresh with a faster copy path for issued codes.

**Architecture:** Keep the current redeem/generation flow centered on the USD arrival amount so upstream code issuance does not change. Add a new `pay_amount_cny` field to the balance tier model and migrate existing rows by mirroring the legacy amount into the new paid amount until an admin edits it. On the frontend, render both money fields wherever tiers are shown, tighten the tier editor input layout so the increment control is visible, and add a simple polling loop plus a prominent copy action in the approval dialog.

**Tech Stack:** Go, SQLite, Gin, Vue 3, Element Plus, TypeScript

---

### Task 1: Add paid amount to tier storage and API models

**Files:**
- Modify: `backend/internal/db/migrate.go`
- Modify: `backend/internal/models/models.go`
- Modify: `backend/internal/app/admin.go`
- Modify: `backend/internal/httpapi/types.go`
- Modify: `backend/internal/httpapi/admin_handlers.go`
- Modify: `backend/internal/db/store_test.go`
- Modify: `backend/internal/app/admin_test.go`

- [ ] **Step 1: Write the failing test**

Add a migration assertion that checks `redeem_balance_tiers` includes the new `pay_amount_cny` column and that seeded rows copy the legacy amount into it.

```go
func TestOpenAndMigrateSeedsTierPaidAmount(t *testing.T) {
    store, err := Open(cfg)
    require.NoError(t, err)
    require.NoError(t, store.Migrate(context.Background()))

    var payAmount float64
    require.NoError(t, store.DB.QueryRowContext(context.Background(),
        `SELECT pay_amount_cny FROM redeem_balance_tiers ORDER BY id LIMIT 1`,
    ).Scan(&payAmount))
    require.Greater(t, payAmount, 0.0)
}
```

- [ ] **Step 2: Run the test and watch it fail**

Run:

```bash
cd backend
go test ./internal/db -run TestOpenAndMigrateSeedsTierPaidAmount -v
```

Expected: fail because the column does not exist yet.

- [ ] **Step 3: Implement the minimal schema and model change**

Add `pay_amount_cny REAL NOT NULL DEFAULT 0` to `redeem_balance_tiers`, extend `models.BalanceTier`, extend `BalanceTierRequest`, and make `scanBalanceTierRow` read the extra column.

```go
type BalanceTier struct {
    ID          int64     `json:"id"`
    Amount      float64   `json:"amount"`
    PayAmountCNY float64  `json:"pay_amount_cny"`
    Label       string    `json:"label"`
    Enabled     bool      `json:"enabled"`
    SortOrder   int       `json:"sort_order"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

- [ ] **Step 4: Re-run the test and confirm it passes**

Run:

```bash
cd backend
go test ./internal/db ./internal/app ./internal/httpapi -v
```

Expected: pass, with existing tiers showing a non-zero paid amount.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrate.go backend/internal/models/models.go backend/internal/app/admin.go backend/internal/httpapi/types.go backend/internal/httpapi/admin_handlers.go backend/internal/db/store_test.go backend/internal/app/admin_test.go
git commit -m "feat: add cny paid amount to balance tiers"
```

### Task 2: Show both money values in the user-facing views

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/views/UserDashboardView.vue`
- Modify: `frontend/src/views/AccessRequestView.vue`
- Modify: `frontend/src/views/AdminTiersView.vue`
- Modify: `frontend/src/components/TierEditor.vue`

- [ ] **Step 1: Write the failing test/build check**

Use the frontend build as the regression gate; the build should fail until the new field is wired through the typed views.

```bash
cd frontend
pnpm build
```

- [ ] **Step 2: Update the type and views**

Add `pay_amount_cny` to `BalanceTier`, then render:

- arrival amount: the existing USD amount
- paid amount: the new CNY value

Use the user dashboard to surface the enabled tier list directly so the homepage shows both numbers without digging into another page.

```vue
<el-table-column label="到账金额" width="140">
  <template #default="{ row }">
    {{ Number(row.amount).toFixed(0) }} USD
  </template>
</el-table-column>
<el-table-column label="实付金额" width="140">
  <template #default="{ row }">
    {{ Number(row.pay_amount_cny).toFixed(0) }} CNY
  </template>
</el-table-column>
```

- [ ] **Step 3: Re-run the build and confirm it passes**

Run:

```bash
cd frontend
pnpm build
```

Expected: pass with both money columns visible.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/views/UserDashboardView.vue frontend/src/views/AccessRequestView.vue frontend/src/views/AdminTiersView.vue frontend/src/components/TierEditor.vue
git commit -m "feat: show arrival and paid amounts"
```

### Task 3: Fix the tier sort input layout

**Files:**
- Modify: `frontend/src/components/TierEditor.vue`

- [ ] **Step 1: Write the failing visual regression check**

Confirm the `el-input-number` controls are visible in the tier table after the layout change.

- [ ] **Step 2: Tighten the input width and control placement**

Give the sort input a full-width wrapper and use right-positioned controls so the increment button is not clipped by the table cell.

```vue
<el-input-number
  v-model="row.sort_order"
  class="tier-sort-input"
  :min="0"
  :step="10"
  controls-position="right"
  @change="emitUpdate"
/>
```

```css
.tier-sort-input {
  width: 100%;
  max-width: 160px;
}
```

- [ ] **Step 3: Verify the build**

Run:

```bash
cd frontend
pnpm build
```

Expected: pass, and the sort increment control is visible in the table cell.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/TierEditor.vue
git commit -m "fix: show tier sort increment control"
```

### Task 4: Auto-refresh the admin approval queue and make approved code copying easier

**Files:**
- Modify: `frontend/src/views/AdminAccessQueueView.vue`
- Modify: `frontend/src/components/CodeTable.vue`

- [ ] **Step 1: Write the failing behavior check**

The approval queue should refresh itself on a timer, and an issued code should have a one-click copy action visible in the approval dialog.

- [ ] **Step 2: Add polling and stronger copy affordance**

Start a refresh interval on mount, clear it on unmount, and call the existing `loadAll()` method on each tick. In the approval dialog, keep the issued code visible and put the copy action right beside it so the user can copy immediately after approval.

```ts
let refreshTimer: number | undefined

onMounted(async () => {
  await loadAll()
  refreshTimer = window.setInterval(loadAll, 15000)
})

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer)
})
```

- [ ] **Step 3: Re-run the frontend build**

Run:

```bash
cd frontend
pnpm build
```

Expected: pass with polling cleanup and the copy path intact.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/AdminAccessQueueView.vue frontend/src/components/CodeTable.vue
git commit -m "feat: refresh approvals and improve copy flow"
```

### Task 5: Final verification

**Files:**
- None new; verify the modified backend and frontend files together.

- [ ] **Step 1: Run the backend test suite**

```bash
cd backend
go test ./...
```

- [ ] **Step 2: Run the frontend production build**

```bash
cd frontend
pnpm build
```

- [ ] **Step 3: Check the working tree**

```bash
git status --short
```

Expected: only the intended feature files remain modified.
