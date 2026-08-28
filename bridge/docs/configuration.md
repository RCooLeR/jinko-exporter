# Configuration

Every option can be passed as a CLI flag or as an environment variable. Docker examples use environment variables.

For legacy secret values, the direct value takes precedence over the matching `*_FILE` option. The dedicated `HOMEASSISTANT_TOKEN` and `HOMEASSISTANT_TOKEN_FILE` options are instead mutually exclusive. Bootstrap secret files are read at startup and trailing newlines are trimmed. `JINKO_TOKEN_STATE_FILE` is different: it is runtime-managed state that can contain rotated access and refresh tokens, so keep it private and writable by the bridge. `MQTT_DISCOVERY_STATE_FILE` is separate non-credential runtime state; it contains the typed Home Assistant entity schema and ownership binding used to regenerate this bridge's exact Discovery topics, and must be kept private in a state directory the bridge can write.

## Core Options

| Environment variable | CLI flag | Default | Description |
| --- | --- | --- | --- |
| `EXPORTER_SOURCE` | `--source` | `jinko` | Single source: `jinko`, `solarman`, or `modbus`. |
| `EXPORTER_SOURCE_PRIORITY` | `--source-priority` | empty | Comma-separated failover order. Overrides `EXPORTER_SOURCE` when set. `modbus,jinko,solarman` tries the locked local reader first, then Jinko, then Solarman. |
| `EXPORTER_LISTEN` | `--listen` | `:9876` | HTTP listen address in `host:port` or `:port` form. |
| `EXPORTER_METRICS_PATH` | `--metrics-path` | `/metrics` | Prometheus metrics path. Must start with `/`. |
| `EXPORTER_POLL_INTERVAL` | `--poll-interval` | `60s` | Poll interval. Must be greater than zero. |
| `EXPORTER_LOG_LEVEL` | `--log-level` | `info` | Zerolog level such as `debug`, `info`, `warn`, or `error`. |
| `EXPORTER_METRIC_PREFIX` | `--metric-prefix` | `solar` | Prefix for Prometheus metric names. Must match `[A-Za-z_][A-Za-z0-9_]*` and contain at least one non-underscore character. |
| `EXPORTER_METRICS_DROP_SOURCE_LABEL` | `--metrics-drop-source-label` | `false` | Drops the `source` label from most generic metric series. Set this to `true` for source-independent Prometheus series in a priority deployment; unless explicitly overridden, it also enables fallback projection and strict filtering of unknown Solarman-only points. Known Solarman points are always canonicalized. |
| `EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS` | `--source-project-failover-metrics` | value of `metrics-drop-source-label` | Projects ordinary fallback telemetry onto the primary source metric surface when priority failover is used. Source-local warning/alarm/fault metrics are always preserved and remain isolated by source domain. |

For a stable `modbus,jinko,solarman` telemetry surface, set `EXPORTER_METRICS_DROP_SOURCE_LABEL=true` and leave `EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS` and `SOLARMAN_CANONICAL_JINKO_METRICS` unset unless an explicit override is intended. Both inherit `true`: known Solarman points already use the shared Jinko labels, the legacy-named Solarman option filters unknown Solarman-only points, and priority projection matches fallback metrics by canonical key and overwrites their group, key, name, and unit with the learned primary labels. This keeps ordinary Prometheus series source-independent and prevents Grafana panels from receiving separate lines for the same logical metric after failover. Source-local warning/alarm/fault metrics bypass that relabelling and retain separate alert domains.

After a primary surface has been learned, a projected fallback must retain at least one compatible non-alert telemetry metric. A non-empty fallback device serial must also exactly match the learned primary serial; a mismatch is treated as a different inverter and priority continues to the next source. An alert-only or zero-intersection projection is likewise an incompatible source failure and cannot mark the exporter ready by itself.

## Jinko Options

