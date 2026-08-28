# syntax=docker/dockerfile:1

# --- Stage 1: Build the Go binary ---
FROM golang:1.27.0-alpine3.24 AS builder

ENV GOTOOLCHAIN=local

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Install git and CA certs (if needed for Go modules)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY --link go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY --link . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Version=${VERSION} -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Commit=${COMMIT} -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Date=${DATE}" \
    -o /out/jinko-exporter .

# --- Stage 2: Create a lightweight image with the binary only ---
FROM alpine:3.24.1

# Add CA certificates for upstream HTTPS and a fixed non-root identity.
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 appuser \
    && adduser -S -D -H -u 65532 -G appuser appuser

COPY --from=builder --chown=65532:65532 --chmod=0555 /out/jinko-exporter /jinko-exporter

EXPOSE 9876

USER 65532:65532
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --start-interval=2s --retries=3 CMD ["/jinko-exporter", "healthcheck"]
ENTRYPOINT ["/jinko-exporter"]
CMD ["serve"]
