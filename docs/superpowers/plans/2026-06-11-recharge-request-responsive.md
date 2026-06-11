# Recharge Request Responsive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a user-facing "充值兑换申请" route with desktop and mobile recharge-oriented layouts, and make approval emails enter through the configured frontend URL.

**Architecture:** Keep the existing access request API and old route for compatibility. Add a new Vue route and page that reuses the same data APIs but changes the visual hierarchy from admin tables to recharge cards and status summaries. Update the backend approval email URL helper to use `app.frontend_url` with a frontend confirmation route, protected by focused Go tests and an explanatory comment.

**Tech Stack:** Vue 3, Vue Router, Element Plus, TypeScript, Go, Gin, SQLite-backed service tests.

---

### Task 1: Backend Approval Email Link

**Files:**
- Modify: `backend/internal/app/service_test.go`
- Modify: `backend/internal/app/service.go`

- [ ] Change the approval URL unit test to expect `app.frontend_url`.
- [ ] Run `go test ./internal/app -run TestApprovalConfirmURLUsesFrontendBase -count=1` and confirm it fails before implementation.
- [ ] Update `approvalConfirmURL` to return `${frontend_url}/approval/confirm?token=...` when `app.frontend_url` is configured, otherwise fall back to `${base_url}/api/admin/redeem-access-requests/confirm?token=...`.
- [ ] Add a comment explaining that email links must use the public frontend URL, not the backend API URL.
- [ ] Re-run the focused test and then the backend test suite.

### Task 2: Frontend Confirmation Route

**Files:**
- Create: `frontend/src/views/ApprovalConfirmView.vue`
- Modify: `frontend/src/router/index.ts`

- [ ] Add `/approval/confirm` as a plain route so email links can land in the SPA.
- [ ] The page should read `token`, call `previewAccessRequest(token)`, show the request details, and only call `confirmAccessRequest(token)` after the admin clicks "确认审批并发码".

### Task 3: Recharge Request Page

**Files:**
- Create: `frontend/src/views/RechargeRequestView.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/AppLayout.vue`
- Modify: `frontend/src/views/UserDashboardView.vue`

- [ ] Add `/recharge-request` route using the new page.
- [ ] Update user navigation and dashboard actions to point to `/recharge-request`.
- [ ] Keep `/access-request` available and redirect `/redeem-request` to `/recharge-request`.
- [ ] Build the new page with plan cards, application summary, note input, request status list, and latest code copy.
- [ ] Add responsive CSS so desktop is two-column and mobile is single-column wallet-style.

### Task 4: Verification

**Files:**
- Verify all changed files.

- [ ] Run backend tests.
- [ ] Run frontend build.
- [ ] Open `/recharge-request` in browser and check desktop layout.
- [ ] Use a mobile viewport and check the single-column layout.
- [ ] Verify the email URL unit test covers the frontend URL behavior.
