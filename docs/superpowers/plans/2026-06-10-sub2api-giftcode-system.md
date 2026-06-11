# Sub2API Giftcode System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Vue 3 + Element Plus frontend and Go backend that gates redeem-code issuance behind email-approved access requests, stores a local ledger, and syncs code state with `sub2api`.

**Architecture:** The backend is a small Go service with SQLite, a `sub2api` client, SMTP mail delivery, and a thin HTTP API. The frontend is a single-page app that uses the backend for login, approval requests, redeem-code issuance, admin tier management, and status lookup.

**Tech Stack:** Go, Gin, SQLite, plain SQL, Vue 3, Vite, Element Plus, Vue Router, Pinia, TypeScript.

---

### Task 1: Repo scaffold and runtime defaults

**Files:**
- Create: `.gitignore`
- Create: `Makefile`
- Create: `README.md`
- Create: `config.example.yaml`
- Create: `backend/.gitignore`
- Create: `backend/go.mod`
- Create: `frontend/.gitignore`
- Create: `frontend/package.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/tsconfig.json`
- Create: `frontend/tsconfig.node.json`
- Create: `frontend/index.html`

- [ ] **Step 1: Create the empty project files and ignore rules**

```gitignore
.worktrees/
backend/bin/
backend/*.db
frontend/node_modules/
frontend/dist/
.env
config.yaml
```

- [ ] **Step 2: Add workspace commands**

```makefile
.PHONY: backend-test frontend-build

backend-test:
\tcd backend && go test ./...

frontend-build:
\tcd frontend && pnpm build
```

- [ ] **Step 3: Add the sample config**

```yaml
app:
  listen_addr: "127.0.0.1:8080"
  base_url: "http://127.0.0.1:8080"
database:
  driver: "sqlite"
  path: "./giftcode.db"
sub2api:
  base_url: "http://127.0.0.1:8081"
  admin_api_key: "admin-xxxxxxxx"
mail:
  smtp_host: "smtp.example.com"
  smtp_port: 587
  smtp_username: "mailer@example.com"
  smtp_password: "change-me"
  from_address: "mailer@example.com"
  admin_to_address: "admin@example.com"
session:
  cookie_secret: "change-me"
sync:
  interval_seconds: 300
```

- [ ] **Step 4: Verify the repo boots cleanly**

Run:

```bash
git status --short
```

Expected: only the scaffold files appear as untracked.

---

### Task 2: Backend foundation

**Files:**
- Create: `backend/cmd/server/main.go`
- Create: `backend/internal/config/config.go`
- Create: `backend/internal/config/load.go`
- Create: `backend/internal/config/load_test.go`
- Create: `backend/internal/db/migrate.go`
- Create: `backend/internal/db/store.go`
- Create: `backend/internal/db/store_test.go`
- Create: `backend/internal/sub2api/client.go`
- Create: `backend/internal/sub2api/client_test.go`
- Create: `backend/internal/mail/mailer.go`
- Create: `backend/internal/mail/mailer_test.go`
- Create: `backend/internal/httpapi/router.go`
- Create: `backend/internal/httpapi/auth_handlers.go`
- Create: `backend/internal/httpapi/access_handlers.go`
- Create: `backend/internal/httpapi/admin_handlers.go`
- Create: `backend/internal/httpapi/types.go`

- [ ] **Step 1: Write config, DB, and API client tests first**

```go
func TestLoadConfigRequiresAdminKey(t *testing.T) {
	cfg, err := Load("testdata/missing-key.yaml")
	require.Error(t, err)
	require.Nil(t, cfg)
}

func TestStoreCreatesTables(t *testing.T) {
	store := NewStore(":memory:")
	require.NoError(t, store.Migrate())
}
```

- [ ] **Step 2: Implement the config loader, SQLite schema, and startup path**

```go
func main() {
	cfg := mustLoadConfig()
	store := mustOpenStore(cfg.Database)
	mustMigrate(store)
	router := httpapi.NewRouter(cfg, store, sub2apiClient, mailer)
	log.Fatal(http.ListenAndServe(cfg.App.ListenAddr, router))
}
```

- [ ] **Step 3: Implement `sub2api` auth and admin client calls**

```go
type Client struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}
```

Support:

- `POST /api/v1/auth/login`
- `GET /api/v1/users/me`
- `POST /api/v1/admin/redeem-codes/generate`
- `GET /api/v1/admin/redeem-codes`
- `GET /api/v1/admin/users/:id`

- [ ] **Step 4: Implement SMTP mail delivery**

```go
func (m *Mailer) SendApprovalEmail(to, subject, body string) error
```

The email must include a one-time approval URL that points back to the backend confirm endpoint.

- [ ] **Step 5: Verify backend unit tests pass**

Run:

```bash
cd backend
go test ./...
```

Expected: pass with the new config, store, mail, and client tests.

---

### Task 3: Local ledger and approval / redeem workflow

