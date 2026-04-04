# ──────────────────────────────────────────────────────────────────
#  Stage 1: Build
# ──────────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# Install git (needed for go mod download) and ca-certs
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache dependencies separately for faster rebuilds
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a fully static binary
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.buildVersion=demo-1.0" \
    -o /out/faultwall-proxy .

# ──────────────────────────────────────────────────────────────────
#  Stage 2: Runtime (minimal Alpine)
# ──────────────────────────────────────────────────────────────────
FROM alpine:3.19

# Timezone data + TLS roots
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary
COPY --from=builder /out/faultwall-proxy /app/faultwall-proxy

# Default policy (can be overridden by mounting a file at /app/policy.yaml)
COPY policy.yaml /app/policy.yaml

# Non-root user for safety
RUN addgroup -S faultwall && adduser -S faultwall -G faultwall
USER faultwall

EXPOSE 8080

# DATABASE_URL can be set at runtime:
#   docker run -e DATABASE_URL="postgres://user:pass@host/db" ...
ENV PORT=8080

ENTRYPOINT ["/app/faultwall-proxy"]
