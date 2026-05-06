# Collected Data

The bridge normalizes source responses into one snapshot shape. Only numeric telemetry becomes Prometheus metrics and Home Assistant metric sensors. Non-numeric metadata is kept as snapshot metadata and, when MQTT is enabled, as diagnostic entities.

## Snapshot Shape

The `fetch` command prints the normalized snapshot:

```json
{
  "source": "jinko",
  "device_sn": "SYNTHETIC_INV_001",
  "parent_sn": "LOGGER123",
  "device_id": "100000001",
  "site_id": "200000001",
  "collected_at": "2026-05-06T12:00:00Z",
  "metrics": [
    {
      "group": "grid",
      "key": "PG_Pt1",
      "name": "Total Grid Power",
      "unit": "W",
      "value": 1234
    }
  ],
  "meta": {
    "base_url": "https://globalapi.solarmanpv.com"
  }
}
```

## Metric Fields

| Field | Meaning |
| --- | --- |
| `group` | Logical metric group such as `grid`, `battery`, `electric`, or `alert`. |
| `key` | Stable source key when available. For Jinko, this is usually the Jinko storage name and may be canonicalized. |
| `name` | Human-readable metric name. Home Assistant uses this as the default sensor name. |
| `unit` | Source unit, normalized for common Home Assistant units. |
| `value` | Numeric value as `float64`. Non-numeric source fields are skipped. |

## Source Behavior

### Jinko

The Jinko parser reads:

- `deviceSn`
- `parDeviceSn`
- `deviceId`
- `siteId`
- `collectionTime`
- numeric values from `paramCategoryList[].fieldList[]`

For each field, the bridge prefers `orgValue`, falls back to `value`, parses the result as a number, and skips the field when parsing fails.

Jinko metric keys are normalized this way:

- `storageName` is preferred as the metric `key`.
- If `storageName` is missing, the displayed field name is sanitized.
- Known Jinko keys and names are canonicalized into stable groups, names, and units.

### Solarman

The Solarman parser reads `dataList` from `/device/v1.0/currentData` and accepts common point fields:

- key: `key`, `dataKey`, `id`, or `sn`
- name: `name`, `dataName`, `title`, or `paramName`
- unit: `unit` or `dataUnit`
- value: `value` or `val`

Solarman groups are inferred from the key and name. When `EXPORTER_METRICS_DROP_SOURCE_LABEL=true`, Solarman metrics are also canonicalized through the Jinko metric dictionary so failover can keep more stable Prometheus and Home Assistant names.

## Groups

| Group | Typical data |
| --- | --- |
| `basic` | Inverter type, rated power, basic electrical limits. |
| `version` | Firmware and protocol version numbers. |
| `electric` | PV strings, AC output, inverter output, production totals. |
| `grid` | Grid voltage, current, power, frequency, import/export energy. |
| `consumption` | Load voltage, load power, total consumption, daily consumption. |
| `battery` | Battery voltage, current, power, SOC, charge/discharge energy. |
| `bms` / `bms2` | BMS voltage, current, temperature, current limits, SOH, capacity. |
| `temperature` | Battery, DC, and AC temperatures. |
| `status` / `state` | Numeric status values. |
| `alert` | Alarm and fault flags. |
| `generator` | Generator voltage, power, runtime, and production. |
| `ups` | UPS load power. |
| `inverter` | Solarman fallback group for generic inverter points. |

## Common Canonical Metrics

The exact set depends on the inverter, source, firmware, and upstream API response. These are the keys most commonly used by the bridge docs and cards.

| Key | Group | Name | Unit |
| --- | --- | --- | --- |
| `DV1` | `electric` | DC Voltage PV1 | `V` |
| `DC1` | `electric` | DC Current PV1 | `A` |
| `DP1` | `electric` | DC Power PV1 | `W` |
| `DV2` | `electric` | DC Voltage PV2 | `V` |
| `DC2` | `electric` | DC Current PV2 | `A` |
| `DP2` | `electric` | DC Power PV2 | `W` |
| `S_P_T` | `electric` | Total Solar Power | `W` |
| `Etdy_ge1` | `electric` | Daily Production (Active) | `kWh` |
| `Et_ge0` | `electric` | Cumulative Production (Active) | `kWh` |
| `INV_O_P_T` | `electric` | Total Inverter Output Power | `W` |
| `G_V_L1` | `grid` | Grid Voltage L1 | `V` |
| `G_C_L1` | `grid` | Grid Current L1 | `A` |
| `G_P_L1` | `grid` | Grid Power L1 | `W` |
| `G_V_L2` | `grid` | Grid Voltage L2 | `V` |
| `G_C_L2` | `grid` | Grid Current L2 | `A` |
| `G_P_L2` | `grid` | Grid Power L2 | `W` |
| `G_V_L3` | `grid` | Grid Voltage L3 | `V` |
| `G_C_L3` | `grid` | Grid Current L3 | `A` |
| `G_P_L3` | `grid` | Grid Power L3 | `W` |
| `PG_F1` | `grid` | Grid Frequency | `Hz` |
| `PG_Pt1` | `grid` | Total Grid Power | `W` |
| `E_B_D` | `grid` | Daily Energy Buy | `kWh` |
| `E_S_D` | `grid` | Daily energy sell | `kWh` |
| `E_B_TO` | `grid` | Total Energy Buy | `kWh` |
| `E_S_TO` | `grid` | Total Energy Sell | `kWh` |
| `E_Puse_t1` | `consumption` | Total Consumption Power | `W` |
| `Etdy_use1` | `consumption` | Daily Consumption | `kWh` |
| `E_C_T` | `consumption` | Total Consumption | `kWh` |
| `C_P_L1` | `consumption` | Load Power L1 | `W` |
| `C_P_L2` | `consumption` | Load Power L2 | `W` |
| `C_P_L3` | `consumption` | Load Power L3 | `W` |
| `B_V1` | `battery` | Battery Voltage | `V` |
| `B_C1` | `battery` | Battery Current | `A` |
| `B_P1` | `battery` | Battery Power | `W` |
| `B_left_cap1` | `battery` | SoC | `%` |
| `Etdy_cg1` | `battery` | Daily Charging Energy | `kWh` |
| `Etdy_dcg1` | `battery` | Daily Discharging Energy | `kWh` |
| `BMS_SOC` | `bms` | BMS_SOC | `%` |
| `BMST` | `bms` | BMS Temperature | `C` |
| `Li_B_SOH` | `bms` | Lithium battery SOH | `%` |
| `T_DC` | `temperature` | DC Temperature | `C` |
| `AC_T` | `temperature` | AC Temperature | `C` |
| `L_B_A_F` | `alert` | Lithium battery alarm flag | none |
| `L_B_F_F` | `alert` | Lithium battery fault flag | none |
| `GEN_P_T` | `generator` | Total Gen Power | `W` |
| `GEN_P_D` | `generator` | Daily Production Generator | `kWh` |
| `R_T_D` | `generator` | Gen Daily Run Time | `h` |
| `UPS_P` | `ups` | UPS Load Power | `W` |

## Sanitization

Metric keys and Home Assistant state keys are sanitized to lowercase ASCII identifiers for MQTT JSON keys:

- lowercase
- non-ASCII and punctuation become `_`
- repeated separators collapse
- leading and trailing `_` are removed

Example:

```text
group = "grid"
key = "PG_Pt1"
state key = "grid_pg_pt1"
```

Home Assistant then creates entity IDs from the discovery name and device context. See [Home Assistant MQTT Discovery](./home-assistant.md) for the full naming pattern.
