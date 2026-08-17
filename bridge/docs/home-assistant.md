# Home Assistant MQTT Discovery

The bridge can create a read-only Home Assistant device through MQTT Discovery. It publishes retained discovery configs and a retained JSON state payload after each successful poll.

## Requirements

- Home Assistant MQTT integration enabled.
- An MQTT broker reachable by the bridge.
- `MQTT_ENABLED=true` in the bridge.

Minimum bridge MQTT configuration:

```yaml
environment:
  MQTT_ENABLED: "true"
  MQTT_BROKER: "tcp://homeassistant.local:1883"
  MQTT_USERNAME: "<MQTT_USER>"
  MQTT_PASSWORD: "<MQTT_PASSWORD>"
```

## Topics

With defaults:

| Topic | Payload |
| --- | --- |
| `jinko-exporter/availability` | `online` or `offline` |
| `jinko-exporter/<device_id>/state` | JSON state payload |
| `homeassistant/sensor/<object_id>/config` | MQTT Discovery config for sensors |
| `homeassistant/binary_sensor/<object_id>/config` | MQTT Discovery config for binary sensors |

The state topic uses the sanitized device ID:

```text
<MQTT_TOPIC_PREFIX>/<sanitized_device_id>/state
```

The device ID is selected from the first non-empty value:

```text
MQTT_DEVICE_ID
snapshot.device_sn
snapshot.device_id
snapshot.parent_sn
snapshot.site_id
snapshot.source
jinko_exporter
```

## Device Naming

Default Home Assistant device name:

```text
Jinko Inverter <device_sn>
```

Override it:

```yaml
environment:
  MQTT_DEVICE_NAME: "Garage Inverter"
  MQTT_DEVICE_ID: "garage_inverter"
```

The Discovery device payload uses:

| Field | Value |
| --- | --- |
| `identifiers` | `jinko_exporter_<sanitized_device_id>` |
| `name` | `MQTT_DEVICE_NAME` or generated name |
| `manufacturer` | `Jinko` |
| `model` | `Solar inverter via jinko-exporter` |
| `serial_number` | Snapshot device serial number |

## State Payload

The retained state payload looks like this:

```json
{
  "source": "jinko",
  "device_sn": "SYNTHETIC_INV_001",
  "parent_sn": "LOGGER123",
  "device_id": "100000001",
  "site_id": "200000001",
  "collected_at": "2026-05-06T12:00:00Z",
  "published_at": "2026-05-06T12:00:01Z",
  "up": true,
  "metrics": {
    "grid_pg_pt1": 1234,
    "battery_b_left_cap1": 71
  },
  "metric_count": 142,
  "alert_metrics": {
    "alert_l_b_a_f": 0,
    "alert_l_b_f_f": 0
  },
  "alert_count": 0,
  "alerts_active": false,
  "poll_duration_seconds": 0.842,
  "meta": {
    "base_url": "https://globalapi.solarmanpv.com"
  }
}
```

Each metric sensor reads from:

```text
value_json.metrics.<state_key>
```

Each alert binary sensor reads from:

```text
value_json.alert_metrics.<state_key>
```

## Created Entities

The bridge creates one Home Assistant device with these entity groups.

### Diagnostic Sensors

| Discovery name | State field | Category |
| --- | --- | --- |
| Data Source | `source` | diagnostic |
| Device Serial | `device_sn` | diagnostic |
| Parent Serial | `parent_sn` | diagnostic |
| Device ID | `device_id` | diagnostic |
| Site ID | `site_id` | diagnostic |
| Collected At | `collected_at` | diagnostic timestamp |
| Published At | `published_at` | diagnostic timestamp |
| Poll Duration | `poll_duration_seconds` | diagnostic duration |
| Metric Count | `metric_count` | diagnostic |
| Current Source Active Warning/Alarm/Fault Count | `alert_count` (number of non-zero alert metrics/raw words in the current source, not set bits; unknown when that source exposes no alert metrics) | diagnostic |

Metadata fields in `snapshot.meta` also become diagnostic sensors named:

```text
Meta <metadata_key>
```

### Diagnostic Binary Sensors

| Discovery name | Meaning | Device class |
| --- | --- | --- |
| Poll Up | Latest successful poll status | `connectivity` |
| Warning/Alarm/Fault Active (`<source>`) | At least one alert, alarm, warning-word, or fault metric from that source is non-zero | `problem` |

Alert binary sensors are missing-safe during source failover. When the current
snapshot does not contain a previously discovered alert metric, its binary
sensor becomes unknown rather than incorrectly reporting `OFF`. Aggregate
problem sensors are source-scoped as well: the Modbus aggregate, for example,
becomes unknown while Jinko or Solarman supplies the snapshot and can return
to `OFF` only after a complete Modbus snapshot reports its own alert words as
clear. The bridge publishes a retained empty discovery payload for the older
cross-source `alarm_or_fault_active` entity so Home Assistant removes that
unsafe aggregate. Inspect the `Data Source` diagnostic sensor alongside alert
entities.

