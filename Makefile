.PHONY: backend-test backend-run frontend-install frontend-build frontend-dev

backend-test:
	cd backend && go test ./...

backend-run:
	cd backend && go run ./cmd/server -config ../config.yaml

frontend-install:
	cd frontend && pnpm install

frontend-build:
	cd frontend && pnpm build

frontend-dev:
	cd frontend && pnpm dev
