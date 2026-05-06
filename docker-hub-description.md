# JinkoBridge

![JinkoBridge](https://raw.githubusercontent.com/RCooLeR/jinko-exporter/main/assets/jinko.png)

JinkoBridge is a lightweight read-only bridge for Jinko and Solarman solar telemetry. It polls the selected source, exposes Prometheus metrics on `/metrics`, and can publish Home Assistant MQTT Discovery entities for dashboards and automations.

## Docker Compose

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    container_name: jinko_bridge
    restart: unless-stopped
    ports:
      - "9876:9876"
    environment:
      EXPORTER_SOURCE: "jinko"
      EXPORTER_LISTEN: ":9876"
      EXPORTER_POLL_INTERVAL: "60s"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN: "<JWT_FROM_BROWSER>"
      MQTT_ENABLED: "true"
      MQTT_BROKER: "tcp://homeassistant.local:1883"
      MQTT_USERNAME: "<MQTT_USER>"
      MQTT_PASSWORD: "<MQTT_PASSWORD>"
```

Prometheus endpoint: `http://localhost:9876/metrics`

Source, documentation, and releases: [github.com/RCooLeR/jinko-exporter](https://github.com/RCooLeR/jinko-exporter)
