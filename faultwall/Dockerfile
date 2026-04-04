# ─────────────────────────────────────────────────────────
# FaultWall Dockerfile
# Multi-stage build: compile Go binary, run minimal image
# ─────────────────────────────────────────────────────────

# ── Stage 1: Build ────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go module files first for layer caching
COPY go.mod ./

# Generate go.sum and download dependencies
RUN go mod download 2>/dev/null || true

# Copy source
COPY src/ ./

# Tidy modules (handles any missing go.sum entries)
RUN go mod tidy

# Build the binary — static, stripped, optimized
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w -X main.version=hackathon-2025" \
    -o faultwall \
    .

# ── Stage 2: Runtime ──────────────────────────────────────
FROM alpine:3.19

WORKDIR /app

# Bring in CA certs and timezone data from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /build/faultwall /app/faultwall

# Default policy location (override via volume mount)
RUN mkdir -p /etc/faultwall

# Run as non-root for security best practice
RUN addgroup -g 1001 faultwall && \
    adduser -D -u 1001 -G faultwall faultwall && \
    chown -R faultwall:faultwall /app /etc/faultwall

USER faultwall

EXPOSE 5433 8080

HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/faultwall"]
