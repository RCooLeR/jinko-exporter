# --- Stage 1: Build the Go binary ---
FROM golang:1.26.4-alpine AS builder

# Install git and CA certs (if needed for Go modules)
RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY . .

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/jinko-exporter .

# --- Stage 2: Create a lightweight image with the binary only ---
FROM alpine:3.23

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
