# Access Approval Code Issue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge access approval and redeem-code issuance into one admin action, while binding each access request to a specific balance tier and moving the remaining forms into dialogs.

**Architecture:** Store the requested tier directly on `redeem_access_requests`, validate it when the request is created, and reuse that tier when the admin approves the request. The approve handler should return both the updated request and the issued code so the admin dialog can show the result immediately. On the frontend, keep the list views but move the submission/approval forms into `el-dialog` overlays and remove the separate redeem-request workflow from navigation.

**Tech Stack:** Go, SQLite, Gin, Vue 3, Element Plus, TypeScript

---

### Task 1: Persist the requested tier on access requests

**Files:**
- Modify: `backend/internal/db/migrate.go`
- Modify: `backend/internal/models/models.go`
- Modify: `backend/internal/app/access.go`
- Modify: `backend/internal/httpapi/types.go`
- Modify: `backend/internal/httpapi/access_handlers.go`
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/access.ts`
- Modify: `frontend/src/views/AccessRequestView.vue`
- Test: `backend/internal/app/service_test.go`
- Test: `backend/internal/db/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCreateAccessRequestStoresTier(t *testing.T) {
    // create in-memory DB, seed tiers, submit an access request with tier ID 1,
    // and assert the saved request has TierID == 1.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./backend/internal/app -run TestCreateAccessRequestStoresTier -v`
Expected: FAIL because `tier_id` is not yet stored/loaded.

- [ ] **Step 3: Write minimal implementation**

```go
type AccessRequest struct {
    // ...
    TierID int64 `json:"tier_id"`
}

type AccessRequestCreateRequest struct {
    TierID int64 `json:"tier_id" binding:"required,gt=0"`
    Note   string `json:"note"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./backend/internal/app ./backend/internal/db -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/db/migrate.go backend/internal/models/models.go backend/internal/app/access.go backend/internal/httpapi/types.go backend/internal/httpapi/access_handlers.go frontend/src/api/types.ts frontend/src/api/access.ts frontend/src/views/AccessRequestView.vue
git commit -m "feat: bind access requests to tiers"
```

### Task 2: Approve and issue in one admin action

**Files:**
- Modify: `backend/internal/app/redeem.go`
- Modify: `backend/internal/app/access.go`
- Modify: `backend/internal/httpapi/access_handlers.go`
- Modify: `backend/internal/httpapi/types.go`
- Modify: `frontend/src/api/admin.ts`
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/views/AdminAccessQueueView.vue`
- Test: `backend/internal/app/admin_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestApproveAccessRequestIssuesTierMatchedCode(t *testing.T) {
    // create an approved-tier access request, call approve, and assert the
    // returned code value matches the requested tier amount and the request
    // is consumed.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./backend/internal/app -run TestApproveAccessRequestIssuesTierMatchedCode -v`
Expected: FAIL because approval still only flips request status.

- [ ] **Step 3: Write minimal implementation**

```go
type RedeemIssueResponse struct {
    Request any `json:"request"`
    Code    any `json:"code,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./backend/internal/app ./backend/internal/httpapi -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/app/redeem.go backend/internal/app/access.go backend/internal/httpapi/access_handlers.go backend/internal/httpapi/types.go frontend/src/api/admin.ts frontend/src/api/types.ts frontend/src/views/AdminAccessQueueView.vue
git commit -m "feat: approve and issue redeem codes together"
```

### Task 3: Move the forms into dialogs and drop the separate redeem page

**Files:**
- Modify: `frontend/src/views/AccessRequestView.vue`
- Modify: `frontend/src/views/AdminAccessQueueView.vue`
- Modify: `frontend/src/views/UserDashboardView.vue`
- Modify: `frontend/src/components/AppLayout.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/stores/session.ts`
- Modify: `frontend/src/views/RedeemRequestView.vue` (remove or keep read-only)

- [ ] **Step 1: Write the failing test**

```ts
// Build check: the app should still compile after the redeem request route is removed
// and the access/admin forms live inside dialogs.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm build`
Expected: FAIL until the router and dialog-based components are updated together.

- [ ] **Step 3: Write minimal implementation**

```vue
<el-dialog v-model="dialogVisible" title="提交申请">
  <!-- tier select + note form -->
</el-dialog>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/views/AccessRequestView.vue frontend/src/views/AdminAccessQueueView.vue frontend/src/views/UserDashboardView.vue frontend/src/components/AppLayout.vue frontend/src/router/index.ts frontend/src/stores/session.ts frontend/src/views/RedeemRequestView.vue
git commit -m "feat: move request flows into dialogs"
```