| Environment variable | CLI flag | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `JINKO_URL` | `--jinko-url` | Jinko detail endpoint | No | Absolute HTTPS detail API URL without embedded user information. |
| `JINKO_TIMEOUT` | `--jinko-timeout` | `20s` | No | HTTP timeout. |
| `JINKO_INSECURE_SKIP_VERIFY` | `--jinko-insecure-skip-verify` | `false` | No | Skip TLS verification for Jinko HTTPS requests. |
| `JINKO_RETRY_ATTEMPTS` | `--jinko-retry-attempts` | `3` | No | Attempts for transient Jinko detail-endpoint errors. Rotating OAuth requests are never replayed. |
| `JINKO_RETRY_BACKOFF` | `--jinko-retry-backoff` | `2s` | No | Initial detail-endpoint retry delay; OAuth and token-state durability do not use this retry policy. |
| `JINKO_DEVICE_ID` | `--jinko-device-id` | empty | Yes | Jinko `deviceId` request field. |
| `JINKO_SITE_ID` | `--jinko-site-id` | empty | Yes | Jinko `siteId` request field. |
| `JINKO_LANGUAGE` | `--jinko-language` | `en` | No | Request language. |
| `JINKO_NEED_REALTIME_DATA` | `--jinko-need-realtime` | `true` | No | Jinko `needRealTimeDataFlag`. |
| `JINKO_BEARER_TOKEN` | `--jinko-bearer-token` | empty | One token source | Initial bearer token copied from the browser session. Legacy bearer-only operation remains supported. |
| `JINKO_BEARER_TOKEN_FILE` | `--jinko-bearer-token-file` | empty | One token source | File containing the initial bearer token. Read at startup only. |
| `JINKO_TOKEN_URL` | `--jinko-token-url` | `https://smart-global.jinkosolar.com/oauth2-s/oauth/token` | No | Absolute HTTPS Jinko OAuth token refresh endpoint. User information and plain HTTP are rejected. |
| `JINKO_REFRESH_TOKEN` | `--jinko-refresh-token` | empty | One token source | Bootstrap OAuth refresh token. Enables automatic bearer renewal and requires `JINKO_TOKEN_STATE_FILE`. |
| `JINKO_REFRESH_TOKEN_FILE` | `--jinko-refresh-token-file` | empty | One token source | File containing the bootstrap refresh token. Read at startup only; automatic refresh also requires `JINKO_TOKEN_STATE_FILE`. |
| `JINKO_TOKEN_STATE_FILE` | `--jinko-token-state-file` | empty | Required for refresh | Private writable file used to load and atomically persist rotated Jinko token state. It may bootstrap the client when it already contains an access or refresh token. An existing file must be recognized token-state JSON and must not be the bootstrap refresh-token file. |
| `JINKO_REFRESH_BEFORE` | `--jinko-refresh-before` | `5m` | No | Refresh an expiring bearer token this long before its expiry. `0` waits until expiry or an authentication failure. |
| `JINKO_SYSTEM` | `--jinko-system` | `JinKO` | No | OAuth refresh form `system` value. |
| `JINKO_AREA` | `--jinko-area` | `FOREIGN_1` | No | OAuth refresh form `area` value. |
| `JINKO_ORIGIN_ID` | `--jinko-origin-id` | empty | No | Optional OAuth refresh form `origin_id` value; the field is sent empty when unset, matching the web client. |
| `JINKO_COOKIE` | `--jinko-cookie` | empty | No | Optional cookie header when bearer-only is not enough. |
| `JINKO_COOKIE_FILE` | `--jinko-cookie-file` | empty | No | File containing the optional cookie header. Read at startup only. |
| `JINKO_USER_AGENT` | `--jinko-user-agent` | `jinko-exporter/1.0` | No | HTTP user agent. |
| `JINKO_REQUEST_JITTER_MAX` | `--jinko-request-jitter-max` | `5s` | No | Random delay before each request. |
| `JINKO_TOKEN_ALERT_WINDOW` | `--jinko-token-alert-window` | `24h` | No | Alert when a bearer token expires within this non-negative window. `0` alerts only at expiry. |

