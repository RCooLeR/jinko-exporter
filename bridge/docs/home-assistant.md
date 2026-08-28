# Home Assistant MQTT Discovery

The bridge can create a read-only Home Assistant device through MQTT Discovery. Discovery configs are always retained so Home Assistant can restore the entities after its own restart. The bridge publishes a JSON state payload after each successful poll; `MQTT_RETAIN` controls whether that state and the availability value are retained.

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

Use an explicit, stable `MQTT_DEVICE_ID` for priority failover and whenever `MQTT_DISCOVERY_STATE_FILE` is configured. The manifest is bound to that identity; changing it is a device migration, not a safe way to hide old retained entities.

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

With the default `MQTT_RETAIN=true`, the retained state payload looks like this:

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

```jinja2
{{ value_json.get('metrics', {}).get('<state_key>') }}
```

The nested lookup is deliberately missing-safe. A metric absent from the current source becomes `unknown` without a Home Assistant template warning. A present numeric `0` remains a real zero.

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
sensor becomes `unavailable` rather than incorrectly reporting `OFF`. Aggregate
problem sensors are source-scoped as well: the Modbus aggregate, for example,
becomes `unavailable` while Jinko or Solarman supplies the snapshot and can return
to `OFF` only after a complete Modbus snapshot reports its own alert words as
clear. The bridge publishes a retained empty discovery payload for the older
cross-source `alarm_or_fault_active` entity so Home Assistant removes that
unsafe aggregate. Inspect the `Data Source` diagnostic sensor alongside alert
entities.

### Metric Sensors

Without persistent Discovery state, every numeric source metric observed by the current process can become a sensor. With `MQTT_DISCOVERY_STATE_FILE`, ordinary inverter sensors are instead learned only from the configured primary source and retained as a monotonic schema across restarts. Source-local alert sensors and optional Shelly `grid_load` sensors use their own monotonic unions.

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

The core local Modbus snapshot now contains 80 metrics before optional enrichment, including canonical `consumption/LPP_A..C`, `bms/BMS_SOC`, `bms/BMST`, raw relay mask `status/AC`, eleven `generator` state keys, and the four output-power keys above. Direct-load phase gauges preserve signed `LPP_A..C` values inside the conservative `-32767..32767 W` coherence domain; dedicated home-load total `E_Puse_t1` remains independently non-negative and is not derived from the phase sum. The source-local `DEYE_MODBUS_R551_POWER_SWITCH_STATE` is a separate diagnostic entity and does not impersonate a cloud status key. The generator entities are backed by three live-verified all-zero register blocks: any nonzero generator word rejects Modbus and allows priority fallback instead of publishing an unverified decode. Output active power is likewise fail-closed outside its conservative signed domain: a high word other than `0x0000`/`0xFFFF`, a joined value outside `-32767..32767 W`, or an exact signed phase/total mismatch rejects Modbus rather than publishing a misleading entity. In a mixed priority deployment, keep one explicit `MQTT_DEVICE_ID`; with fallback projection enabled, these entities and the PV entities above retain the primary surface's key, group, name, and unit across source changes. The legacy-named `SOLARMAN_CANONICAL_JINKO_METRICS=true` setting additionally filters unknown Solarman-only points; it is not required to canonicalize recognized points. Previously discovered ordinary metrics that are absent from a fallback are published as `null` rather than retaining a stale value. Alert entities remain source-scoped as described above and are never projected into another source's warning/fault domain. With persistent Discovery state and `modbus,jinko,solarman`, Modbus defines the ordinary Home Assistant surface even when the process starts during a Modbus outage: the cold Jinko fallback supplies state and Jinko alerts but cannot add cloud-only ordinary entities.

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

Alert entities use two Home Assistant availability conditions in `all` mode:
the global bridge availability topic and the JSON state topic. A source
aggregate is available only when the latest successful snapshot has that exact
`alert_domain` and contains at least one finite alert value. A numeric alert and
its matching binary sensor are available only when the manifest records that
state key for the current alert domain and the key is present in
`alert_metrics`. Consequently, an explicit finite zero is a real `0`/`OFF`, a
non-zero value is `ON`, and an inactive source, omitted value, or non-finite
value is `unavailable` rather than `unknown` or a fabricated `OFF`. A global
poll failure still makes every entity unavailable.

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

