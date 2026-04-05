# Docker Compose Examples

This file shows example Docker Compose setups for all three source modes supported by `jinko-exporter`.

Notes:

- Replace placeholder credentials before use.
- The Jinko bearer token is expected to be copied from an active browser session.
- SMTP alerts are optional; examples below use placeholders only.
- The Modbus source is still a placeholder until protocol/register details are available.

## Build the image locally

This repository includes a `Dockerfile`, so you can either build the image directly:

```powershell
docker build -t rcooler/jinko-exporter:local .
```

or use `build: .` in Compose.

Example placeholder image name used below:

- `rcooler/jinko-exporter:local`

## Jinko detail API

```yaml
services:
  jinko_exporter:
    build: .
    image: rcooler/jinko-exporter:local
    container_name: jinko_exporter
    restart: unless-stopped
    environment:
      EXPORTER_SOURCE: "jinko"
      EXPORTER_LISTEN: ":9876"
      EXPORTER_METRICS_PATH: "/metrics"
      EXPORTER_POLL_INTERVAL: "60s"
      EXPORTER_LOG_LEVEL: "info"
      EXPORTER_METRIC_PREFIX: "solar"

      JINKO_URL: "https://smart-global.jinkosolar.com/device-s/device/v3/detail"
      JINKO_TIMEOUT: "20s"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_LANGUAGE: "en"
      JINKO_NEED_REALTIME_DATA: "true"
      JINKO_BEARER_TOKEN: "<JWT>"
      # Optional if bearer-only auth is not enough:
      # JINKO_COOKIE: "cookie1=value1; cookie2=value2"
      JINKO_REQUEST_JITTER_MAX: "30s"
      JINKO_TOKEN_ALERT_WINDOW: "24h"

      ALERTS_ENABLED: "true"
      ALERTS_COOLDOWN: "6h"
      SMTP_TIMEOUT: "15s"
      SMTP_HOST: "smtp.example.com"
      SMTP_PORT: "587"
      SMTP_USERNAME: "<SMTP_USERNAME>"
      SMTP_PASSWORD: "<SMTP_PASSWORD>"
      SMTP_FROM_EMAIL: "alerts@example.com"
      SMTP_FROM_NAME: "Jinko Exporter"
      SMTP_TO_EMAILS: "ops@example.com"
      SMTP_USE_TLS: "false"
      SMTP_STARTTLS: "true"
      ALERT_NO_SUCCESSFUL_POLL_WINDOW: "10m"
      ALERT_GRID_DOWN_VOLTAGE_THRESHOLD: "20"
      ALERT_BATTERY_SOC_LOW_THRESHOLD: "15"
      ALERT_HIGH_TEMPERATURE_THRESHOLD: "55"

    ports:
      - "9876:9876"
```

## Solarman OpenAPI

```yaml
services:
  jinko_exporter:
    build: .
    image: rcooler/jinko-exporter:local
    container_name: jinko_exporter
    restart: unless-stopped
    environment:
      EXPORTER_SOURCE: "solarman"
      EXPORTER_LISTEN: ":9876"
      EXPORTER_METRICS_PATH: "/metrics"
      EXPORTER_POLL_INTERVAL: "60s"
      EXPORTER_LOG_LEVEL: "info"
      EXPORTER_METRIC_PREFIX: "solar"

      SOLARMAN_BASE_URL: "https://globalapi.solarmanpv.com"
      SOLARMAN_API_VERSION: "v1.0"
      SOLARMAN_LANGUAGE: "en"
      SOLARMAN_TIMEOUT: "20s"
      SOLARMAN_APP_ID: "<APP_ID>"
      SOLARMAN_APP_SECRET: "<APP_SECRET>"
      SOLARMAN_EMAIL: "user@example.com"

      # Use one of these:
      SOLARMAN_PASSWORD: "<PASSWORD>"
      # SOLARMAN_PASSWORD_SHA256: "<PASSWORD_SHA256>"

      # Recommended to skip discovery if you already know it:
      SOLARMAN_DEVICE_SN: "1234567890"
      # Optional if you want discovery through a specific station:
      # SOLARMAN_STATION_ID: "123456"

      ALERTS_ENABLED: "true"
      ALERTS_COOLDOWN: "6h"
      SMTP_TIMEOUT: "15s"
      SMTP_HOST: "smtp.example.com"
      SMTP_PORT: "587"
      SMTP_USERNAME: "<SMTP_USERNAME>"
      SMTP_PASSWORD: "<SMTP_PASSWORD>"
      SMTP_FROM_EMAIL: "alerts@example.com"
      SMTP_FROM_NAME: "Jinko Exporter"
      SMTP_TO_EMAILS: "ops@example.com"
      SMTP_USE_TLS: "false"
      SMTP_STARTTLS: "true"
      ALERT_NO_SUCCESSFUL_POLL_WINDOW: "10m"
      ALERT_GRID_DOWN_VOLTAGE_THRESHOLD: "20"
      ALERT_BATTERY_SOC_LOW_THRESHOLD: "15"
      ALERT_HIGH_TEMPERATURE_THRESHOLD: "55"

    ports:
      - "9876:9876"
```

## Modbus placeholder

This shows the future config shape only. The current implementation will return a clear `not implemented` error.

```yaml
services:
  jinko_exporter:
    build: .
    image: rcooler/jinko-exporter:local
    container_name: jinko_exporter
    restart: unless-stopped
    environment:
      EXPORTER_SOURCE: "modbus"
      EXPORTER_LISTEN: ":9876"
      EXPORTER_METRICS_PATH: "/metrics"
      EXPORTER_POLL_INTERVAL: "60s"
      EXPORTER_LOG_LEVEL: "info"
      EXPORTER_METRIC_PREFIX: "solar"

      MODBUS_HOST: "192.168.120.10"
      MODBUS_PORT: "8899"
      MODBUS_LOGGER_SERIAL: "<LOGGER_SERIAL>"
      MODBUS_UNIT_ID: "1"
      MODBUS_TIMEOUT: "5s"

    ports:
      - "9876:9876"
```

## Optional Prometheus example

```yaml
services:
  jinko_exporter:
    build: .
    image: rcooler/jinko-exporter:local
    restart: unless-stopped
    environment:
      EXPORTER_SOURCE: "jinko"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN: "<JWT>"
    ports:
      - "9876:9876"

  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    restart: unless-stopped
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    ports:
      - "9090:9090"
```

Example `prometheus.yml`:

```yaml
global:
  scrape_interval: 30s

scrape_configs:
  - job_name: jinko_exporter
    metrics_path: /metrics
    static_configs:
      - targets:
          - jinko_exporter:9876
```
