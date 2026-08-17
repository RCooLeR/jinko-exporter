# Prometheus Metrics

The bridge exposes a small fixed set of metric families. Source telemetry is exported through one generic metric family with labels that describe the original field.

## Scrape Endpoint

Default endpoint:

```text
http://localhost:9876/metrics
```

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: jinko_bridge
    scrape_interval: 60s
    metrics_path: /metrics
    static_configs:
      - targets:
          - jinko_bridge:9876
```

If the bridge runs on the host instead of inside the Prometheus Compose network:

```yaml
static_configs:
  - targets:
      - host.docker.internal:9876
```

## Metric Prefix

Metric names use `EXPORTER_METRIC_PREFIX`, defaulting to `solar`.

For example:

```text
solar_up
solar_metric
```

Set a custom prefix:

```yaml
environment:
  EXPORTER_METRIC_PREFIX: "jinko"
```

## Exported Metric Families

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `<prefix>_build_info` | Gauge | `version`, `commit`, `date` | Always `1`; exporter build metadata. |
| `<prefix>_up` | Gauge | `source`, `device_sn` | `1` when the latest poll succeeded, `0` otherwise. |
| `<prefix>_poll_success` | Gauge | `source` | `1` when the latest source poll succeeded, `0` otherwise. |
| `<prefix>_polls_total` | Counter | `source`, `result` | Total polls by result. Result is `success` or `error`. |
| `<prefix>_last_update_timestamp_seconds` | Gauge | `source`, `device_sn` | Upstream data collection timestamp from the current snapshot. |
| `<prefix>_last_poll_success_timestamp_seconds` | Gauge | `source` | Unix timestamp when the exporter last completed a successful poll. |
| `<prefix>_last_source_sync_timestamp_seconds` | Gauge | `source` | Unix timestamp of latest successful poll by source. Keeps `source` even when source labels are otherwise dropped. |
| `<prefix>_poll_duration_seconds` | Gauge | `source` | Duration of the latest poll in seconds. |
| `<prefix>_request_errors_total` | Counter | `source` | Total failed polls since process start. |
| `<prefix>_metric` | Gauge | `source`, `device_sn`, `group`, `key`, `name`, `unit` | Numeric telemetry values from the current snapshot. |

## Example Output

```text
solar_build_info{version="dev",commit="unknown",date="unknown"} 1
solar_up{source="jinko",device_sn="SYNTHETIC_INV_001"} 1
solar_poll_success{source="jinko"} 1
solar_polls_total{source="jinko",result="success"} 1
solar_polls_total{source="jinko",result="error"} 0
solar_last_update_timestamp_seconds{source="jinko",device_sn="SYNTHETIC_INV_001"} 1778068800
solar_last_poll_success_timestamp_seconds{source="jinko"} 1778068801
solar_poll_duration_seconds{source="jinko"} 0.842
solar_request_errors_total{source="jinko"} 0
solar_metric{source="jinko",device_sn="SYNTHETIC_INV_001",group="grid",key="PG_Pt1",name="Total Grid Power",unit="W"} 1234
solar_metric{source="jinko",device_sn="SYNTHETIC_INV_001",group="battery",key="B_left_cap1",name="SoC",unit="%"} 71
```

## Dropping The Source Label

For a source-independent `modbus,jinko,solarman` deployment, set:

```yaml
environment:
  EXPORTER_METRICS_DROP_SOURCE_LABEL: "true"
```

This changes most metrics to remove `source`:

```text
solar_up{device_sn="SYNTHETIC_INV_001"} 1
solar_poll_success 1
solar_polls_total{result="success"} 1
solar_last_poll_success_timestamp_seconds 1778068801
solar_metric{device_sn="SYNTHETIC_INV_001",group="grid",key="PG_Pt1",name="Total Grid Power",unit="W"} 1234
```

`solar_last_source_sync_timestamp_seconds{source=...}` keeps `source` so failover visibility is not lost. `solar_poll_success` follows the source-label setting because it reports the active exporter view rather than a per-source sync timestamp.

When source labels are dropped, duplicate source-specific metric label sets are collapsed inside one collection pass. This is useful for failover dashboards, but it can hide source-specific differences if two sources report the same logical metric differently.

Priority failover can also project fallback metrics onto the primary source surface with `EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS=true`. When unset, that option defaults to `EXPORTER_METRICS_DROP_SOURCE_LABEL`; `SOLARMAN_CANONICAL_JINKO_METRICS` inherits the same default. Recognized Solarman points are always canonicalized. The legacy-named Solarman option instead filters unknown Solarman-only points when true. Thus the recommended `EXPORTER_METRICS_DROP_SOURCE_LABEL=true` setting enables projection, selects the strict shared-dictionary Solarman surface, and removes the active source from ordinary series unless either dependent option is explicitly overridden.

Jinko, recognized Solarman points, and Modbus use identical key/group/name/unit labels for `DP1`, `DP2`, `DV1`, `DC1`, `DV2`, `DC2`, `S_P_T`, `INV_O_P_L1`, `INV_O_P_L2`, `INV_O_P_L3`, `INV_O_P_T`, `B_T1`, `BMST`, `BMS_SOC`, and `AC`. After the primary surface has been learned, projection matches a fallback by canonical key and overwrites its ordinary metric labels with the learned primary labels, so these shared series do not churn during failover. Combined with dropping `source`, this prevents Grafana queries from rendering parallel source-specific lines for the same logical metric. Source-local warning/alarm/fault metrics bypass projection and keep their own keys and domains; the source-local register-551 power-switch key likewise has no cloud alias to collide with. `last_source_sync_timestamp_seconds{source=...}` remains available to identify source health even when ordinary source labels are dropped.

`device_sn` intentionally remains on telemetry. In a mixed priority chain, `MODBUS_DEVICE_SN` is therefore required and must be the same inverter serial returned by Jinko/Solarman. Once the primary surface has been learned, projection rejects a fallback with a different non-empty serial and tries the next source instead of mixing another inverter into the same logical series. `MQTT_DEVICE_ID` stabilizes Home Assistant topics only; it does not replace the Prometheus `device_sn` label.

## Query Examples

Current solar production:

```promql
solar_metric{group="electric",key="S_P_T"}
```

PV string power, voltage, and current:

```promql
solar_metric{group="electric",key=~"DP1|DP2|DV1|DV2|DC1|DC2"}
```

Total inverter output power:

```promql
solar_metric{group="electric",key="INV_O_P_T"}
```

Battery state of charge:

```promql
solar_metric{group=~"battery|bms",key=~"B_left_cap1|BMS_SOC"}
```

Grid import/export power:

```promql
solar_metric{group="grid",key="PG_Pt1"}
```

Any active alarm/fault flags:

```promql
solar_metric{group="alert"} != 0
```

Poll failure rate over 15 minutes:

```promql
increase(solar_request_errors_total[15m])
```

Poll success rate over 15 minutes:

```promql
sum by (result) (increase(solar_polls_total[15m]))
```

No successful bridge poll in more than 10 minutes:

```promql
time() - solar_last_poll_success_timestamp_seconds > 600
```

Upstream data has not refreshed in more than 1 hour:

```promql
time() - solar_last_update_timestamp_seconds > 3600
```

## Grafana Notes

Because source telemetry is exposed as one generic metric family, use labels for dashboard filtering:

- `group` for broad panels such as grid, battery, PV, generator.
- `key` for stable queries.
- `name` for user-facing legends.
- `unit` for display unit selection.

Prefer `key` over `name` in alert rules. Names are easier to read, but keys are usually more stable across languages and upstream UI changes.
