# Configuration

Every option can be passed as a CLI flag or as an environment variable. Docker examples use environment variables.

For secret values, the direct value takes precedence over the matching `*_FILE` option. File-based secrets are read once at startup and trailing newlines are trimmed.

## Core Options

| Environment variable | CLI flag | Default | Description |
| --- | --- | --- | --- |
| `EXPORTER_SOURCE` | `--source` | `jinko` | Single source: `jinko`, `solarman`, or `modbus`. |
| `EXPORTER_SOURCE_PRIORITY` | `--source-priority` | empty | Comma-separated failover order. Overrides `EXPORTER_SOURCE` when set. |
| `EXPORTER_LISTEN` | `--listen` | `:9876` | HTTP listen address in `host:port` or `:port` form. |
| `EXPORTER_METRICS_PATH` | `--metrics-path` | `/metrics` | Prometheus metrics path. Must start with `/`. |
| `EXPORTER_POLL_INTERVAL` | `--poll-interval` | `60s` | Poll interval. Must be greater than zero. |
| `EXPORTER_LOG_LEVEL` | `--log-level` | `info` | Zerolog level such as `debug`, `info`, `warn`, or `error`. |
| `EXPORTER_METRIC_PREFIX` | `--metric-prefix` | `solar` | Prefix for Prometheus metric names. Must not be empty after trimming underscores. |
| `EXPORTER_METRICS_DROP_SOURCE_LABEL` | `--metrics-drop-source-label` | `false` | Drops the `source` label from most generic metric series. |
| `EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS` | `--source-project-failover-metrics` | value of `metrics-drop-source-label` | Projects fallback source metrics onto the primary source metric surface when priority failover is used. |

## Jinko Options

| Environment variable | CLI flag | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `JINKO_URL` | `--jinko-url` | Jinko detail endpoint | No | Detail API URL. |
| `JINKO_TIMEOUT` | `--jinko-timeout` | `20s` | No | HTTP timeout. |
| `JINKO_INSECURE_SKIP_VERIFY` | `--jinko-insecure-skip-verify` | `false` | No | Skip TLS verification for Jinko HTTPS requests. |
| `JINKO_RETRY_ATTEMPTS` | `--jinko-retry-attempts` | `3` | No | Attempts for transient transport errors. |
| `JINKO_RETRY_BACKOFF` | `--jinko-retry-backoff` | `2s` | No | Initial retry delay. |
| `JINKO_DEVICE_ID` | `--jinko-device-id` | empty | Yes | Jinko `deviceId` request field. |
| `JINKO_SITE_ID` | `--jinko-site-id` | empty | Yes | Jinko `siteId` request field. |
| `JINKO_LANGUAGE` | `--jinko-language` | `en` | No | Request language. |
| `JINKO_NEED_REALTIME_DATA` | `--jinko-need-realtime` | `true` | No | Jinko `needRealTimeDataFlag`. |
| `JINKO_BEARER_TOKEN` | `--jinko-bearer-token` | empty | Yes | Bearer token copied from the browser session. |
| `JINKO_BEARER_TOKEN_FILE` | `--jinko-bearer-token-file` | empty | Yes if token is not set | File containing the bearer token. Read at startup only. |
| `JINKO_COOKIE` | `--jinko-cookie` | empty | No | Optional cookie header when bearer-only is not enough. |
| `JINKO_COOKIE_FILE` | `--jinko-cookie-file` | empty | No | File containing the optional cookie header. Read at startup only. |
| `JINKO_USER_AGENT` | `--jinko-user-agent` | `jinko-exporter/1.0` | No | HTTP user agent. |
| `JINKO_REQUEST_JITTER_MAX` | `--jinko-request-jitter-max` | `5s` | No | Random delay before each request. |
| `JINKO_TOKEN_ALERT_WINDOW` | `--jinko-token-alert-window` | `24h` | No | Alert when a bearer token expires within this window. |

## Solarman Options

