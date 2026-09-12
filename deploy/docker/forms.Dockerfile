# syntax=docker/dockerfile:1.7
#
# The forms service: the public face of hosted lead-capture forms. The React
# (TanStack) app in forms/ builds to static files; the Go binary serves them,
# the per-form page shells and the same-origin submit API. Always CGO-free —
# no event bus, no database, no cache; the backend's internal API is its only
# dependency.
# Builders run on $BUILDPLATFORM and cross-compile / emit static assets for
# $TARGETARCH (no QEMU).
FROM --platform=$BUILDPLATFORM node:22-alpine AS appbuilder
WORKDIR /app
# No TTY in a build: CI=true makes pnpm reinstall instead of prompting.
ENV CI=true
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY forms/package.json forms/pnpm-lock.yaml forms/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY forms/ .
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS TARGETARCH
# Build identity shown in the admin panel; see internal/version.
ARG VERSION="" COMMIT="" BUILT_AT=""
RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,id=s/8b5712a5-156b-4b4e-92e2-38a935c8efb1-/go/pkg/mod,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,id=s/8b5712a5-156b-4b4e-92e2-38a935c8efb1-/go/pkg/mod,target=/go/pkg/mod \
    --mount=type=cache,id=s/8b5712a5-156b-4b4e-92e2-38a935c8efb1-/root/.cache/go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w -X github.com/warmbly/warmbly/internal/version.Version=$VERSION -X github.com/warmbly/warmbly/internal/version.Commit=$COMMIT -X github.com/warmbly/warmbly/internal/version.BuiltAt=$BUILT_AT" -o /out/forms ./cmd/forms

# Runtime stage
FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 warmbly

COPY --from=builder /out/forms /app/forms
COPY --from=appbuilder /app/dist /app/static
ENV FORMS_STATIC_DIR=/app/static

USER warmbly
EXPOSE 8090

# 127.0.0.1, not localhost: busybox wget tries ::1 first but the server binds IPv4.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8090/health || exit 1

ENTRYPOINT ["/app/forms"]