### Metric Sensors

Every numeric source metric becomes a sensor.

Discovery name:

```text
<metric.name>
```

State key:

```text
<sanitize(metric.group + "_" + metric.key)>
```

Unique ID:

```text
<sanitized_device_id>_<state_key>
```

Example:

| Metric | State key | Likely entity ID |
| --- | --- | --- |
| `group=grid`, `key=PG_Pt1`, `name=Total Grid Power` | `grid_pg_pt1` | `sensor.jinko_inverter_synthetic_inv_001_total_grid_power` |
| `group=battery`, `key=B_left_cap1`, `name=SoC` | `battery_b_left_cap1` | `sensor.jinko_inverter_synthetic_inv_001_soc` |
| `group=electric`, `key=S_P_T`, `name=Total Solar Power` | `electric_s_p_t` | `sensor.jinko_inverter_synthetic_inv_001_total_solar_power` |
| `group=grid_load`, `key=total_power`, `name=Grid Load Total Power` | `grid_load_total_power` | `sensor.jinko_inverter_synthetic_inv_001_grid_load_total_power` |

Home Assistant owns the final entity ID. If an entity with the same slug already exists, Home Assistant may append a suffix such as `_2`.

The two-MPPT PV entities use the same discovery names and state keys for local Modbus, Jinko, and recognized Solarman points, which are always canonicalized through the shared dictionary:

| Discovery name | Canonical key | State key | Unit |
| --- | --- | --- | --- |
| DC Power PV1 | `DP1` | `electric_dp1` | `W` |
| DC Power PV2 | `DP2` | `electric_dp2` | `W` |
| DC Voltage PV1 | `DV1` | `electric_dv1` | `V` |
| DC Current PV1 | `DC1` | `electric_dc1` | `A` |
| DC Voltage PV2 | `DV2` | `electric_dv2` | `V` |
| DC Current PV2 | `DC2` | `electric_dc2` | `A` |
| Total Solar Power | `S_P_T` | `electric_s_p_t` | `W` |

The staged output-power entities use the same canonical discovery surface across Modbus and cloud fallback:

| Discovery name | Canonical key | State key | Unit |
| --- | --- | --- | --- |
| Inverter Output Power L1 | `INV_O_P_L1` | `electric_inv_o_p_l1` | `W` |
| Inverter Output Power L2 | `INV_O_P_L2` | `electric_inv_o_p_l2` | `W` |
| Inverter Output Power L3 | `INV_O_P_L3` | `electric_inv_o_p_l3` | `W` |
| Total Inverter Output Power | `INV_O_P_T` | `electric_inv_o_p_t` | `W` |

The core local Modbus snapshot now contains 80 metrics before optional enrichment, including canonical `consumption/LPP_A..C`, `bms/BMS_SOC`, `bms/BMST`, raw relay mask `status/AC`, eleven `generator` state keys, and the four output-power keys above. The source-local `DEYE_MODBUS_R551_POWER_SWITCH_STATE` is a separate diagnostic entity and does not impersonate a cloud status key. The generator entities are backed by three live-verified all-zero register blocks: any nonzero generator word rejects Modbus and allows priority fallback instead of publishing an unverified decode. Output active power is likewise fail-closed outside its conservative signed domain: a high word other than `0x0000`/`0xFFFF`, a joined value outside `-32767..32767 W`, or an exact signed phase/total mismatch rejects Modbus rather than publishing a misleading entity. In a mixed priority deployment, keep one explicit `MQTT_DEVICE_ID`; with fallback projection enabled, these entities and the PV entities above retain the primary surface's key, group, name, and unit across source changes. The legacy-named `SOLARMAN_CANONICAL_JINKO_METRICS=true` setting additionally filters unknown Solarman-only points; it is not required to canonicalize recognized points. Previously discovered ordinary metrics that are absent from a fallback are published as `null` rather than retaining a stale value. Alert entities remain source-scoped as described above and are never projected into another source's warning/fault domain.

### Shelly Grid Load Sensors

When `SHELLY_GRID_LOAD_ENABLED=true`, the bridge appends Shelly Pro 3EM metrics under the `grid_load` group. These entities are useful for hybrid inverter installations where the inverter only measures backup/UPS load and does not expose grid-side load.

Primary card-facing sensors:

| Discovery name | State key | Unit |
| --- | --- | --- |
| Grid Load Total Power | `grid_load_total_power` | `W` |
| Grid Load Total Current | `grid_load_total_current` | `A` |
| Grid Load L1 Voltage | `grid_load_l1_voltage` | `V` |
| Grid Load L2 Voltage | `grid_load_l2_voltage` | `V` |
| Grid Load L3 Voltage | `grid_load_l3_voltage` | `V` |
| Grid Load L1 Current | `grid_load_l1_current` | `A` |
| Grid Load L2 Current | `grid_load_l2_current` | `A` |
| Grid Load L3 Current | `grid_load_l3_current` | `A` |
| Grid Load L1 Power | `grid_load_l1_power` | `W` |
| Grid Load L2 Power | `grid_load_l2_power` | `W` |
| Grid Load L3 Power | `grid_load_l3_power` | `W` |