At least one of the bearer token, refresh token, or a populated token-state file is required. When the state file is the only token source, it must already be valid JSON containing a non-empty `access_token` or `refresh_token`. A bootstrap refresh token always requires a state-file path; the file may be absent on first start and is created before the bridge consumes the refresh token. A configured bearer token and refresh token can coexist: the bearer seeds the first request, then the bridge refreshes it before expiry and persists any rotated pair. Without refresh configuration, bearer-only behavior is unchanged.

During `serve`, Jinko token maintenance runs independently of priority polling on the same Jinko client. Thus a healthy primary Modbus source does not prevent an expiry-aware Jinko access/refresh pair from rotating and being persisted. The maintainer calls only the OAuth token endpoint; it never performs a speculative Jinko detail request, never changes the selected telemetry source, and a maintenance failure cannot invalidate an otherwise successful Modbus poll. Once a refresh-token request has started, shutdown lets that bounded transaction finish and persist the response; a transient final state-write failure is retried without consuming the refresh token again. `fetch` intentionally does not start the background loop.

The next background rotation can be scheduled only when the access JWT contains `exp` or the OAuth response supplies a valid `expires_in`. For an opaque access token with neither value, the maintainer performs one immediate bootstrap rotation per process and then pauses rather than guessing a cadence; a sanitized warning/alert explains that the next rotation will be triggered only by a real Jinko fallback `401`. Solarman access tokens remain on-demand because Solarman can recreate them from the configured account credentials and has no durable rotating refresh credential in this bridge.

Before an OAuth refresh-token POST, the state file is atomically marked with `refresh_outcome_uncertain`. A valid response pair clears that marker only when the new pair is durable. If the request may have reached Jinko but no valid response can be proven, the bridge never replays the possibly consumed refresh token and the persisted marker keeps automatic refresh paused across restarts. Recovery is manual and pair-based: stop the bridge, obtain a complete new access/refresh pair, replace or remove the old state file as appropriate, then restart. Never clear only the marker while reusing the old refresh token.

`JINKO_INSECURE_SKIP_VERIFY` applies only to the legacy detail request. OAuth token refresh always verifies the token endpoint TLS certificate because it transmits the long-lived refresh credential.

The token-state file contains credentials and transaction state. Store it outside the image, grant access only to the bridge user, and mount only its dedicated state directory writable. Exactly one bridge process may write a given state file. Do not point it at a read-only Docker secret or at the bootstrap refresh-token file. Runtime state normally wins when it contains later-expiring rotated JWTs. When intentionally installing a completely new manual credential pair, stop the bridge and remove the old state file first so the new bootstrap pair is unambiguous.

## Solarman Options

| Environment variable | CLI flag | Default | Required | Description |
| --- | --- | --- | --- | --- |
| `SOLARMAN_BASE_URL` | `--solarman-base-url` | `https://globalapi.solarmanpv.com` | No | Absolute HTTPS OpenAPI base URL without embedded user information. |
| `SOLARMAN_API_VERSION` | `--solarman-api-version` | `v1.0` | No | Single safe API-version path segment using letters, digits, `.`, `_`, or `-`; path traversal and URL delimiters are rejected. |
| `SOLARMAN_LANGUAGE` | `--solarman-language` | `en` | No | Request language. |
| `SOLARMAN_TIMEOUT` | `--solarman-timeout` | `20s` | No | Positive overall HTTP timeout. |
| `SOLARMAN_INSECURE_SKIP_VERIFY` | `--solarman-insecure-skip-verify` | `false` | No | Skip TLS verification for Solarman HTTPS requests. |
| `SOLARMAN_CANONICAL_JINKO_METRICS` | `--solarman-canonical-jinko-metrics` | value of `metrics-drop-source-label` | No | Limit Solarman output to the shared Jinko metric dictionary by filtering unknown Solarman-only points. Recognized points are always canonicalized regardless of this flag. |
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

