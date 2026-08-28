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

`/readyz` returns `503` until the first successful poll, and returns `503` again if the last successful poll is older than three poll intervals. `/healthz` remains a process-health endpoint for Docker healthchecks.

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

### Automatic Jinko Token Refresh

If the authenticated Jinko login response provides a refresh token, the bridge can use it to renew the short-lived bearer token. The existing bearer token remains a valid startup seed and can be configured together with the refresh token.

Keep the bootstrap tokens read-only and give the bridge a separate writable directory for rotated token state:

```bash
sudo install -d -m 0700 -o 65532 -g 65532 ./state
```

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    volumes:
      - ./secrets:/run/secrets:ro
      - ./state:/var/lib/jinko-bridge
    environment:
      EXPORTER_SOURCE: "jinko"
      JINKO_DEVICE_ID: "100000001"
      JINKO_SITE_ID: "200000001"
      JINKO_BEARER_TOKEN_FILE: "/run/secrets/jinko_bearer_token"
      JINKO_REFRESH_TOKEN_FILE: "/run/secrets/jinko_refresh_token"
      JINKO_TOKEN_STATE_FILE: "/var/lib/jinko-bridge/jinko-token-state.json"
      JINKO_REFRESH_BEFORE: "5m"
```

The default token endpoint is `https://smart-global.jinkosolar.com/oauth2-s/oauth/token`, with `system=JinKO` and `area=FOREIGN_1`. Set `JINKO_ORIGIN_ID` only when the authenticated browser request includes a non-empty `origin_id`; all four values remain configurable for accounts that use a different login form. This is a private web-client contract rather than a published Jinko API, so keep Solarman failover enabled and expect the form to need maintenance if Jinko changes its portal.

The state file contains rotated credentials. Ensure the host state directory is private and writable by container UID `65532`, and allow exactly one bridge process to write a given state file. Do not place the state file inside the read-only secrets mount or reuse the bootstrap refresh-token path. Before contacting the token endpoint, the bridge atomically writes the current pair to this path, so an unwritable mount fails safely without consuming the refresh token. If the final atomic write fails after Jinko has rotated the pair, `serve` keeps the new pair pending and retries only the disk write—never the OAuth request—while telemetry priority remains independent. If `JINKO_TOKEN_STATE_FILE` already exists, startup accepts it only as recognized token-state JSON with an access or refresh token; a missing state file may be created from a separately configured bootstrap credential.

Expiry-aware rotation requires either JWT `exp` or OAuth `expires_in`. If Jinko returns an opaque access token without either, the bridge rotates once at startup, emits a sanitized warning, and does not invent a recurring interval; a later real fallback `401` safely triggers the next rotation.

The bridge also journals an internal `refresh_outcome_uncertain` marker before sending a refresh-token request. If Jinko may have consumed the refresh token but no valid response pair is proven, automatic refresh stays paused across restarts and the credential is never replayed. To recover, stop the bridge, obtain a complete new access/refresh pair, replace or remove the old state file as appropriate, and restart. Do not simply edit the marker to `false` while retaining the possibly consumed refresh token.

When running under Compose, use the example's `stop_grace_period: 70s`. This
lets an already-started OAuth transaction finish its bounded request and
durable state write, and lets an enabled alert-correlation worker cancel/join
its separately bounded HA and cloud phases before the container is terminated.

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

## Local Modbus Source

The Modbus implementation is intentionally limited to the locked Jinko/Deye three-phase HV profile. It talks directly to the Solarman-compatible logger on TCP port `8899` and emits only Modbus FC03 read requests inside Solarman V5 frames. Use Modbus alone for the first supervised one-shot; after that validation it may be placed first in a failover chain.

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    environment:
      EXPORTER_SOURCE: "modbus"
      EXPORTER_POLL_INTERVAL: "60s"
      MODBUS_HOST: "<LOGGER_PRIVATE_IPV4>"
      MODBUS_PORT: "8899"
      MODBUS_LOGGER_SERIAL: "<LOGGER_SERIAL>"
      MODBUS_DEVICE_SN: "<SAME_INVERTER_SERIAL_AS_CLOUD>"
      MODBUS_UNIT_ID: "1"
      MODBUS_TIMEOUT: "5s"
