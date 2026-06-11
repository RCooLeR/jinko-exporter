# Installation

The recommended installation is the published Docker image. You can also build the image locally or run the Go binary directly from `bridge/`.

## Docker Compose

Create a Compose file on the host that should run the bridge.
The repository also includes a hardened starter file at `compose.example.yaml`.

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
```

Start it:

```shell
docker compose up -d
```

Check health:

```shell
curl http://localhost:9876/healthz
curl http://localhost:9876/readyz
curl http://localhost:9876/metrics
```

`/readyz` returns `503` until the first successful poll.

## Jinko Source

The Jinko source calls the private detail endpoint used by the web application:

```text
POST https://smart-global.jinkosolar.com/device-s/device/v3/detail
```

Minimum configuration:

```yaml
environment:
  EXPORTER_SOURCE: "jinko"
  JINKO_DEVICE_ID: "100000001"
  JINKO_SITE_ID: "200000001"
  JINKO_BEARER_TOKEN: "<JWT_FROM_BROWSER>"
```

Optional fields commonly needed in real deployments:

```yaml
environment:
  JINKO_COOKIE: "<OPTIONAL_COOKIE_HEADER>"
  JINKO_LANGUAGE: "en"
  JINKO_RETRY_ATTEMPTS: "3"
  JINKO_RETRY_BACKOFF: "2s"
  JINKO_REQUEST_JITTER_MAX: "5s"
  JINKO_TOKEN_ALERT_WINDOW: "24h"
```

If the Jinko endpoint serves a broken TLS certificate, `JINKO_INSECURE_SKIP_VERIFY=true` can bypass certificate verification for Jinko requests only. Use it only as a last resort.

To keep secrets out of Compose environment blocks, mount secret files and point the bridge at them. Secret files are read once when the process starts.

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    volumes:
      - ./secrets:/run/secrets:ro
    environment:
      EXPORTER_SOURCE: "jinko"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN_FILE: "/run/secrets/jinko_bearer_token"
      JINKO_COOKIE_FILE: "/run/secrets/jinko_cookie"
```

## Solarman Source

The Solarman source uses Solarman OpenAPI:

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    restart: unless-stopped
    ports:
      - "9876:9876"
    environment:
      EXPORTER_SOURCE: "solarman"
      SOLARMAN_APP_ID: "<APP_ID>"
      SOLARMAN_APP_SECRET: "<APP_SECRET>"
      SOLARMAN_EMAIL: "<ACCOUNT_EMAIL>"
      SOLARMAN_PASSWORD: "<ACCOUNT_PASSWORD>"
      SOLARMAN_DEVICE_SN: "<DEVICE_SN>"
```

`SOLARMAN_DEVICE_SN` skips station/device discovery. If you do not know it, omit it and optionally set `SOLARMAN_STATION_ID` to limit discovery to one station.

To pace requests against a yearly quota:

```yaml
environment:
  SOLARMAN_YEARLY_REQUEST_LIMIT: "200000"
  SOLARMAN_DISCOVERY_REFRESH_INTERVAL: "24h"
```

Solarman secrets can also come from files:

```yaml
volumes:
  - ./secrets:/run/secrets:ro
environment:
  SOLARMAN_APP_SECRET_FILE: "/run/secrets/solarman_app_secret"
  SOLARMAN_PASSWORD_FILE: "/run/secrets/solarman_password"
```

## Source Failover

Use `EXPORTER_SOURCE_PRIORITY` to try more than one source in order:

```yaml
environment:
  EXPORTER_SOURCE_PRIORITY: "jinko,solarman"
  JINKO_DEVICE_ID: "100000001"
  JINKO_SITE_ID: "200000001"
  JINKO_BEARER_TOKEN: "<JWT_FROM_BROWSER>"
  SOLARMAN_APP_ID: "<APP_ID>"
  SOLARMAN_APP_SECRET: "<APP_SECRET>"
  SOLARMAN_EMAIL: "<ACCOUNT_EMAIL>"
  SOLARMAN_PASSWORD: "<ACCOUNT_PASSWORD>"
  SOLARMAN_DEVICE_SN: "<DEVICE_SN>"
```

All sources listed in the priority must have valid configuration. The poll is successful when the first source in the list returns a valid snapshot.

## Home Assistant MQTT

Enable MQTT Discovery when Home Assistant should create sensors automatically:

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    restart: unless-stopped
    ports:
      - "9876:9876"
    environment:
      EXPORTER_SOURCE: "jinko"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN: "<JWT_FROM_BROWSER>"
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

When the bridge and Mosquitto are in the same Compose project, use the broker service name:

```yaml
MQTT_BROKER: "tcp://mosquitto:1883"
```

See [Home Assistant MQTT Discovery](./home-assistant.md) for topics, device naming, and entity naming details.

Use `MQTT_PASSWORD_FILE` and `SMTP_PASSWORD_FILE` when those passwords are mounted as Docker secrets.

## Build Locally

Build the Docker image from the repository root:

```shell
docker build -f bridge/Dockerfile -t rcooler/jinko-exporter:local bridge
```

Use the local image in Compose:

```yaml
services:
  jinko_bridge:
    build: ./bridge
    image: rcooler/jinko-exporter:local
```

## Run Without Docker

From the repository root:

```shell
cd bridge
go run . serve --source jinko --jinko-device-id 100000001 --jinko-site-id 200000001 --jinko-bearer-token "<JWT_FROM_BROWSER>"
```

Fetch once and print the normalized snapshot:

```shell
cd bridge
go run . fetch --source jinko --jinko-device-id 100000001 --jinko-site-id 200000001 --jinko-bearer-token "<JWT_FROM_BROWSER>"
```