Solarman token responses must advertise a positive `expires_in` no longer than 365 days. This evidence-neutral safety ceiling is not an API lifetime guarantee; it prevents malformed upstream values from producing an overflowed or effectively unbounded cached credential.

## Shelly Grid Load Options

Shelly grid-load collection is an optional enrichment layer for hybrid inverter setups where the inverter does not directly report non-backup/grid-side load. When enabled, the bridge still polls the selected inverter source, then appends `grid_load_*` metrics from a Shelly Pro 3EM Gen2 RPC device.

If the Shelly request fails, the inverter poll remains successful and the Shelly-backed Home Assistant entities are left missing or `null` until the next successful Shelly read.

| Environment variable | CLI flag | Default | Required when enabled | Description |
| --- | --- | --- | --- | --- |
| `SHELLY_GRID_LOAD_ENABLED` | `--shelly-grid-load-enabled` | `false` | No | Enable Shelly Pro 3EM grid-load metric enrichment. |
| `SHELLY_GRID_LOAD_URL` | `--shelly-grid-load-url` | empty | Yes | Shelly base URL, for example `http://192.0.2.50` (RFC 5737 TEST-NET placeholder). |
| `SHELLY_GRID_LOAD_EM_ID` | `--shelly-grid-load-em-id` | `0` | No | Shelly EM component id. |
| `SHELLY_GRID_LOAD_TIMEOUT` | `--shelly-grid-load-timeout` | `5s` | No | Shelly HTTP timeout. |

## MQTT Options

| Environment variable | CLI flag | Default | Required when MQTT enabled | Description |
| --- | --- | --- | --- | --- |
| `MQTT_ENABLED` | `--mqtt-enabled` | `false` | No | Enables MQTT state publishing and Home Assistant Discovery. |
| `MQTT_BROKER` | `--mqtt-broker` | `tcp://localhost:1883` | Yes | Absolute broker URL with an explicit port. Only `tcp://`, `tls://`, and `ssl://` are accepted; credentials, paths, queries, and fragments are rejected. |
| `MQTT_CLIENT_ID` | `--mqtt-client-id` | `jinko-exporter` | Yes | MQTT client ID. Use a unique value per bridge. |
| `MQTT_USERNAME` | `--mqtt-username` | empty | No | MQTT username. |
| `MQTT_PASSWORD` | `--mqtt-password` | empty | No | MQTT password. |
| `MQTT_PASSWORD_FILE` | `--mqtt-password-file` | empty | No | File containing the MQTT password. |
| `MQTT_TOPIC_PREFIX` | `--mqtt-topic-prefix` | `jinko-exporter` | Yes | Base topic for availability and state. MQTT wildcards, NUL, invalid UTF-8, and empty interior topic levels are rejected. |
| `MQTT_DISCOVERY_PREFIX` | `--mqtt-discovery-prefix` | `homeassistant` | Yes | Home Assistant discovery prefix with the same safe topic-prefix rules. |
| `MQTT_DEVICE_NAME` | `--mqtt-device-name` | generated | No | Home Assistant device name. |
| `MQTT_DEVICE_ID` | `--mqtt-device-id` | generated | With mixed priority | Stable Home Assistant device identifier. It is required when MQTT is enabled with multiple priority sources so a failover cannot strand retained state on a second device topic. |
| `MQTT_QOS` | `--mqtt-qos` | `0` | No | QoS `0`, `1`, or `2`. |
| `MQTT_RETAIN` | `--mqtt-retain` | `true` | No | Retain availability and state messages. Home Assistant Discovery configs are always retained so Home Assistant can restore the entities after its own restart. |
| `MQTT_TIMEOUT` | `--mqtt-timeout` | `10s` | No | Connect and publish timeout. |
| `MQTT_INSECURE_SKIP_VERIFY` | `--mqtt-insecure-skip-verify` | `false` | No | Skip TLS verification for MQTT TLS connections. |
| `MQTT_DISCOVERY_STATE_FILE` | `--mqtt-discovery-state-file` | empty | No | Optional private manifest for the retained Discovery metric schema and ownership binding, stored in a writable state directory. An explicit `MQTT_DEVICE_ID` is required when it is set. It is strongly recommended for a mixed priority deployment. |