| Environment variable | CLI flag | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `SOLARMAN_BASE_URL` | `--solarman-base-url` | `https://globalapi.solarmanpv.com` | No | OpenAPI base URL. |
| `SOLARMAN_API_VERSION` | `--solarman-api-version` | `v1.0` | No | API version path segment. |
| `SOLARMAN_LANGUAGE` | `--solarman-language` | `en` | No | Request language. |
| `SOLARMAN_TIMEOUT` | `--solarman-timeout` | `20s` | No | HTTP timeout. |
| `SOLARMAN_INSECURE_SKIP_VERIFY` | `--solarman-insecure-skip-verify` | `false` | No | Skip TLS verification for Solarman HTTPS requests. |
| `SOLARMAN_CANONICAL_JINKO_METRICS` | `--solarman-canonical-jinko-metrics` | value of `metrics-drop-source-label` | No | Canonicalize Solarman telemetry through the Jinko metric dictionary for stable names and groups. |
| `SOLARMAN_YEARLY_REQUEST_LIMIT` | `--solarman-yearly-request-limit` | `0` | No | Request pacing budget. `0` disables pacing. |
| `SOLARMAN_DISCOVERY_REFRESH_INTERVAL` | `--solarman-discovery-refresh-interval` | `24h` | No | Device discovery cache refresh interval. `0` caches forever. |
| `SOLARMAN_APP_ID` | `--solarman-app-id` | empty | Yes | OpenAPI app ID. |
| `SOLARMAN_APP_SECRET` | `--solarman-app-secret` | empty | Yes | OpenAPI app secret. |
| `SOLARMAN_APP_SECRET_FILE` | `--solarman-app-secret-file` | empty | Yes if app secret is not set | File containing the OpenAPI app secret. |
| `SOLARMAN_EMAIL` | `--solarman-email` | empty | Yes | Solarman account email. |
| `SOLARMAN_PASSWORD` | `--solarman-password` | empty | One password option | Plain account password. |
| `SOLARMAN_PASSWORD_FILE` | `--solarman-password-file` | empty | One password option | File containing the plain account password. |
| `SOLARMAN_PASSWORD_SHA256` | `--solarman-password-sha256` | empty | One password option | Precomputed SHA256 password hex. |
| `SOLARMAN_PASSWORD_SHA256_FILE` | `--solarman-password-sha256-file` | empty | One password option | File containing the precomputed SHA256 password hex. |
| `SOLARMAN_DEVICE_SN` | `--solarman-device-sn` | empty | No | Device serial. Skips discovery when set. |
| `SOLARMAN_STATION_ID` | `--solarman-station-id` | empty | No | Station ID used during discovery. |

## Shelly Grid Load Options

Shelly grid-load collection is an optional enrichment layer for hybrid inverter setups where the inverter does not directly report non-backup/grid-side load. When enabled, the bridge still polls the selected inverter source, then appends `grid_load_*` metrics from a Shelly Pro 3EM Gen2 RPC device.

If the Shelly request fails, the inverter poll remains successful and the Shelly-backed Home Assistant entities are left missing or `null` until the next successful Shelly read.

| Environment variable | CLI flag | Default | Required when enabled | Description |
| --- | --- | --- | --- | --- |
| `SHELLY_GRID_LOAD_ENABLED` | `--shelly-grid-load-enabled` | `false` | No | Enable Shelly Pro 3EM grid-load metric enrichment. |
| `SHELLY_GRID_LOAD_URL` | `--shelly-grid-load-url` | empty | Yes | Shelly base URL, for example `http://192.168.120.50`. |
| `SHELLY_GRID_LOAD_EM_ID` | `--shelly-grid-load-em-id` | `0` | No | Shelly EM component id. |
| `SHELLY_GRID_LOAD_TIMEOUT` | `--shelly-grid-load-timeout` | `5s` | No | Shelly HTTP timeout. |

## MQTT Options

