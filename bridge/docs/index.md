# Bridge Documentation

The bridge is the Go service in this repository. It reads solar telemetry from the first successful source in a configured ordered chain and exposes that data through HTTP, Prometheus, and optional Home Assistant MQTT Discovery.

## What The Bridge Does

- Polls sources in strict configured priority order on a fixed interval and stops at the first complete success.
- Normalizes numeric telemetry into a common snapshot shape.
- Serves Prometheus metrics from `/metrics`.
- Serves `/healthz` and `/readyz`.
- Optionally publishes always-retained MQTT Discovery configs and Home Assistant state JSON whose retention is controlled by `MQTT_RETAIN`.
- Optionally sends SMTP alerts for request failures, inverter alarms, grid-down checks, low battery SOC, high temperature, and missing successful polls.
- Optionally turns a non-zero raw Modbus warning/fault vector into an immediate Home Assistant mobile push and a bounded, evidence-only Jinko/Solarman correlation job.

## Documentation

- [Installation](./installation.md)
- [Configuration](./configuration.md)
- [Collected data](./collected-data.md)
- [Local Modbus validation ledger](./modbus-validation.md)
- [Local Modbus validation backlog](./modbus-backlog.md)
- [Prometheus metrics](./prometheus.md)
- [Home Assistant MQTT Discovery](./home-assistant.md)
- [Alerts](./alerts.md)
- [Development and releases](./development.md)

## Supported Sources

| Source | Status | Notes |
| --- | --- | --- |
| `jinko` | Supported | Uses the browser-backed Jinko detail endpoint. Requires device ID, site ID, and a bearer token, refresh token, or persisted token state from an authenticated browser session. |
| `solarman` | Supported | Uses Solarman OpenAPI. Requires OpenAPI credentials and account credentials. |
| `modbus` | Supported, narrow profile | Local Solarman V5/TCP polling with `jks-6-20h-ei-readonly-v1`; only the 12 kW target is live-verified. Only allowlisted FC03 reads are implemented. |

## Runtime Endpoints

| Endpoint | Purpose |
| --- | --- |
| `/metrics` | Prometheus scrape endpoint. Configurable with `EXPORTER_METRICS_PATH`. |
| `/healthz` | Process health. Returns `200 OK` when the HTTP server is running. |
| `/readyz` | Readiness. Returns `200 OK` after a recent successful poll, and `503` before the first poll or after sustained poll failures. |

## Data Flow

```text
Jinko detail API / Solarman OpenAPI / local Solarman V5 logger
        |
        v
bridge poller
        |
        +--> Prometheus metrics
        |
        +--> MQTT Discovery + state JSON
        |
        +--> optional SMTP alerts
        |
        +--> optional HA push + cloud alert correlation
```

## Important Notes

- The bridge is read-only. It does not write inverter settings or control relays.
- The local Modbus profile may run alone or first in a strict failover chain. Each fetch uses one fresh short-lived TCP connection and sequentially transmits only fixed FC03 reads under one shared deadline: twenty-four requests on the first fetch and twenty-two after the profile gates are cached. The three generator reads remain an exact all-zero gate on every fetch; the paired output-power and power-switch decoders likewise fail closed outside their accepted domains. The profile contains no Modbus write function, arbitrary-register interface, logger HTTP configuration, discovery, retry, or reconnect within the same fetch.
- The [Modbus validation backlog](./modbus-backlog.md) lists hardware-dependent and condition-dependent gaps such as nonzero generator, Smart Load/AUX, AC-coupled solar, external CT, full UPS power, extended BMS data, additional MPPTs, and unverified model variants. Missing values are not synthesized as zeros or aliases.
- In `serve`, expiry-aware Jinko OAuth rotation is maintained independently of whichever telemetry source wins; it does not perform Jinko detail calls while Modbus is healthy. Opaque tokens without expiry metadata use the documented conservative pause-and-401 policy.
- Jinko bearer tokens expire. Configure a refresh token and private writable token-state file for automatic renewal; bearer-only installations still require a fresh token after expiry.
- Solarman API quotas are external to this project. Use request pacing if your API plan has a strict yearly request limit.
- Home Assistant entity names and entity IDs are produced by Home Assistant from the MQTT Discovery payload. Exact entity IDs can change if Home Assistant has existing conflicting entities.