Keep MQTT credentials in `MQTT_USERNAME` and `MQTT_PASSWORD`/`MQTT_PASSWORD_FILE`. Do not embed them in `MQTT_BROKER`; credential-bearing broker URLs are rejected so secrets cannot enter broker logs.

When a discovery-state file is configured, the first item in `EXPORTER_SOURCE_PRIORITY` defines the ordinary Home Assistant metric surface; in single-source mode, `EXPORTER_SOURCE` does. The surface grows monotonically when that configured primary succeeds and is persisted across restarts. A cold Jinko or Solarman fallback may still supply current state and source-local alerts, but it cannot add fallback-only ordinary entities before the primary surface is known. Source-local alert entities and optional Shelly `grid_load` enrichment are maintained as separate monotonic unions, so they remain available without changing the primary inverter schema. Every dynamic metric template uses a nested missing-safe lookup; a key absent from the current source becomes `unknown` rather than producing a Home Assistant template warning, while a real numeric zero remains zero.

Store the manifest in a dedicated file such as `/var/lib/jinko-bridge/mqtt-discovery-state.json`, using the same private writable state directory as the token-state file but never the same filename. Its parent directory must already exist, and exactly one bridge process may write a given manifest. An existing path must be a regular file, not a symlink, and it must not alias a configured secret or token-state file. A missing manifest is created before the MQTT connection starts. An existing manifest is opened and validated at startup but is not rewritten merely to probe writability; its directory must remain writable because a later successful poll that expands the schema commits an atomic replacement before publishing that schema. The bridge validates the manifest version, full publisher binding, primary source, normalized metadata keys, and typed metric definitions. Corrupt, oversized, ownership-mismatched, or unpersistable updated state fails closed: the bridge does not silently discard it, invent a replacement schema, or delete retained broker topics.

## Alerts, SMTP, And Modbus Alert Correlation Options

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
| `ALERT_BATTERY_SOC_LOW_THRESHOLD` | `--alert-battery-soc-low-threshold` | `0` | Low battery SOC threshold in percent. Values must be below `100`; `0` disables it. |
| `ALERT_HIGH_TEMPERATURE_THRESHOLD` | `--alert-high-temperature-threshold` | `0` | High temperature threshold in C. `0` disables it. |
| `MODBUS_ALERT_CORRELATION_ENABLED` | `--modbus-alert-correlation-enabled` | `false` | Explicitly enable a bounded Jinko/Solarman correlation job and dedicated Home Assistant push when a raw Modbus warning/fault word is non-zero. Requires `modbus` first and both cloud sources in `EXPORTER_SOURCE_PRIORITY`. |
| `MODBUS_ALERT_CORRELATION_COOLDOWN` | `--modbus-alert-correlation-cooldown` | `6h` | Minimum interval between repeated jobs for an unchanged non-zero Modbus alert signature. |
| `MODBUS_ALERT_CORRELATION_JOB_TIMEOUT` | `--modbus-alert-correlation-job-timeout` | `45s` | Fresh absolute deadline for the concurrent Jinko/Solarman evidence phase. It starts after the separately bounded HA notification phase. |
| `HOMEASSISTANT_URL` | `--homeassistant-url` | empty | Home Assistant base URL used only for this dedicated push. HTTPS is required unless the explicit insecure-HTTP guard below is enabled. Paths, credentials, query strings, and fragments are rejected. |
| `HOMEASSISTANT_TOKEN` | `--homeassistant-token` | empty | Dedicated Home Assistant bearer token. Mutually exclusive with `HOMEASSISTANT_TOKEN_FILE`. |
| `HOMEASSISTANT_TOKEN_FILE` | `--homeassistant-token-file` | empty | Private regular non-symlink file containing the dedicated Home Assistant token. Mutually exclusive with `HOMEASSISTANT_TOKEN`. |
| `JINKO_HA_NOTIFY_SERVICE` | `--jinko-ha-notify-service` | empty | One mobile-app notify service, written as `mobile_app_*` or `notify.mobile_app_*`. Other HA service domains and path characters are rejected. |
| `HOMEASSISTANT_TIMEOUT` | `--homeassistant-timeout` | `10s` | Timeout for each detected/recovered Home Assistant notification phase. A notification timeout does not consume the later cloud-evidence budget. |
| `HOMEASSISTANT_ALLOW_INSECURE_HTTP` | `--homeassistant-allow-insecure-http` | `false` | Explicitly permit HTTP only to a private/loopback literal IP or a valid single-label internal hostname. Every DNS answer for a single-label host must be private/loopback and the client dials an approved IP directly. |

