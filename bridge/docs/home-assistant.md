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
| Active Alarm Or Fault Count | `alert_count` | diagnostic |

Metadata fields in `snapshot.meta` also become diagnostic sensors named:

```text
Meta <metadata_key>
```

### Diagnostic Binary Sensors

| Discovery name | Meaning | Device class |
| --- | --- | --- |
| Poll Up | Latest successful poll status | `connectivity` |
| Alarm Or Fault Active | At least one alarm/fault metric is non-zero | `problem` |

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
| `group=battery`, `key=B_left_cap1`, `name=SoC` | `battery_b_left_cap1` | `sensor.jinko_inverter_SYNTHETIC_INV_001_soc` |
| `group=electric`, `key=S_P_T`, `name=Total Solar Power` | `electric_s_p_t` | `sensor.jinko_inverter_SYNTHETIC_INV_001_total_solar_power` |

Home Assistant owns the final entity ID. If an entity with the same slug already exists, Home Assistant may append a suffix such as `_2`.

### Alarm/Fault Binary Sensors

If a metric is an alarm or fault metric, the bridge creates an additional binary sensor.

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
