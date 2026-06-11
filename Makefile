.PHONY: backend-test backend-run frontend-install frontend-build frontend-dev

backend-test:
\tcd backend && go test ./...

backend-run:
\tcd backend && go run ./cmd/server -config ../config.yaml

frontend-install:
\tcd frontend && pnpm install

frontend-build:
\tcd frontend && pnpm build

frontend-dev:
\tcd frontend && pnpm dev