## Modbus Options

The Modbus source is the deliberately locked read-only profile `jks-6-20h-ei-readonly-v1` for the JKS-6/8/10/12/15/20H-EI three-phase HV line. The 12 kW target is the only member whose profile fingerprint and telemetry have been live-verified. It uses Solarman V5 over TCP and can encode only twenty-four compile-time FC03 blocks: two cached target-profile gates and twenty-two reads performed on every fetch. The first fetch performs all twenty-four; later fetches reuse the validated target profile and perform twenty-two. It has no raw-register option and no Modbus write-function path. It may be used alone or first in `EXPORTER_SOURCE_PRIORITY=modbus,jinko,solarman`; all listed sources must have valid configuration.

| Environment variable | CLI flag | Default | Description |
| --- | --- | --- | --- |
| `MODBUS_HOST` | `--modbus-host` | empty | Private literal IPv4 address of the Solarman V5 logger. Public addresses, hostnames, DNS, and IPv6 are rejected. |
| `MODBUS_PORT` | `--modbus-port` | `8899` | Modbus TCP/logger port. |
| `MODBUS_LOGGER_SERIAL` | `--modbus-logger-serial` | empty | Required non-zero decimal logger serial embedded in every Solarman V5 frame. |
| `MODBUS_DEVICE_SN` | `--modbus-device-sn` | empty | Inverter serial used as snapshot identity. It is optional for Modbus-only operation, but required with multiple priority sources and must match the cloud inverter serial so Prometheus `device_sn` remains stable. It is never sent to the device. |
| `MODBUS_UNIT_ID` | `--modbus-unit-id` | `1` | Must remain `1` for this locked profile. |
| `MODBUS_TIMEOUT` | `--modbus-timeout` | `5s` | One absolute deadline for the complete fetch. Must be greater than zero and at most `30s`. |

When `modbus` is enabled, `EXPORTER_POLL_INTERVAL` must be at least `60s`. The first fetch validates both fixed profile gates: device type at decimal register `0` (`0x0000 x 1`) and rated power/MPPT/phases at decimal registers `20-22` (`0x0014 x 3`). The capability gate accepts only the official JKS-6/8/10/12/15/20H-EI line ratings `6000`, `8000`, `10000`, `12000`, `15000`, or `20000 W`, with device type `0x0006`, two MPPTs, three phases, and exact raw register 22 value `0x0203`. Only the 12 kW target has live-confirmed raw gate values; the other listed ratings are admitted from the official line specification but have not been individually live-verified. The 25 kW two-MPPT sibling is deliberately outside this declared JKS-6-20 profile and remains unvalidated; the structurally separate 29.9/30 kW BM3 three-MPPT variants are also rejected.

