# syntax=docker/dockerfile:1.7

ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.23-alpine
ARG RUNTIME_IMAGE=alpine:3.21

FROM ${NODE_IMAGE} AS frontend-build
WORKDIR /src/frontend
RUN corepack enable && corepack prepare pnpm@10.33.0 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
COPY frontend/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm build

FROM ${GO_IMAGE} AS backend-build
WORKDIR /src/backend
RUN apk add --no-cache ca-certificates
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sub2api-giftcode ./cmd/server

FROM ${RUNTIME_IMAGE}
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && \
    adduser -S -G app app && \
    mkdir -p /app/data /app/public && \
    chown -R app:app /app
COPY --from=backend-build /out/sub2api-giftcode /app/sub2api-giftcode
COPY --from=frontend-build /src/frontend/dist/ /app/public/
COPY config.example.yaml /app/config.example.yaml
USER app
EXPOSE 8080
ENTRYPOINT ["/app/sub2api-giftcode"]
CMD ["-config", "/app/config.yaml"]