Shelly `EMData` totals are exposed as kWh energy sensors after converting the Shelly Wh counters.

### Warning/Alarm/Fault Binary Sensors

If a metric is a warning, alarm, or fault metric, the bridge creates an additional binary sensor.

The metric is treated as an alert metric when:

- the group is `alert`, or
- the group, key, or name contains `alarm`, or
- the group, key, or name contains `fault`.

Binary sensor discovery name:

```text
<metric.name> Active
```

State key:

```text
<metric_state_key>_active
```

Example:

| Metric | Binary sensor name | Device class |
| --- | --- | --- |
| Lithium battery alarm flag | Lithium battery alarm flag Active | `problem` |
| Lithium battery fault flag | Lithium battery fault flag Active | `problem` |

The Modbus source additionally creates one unitless numeric
diagnostic sensor and one diagnostic `problem` binary sensor for each raw
warning/fault word. A binary sensor is `ON` exactly when its corresponding raw
U16 word is non-zero. While Modbus is the current source, `alert_count` counts
non-zero words rather than individual set bits, and its Modbus aggregate is
true when any of the six words is non-zero.

| Register | Numeric state key | Binary sensor state key |
| --- | --- | --- |
| `553` | `alert_deye_modbus_r553_warning_word_1_raw` | `alert_deye_modbus_r553_warning_word_1_raw_active` |
| `554` | `alert_deye_modbus_r554_warning_word_2_raw` | `alert_deye_modbus_r554_warning_word_2_raw_active` |
| `555` | `alert_deye_modbus_r555_fault_word_1_raw` | `alert_deye_modbus_r555_fault_word_1_raw_active` |
| `556` | `alert_deye_modbus_r556_fault_word_2_raw` | `alert_deye_modbus_r556_fault_word_2_raw_active` |
| `557` | `alert_deye_modbus_r557_fault_word_3_raw` | `alert_deye_modbus_r557_fault_word_3_raw_active` |
| `558` | `alert_deye_modbus_r558_fault_word_4_raw` | `alert_deye_modbus_r558_fault_word_4_raw_active` |

These are source-specific inverter words. They are deliberately separate from
the Jinko lithium-battery alert entities and have no decoded bit names or
automatic control action.

## Device Classes And Units

The bridge assigns Home Assistant metadata from units and metric text.

| Unit or text | Device class | State class |
| --- | --- | --- |
| `W`, `kW` | `power` | `measurement` |
| `Wh`, `kWh` | `energy` | `total_increasing` |
| `V` | `voltage` | `measurement` |
| `A` | `current` | `measurement` |
| `Hz` | `frequency` | `measurement` |
| `C` | `temperature` | `measurement` |
| `VA` | `apparent_power` | `measurement` |
| `var` | `reactive_power` | `measurement` |
| `%` with SOC, SOH, battery, or capacity text | `battery` | `measurement` |
| `h` | `duration` | `measurement` |
| text containing `power factor` | `power_factor` | `measurement` |

Groups `basic`, `version`, `status`, `state`, and `alert` are marked as diagnostic.

## Availability

Availability topic:

```text
<MQTT_TOPIC_PREFIX>/availability
```

Behavior:

- On successful poll, the bridge publishes `online`.
- On poll failure, the bridge publishes `offline`.
- On clean shutdown, the bridge publishes `offline`.
- The MQTT will message also uses `offline`.
- On broker reconnect, the bridge republishes the latest retained discovery messages and state payload, then republishes the last known availability value. If the last poll failed, reconnect keeps availability `offline` until a later successful poll.

If a source temporarily fails, Home Assistant marks the device entities unavailable until a later successful poll publishes `online` again.

## Discovery Refresh

Discovery messages are republished when the shape changes:

- device identity changes
- state topic changes
- metric key/name/unit/group set changes
- metadata keys change

Messages are retained by default so Home Assistant can restore entities after restart.

## Troubleshooting

Check retained availability:

```shell
mosquitto_sub -h homeassistant.local -t 'jinko-exporter/availability' -v
```

Check state payload:

```shell
mosquitto_sub -h homeassistant.local -t 'jinko-exporter/+/state' -v
```

Check discovery configs:

```shell
mosquitto_sub -h homeassistant.local -t 'homeassistant/+/+/config' -v
```

Common issues:

- No device appears: confirm Home Assistant MQTT integration is enabled and discovery prefix is `homeassistant`.
- Entities unavailable: inspect the availability topic and bridge logs for poll failures.
- Entity IDs differ from examples: Home Assistant generated a unique slug from the discovery name and existing entity registry state.
- Old entities remain after renaming a device: remove stale MQTT entities from Home Assistant or clear retained discovery topics manually.