The source then reads only twenty-two approved every-fetch blocks, in this fixed order: generator port mode `133 x 1` (`0x0085`, sequence `0x16`); generator energy/runtime `536 x 4` (`0x0218-0x021B`, sequence `0x17`); generator voltage/power `661 x 11` (`0x0295-0x029F`, sequence `0x18`); UPS power `643 x 1`; load voltage block `640 x 7`; direct-load power low words `650 x 4` (`0x028A-0x028D`, sequence `0x12`); load frequency/high-word block `655 x 5` (`0x028F-0x0293`, sequence `0x13`); grid voltages `598 x 3`; grid-power low words `622 x 4`; grid-power high words `687 x 4`; output scalars and active-power low words `627 x 12` (`0x0273-0x027E`, sequence `0x0E`); output-power high words `691 x 5` (`0x02B3-0x02B7`, sequence `0x0F`); PV inputs `672 x 8`; DC/AC temperatures `540 x 2`; battery temperature `586 x 1`; battery voltage/SOC `587 x 2`; battery power/current `590 x 2`; energy counters `514 x 16`; power/AC-relay status `551 x 2` (`0x0227-0x0228`, sequence `0x19`); warning/fault raw words `553 x 6` (`0x0229-0x022E`, sequence `0x14`); run-state block `500 x 6` (`0x01F4-0x01F9`, sequence `0x04`); and grid frequency/current block `609 x 11` (`0x0261-0x026B`, sequence `0x0B`).

The generator promotion is intentionally restricted to the exact live-verified zero domain. The supervised one-shot returned `R133=0` and every word in `536-539` and `661-671` equal to zero on one connection with three sequential FC03 requests and no retry. Production therefore re-reads all three blocks on every fetch, requires every one of those sixteen words to remain zero, and emits eleven canonical zero-valued metrics: `GEN_P_L1..3`, `GEN_V_L1..3`, `R_T_D`, `EG_P_CT1`, `GEN_P_T`, `GEN_P_D`, and `GEN_P_TO`. `EG_P_CT1` is only a zero-domain compatibility view of the same observed total as `GEN_P_T`; no non-zero equivalence, scale, sign, or word-pairing inference is made. Any nonzero generator word rejects the entire Modbus snapshot so the priority chain can use a cloud source instead. Register 133 is mutable configuration and is never cached.

From `640 x 7`, the voltage decoder emits only registers 644-646 and ignores registers 640-643. Register 643 is nevertheless read by the separate preceding `643 x 1` request and emitted only as unsigned-U16 `UPS_P`; full phase/total UPS pairing with candidate high words 696-699 remains unimplemented. The `650 x 4` low-word read is followed immediately by the `655 x 5` frequency/high-word read. Low-first pairs 650/656, 651/657, and 652/658 emit canonical signed `LPP_A/B/C`. Their high words must be canonical sign extension `0x0000` or `0xFFFF`, and every joined phase must remain inside the conservative `-32767..32767 W` coherence envelope. The symmetric boundary rejects stale sign extension when the adjacent reads straddle zero; it is not an inverter or pass-through rating. Dedicated pair 653/659 remains an independent non-negative `E_Puse_t1`: register 659 must be zero and register 653 accepts the full numerically U16 `0..65535 W` subset of the documented signed wire pair, preserving pass-through headroom. A live same-frame sample disproved exact phase-sum equality, so the three phases and total are decoded independently and their sum is never a gate. Any failed phase or total gate rejects the whole snapshot. Phase aliases `C_P_L1..3` remain excluded. Register 655 independently emits `L_F`.

Grid power is decoded as a true signed 32-bit low-word-first value. High words must be `0x0000` or `0xFFFF`, but the low-word bit 15 is not treated as the sign of the full value. Because registers 622-625 and 687-690 require two consecutive reads, every joined phase and total must remain inside the conservative `-32767..32767 W` coherence envelope and the three phases must sum exactly to the total. The symmetric boundary rejects a low word paired with stale sign extension when the reads straddle zero; it is not an inverter-rating or pass-through cap. Any failed gate rejects the whole Modbus snapshot so configured priority fallback can run.