```

For the first deployment, do not start the long-running service yet. Run one disposable `fetch` process and inspect its JSON output:

```shell
docker compose run --rm --no-deps jinko_bridge fetch
```

The first fetch opens one fresh short-lived TCP connection and performs exactly twenty-four already reviewed requests on it: two target-profile gates followed by twenty-two every-fetch reads. Once both gates pass together, each later fetch opens a new connection, reuses the cached profile, and performs exactly twenty-two requests. Two successful fetches therefore use two connections and 46 requests. All requests within a fetch run sequentially under one shared absolute deadline and in the fixed allowlisted order. The first failure aborts the fetch and closes the connection without retry, reconnect, or a partial snapshot. The locked core snapshot contains 80 metrics. The `jks-6-20h-ei-readonly-v1` gate accepts only official JKS-6/8/10/12/15/20H-EI ratings with two MPPTs and three phases. The 12 kW target is the only member with live-confirmed raw gate values and telemetry; the other declared ratings are specification-gated. The unvalidated 25 kW two-MPPT sibling is outside this profile, while the 29.9/30 kW BM3 three-MPPT variants are structurally separate; all remain unsupported.

The first three every-fetch reads are the generator zero-domain contract: register `133 x 1`, sequence `0x16`; registers `536 x 4`, sequence `0x17`; and registers `661 x 11`, sequence `0x18`. The isolated supervised one-shot returned all sixteen words as zero on one connection with no retry. Because the bridge was stopped for the probe, there was no fresh simultaneous cloud bracket; the result is only consistent with previously observed cloud generator zeros. Production therefore emits `GEN_P_L1..3`, `GEN_V_L1..3`, `R_T_D`, `EG_P_CT1`, `GEN_P_T`, `GEN_P_D`, and `GEN_P_TO` as raw-backed zeros only while every word remains zero. Register 133 is re-read rather than cached. Any nonzero word rejects the complete Modbus snapshot and allows configured cloud fallback; no nonzero generator scale, sign, pairing, or alias is inferred.

The existing PV input request reads registers 672-679 once. It emits `DP1=R672×10 W`, `DP2=R673×10 W`, `DV1=R676×0.1 V`, `DC1=R677×0.1 A`, `DV2=R678×0.1 V`, `DC2=R679×0.1 A`, and `S_P_T=(R672+R673)×10 W`. The exact reviewed response, compatible read-only protocol maps, same-frame `V×I` agreement, and separately bracketed cloud aggregate establish the register provenance and scales. The capability gate requires two MPPTs, registers 674-675 must remain zero for unused PV3/PV4 power, each channel and aggregate power are capped at twice rated power, voltage is bounded to `1000 V`, and current is bounded to `100 A`. Promoting the six per-channel values added no PV request; the current 24/22 plan also includes the three later generator zero-domain reads, one later output-power high-word read, and the power/AC-relay status read.

The direct-load low-word read `650 x 4`, sequence `0x12`, runs immediately before the existing `655 x 5`, sequence `0x13`, frequency/high-word read. Low-first pairs 650/656, 651/657, and 652/658 emit canonical signed `LPP_A/B/C`. Each phase high word must be `0x0000` or `0xFFFF`, and the joined value must remain inside the conservative `-32767..32767 W` coherence envelope so a stale sign word across zero fails closed. Dedicated pair 653/659 emits independent non-negative `E_Puse_t1`: R659 must be zero, while R653 retains the full numerically U16 `0..65535 W` subset of its documented signed wire pair. A live same-frame sample measured the phase sum at `243 W` while the dedicated total was `251 W`, so phase-sum equality is deliberately not a gate. A failed phase or total gate rejects the whole snapshot; `C_P_L1..3` aliases remain excluded. The phase guard is not a `Pr1` or pass-through cap, and the wider dedicated-total range is preserved. Register 588 exposes its one validated SOC value through both canonical keys `B_left_cap1` and `BMS_SOC`; register 586 likewise exposes the validated temperature through `B_T1` and `BMST`. None is filled with a guessed zero.

Grid phase and total powers use two consecutive fixed reads: low words `622 x 4`, sequence `0x0C`, followed immediately by high words `687 x 4`, sequence `0x0D`. Production joins each pair low-word-first as signed 32-bit, requires canonical high words `0x0000` or `0xFFFF`, restricts every value to the conservative `-32767..32767 W` coherence envelope, and requires the exact signed phase sum to equal the total. The symmetric `0x7FFF` boundary makes a stale sign-extension word fail closed if the two reads straddle zero; it is not derived from `Pr1` or a pass-through rating. An invalid high word, out-of-envelope value, or signed-sum mismatch rejects the whole snapshot and allows configured cloud fallback.

Output active power uses two consecutive fixed reads: `627 x 12`, sequence `0x0E`, contains low words 633-636, and the immediately following `691 x 5`, sequence `0x0F`, contains their high words 691-694. Production joins each pair low-word-first as signed 32-bit and emits `INV_O_P_L1`, `INV_O_P_L2`, `INV_O_P_L3`, and `INV_O_P_T` only inside a conservative coherence envelope: every active high word must be `0x0000` or `0xFFFF`, every joined value must be between `-32767` and `32767 W`, and the exact signed phase sum must equal the total. The symmetric `0x7FFF` guard protects against combining a low word with a stale sign-extension word when the two reads straddle zero; it is not derived from `Pr1` or an inverter/pass-through rating. An invalid high word, out-of-envelope value, or signed-sum mismatch rejects the whole snapshot and allows configured cloud fallback. The target has produced `R691=0xFFFF`, although that log did not preserve a complete negative raw pair for exact magnitude correlation. Apparent-power candidate words 637/695 remain ignored, so `O_P` is not emitted.

The fixed status request reads registers 551-552 at sequence `0x19`. Register 551 exposes only its documented low-nibble power switch state as source-local `DEYE_MODBUS_R551_POWER_SWITCH_STATE`: `0` is off and `1` is on; codes 2-15 reject the snapshot, while undefined upper bits are ignored. Register 552 is the complete raw U16 AC relay mask and emits canonical `AC`; the bridge preserves every bit instead of rejecting undefined or firmware-specific bits. Documented meanings include bit 0 inverter relay, bit 2 grid relay, bit 3 generator relay, bit 4 grid-supply relay, and bits 7-8 dry contacts; bit 1 is reserved/undefined in the primary table and is still preserved. The following fixed FC03 warning/fault block reads registers 553-558; inspect its six raw U16 words in the fetch output. A non-zero word is a status bitmask to investigate, never an instruction for the bridge to clear, acknowledge, or write anything. The next block reads registers 500-505 with sequence `0x04`, but emits only register 500 as `DEYE_MODBUS_R500_RUN_STATE`; registers 501-505 remain ignored and no Jinko status/model alias is synthesized. The final block reads registers 609-619 with sequence `0x0B`, emits only grid frequency 609 and internal currents 610-612, and ignores external-CT registers 613-619. Every block is compile-time fixed and FC03-only. The process exits on the first timeout, protocol mismatch, checksum failure, exception, range failure, or target-profile mismatch. It does not retry, reconnect within that fetch, or return a partial snapshot. Redact all identity fields before sharing fetch JSON. Only after this one-shot result has been reviewed should a supervised Modbus-only `serve` be considered. For that first controlled run, use `restart: "no"`; change the restart policy only after observing stable 60-second polling.

The profile deliberately does not expose arbitrary registers, FC05/FC06/FC0F/FC10, logger HTTP pages, AP settings, Server A/B settings, restart commands, discovery, or DNS. Do not run another local Modbus poller or the development one-shot tools against the logger at the same time.

## Source Failover

Use `EXPORTER_SOURCE_PRIORITY` to try more than one source in order. The recommended local-first chain is:

```yaml
environment:
  EXPORTER_SOURCE_PRIORITY: "modbus,jinko,solarman"
  EXPORTER_METRICS_DROP_SOURCE_LABEL: "true"
  EXPORTER_POLL_INTERVAL: "60s"
  MODBUS_HOST: "<LOGGER_PRIVATE_IPV4>"
  MODBUS_LOGGER_SERIAL: "<LOGGER_SERIAL>"
  MODBUS_DEVICE_SN: "<INVERTER_SERIAL>"
  MODBUS_UNIT_ID: "1"
  MODBUS_TIMEOUT: "5s"
  JINKO_DEVICE_ID: "100000001"
  JINKO_SITE_ID: "200000001"
  JINKO_BEARER_TOKEN_FILE: "/run/secrets/jinko_bearer_token"
  JINKO_REFRESH_TOKEN_FILE: "/run/secrets/jinko_refresh_token"
  JINKO_TOKEN_STATE_FILE: "/var/lib/jinko-bridge/jinko-token-state.json"
  SOLARMAN_APP_ID: "<APP_ID>"
  SOLARMAN_APP_SECRET: "<APP_SECRET>"
  SOLARMAN_EMAIL: "<ACCOUNT_EMAIL>"
  SOLARMAN_PASSWORD: "<ACCOUNT_PASSWORD>"
  SOLARMAN_DEVICE_SN: "<DEVICE_SN>"