**Files:**
- Create: `backend/internal/domain/models.go`
- Create: `backend/internal/domain/service.go`
- Create: `backend/internal/domain/approval.go`
- Create: `backend/internal/domain/redeem.go`
- Create: `backend/internal/domain/tiers.go`
- Modify: `backend/internal/httpapi/auth_handlers.go`
- Modify: `backend/internal/httpapi/access_handlers.go`
- Modify: `backend/internal/httpapi/admin_handlers.go`
- Create: `backend/internal/httpapi/auth_handlers_test.go`
- Create: `backend/internal/httpapi/access_handlers_test.go`
- Create: `backend/internal/httpapi/admin_handlers_test.go`

- [ ] **Step 1: Write failing tests for approval gating and one-time consumption**

```go
func TestAccessRequestApproveThenConsumeOnce(t *testing.T) {
    // create request
    // approve with token
    // redeem once succeeds
    // second redeem fails
}
```

- [ ] **Step 2: Implement the access-request table and approval token flow**

```go
type AccessRequest struct {
	ID        int64
	Status    string
	TokenHash string
}
```

Rules:

- a user may only redeem after approval
- one approval can be consumed only once
- approval token is one-time and expires

- [ ] **Step 3: Implement tier CRUD for balance amounts**

Defaults:

```go
[]BalanceTier{
  {Amount: 120, Label: "$120", Enabled: true, SortOrder: 10},
  {Amount: 240, Label: "$240", Enabled: true, SortOrder: 20},
}
```

- [ ] **Step 4: Implement redeem request creation and upstream code generation**

```go
func (s *Service) CreateRedeemRequest(ctx context.Context, userID int64, tierID int64, note string) (*RedeemRequest, error)
```

Behavior:

- check access request status
- ensure selected tier is enabled
- call `sub2api` admin generate API
- persist generated codes
- mark access request consumed

- [ ] **Step 5: Implement admin listing, sync, and summary endpoints**

Admin endpoints must list:

- all access requests
- all redeem requests
- per-user redeem history
- current balance tiers
- sync status

- [ ] **Step 6: Verify backend tests and API routes**

Run:

```bash
cd backend
go test ./...
```

Expected: pass all handler and domain tests.

---

### Task 4: Vue 3 frontend

**Files:**
- Create: `frontend/src/main.ts`
- Create: `frontend/src/App.vue`
- Create: `frontend/src/router/index.ts`
- Create: `frontend/src/stores/session.ts`
- Create: `frontend/src/api/http.ts`
- Create: `frontend/src/api/auth.ts`
- Create: `frontend/src/api/access.ts`
- Create: `frontend/src/api/redeem.ts`
- Create: `frontend/src/api/admin.ts`
- Create: `frontend/src/views/LoginView.vue`
- Create: `frontend/src/views/UserDashboardView.vue`
- Create: `frontend/src/views/AccessRequestView.vue`
- Create: `frontend/src/views/RedeemRequestView.vue`
- Create: `frontend/src/views/AdminDashboardView.vue`
- Create: `frontend/src/views/AdminAccessQueueView.vue`
- Create: `frontend/src/views/AdminTiersView.vue`
- Create: `frontend/src/components/AppLayout.vue`
- Create: `frontend/src/components/StatusTag.vue`
- Create: `frontend/src/components/CodeTable.vue`
- Create: `frontend/src/components/TierEditor.vue`

- [ ] **Step 1: Write component tests for the core render paths**

```ts
it("renders login form and submits credentials", async () => {
  // mount, fill, submit, assert api call
})
```

- [ ] **Step 2: Build the app shell and router guards**

```ts
router.beforeEach((to) => {
  if (to.meta.requiresAuth && !session.isLoggedIn) return "/login"
})
```

- [ ] **Step 3: Build user views**

Requirements:

- login with `sub2api` credentials
- submit access request
- wait for approval
- submit redeem request against a selected enabled tier
- show the returned code immediately

- [ ] **Step 4: Build admin views**

Requirements:

- show access queue
- approve/reject requests
- edit balance tiers
- trigger sync
- view per-user and global code lists

- [ ] **Step 5: Verify frontend build**

Run:

```bash
cd frontend
pnpm install
pnpm build
```

Expected: successful production build.

---

### Task 5: End-to-end wiring and polish

**Files:**
- Modify: `README.md`
- Create: `.env.example`
- Create: `docs/runbook.md`
- Create: `backend/internal/httpapi/e2e_test.go`

- [ ] **Step 1: Add startup documentation and environment examples**

Include:

- backend listen address
- SQLite path
- `sub2api` base URL and admin API key
- SMTP host and admin recipient

- [ ] **Step 2: Add a smoke test for the approval-to-redeem path**

```go
func TestEndToEndApprovalToRedeem(t *testing.T) {
    // login
    // create access request
    // approve via token
    // create redeem request
    // assert one code returned
}
```

- [ ] **Step 3: Run full verification**

Run:

```bash
cd backend && go test ./...
cd ../frontend && pnpm build
```

Expected: both commands pass.

- [ ] **Step 4: Commit the implementation**

```bash
git add .
git commit -m "feat: initialize sub2api giftcode system"
```