| Environment variable | CLI flag | Default | Required when MQTT enabled | Description |
| --- | --- | --- | --- | --- |
| `MQTT_ENABLED` | `--mqtt-enabled` | `false` | No | Enables MQTT state publishing and Home Assistant Discovery. |
| `MQTT_BROKER` | `--mqtt-broker` | `tcp://localhost:1883` | Yes | Broker URL. Supports `tcp://`, `tls://`, and `ssl://`. |
| `MQTT_CLIENT_ID` | `--mqtt-client-id` | `jinko-exporter` | Yes | MQTT client ID. Use a unique value per bridge. |
| `MQTT_USERNAME` | `--mqtt-username` | empty | No | MQTT username. |
| `MQTT_PASSWORD` | `--mqtt-password` | empty | No | MQTT password. |
| `MQTT_PASSWORD_FILE` | `--mqtt-password-file` | empty | No | File containing the MQTT password. |
| `MQTT_TOPIC_PREFIX` | `--mqtt-topic-prefix` | `jinko-exporter` | Yes | Base topic for availability and state. |
| `MQTT_DISCOVERY_PREFIX` | `--mqtt-discovery-prefix` | `homeassistant` | Yes | Home Assistant discovery prefix. |
| `MQTT_DEVICE_NAME` | `--mqtt-device-name` | generated | No | Home Assistant device name. |
| `MQTT_DEVICE_ID` | `--mqtt-device-id` | generated | No | Stable Home Assistant device identifier. |
| `MQTT_QOS` | `--mqtt-qos` | `0` | No | QoS `0`, `1`, or `2`. |
| `MQTT_RETAIN` | `--mqtt-retain` | `true` | No | Retain discovery, availability, and state messages. |
| `MQTT_TIMEOUT` | `--mqtt-timeout` | `10s` | No | Connect and publish timeout. |
| `MQTT_INSECURE_SKIP_VERIFY` | `--mqtt-insecure-skip-verify` | `false` | No | Skip TLS verification for MQTT TLS connections. |

## Alerts And SMTP Options

See [Alerts](./alerts.md) for behavior details.

| Environment variable | CLI flag | Default | Description |
| --- | --- | --- | --- |
| `ALERTS_ENABLED` | `--alerts-enabled` | `false` | Enable outbound alerts. |
| `ALERTS_NOTIFY_RECOVERY` | `--alerts-notify-recovery` | `false` | Send recovery notifications when alert conditions clear. |
| `ALERTS_COOLDOWN` | `--alerts-cooldown` | `6h` | Minimum time between repeated alerts with the same key. |
| `SMTP_TIMEOUT` | `--smtp-timeout` | `15s` | SMTP dial/send timeout. |
| `SMTP_HOST` | `--smtp-host` | empty | SMTP server hostname. |
| `SMTP_PORT` | `--smtp-port` | `587` | SMTP server port. |
| `SMTP_USERNAME` | `--smtp-username` | empty | SMTP username. |
| `SMTP_PASSWORD` | `--smtp-password` | empty | SMTP password. |
| `SMTP_PASSWORD_FILE` | `--smtp-password-file` | empty | File containing the SMTP password. |
| `SMTP_FROM_EMAIL` | `--smtp-from-email` | empty | Sender email. |
| `SMTP_FROM_NAME` | `--smtp-from-name` | empty | Sender display name. |
| `SMTP_TO_EMAILS` | `--smtp-to-email` | empty | Recipient list. Comma-separated or repeated flag. |
| `SMTP_USE_TLS` | `--smtp-use-tls` | `false` | Use implicit TLS. |
| `SMTP_STARTTLS` | `--smtp-starttls` | `true` | Use STARTTLS when supported. |
| `ALERT_NO_SUCCESSFUL_POLL_WINDOW` | `--alert-no-successful-poll-window` | `0` | Alert after no successful poll for this duration. `0` disables it. |
| `ALERT_GRID_DOWN_VOLTAGE_THRESHOLD` | `--alert-grid-down-voltage-threshold` | `20` | Grid-down voltage threshold. |
| `ALERT_BATTERY_SOC_LOW_THRESHOLD` | `--alert-battery-soc-low-threshold` | `0` | Low battery SOC threshold. `0` disables it. |
| `ALERT_HIGH_TEMPERATURE_THRESHOLD` | `--alert-high-temperature-threshold` | `0` | High temperature threshold in C. `0` disables it. |

## Modbus Options

The Modbus source is not implemented yet. These options define the planned configuration shape.

| Environment variable | CLI flag | Default | Description |
| --- | --- | --- | --- |
| `MODBUS_HOST` | `--modbus-host` | empty | Modbus logger or inverter host. |
| `MODBUS_PORT` | `--modbus-port` | `8899` | Modbus TCP/logger port. |
| `MODBUS_LOGGER_SERIAL` | `--modbus-logger-serial` | empty | Logger serial for Solarman V5-over-TCP devices. |
| `MODBUS_UNIT_ID` | `--modbus-unit-id` | `1` | Modbus unit/slave ID. |
| `MODBUS_TIMEOUT` | `--modbus-timeout` | `5s` | Modbus timeout. |
