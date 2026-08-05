# --- Stage 1: Build the Go binary ---
FROM golang:1.26.4-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Install git and CA certs (if needed for Go modules)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Version=${VERSION} -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Commit=${COMMIT} -X github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo.Date=${DATE}" \
    -o /out/jinko-exporter .

# --- Stage 2: Create a lightweight image with the binary only ---
FROM alpine:3.24

# Add CA certs for HTTPS support
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates \
    && adduser -D -H -u 65532 appuser

COPY --from=builder /out/jinko-exporter /jinko-exporter

EXPOSE 9876

USER appuser
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/jinko-exporter", "healthcheck"]
ENTRYPOINT ["/jinko-exporter"]
CMD ["serve"]
