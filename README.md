# jinko-exporter

`jinko-exporter` is a Prometheus exporter with pluggable data sources:

- `jinko`: private browser-backed Jinko detail API
- `solarman`: Solarman OpenAPI
- `modbus`: placeholder module until the local protocol/register map is confirmed

The exporter polls upstream on a fixed interval, keeps the latest snapshot in memory, and exposes Prometheus metrics on HTTP.

## Features

- Go `1.26.1` toolchain via `toolchain go1.26.1`
- CLI and env var config through `github.com/urfave/cli/v2`
- Structured logging through `github.com/rs/zerolog`
- Prometheus metrics through `github.com/prometheus/client_golang`
- Request logging for every outbound API call
- Optional SMTP mail alerts for Jinko token issues and Solarman request failures
- One-shot `fetch` mode for debugging source integration

## Build

```powershell
go build .
```

## Metrics

The exporter exposes:

- `solar_up{source,device_sn}`
- `solar_last_update_timestamp_seconds{source,device_sn}`
- `solar_poll_duration_seconds{source}`
- `solar_request_errors_total{source}`
- `solar_metric{source,device_sn,group,key,name,unit}`

`solar_metric` is the generic numeric metric stream. For the Jinko detail source it uses values from `paramCategoryList.fieldList`, typically keyed by `storageName`.

## Command usage

Run the exporter:

```powershell
.\jinko-exporter.exe serve --source jinko
```

Fetch once and print normalized JSON:

```powershell
.\jinko-exporter.exe fetch --source jinko
```

## Alerts

SMTP alerts are optional and disabled by default.

Supported first alert path:

- Jinko bearer token already expired
- Jinko bearer token expiring within a configurable window
- Jinko API `401` / `403` responses
- Solarman token/discovery/currentData request failures
- No successful poll for a configured time window
- Active inverter alarm/fault metrics
- Grid down when all available grid voltages collapse below a threshold
- Optional low battery SOC and high temperature thresholds

Alert config:

- `--alerts-enabled` / `ALERTS_ENABLED`
- `--alerts-cooldown` / `ALERTS_COOLDOWN`
- `--smtp-host` / `SMTP_HOST`
- `--smtp-port` / `SMTP_PORT`
- `--smtp-username` / `SMTP_USERNAME`
- `--smtp-password` / `SMTP_PASSWORD`
- `--smtp-from-email` / `SMTP_FROM_EMAIL`
- `--smtp-from-name` / `SMTP_FROM_NAME`
- `--smtp-to-email` / `SMTP_TO_EMAILS`
- `--smtp-use-tls` / `SMTP_USE_TLS`
- `--smtp-starttls` / `SMTP_STARTTLS`
- `--smtp-timeout` / `SMTP_TIMEOUT`
- `--alert-no-successful-poll-window` / `ALERT_NO_SUCCESSFUL_POLL_WINDOW`
- `--alert-grid-down-voltage-threshold` / `ALERT_GRID_DOWN_VOLTAGE_THRESHOLD`
- `--alert-battery-soc-low-threshold` / `ALERT_BATTERY_SOC_LOW_THRESHOLD`
- `--alert-high-temperature-threshold` / `ALERT_HIGH_TEMPERATURE_THRESHOLD`

If `SMTP_TO_EMAILS` is not set, the exporter falls back to `SMTP_FROM_EMAIL` as the recipient.

Metric-value alerts:

- inverter alarm/fault metrics: enabled automatically when alerts are enabled
- no successful poll: disabled by default until `ALERT_NO_SUCCESSFUL_POLL_WINDOW` is set above `0`
- grid down: enabled automatically using `ALERT_GRID_DOWN_VOLTAGE_THRESHOLD` and defaults to `20`
- low battery SOC: disabled by default until `ALERT_BATTERY_SOC_LOW_THRESHOLD` is set above `0`
- high temperature: disabled by default until `ALERT_HIGH_TEMPERATURE_THRESHOLD` is set above `0`

## Jinko detail source

This source calls:

- `POST https://smart-global.jinkosolar.com/device-s/device/v3/detail`

Required config:

- `--jinko-device-id` / `JINKO_DEVICE_ID`
- `--jinko-site-id` / `JINKO_SITE_ID`
- `--jinko-bearer-token` / `JINKO_BEARER_TOKEN`

Optional config:

- `--jinko-cookie` / `JINKO_COOKIE`
- `--jinko-request-jitter-max` / `JINKO_REQUEST_JITTER_MAX`
- `--jinko-token-alert-window` / `JINKO_TOKEN_ALERT_WINDOW`
- `--jinko-language` / `JINKO_LANGUAGE`
- `--jinko-need-realtime` / `JINKO_NEED_REALTIME_DATA`

Example:

```powershell
.\jinko-exporter.exe serve `
  --source jinko `
  --listen :9876 `
  --poll-interval 60s `
  --jinko-device-id 100000001 `
  --jinko-site-id 200000001 `
  --jinko-bearer-token "<JWT>" `
  --jinko-request-jitter-max 7s `
  --alerts-enabled `
  --alerts-cooldown 6h `
  --smtp-host "smtp.example.com" `
  --smtp-port 587 `
  --smtp-username "<SMTP_USERNAME>" `
  --smtp-password "<SMTP_PASSWORD>" `
  --smtp-from-email "alerts@example.com" `
  --smtp-from-name "Jinko Exporter" `
  --smtp-to-email "ops@example.com" `
  --smtp-starttls `
  --jinko-token-alert-window 24h `
  --alert-no-successful-poll-window 10m `
  --alert-grid-down-voltage-threshold 20 `
  --alert-battery-soc-low-threshold 15 `
  --alert-high-temperature-threshold 55
```