Output active power uses a narrower conservative signed contract. The `627 x 12` read is followed immediately by `691 x 5`, before any other device request. Registers 633-636 pair low-word-first with active high words 691-694 as signed 32-bit values and emit canonical `INV_O_P_L1`, `INV_O_P_L2`, `INV_O_P_L3`, and `INV_O_P_T` in watts. Every active high word must be the canonical sign extension `0x0000` or `0xFFFF`, every joined value must remain inside `-32767..32767 W`, and the exact signed sum of the three phases must equal the dedicated total. The symmetric boundary is an encoding-coherence guard: a low word combined with a stale high word across a zero crossing decodes outside it. It is not derived from `Pr1` or a pass-through rating. Any failed gate rejects the entire Modbus snapshot and allows the configured priority fallback. Production has observed `R691=0xFFFF`, confirming that the negative sign-extension domain occurs, but the log did not retain a complete raw pair and therefore does not establish an exact live negative magnitude. Apparent-power candidate registers 637/695 are ignored, and canonical `O_P` is not emitted.

The existing `672 x 8` response emits canonical PV1/PV2 power `DP1`/`DP2` from registers 672/673 at `10 W/count`, voltage `DV1`/`DV2` from 676/678 at `0.1 V/count`, current `DC1`/`DC2` from 677/679 at `0.1 A/count`, and aggregate `S_P_T` as `(672 + 673) × 10 W`. The two-MPPT profile still requires the PV3/PV4 power words 674-675 to be zero. Each channel and aggregate power are bounded to twice rated power, voltage to `1000 V`, and current to `100 A`. These six per-channel metrics reuse the already-approved request and grew the surface from 52 to 58 metrics; `LPP_A/B/C` and the canonical `BMS_SOC` alias then brought it to 62, the eleven zero-domain generator metrics brought it to 73, and the four staged output-power metrics brought it to 77. Register 586 now exposes the same validated temperature through both canonical `B_T1` and `BMST`. The `551 x 2` block emits source-local `DEYE_MODBUS_R551_POWER_SWITCH_STATE` from the documented register-551 low nibble and canonical `AC` from the complete raw register-552 U16 relay mask, bringing the current surface to 80 metrics. Register-551 codes above 1 reject the snapshot; its undefined upper bits are ignored. No bit of register 552 is filtered or rejected. The `540 x 2` block emits only the bracket-validated `T_DC` and `AC_T` temperatures. Register 588 emits its one validated SOC value as both `B_left_cap1` and `BMS_SOC`. Registers 553-558 are emitted as six independent Modbus-local raw U16 metrics in group `alert`, with no bit decoding, combined mask, or Jinko lithium alias. From `500 x 6`, only register 500 is emitted as source-local `DEYE_MODBUS_R500_RUN_STATE` in group `status`; documented codes are 0 standby, 1 self-check, 2 normal, 3 alarm, 4 fault, and 5 activating. Codes above 5 reject the snapshot. Registers 501-505 are ignored, and neither `ST_PG1` nor `INV_MOD1` is synthesized. From the final `609 x 11` block, register 609 emits canonical `PG_F1` as `U16 / 100 Hz`, and internal-current registers 610-612 emit `G_C_L1`, `G_C_L2`, and `G_C_L3` as `S16 / 100 A`; frequency above 100 Hz or absolute current above 100 A rejects the snapshot. Registers 613-619 remain ignored, including incomplete external-CT power low words.

Each fetch opens one fresh short-lived TCP connection and sends the required fixed requests sequentially on it: twenty-four on the first fetch and twenty-two on each later fetch after the two profile gates are cached. All requests share the fetch's one absolute deadline, are sent exactly once in the fixed allowlisted order, and receive strict V5 and Modbus CRC validation. A failed block aborts the fetch, closes the connection, and rejects the whole snapshot without retry, reconnect, or a partial result.

Priority selection remains strictly sequential on every poll: a valid Modbus snapshot prevents both cloud detail calls; a Modbus error tries Jinko; only a Jinko error then tries Solarman. Context cancellation stops the chain. Jinko token maintenance is a separate serve-only credential task and is not a telemetry-source attempt. Warning/fault recovery state is source-scoped, so a clear cloud snapshot cannot resolve a previously active Deye/Modbus raw warning word.