The optional direct mobile push is independent of MQTT Discovery. When
`MODBUS_ALERT_CORRELATION_ENABLED=true`, the first complete non-zero Modbus
vector sends one detected push and starts bounded Jinko/Solarman evidence
collection. The cloud completion is logged without creating a second push; a
later complete all-zero Modbus vector sends the recovery push. Both pushes use
one privacy-safe per-device tag, so recovery replaces the active notification
without exposing the device serial. See [Alerts](./alerts.md) for configuration
and safety boundaries.

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
- After the first successful poll in the current process, a broker reconnect republishes the complete owned Discovery schema and latest state payload, then republishes the last known availability value. Before that first success, broker-retained Discovery configs are left in place and the bridge publishes `offline`; if the latest poll failed, reconnect likewise keeps availability `offline` until a later successful poll.

If a source temporarily fails, Home Assistant marks the device entities unavailable until a later successful poll publishes `online` again. Source-scoped alert entities additionally require a matching, known current alert domain on the retained state topic. Switching from Modbus to Jinko therefore makes the Modbus alert entities unavailable and makes only the explicitly reported Jinko alert entities available; switching back reverses that relationship.

## Persistent Discovery Schema

For stable retained Discovery across bridge restarts, configure a private manifest in a writable state directory:

```yaml
volumes:
  - ./state:/var/lib/jinko-bridge
environment:
  MQTT_DEVICE_ID: "synthetic_inverter_001"
  MQTT_DISCOVERY_STATE_FILE: "/var/lib/jinko-bridge/mqtt-discovery-state.json"
```

This option is optional for backward compatibility and strongly recommended for mixed priority. The first item in `EXPORTER_SOURCE_PRIORITY` is the primary schema source; in single-source mode, `EXPORTER_SOURCE` is primary. The behavior is deliberately asymmetric:

- Ordinary inverter metrics are added only by successful primary snapshots and never removed merely because a later primary response omits one.
- Dynamic metadata diagnostics follow the same primary-only monotonic rule.
- A cold fallback remains usable for live state and availability, but fallback-only ordinary metrics do not expand the schema.
- Warning/alarm/fault entities form a separate source-scoped union so an important fallback alert is not hidden. The typed per-source ownership in the manifest is also used to regenerate entity-scoped availability templates after restart; alert keys shared by several sources are accepted only with identical metric semantics.
- Shelly `grid_load` entities form a separate enrichment union and become `unknown` when an optional value or complete Shelly read is unavailable.
- Previously owned ordinary metrics absent from the current snapshot are included as `null`, and every Discovery template is missing-safe. Missing ordinary telemetry remains `unknown`; missing or inactive source-scoped alerts are `unavailable`.

The manifest stores a strict ownership binding plus typed ordinary, Shelly, alert, and primary-metadata schemas. The current binary regenerates the exact topics and payloads from that typed state, so template and security fixes also apply to entities learned by an older binary. A missing manifest is created before MQTT connects. An existing manifest is read and validated at startup without being rewritten solely to test writability; keep its parent directory writable by container UID `65532` because a later schema change is committed there as an atomic replacement before the new schema is published. New and atomically replaced manifest files are written with private permissions. A manifest must be used by only one bridge process and must be separate from all secrets and `JINKO_TOKEN_STATE_FILE`.

Manifest validation fails closed. An invalid version, malformed or oversized file, non-regular path, changed device ownership, incompatible discovery prefix, or different configured primary source stops startup rather than silently replacing the manifest or issuing retained deletions. Correct the configuration or perform an explicit migration; do not edit ownership fields merely to bypass the check.

### Migrating Legacy Retained Topics

The first manifest created after an upgrade cannot prove ownership of retained topics published by an older process. It therefore does not discover or delete arbitrary broker topics automatically. Migrate an existing installation in two controlled phases:

1. **Make retained configs safe.** Export the candidate retained configs and verify each payload belongs to this bridge by its exact Discovery topic, `unique_id`, state topic, availability topic, and device identifier. In a reviewed one-time broker migration, republish only that verified set with the missing-safe metric template. Keep the old entities temporarily so dashboards and automations can be checked without a destructive schema change.
2. **Remove only confirmed stale entities.** After the configured primary schema has been observed and reviewed, publish retained empty payloads only to the exact verified cloud-only ordinary topics that are no longer desired. Preserve source-specific alert topics and Shelly enrichment topics, then keep the committed manifest as the ownership record for future reconciliation.

Never clear `homeassistant/#`, never delete by a partial object-ID prefix, and never change `MQTT_DEVICE_ID` as a cleanup shortcut. If an old topic was not verified or recorded as owned, leave it untouched and investigate it separately.

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
- Old entities remain from a pre-manifest release: follow the two-phase verified migration above. Do not clear the whole Home Assistant Discovery prefix.
- Startup reports a discovery-manifest mismatch or corrupt file: stop and reconcile the configured device ID, prefixes, primary source, and state file. The bridge intentionally refuses to overwrite or clean up from untrusted state.
