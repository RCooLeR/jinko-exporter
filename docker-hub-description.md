# JinkoBridge

![JinkoBridge](https://raw.githubusercontent.com/RCooLeR/jinko-exporter/main/assets/jinko.png)

JinkoBridge is an unofficial read-only bridge for Jinko and compatible
Solarman-backed solar telemetry. It supports Jinko, Solarman OpenAPI, and a
locked local Solarman V5/Modbus profile, exposes Prometheus metrics on
`/metrics`, and can publish Home Assistant MQTT Discovery entities.

## Local-First Docker Compose

This example contains placeholders only. Create the referenced secret files
locally; do not put tokens or passwords in Compose environment values or commit
them to source control.

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    container_name: jinko_bridge
    restart: unless-stopped
    stop_grace_period: 40s
    ports:
      - "9876:9876"
    read_only: true
    tmpfs:
      - /tmp
    volumes:
      - ./state:/var/lib/jinko-bridge
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    environment:
      EXPORTER_SOURCE_PRIORITY: "modbus,jinko,solarman"
      EXPORTER_LISTEN: ":9876"
      EXPORTER_POLL_INTERVAL: "60s"
      EXPORTER_METRICS_DROP_SOURCE_LABEL: "true"

      MODBUS_HOST: "<INVERTER_LAN_IP>"
      MODBUS_PORT: "8899"
      MODBUS_LOGGER_SERIAL: "<LOGGER_SERIAL>"
      MODBUS_DEVICE_SN: "<INVERTER_SERIAL>"
      MODBUS_UNIT_ID: "1"
      MODBUS_TIMEOUT: "5s"

      JINKO_DEVICE_ID: "<JINKO_DEVICE_ID>"
      JINKO_SITE_ID: "<JINKO_SITE_ID>"
      JINKO_REFRESH_TOKEN_FILE: "/run/secrets/jinko_refresh_token"
      JINKO_TOKEN_STATE_FILE: "/var/lib/jinko-bridge/jinko-token-state.json"

      SOLARMAN_APP_ID: "<SOLARMAN_APP_ID>"
      SOLARMAN_APP_SECRET_FILE: "/run/secrets/solarman_app_secret"
      SOLARMAN_EMAIL: "<SOLARMAN_ACCOUNT_EMAIL>"
      SOLARMAN_PASSWORD_FILE: "/run/secrets/solarman_password"
      SOLARMAN_DEVICE_SN: "<INVERTER_SERIAL>"

      # Required as one stable, source-independent identity when MQTT is used
      # with more than one priority source.
      # MQTT_ENABLED: "true"
      # MQTT_DEVICE_ID: "<STABLE_HOME_ASSISTANT_DEVICE_ID>"
      # MQTT_BROKER: "tcp://homeassistant.local:1883"
      # MQTT_USERNAME: "<MQTT_USERNAME>"
      # MQTT_PASSWORD_FILE: "/run/secrets/mqtt_password"
    secrets:
      - jinko_refresh_token
      - solarman_app_secret
      - solarman_password
      # - mqtt_password

secrets:
  jinko_refresh_token:
    file: ./secrets/jinko_refresh_token
  solarman_app_secret:
    file: ./secrets/solarman_app_secret
  solarman_password:
    file: ./secrets/solarman_password
  # mqtt_password:
  #   file: ./secrets/mqtt_password
```

All three source configurations are required when all three appear in
`EXPORTER_SOURCE_PRIORITY`. Run one supervised Modbus-only `fetch` before
enabling the long-running local-first chain. The local profile uses one fresh
connection per fetch, fixed FC03 reads only, and no write or arbitrary-register
path. It targets the JKS-6/8/10/12/15/20H-EI family shape; only the 12 kW target
has been fully live-correlated.

`EXPORTER_METRICS_DROP_SOURCE_LABEL=true` also enables primary-surface
projection by default, keeping ordinary Prometheus key/group/name/unit labels
stable through failover. Jinko token expiry tracking—and token rotation when
refresh is configured—continues in `serve` even while Modbus supplies every
snapshot, without making Jinko detail requests while the local source is
healthy. The private writable state mount is therefore required when automatic
refresh is enabled.

Prometheus endpoint: `http://localhost:9876/metrics`

Full setup, source-specific requirements, Modbus validation evidence, and open
hardware-dependent work are documented at
[github.com/RCooLeR/jinko-exporter](https://github.com/RCooLeR/jinko-exporter).
Published images and releases are available from
[Docker Hub](https://hub.docker.com/r/rcooler/jinko_exporter) and
[GitHub Releases](https://github.com/RCooLeR/jinko-exporter/releases).
