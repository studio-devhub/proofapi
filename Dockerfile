# ── Stage 1: Builder ─────────────────────────────────────
FROM golang:1.22.2-alpine AS builder

WORKDIR /app

# Install git (needed for some go modules)
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./

# Copy vendor-local (yaml.v3 replacement)
COPY vendor-local/ ./vendor-local/

# Download dependencies
RUN GONOSUMDB="*" GOFLAGS="-mod=mod" GOPROXY="direct" go mod download

# Copy source
COPY . .

# Build binary — optimized, stripped
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    GONOSUMDB="*" GOFLAGS="-mod=mod" \
    go build \
    -ldflags="-s -w -X main.version=$(date +%Y%m%d)" \
    -o /app/api \
    ./cmd/api

# ── Stage 2: Final Image ──────────────────────────────────
FROM scratch

# SSL certs & timezone
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Binary only
COPY --from=builder /app/api /api

EXPOSE 4003

ENTRYPOINT ["/api"]
