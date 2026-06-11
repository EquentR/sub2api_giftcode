# Docker GHCR Deployment Design

## Goal

Build Sub2API Giftcode into a single Docker image that contains both the Go backend and the Vue frontend, serves the frontend through the backend, and publishes the image automatically from GitHub Actions when `main` is updated.

## Product Positioning

The README should describe the project as:

> Sub2API站长无真实充值渠道时的审批发码平台

## Architecture

- The Vue frontend is built with pnpm/Vite into `frontend/dist`.
- The Go backend is built as a Linux binary and copied into a small runtime image.
- The final image stores static frontend files under `/app/public`.
- The backend serves API routes under `/api/*` and serves the SPA for other routes when static files are available.
- Runtime configuration remains YAML based. `config.yaml` and the SQLite data directory are mounted from the host in Compose.

## GitHub Actions

- On pull requests and non-main branches, the workflow builds the image for validation.
- On pushes to `main`, the workflow pushes the image to GitHub Container Registry.
- The image is tagged as `ghcr.io/<owner>/<repo>:latest` and `ghcr.io/<owner>/<repo>:<commit-sha>`.

## Deployment

- Provide a `compose.yaml` template that runs the published image, mounts `config.yaml`, mounts a persistent `data/` directory, and exposes port `8080`.
- The container must listen on `0.0.0.0:8080`.
- `app.frontend_url` can stay empty for unified backend-hosted frontend access.

## License

Use Apache-2.0 unless the maintainer later chooses AGPL-3.0 for stronger network-service copyleft.

## Verification

- Backend tests pass with `go test ./...`.
- Frontend builds with `pnpm build`.
- Docker image builds successfully.
- The built container can serve `/` through the backend-hosted SPA.
