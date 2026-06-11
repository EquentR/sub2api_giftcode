# Docker GHCR Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Docker, GitHub Actions, README, license, and Compose support for publishing a single backend-hosted frontend image.

**Architecture:** Build Vue assets first, copy them into the Go runtime image, and let Gin serve API routes plus SPA static assets from one process. Publish the image to GHCR on `main` pushes.

**Tech Stack:** Go, Gin, Vue 3, Vite, pnpm, Docker BuildKit, GitHub Actions, GHCR, Docker Compose.

---

### Task 1: Backend SPA Static Hosting

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/load.go`
- Modify: `backend/internal/config/load_test.go`
- Modify: `backend/internal/httpapi/router.go`
- Create: `backend/internal/httpapi/static_test.go`

- [ ] Add `app.static_dir` to runtime configuration with a default of `./public`.
- [ ] Write failing router tests that verify `/` and nested SPA routes return `index.html` when static assets exist.
- [ ] Implement static file serving while preserving JSON 404 responses for `/api/*`.
- [ ] Run `cd backend && go test ./internal/config ./internal/httpapi`.

### Task 2: Container Build And Compose Template

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `compose.yaml`
- Modify: `config.example.yaml`

- [ ] Create a multi-stage Dockerfile using `pnpm` for the frontend build and Go for the backend build.
- [ ] Copy the backend binary and frontend assets into `/app`.
- [ ] Add `.dockerignore` entries for local build outputs, Git metadata, IDE files, and local config.
- [ ] Add a Compose template that mounts `./config.yaml:/app/config.yaml:ro` and `./data:/app/data`.
- [ ] Adjust `config.example.yaml` for container-friendly `listen_addr`, `database.path`, and unified frontend access.

### Task 3: GitHub Actions Image Publishing

**Files:**
- Create: `.github/workflows/docker-image.yml`

- [ ] Add a workflow for pushes to `main`, tags, and pull requests.
- [ ] Use `docker/login-action`, `docker/metadata-action`, and `docker/build-push-action`.
- [ ] Push only for non-PR events.
- [ ] Tag `latest` on the default branch and tag every build with the commit SHA.

### Task 4: README And License

**Files:**
- Modify: `README.md`
- Create: `LICENSE`

- [ ] Rewrite README around the positioning: `Sub2API站长无真实充值渠道时的审批发码平台`.
- [ ] Document local development, Docker Compose deployment, GHCR image publishing, configuration, and data persistence.
- [ ] Add Apache-2.0 license text.

### Task 5: Verification And Publish

**Files:**
- No source file changes expected.

- [ ] Run backend tests.
- [ ] Run frontend build.
- [ ] Build the Docker image, using WSL if Docker is available there.
- [ ] Create a public GitHub repository if credentials allow it.
- [ ] Add the GitHub remote and push `main`.