Equivalent env-based example:

```powershell
$env:EXPORTER_SOURCE="jinko"
$env:JINKO_DEVICE_ID="100000001"
$env:JINKO_SITE_ID="200000001"
$env:JINKO_BEARER_TOKEN="<JWT>"
$env:JINKO_REQUEST_JITTER_MAX="7s"
$env:ALERTS_ENABLED="true"
$env:ALERTS_COOLDOWN="6h"
$env:SMTP_HOST="smtp.example.com"
$env:SMTP_PORT="587"
$env:SMTP_USERNAME="<SMTP_USERNAME>"
$env:SMTP_PASSWORD="<SMTP_PASSWORD>"
$env:SMTP_FROM_EMAIL="alerts@example.com"
$env:SMTP_FROM_NAME="Jinko Exporter"
$env:SMTP_TO_EMAILS="ops@example.com"
$env:SMTP_STARTTLS="true"
$env:JINKO_TOKEN_ALERT_WINDOW="24h"
$env:ALERT_NO_SUCCESSFUL_POLL_WINDOW="10m"
$env:ALERT_GRID_DOWN_VOLTAGE_THRESHOLD="20"
$env:ALERT_BATTERY_SOC_LOW_THRESHOLD="15"
$env:ALERT_HIGH_TEMPERATURE_THRESHOLD="55"
.\jinko-exporter.exe serve
```

Token note:

- this exporter currently expects a bearer token copied from the browser session
- if the token expires, fetches will fail with `401` until you provide a fresh token
- with SMTP alerts enabled, the exporter can notify you before expiry and on `401` / `403`
- if bearer-only is not enough for your account, set `JINKO_COOKIE` too

## Solarman OpenAPI source

Required config:

- `--solarman-app-id` / `SOLARMAN_APP_ID`
- `--solarman-app-secret` / `SOLARMAN_APP_SECRET`
- `--solarman-email` / `SOLARMAN_EMAIL`
- `--solarman-password` or `--solarman-password-sha256`

Optional config:

- `--solarman-device-sn` to skip discovery
- `--solarman-station-id` to guide discovery
- `--solarman-base-url`
- `--solarman-api-version`

Example:

```powershell
.\jinko-exporter.exe serve `
  --source solarman `
  --listen :9876 `
  --poll-interval 60s `
  --solarman-app-id "<APP_ID>" `
  --solarman-app-secret "<APP_SECRET>" `
  --solarman-email "user@example.com" `
  --solarman-password "<PASSWORD>" `
  --solarman-device-sn "1234567890"
```

Env-based example:

```powershell
$env:EXPORTER_SOURCE="solarman"
$env:SOLARMAN_APP_ID="<APP_ID>"
$env:SOLARMAN_APP_SECRET="<APP_SECRET>"
$env:SOLARMAN_EMAIL="user@example.com"
$env:SOLARMAN_PASSWORD="<PASSWORD>"
$env:SOLARMAN_DEVICE_SN="1234567890"
$env:ALERTS_ENABLED="true"
$env:SMTP_HOST="smtp.example.com"
$env:SMTP_PORT="587"
$env:SMTP_USERNAME="<SMTP_USERNAME>"
$env:SMTP_PASSWORD="<SMTP_PASSWORD>"
$env:SMTP_FROM_EMAIL="alerts@example.com"
$env:SMTP_TO_EMAILS="ops@example.com"
$env:SMTP_STARTTLS="true"
$env:ALERT_NO_SUCCESSFUL_POLL_WINDOW="10m"
$env:ALERT_GRID_DOWN_VOLTAGE_THRESHOLD="20"
$env:ALERT_BATTERY_SOC_LOW_THRESHOLD="15"
$env:ALERT_HIGH_TEMPERATURE_THRESHOLD="55"
.\jinko-exporter.exe serve
```

## Modbus source

The module is intentionally a TODO placeholder until you have:

- the exact inverter/logger protocol docs
- a verified register map
- read-safe function/address combinations

Current config shape:

- `--modbus-host`
- `--modbus-port`
- `--modbus-logger-serial`
- `--modbus-unit-id`

Example:

```powershell
.\jinko-exporter.exe fetch `
  --source modbus `
  --modbus-host 192.168.120.10 `
  --modbus-port 8899 `
  --modbus-logger-serial "<LOGGER_SERIAL>" `
  --modbus-unit-id 1
```

The current result is an explicit `not implemented` error rather than a guessed protocol interaction.

## Useful endpoints

- Metrics: `http://localhost:9876/metrics`
- Health: `http://localhost:9876/healthz`
- Ready: `http://localhost:9876/readyz`

## Development

Run tests:

```powershell
go test ./...
```

The repository includes a Jinko detail response fixture at:

- `testdata/jinko_detail_response.json`
