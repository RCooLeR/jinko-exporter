# Home Assistant MQTT

`jinko-exporter` can publish read-only Home Assistant MQTT Discovery messages.
It does not create command topics and does not control the inverter.

## What Home Assistant gets

On each successful poll, the exporter publishes:

- retained MQTT discovery configs under `homeassistant/.../config`
- retained state JSON under `jinko-exporter/<device>/state`
- retained availability under `jinko-exporter/availability`

The exporter creates one Home Assistant device and adds entities for every numeric metric returned by the selected source. The exact list depends on the inverter/logger and source response, but typically includes:

- PV voltage/current/power for every available string
- AC/grid voltage/current/frequency/power for every available phase
- daily and total production energy
- grid import/export energy
- load/consumption power and energy
- battery voltage/current/power/SOC/SOH/charge/discharge energy when available
- BMS values when available
- inverter, AC, DC, battery, and BMS temperatures when available
- generator and UPS values when available
- status/version/basic diagnostic values exposed by the source
- numeric alarm/fault values
- binary problem sensors for overall alarm/fault state and each alarm/fault metric
- diagnostic entities for source, serials, IDs, collection time, publish time, poll duration, metric count, and active alarm/fault count

## Home Assistant setup

1. Install and start an MQTT broker.

   The usual Home Assistant option is the Mosquitto broker add-on.

2. Add the MQTT integration in Home Assistant.

   Go to `Settings` -> `Devices & services` -> `Add integration` -> `MQTT`.

3. Create MQTT credentials.

   If you use the Mosquitto add-on, create a login in the add-on configuration or use a Home Assistant user that is allowed by your broker setup.

4. Enable MQTT in `jinko-exporter`.

   Minimum variables:

   ```yaml
   MQTT_ENABLED: "true"
   MQTT_BROKER: "tcp://homeassistant.local:1883"
   MQTT_USERNAME: "<MQTT_USER>"
   MQTT_PASSWORD: "<MQTT_PASSWORD>"
   ```

5. Start or restart `jinko-exporter`.

   After the first successful poll, Home Assistant should discover the device automatically.

## Docker Compose example

Use the same Jinko/Solarman source variables you already use, then add:

```yaml
services:
  jinko_exporter:
    image: rcooler/jinko-exporter:local
    restart: unless-stopped
    environment:
      EXPORTER_SOURCE: "jinko"
      EXPORTER_POLL_INTERVAL: "60s"

      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN: "<JWT>"

      MQTT_ENABLED: "true"
      MQTT_BROKER: "tcp://homeassistant.local:1883"
      MQTT_USERNAME: "<MQTT_USER>"
      MQTT_PASSWORD: "<MQTT_PASSWORD>"
      MQTT_TOPIC_PREFIX: "jinko-exporter"
      MQTT_DISCOVERY_PREFIX: "homeassistant"
      MQTT_DEVICE_NAME: "Jinko Inverter"
      MQTT_RETAIN: "true"
      MQTT_QOS: "0"
```

If the exporter runs in the same Compose project as Mosquitto, use the broker service name:

```yaml
MQTT_BROKER: "tcp://mosquitto:1883"
```

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `MQTT_ENABLED` | `false` | Enables read-only Home Assistant MQTT publishing. |
| `MQTT_BROKER` | `tcp://localhost:1883` | Broker URL. Use `tcp://host:1883` or `tls://host:8883`. |
| `MQTT_CLIENT_ID` | `jinko-exporter` | MQTT client ID. Use a unique value if you run more than one exporter. |
| `MQTT_USERNAME` | empty | Broker username. |
| `MQTT_PASSWORD` | empty | Broker password. |
| `MQTT_TOPIC_PREFIX` | `jinko-exporter` | Base topic for state and availability messages. |
| `MQTT_DISCOVERY_PREFIX` | `homeassistant` | Home Assistant discovery prefix. Change only if your HA MQTT integration uses a custom prefix. |
| `MQTT_DEVICE_NAME` | auto | Optional Home Assistant device name. Defaults to `Jinko Inverter <serial>` when a serial is available. |
| `MQTT_DEVICE_ID` | auto | Optional stable HA device identifier. Defaults to device serial, device ID, parent serial, site ID, or source name. |
| `MQTT_QOS` | `0` | QoS for discovery, availability, and state publishes. |
| `MQTT_RETAIN` | `true` | Retains discovery, state, and availability messages so HA restores them after restart. |
| `MQTT_TIMEOUT` | `10s` | MQTT connect and publish timeout. |
| `MQTT_INSECURE_SKIP_VERIFY` | `false` | Skips TLS certificate verification for `tls://` brokers. Use only for trusted private networks. |

## Topics

With defaults and a device serial of `ABC123`, topics look like this:

```text
jinko-exporter/availability
jinko-exporter/abc123/state
homeassistant/sensor/abc123_electric_dp1/config
homeassistant/binary_sensor/abc123_alarm_or_fault_active/config
```

Example state payload:

```json
{
  "source": "jinko",
  "device_sn": "ABC123",
  "device_id": "100000001",
  "site_id": "200000001",
  "collected_at": "2026-04-20T10:30:00Z",
  "published_at": "2026-04-20T10:30:02Z",
  "up": true,
  "metrics": {
    "electric_dp1": 1840,
    "electric_dv1": 412.3,
    "grid_pg_f1": 49.99,
    "battery_b_left_cap1": 82
  },
  "metric_count": 128,
  "alert_metrics": {
    "alert_l_b_f_f": 0
  },
  "alert_count": 0,
  "alerts_active": false,
  "poll_duration_seconds": 1.23
}
```

## Removing old entities

Home Assistant MQTT discovery configs are retained. If you change `MQTT_TOPIC_PREFIX`, `MQTT_DEVICE_ID`, or the device serial changes, HA may keep the old entities.

Remove them by publishing an empty retained payload to the old discovery config topics, or delete the old device/entities in Home Assistant and clear retained MQTT discovery messages with an MQTT tool such as MQTT Explorer.

## Troubleshooting

- No device appears: check the exporter logs for `connected MQTT publisher` and `published MQTT state`.
- Device appears but entities are unavailable: check `jinko-exporter/availability`; it should be `online` after a successful poll.
- Values are stale: the exporter publishes `offline` after poll failures and on clean shutdown. Check source credentials and the exporter logs.
- Discovery works only after restart: retained discovery should be enabled with `MQTT_RETAIN=true`.
- Running multiple exporters: set unique `MQTT_CLIENT_ID`, `MQTT_TOPIC_PREFIX`, and preferably `MQTT_DEVICE_ID` for each instance.

Home Assistant references:

- MQTT integration and discovery: https://www.home-assistant.io/integrations/mqtt/
- MQTT sensor: https://www.home-assistant.io/integrations/sensor.mqtt/
- MQTT binary sensor: https://www.home-assistant.io/integrations/binary_sensor.mqtt/
