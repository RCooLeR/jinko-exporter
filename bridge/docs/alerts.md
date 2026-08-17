# Alerts

Alerts are optional and disabled by default. When enabled, the bridge sends email through SMTP.

## Enable Alerts

Minimum configuration:

```yaml
environment:
  ALERTS_ENABLED: "true"
  SMTP_HOST: "smtp.example.com"
  SMTP_PORT: "587"
  SMTP_USERNAME: "<SMTP_USER>"
  SMTP_PASSWORD: "<SMTP_PASSWORD>"
  SMTP_FROM_EMAIL: "jinko@example.com"
  SMTP_TO_EMAILS: "admin@example.com"
```

If `SMTP_TO_EMAILS` is empty, the bridge falls back to `SMTP_FROM_EMAIL` as the recipient.

## SMTP TLS Modes

STARTTLS is enabled by default:

```yaml
SMTP_STARTTLS: "true"
SMTP_USE_TLS: "false"
```

Use implicit TLS for servers that expect TLS from the first byte:

```yaml
SMTP_STARTTLS: "false"
SMTP_USE_TLS: "true"
SMTP_PORT: "465"
```

`SMTP_STARTTLS` and `SMTP_USE_TLS` cannot both be enabled.

## Cooldown

Repeated alerts with the same key are suppressed for `ALERTS_COOLDOWN`.

Default:

```yaml
ALERTS_COOLDOWN: "6h"
```

## Recovery Notifications

Recovery notifications are disabled by default. Enable them with:

```yaml
ALERTS_NOTIFY_RECOVERY: "true"
```

The bridge tracks active alert conditions even when recovery emails are disabled. Grid-down and warning/alarm/fault alerts recover when the underlying condition clears. Low battery SOC recovers at the configured threshold plus `5%`; high temperature recovers at the configured threshold minus `5 C`. This hysteresis prevents threshold-edge values from flapping between alert and recovery.

## Alert Types

### Jinko Token And Request Alerts

The Jinko source can send alerts for:

- a legacy bearer-only token already expired
- a legacy bearer-only token expiring within `JINKO_TOKEN_ALERT_WINDOW`
- token refresh failures
- Jinko `401` or `403` responses

Authentication alerts include status and endpoint context but never include bearer tokens, refresh tokens, or upstream response bodies.

### Solarman Request Alerts

The Solarman source can send alerts for:

- device discovery failure
- token request failure
- token refresh failure after `401`
- current data request failure
- API response decode failure

### No Successful Poll

Disabled by default. Enable it with:

```yaml
ALERT_NO_SUCCESSFUL_POLL_WINDOW: "10m"
```

The bridge alerts when no successful poll has completed within the configured duration. Before the first successful poll, the timer starts at exporter startup.

### Inverter Warning/Alarm/Fault Metrics

Enabled automatically when alerts are enabled.

Any metric is considered a warning/alarm/fault metric when:

- its group is `alert`
- its group, key, or name contains `alarm`
- its group, key, or name contains `fault`

The alert fires when one or more matching metrics are non-zero.

The local Modbus profile emits registers 553-558 as six independent raw U16
metrics in group `alert`: two warning words followed by four fault words. Any
non-zero word participates in the source-and-device-scoped email aggregate;
the cooldown is shared by that aggregate rather than applied per word or bit.
An all-zero set in a successful complete snapshot from the same source
resolves the aggregate condition. A failed poll produces no snapshot and
therefore cannot resolve it.

Aggregate warning/alarm/fault recovery is scoped by both source and device.
Consequently, a clear Jinko or Solarman fallback snapshot cannot resolve an
active Deye/Modbus raw-word condition; that condition resolves only after a
later successful Modbus snapshot explicitly reports its words as zero.
Home Assistant uses a separate aggregate problem entity per source and makes
the Modbus entity unknown during cloud fallback; it never interprets a clear
cloud domain as a Deye-word recovery.

The reported values are raw decimal U16 words, not decoded fault codes, and
the alert count is the number of non-zero words rather than the number of set
bits. The bridge never clears or acknowledges a word and sends no Modbus write
in response to an alert. These inverter-wide Deye words are not aliases for
the Jinko `L_B_A_F`, `L_B_F_F`, `L_B_A_F2`, or `L_B_F_F2` lithium-BMS flags.

### Grid Down

Enabled by default when alerts are enabled. The bridge checks these grid voltage keys:

```text
G_V_L1
G_V_L2
G_V_L3
```

If all available grid phase voltage metrics are at or below the threshold, the bridge sends a grid-down alert.

Default threshold:

```yaml
ALERT_GRID_DOWN_VOLTAGE_THRESHOLD: "20"
```

Set the threshold to `0` to disable this alert.

### Low Battery SOC

Disabled by default. Enable it with a percentage threshold:

```yaml
ALERT_BATTERY_SOC_LOW_THRESHOLD: "20"
```

The configured threshold must be below `100`; `0` disables the rule. Recovery
uses a 5-point hysteresis capped at `100%`, so high thresholds such as `98%`
can still recover when the battery reaches full charge.

The bridge checks:

```text
BMS_SOC
B_left_cap1
```

### High Temperature

Disabled by default. Enable it with a temperature threshold in C:

```yaml
ALERT_HIGH_TEMPERATURE_THRESHOLD: "65"
```

The bridge checks:

```text
AC_T
T_DC
B_T1
BMST
```

## Example

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
      ALERTS_ENABLED: "true"
      ALERTS_NOTIFY_RECOVERY: "true"
      ALERTS_COOLDOWN: "2h"
      ALERT_NO_SUCCESSFUL_POLL_WINDOW: "10m"
      ALERT_BATTERY_SOC_LOW_THRESHOLD: "20"
      ALERT_HIGH_TEMPERATURE_THRESHOLD: "65"
      SMTP_HOST: "smtp.example.com"
      SMTP_PORT: "587"
      SMTP_USERNAME: "<SMTP_USER>"
      SMTP_PASSWORD: "<SMTP_PASSWORD>"
      SMTP_FROM_EMAIL: "jinko@example.com"
      SMTP_TO_EMAILS: "admin@example.com"
      SMTP_STARTTLS: "true"
```