```

All sources listed in the priority must have valid configuration. Every poll starts at the beginning: Modbus wins when its complete snapshot is valid, Jinko is tried only after a Modbus failure, and Solarman only after both earlier sources fail. No cloud detail request runs speculatively while Modbus is healthy.

`EXPORTER_METRICS_DROP_SOURCE_LABEL=true` is recommended for this chain. Unless separately overridden, it also turns on fallback projection and strict filtering of unknown Solarman-only points; recognized Solarman points are canonicalized through the shared Jinko dictionary regardless of that strict flag. Jinko, known Solarman points, and Modbus therefore expose PV keys `DP1`, `DP2`, `DV1`, `DC1`, `DV2`, `DC2`, and `S_P_T` with the same `electric` group, names, and units. Once the Modbus primary surface has been learned, projection replaces a matching fallback metric's labels with those primary labels, producing one source-independent Prometheus series per logical metric and preventing duplicate Grafana lines after failover. Warning/alarm/fault metrics remain source-local and are not rewritten into another source's alert domain; `last_source_sync` and the MQTT Data Source diagnostic still show which source supplied data.

In long-running `serve`, the Jinko OAuth credential maintainer is independent of that selection and keeps the rotating access/refresh pair current through the token endpoint only when usable expiry metadata is available. With unknown opaque-token expiry it follows the conservative pause-and-401 behavior described above. Its failure is reported separately and cannot turn a successful Modbus poll into a failed poll. Solarman obtains its short-lived access token on demand from its reusable account credentials. A one-shot `fetch` starts no background token task.

Optional raw-warning correlation is also a `serve`-only background task. Set
`MODBUS_ALERT_CORRELATION_ENABLED=true` only with the exact
`modbus,jinko,solarman` priority above, then configure `HOMEASSISTANT_URL`, one
of `HOMEASSISTANT_TOKEN`/`HOMEASSISTANT_TOKEN_FILE`, and a dedicated
`JINKO_HA_NOTIFY_SERVICE=mobile_app_*`. A complete Modbus snapshot with any
non-zero R553-R558 word first sends one bounded HA push, then gives Jinko and
Solarman a fresh concurrent evidence budget. Cloud results are diagnostic only:
they never replace the accepted Modbus snapshot or clear its alert. An all-zero
complete Modbus snapshot sends the recovery push. Completion details are logged
without a second mobile notification. For an internal HTTP HA URL, the explicit
`HOMEASSISTANT_ALLOW_INSECURE_HTTP=true` opt-in accepts only a private/loopback
literal address or a single-label hostname whose resolved addresses are pinned
to the private network. Prefer HTTPS and a dedicated least-privilege HA token.
A one-shot `fetch` never starts this worker or contacts HA/cloud for correlation.

For the hardware acceptance read, temporarily force `EXPORTER_SOURCE=modbus` and clear `EXPORTER_SOURCE_PRIORITY`; otherwise a cloud fallback could conceal a Modbus failure. Never run another Modbus poller against the same logger concurrently.

When MQTT is enabled for a mixed priority chain, set one explicit `MQTT_DEVICE_ID` that remains stable across all three sources. Set `MODBUS_DEVICE_SN` to the same inverter serial exposed by the cloud sources as well; it is snapshot identity only and is never sent to the logger. Persistent Discovery state is optional for compatibility but strongly recommended: set `MQTT_DISCOVERY_STATE_FILE=/var/lib/jinko-bridge/mqtt-discovery-state.json` and mount the private `./state` directory shown below. The configured first-priority source then owns the ordinary Home Assistant surface across process restarts; a cold cloud fallback cannot add cloud-only ordinary entities before that primary succeeds.

## Home Assistant MQTT

Enable MQTT Discovery when Home Assistant should create sensors automatically:

```yaml
services:
  jinko_bridge:
    image: rcooler/jinko_exporter:latest
    restart: unless-stopped
    ports:
      - "9876:9876"
    volumes:
      - ./state:/var/lib/jinko-bridge
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
      MQTT_DEVICE_ID: "synthetic_inverter_001"
      MQTT_DISCOVERY_STATE_FILE: "/var/lib/jinko-bridge/mqtt-discovery-state.json"
      MQTT_RETAIN: "true"
      MQTT_QOS: "0"
```

Create `./state` as a private directory writable by container UID `65532`, as in the token-refresh example above. Use a different manifest file for every bridge device, never point it at a Docker secret or `JINKO_TOKEN_STATE_FILE`, and allow exactly one bridge process to write it. `MQTT_RETAIN` controls state and availability retention; Discovery configs are always retained. If the manifest is corrupt or its stored ownership no longer matches the configured device, prefix, or primary source, startup fails closed rather than replacing the file or deleting broker topics.

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
