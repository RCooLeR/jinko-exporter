# Bridge Documentation

The bridge is the Go service in this repository. It reads solar telemetry from one configured source and exposes that data through HTTP, Prometheus, and optional Home Assistant MQTT Discovery.

## What The Bridge Does

- Polls one source on a fixed interval.
- Normalizes numeric telemetry into a common snapshot shape.
- Serves Prometheus metrics from `/metrics`.
- Serves `/healthz` and `/readyz`.
- Optionally publishes retained MQTT Discovery configs and retained state JSON for Home Assistant.
- Optionally sends SMTP alerts for request failures, inverter alarms, grid-down checks, low battery SOC, high temperature, and missing successful polls.

## Documentation

- [Installation](./installation.md)
- [Configuration](./configuration.md)
- [Collected data](./collected-data.md)
- [Prometheus metrics](./prometheus.md)
- [Home Assistant MQTT Discovery](./home-assistant.md)
- [Alerts](./alerts.md)
- [Development and releases](./development.md)

## Supported Sources

| Source | Status | Notes |
| --- | --- | --- |
| `jinko` | Supported | Uses the browser-backed Jinko detail endpoint. Requires device ID, site ID, and a bearer token copied from an authenticated browser session. |
| `solarman` | Supported | Uses Solarman OpenAPI. Requires OpenAPI credentials and account credentials. |
| `modbus` | Placeholder | Configuration is present, but the source returns a not-implemented error until the protocol and register map are confirmed. |

## Runtime Endpoints

| Endpoint | Purpose |
| --- | --- |
| `/metrics` | Prometheus scrape endpoint. Configurable with `EXPORTER_METRICS_PATH`. |
| `/healthz` | Process health. Returns `200 OK` when the HTTP server is running. |
| `/readyz` | Readiness. Returns `200 OK` after a recent successful poll, and `503` before the first poll or after sustained poll failures. |

## Data Flow

```text
Jinko detail API / Solarman OpenAPI
        |
        v
bridge poller
        |
        +--> Prometheus metrics
        |
        +--> MQTT Discovery + state JSON
        |
        +--> optional SMTP alerts
```

## Important Notes

- The bridge is read-only. It does not write inverter settings or control relays.
- Jinko bearer tokens expire. When they expire, Jinko polling fails until a fresh token is configured.
- Solarman API quotas are external to this project. Use request pacing if your API plan has a strict yearly request limit.
- Home Assistant entity names and entity IDs are produced by Home Assistant from the MQTT Discovery payload. Exact entity IDs can change if Home Assistant has existing conflicting entities.
