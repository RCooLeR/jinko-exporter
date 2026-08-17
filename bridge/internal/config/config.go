package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urfave/cli/v2"
)

const maxJinkoTokenStateBytes = 64 * 1024

var (
	metricPrefixPattern       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	solarmanAPIVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type Config struct {
	Source                 string
	SourcePriority         []string
	ListenAddress          string
	MetricsPath            string
	PollInterval           time.Duration
	LogLevel               string
	MetricPrefix           string
	DropSourceLabel        bool
	ProjectFailoverMetrics bool
	Alerts                 AlertConfig
	MQTT                   MQTTConfig
	Jinko                  JinkoConfig
	Solarman               SolarmanConfig
	Modbus                 ModbusConfig
	ShellyGridLoad         ShellyGridLoadConfig
}

func (cfg Config) Redacted() Config {
	redacted := cfg
	redacted.Alerts.SMTPPassword = redactSecret(redacted.Alerts.SMTPPassword)
	redacted.MQTT.Password = redactSecret(redacted.MQTT.Password)
	redacted.Jinko.BearerToken = redactSecret(redacted.Jinko.BearerToken)
	redacted.Jinko.RefreshToken = redactSecret(redacted.Jinko.RefreshToken)
	redacted.Jinko.Cookie = redactSecret(redacted.Jinko.Cookie)
	redacted.Solarman.AppSecret = redactSecret(redacted.Solarman.AppSecret)
	redacted.Solarman.Password = redactSecret(redacted.Solarman.Password)
	redacted.Solarman.PasswordSHA256 = redactSecret(redacted.Solarman.PasswordSHA256)
	return redacted
}

func redactSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "********"
}

type AlertConfig struct {
	Enabled                  bool
	NotifyRecovery           bool
	Cooldown                 time.Duration
	Timeout                  time.Duration
	SMTPHost                 string
	SMTPPort                 int
	SMTPUsername             string
	SMTPPassword             string
	SMTPFromEmail            string
	SMTPFromName             string
	SMTPToEmails             []string
	SMTPUseTLS               bool
	SMTPStartTLS             bool
	NoSuccessfulPollWindow   time.Duration
	GridDownVoltageThreshold float64
	BatterySOCLowThreshold   float64
	HighTemperatureThreshold float64
}

type MQTTConfig struct {
	Enabled            bool
	Broker             string
	ClientID           string
	Username           string
	Password           string
	TopicPrefix        string
	DiscoveryPrefix    string
	DeviceName         string
	DeviceID           string
	QOS                byte
	Retain             bool
	Timeout            time.Duration
	InsecureSkipVerify bool
}

type JinkoConfig struct {
	URL                string
	TokenURL           string
	Timeout            time.Duration
	InsecureSkipVerify bool
	RetryAttempts      int
	RetryBackoff       time.Duration
	DeviceID           int64
	SiteID             int64
	Language           string
	NeedRealtimeData   bool
	BearerToken        string
	RefreshToken       string
	RefreshTokenFile   string
	TokenStateFile     string
	RefreshBefore      time.Duration
	System             string
	Area               string
	OriginID           string
	Cookie             string
	UserAgent          string
	RequestJitterMax   time.Duration
	TokenAlertWindow   time.Duration
}

type SolarmanConfig struct {
	BaseURL                  string
	APIVersion               string
	Language                 string
	Timeout                  time.Duration
	InsecureSkipVerify       bool
	CanonicalJinkoMetrics    bool
	YearlyRequestLimit       int
	DiscoveryRefreshInterval time.Duration
	AppID                    string
	AppSecret                string
	Email                    string
	Password                 string
	PasswordSHA256           string
	DeviceSN                 string
	StationID                int64
}

type ModbusConfig struct {
	Host         string
	Port         int
	LoggerSerial string
	DeviceSN     string
	UnitID       uint
	Timeout      time.Duration
}

type ShellyGridLoadConfig struct {
	Enabled bool
	BaseURL string
	EMID    int
	Timeout time.Duration
}

func Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "source", Value: "jinko", Usage: "Data source: jinko, solarman, modbus", EnvVars: []string{"EXPORTER_SOURCE"}},
		&cli.StringFlag{Name: "source-priority", Usage: "Comma-separated source failover priority list; overrides source when set", EnvVars: []string{"EXPORTER_SOURCE_PRIORITY", "EXPORTER_SOURCE_priority"}},
		&cli.StringFlag{Name: "listen", Value: ":9876", Usage: "HTTP listen address", EnvVars: []string{"EXPORTER_LISTEN"}},
		&cli.StringFlag{Name: "metrics-path", Value: "/metrics", Usage: "Prometheus metrics path", EnvVars: []string{"EXPORTER_METRICS_PATH"}},
		&cli.DurationFlag{Name: "poll-interval", Value: 60 * time.Second, Usage: "Polling interval", EnvVars: []string{"EXPORTER_POLL_INTERVAL"}},
		&cli.StringFlag{Name: "log-level", Value: "info", Usage: "zerolog level", EnvVars: []string{"EXPORTER_LOG_LEVEL"}},
		&cli.StringFlag{Name: "metric-prefix", Value: "solar", Usage: "Metric name prefix", EnvVars: []string{"EXPORTER_METRIC_PREFIX"}},
		&cli.BoolFlag{Name: "metrics-drop-source-label", Value: false, Usage: "Drop the source label from generic exporter metrics; last source sync keeps the source label", EnvVars: []string{"EXPORTER_METRICS_DROP_SOURCE_LABEL"}},
		&cli.BoolFlag{Name: "source-project-failover-metrics", Value: false, Usage: "Project fallback source metrics onto the primary source metric surface; defaults to metrics-drop-source-label when unset", EnvVars: []string{"EXPORTER_SOURCE_PROJECT_FAILOVER_METRICS"}},

		&cli.BoolFlag{Name: "mqtt-enabled", Value: false, Usage: "Enable read-only Home Assistant MQTT discovery and state publishing", EnvVars: []string{"MQTT_ENABLED"}},
		&cli.StringFlag{Name: "mqtt-broker", Value: "tcp://localhost:1883", Usage: "MQTT broker URL, for example tcp://homeassistant.local:1883 or tls://broker.example:8883", EnvVars: []string{"MQTT_BROKER"}},
		&cli.StringFlag{Name: "mqtt-client-id", Value: "jinko-exporter", Usage: "MQTT client ID", EnvVars: []string{"MQTT_CLIENT_ID"}},
		&cli.StringFlag{Name: "mqtt-username", Usage: "MQTT username", EnvVars: []string{"MQTT_USERNAME"}},
		&cli.StringFlag{Name: "mqtt-password", Usage: "MQTT password", EnvVars: []string{"MQTT_PASSWORD"}},
		&cli.StringFlag{Name: "mqtt-password-file", Usage: "Read MQTT password from a file when mqtt-password is empty", EnvVars: []string{"MQTT_PASSWORD_FILE"}},
		&cli.StringFlag{Name: "mqtt-topic-prefix", Value: "jinko-exporter", Usage: "Base topic for state and availability payloads", EnvVars: []string{"MQTT_TOPIC_PREFIX"}},
		&cli.StringFlag{Name: "mqtt-discovery-prefix", Value: "homeassistant", Usage: "Home Assistant MQTT discovery prefix", EnvVars: []string{"MQTT_DISCOVERY_PREFIX"}},
		&cli.StringFlag{Name: "mqtt-device-name", Usage: "Optional Home Assistant device name; defaults to Jinko Inverter plus device serial when available", EnvVars: []string{"MQTT_DEVICE_NAME"}},
		&cli.StringFlag{Name: "mqtt-device-id", Usage: "Optional stable Home Assistant device identifier; defaults to the inverter/logger serial when available", EnvVars: []string{"MQTT_DEVICE_ID"}},
		&cli.IntFlag{Name: "mqtt-qos", Value: 0, Usage: "MQTT QoS for discovery and state publishes: 0, 1, or 2", EnvVars: []string{"MQTT_QOS"}},
		&cli.BoolFlag{Name: "mqtt-retain", Value: true, Usage: "Retain Home Assistant discovery, availability, and state messages", EnvVars: []string{"MQTT_RETAIN"}},
		&cli.DurationFlag{Name: "mqtt-timeout", Value: 10 * time.Second, Usage: "MQTT connect and publish timeout", EnvVars: []string{"MQTT_TIMEOUT"}},
		&cli.BoolFlag{Name: "mqtt-insecure-skip-verify", Value: false, Usage: "Skip TLS certificate verification for MQTT TLS connections; insecure", EnvVars: []string{"MQTT_INSECURE_SKIP_VERIFY"}},

		&cli.BoolFlag{Name: "alerts-enabled", Value: false, Usage: "Enable outbound alert delivery", EnvVars: []string{"ALERTS_ENABLED"}},
		&cli.BoolFlag{Name: "alerts-notify-recovery", Value: false, Usage: "Send recovery notifications when alert conditions clear", EnvVars: []string{"ALERTS_NOTIFY_RECOVERY"}},
		&cli.DurationFlag{Name: "alerts-cooldown", Value: 6 * time.Hour, Usage: "Minimum interval between repeated alerts with the same key", EnvVars: []string{"ALERTS_COOLDOWN"}},
		&cli.DurationFlag{Name: "smtp-timeout", Value: 15 * time.Second, Usage: "SMTP dial/send timeout", EnvVars: []string{"SMTP_TIMEOUT"}},
		&cli.StringFlag{Name: "smtp-host", Usage: "SMTP server hostname", EnvVars: []string{"SMTP_HOST"}},
		&cli.IntFlag{Name: "smtp-port", Value: 587, Usage: "SMTP server port", EnvVars: []string{"SMTP_PORT"}},
		&cli.StringFlag{Name: "smtp-username", Usage: "SMTP username", EnvVars: []string{"SMTP_USERNAME"}},
		&cli.StringFlag{Name: "smtp-password", Usage: "SMTP password", EnvVars: []string{"SMTP_PASSWORD"}},
		&cli.StringFlag{Name: "smtp-password-file", Usage: "Read SMTP password from a file when smtp-password is empty", EnvVars: []string{"SMTP_PASSWORD_FILE"}},
		&cli.StringFlag{Name: "smtp-from-email", Usage: "Alert sender email address", EnvVars: []string{"SMTP_FROM_EMAIL"}},
		&cli.StringFlag{Name: "smtp-from-name", Usage: "Alert sender display name", EnvVars: []string{"SMTP_FROM_NAME"}},
		&cli.StringSliceFlag{Name: "smtp-to-email", Usage: "Alert recipient email address; repeat or comma-separate to add more than one", EnvVars: []string{"SMTP_TO_EMAILS"}},
		&cli.BoolFlag{Name: "smtp-use-tls", Value: false, Usage: "Use implicit TLS for SMTP connections", EnvVars: []string{"SMTP_USE_TLS"}},
		&cli.BoolFlag{Name: "smtp-starttls", Value: true, Usage: "Use STARTTLS when the SMTP server supports it", EnvVars: []string{"SMTP_STARTTLS"}},
		&cli.DurationFlag{Name: "alert-no-successful-poll-window", Value: 0, Usage: "Optional alert when no successful poll occurs within this time window; 0 disables it", EnvVars: []string{"ALERT_NO_SUCCESSFUL_POLL_WINDOW"}},
		&cli.Float64Flag{Name: "alert-grid-down-voltage-threshold", Value: 20, Usage: "Alert when all available grid phase voltages are at or below this threshold", EnvVars: []string{"ALERT_GRID_DOWN_VOLTAGE_THRESHOLD"}},
		&cli.Float64Flag{Name: "alert-battery-soc-low-threshold", Value: 0, Usage: "Optional battery SOC alert threshold in percent; must be below 100, and 0 disables it", EnvVars: []string{"ALERT_BATTERY_SOC_LOW_THRESHOLD"}},
		&cli.Float64Flag{Name: "alert-high-temperature-threshold", Value: 0, Usage: "Optional temperature alert threshold in C; 0 disables it", EnvVars: []string{"ALERT_HIGH_TEMPERATURE_THRESHOLD"}},

		&cli.StringFlag{Name: "jinko-url", Value: "https://smart-global.jinkosolar.com/device-s/device/v3/detail", Usage: "Jinko detail endpoint", EnvVars: []string{"JINKO_URL"}},
		&cli.DurationFlag{Name: "jinko-timeout", Value: 20 * time.Second, Usage: "Jinko HTTP timeout", EnvVars: []string{"JINKO_TIMEOUT"}},
		&cli.BoolFlag{Name: "jinko-insecure-skip-verify", Value: false, Usage: "Skip TLS certificate verification for Jinko HTTPS requests; insecure", EnvVars: []string{"JINKO_INSECURE_SKIP_VERIFY"}},
		&cli.IntFlag{Name: "jinko-retry-attempts", Value: 3, Usage: "Maximum Jinko detail-endpoint attempts for transient errors; rotating OAuth requests are never replayed", EnvVars: []string{"JINKO_RETRY_ATTEMPTS"}},
		&cli.DurationFlag{Name: "jinko-retry-backoff", Value: 2 * time.Second, Usage: "Initial delay between Jinko detail-endpoint retry attempts", EnvVars: []string{"JINKO_RETRY_BACKOFF"}},
		&cli.Int64Flag{Name: "jinko-device-id", Usage: "Jinko deviceId request field", EnvVars: []string{"JINKO_DEVICE_ID"}},
		&cli.Int64Flag{Name: "jinko-site-id", Usage: "Jinko siteId request field", EnvVars: []string{"JINKO_SITE_ID"}},
		&cli.StringFlag{Name: "jinko-language", Value: "en", Usage: "Jinko request language", EnvVars: []string{"JINKO_LANGUAGE"}},
		&cli.BoolFlag{Name: "jinko-need-realtime", Value: true, Usage: "Jinko needRealTimeDataFlag", EnvVars: []string{"JINKO_NEED_REALTIME_DATA"}},
		&cli.StringFlag{Name: "jinko-bearer-token", Usage: "Jinko bearer token copied from the browser session", EnvVars: []string{"JINKO_BEARER_TOKEN"}},
		&cli.StringFlag{Name: "jinko-bearer-token-file", Usage: "Read Jinko bearer token from a file when jinko-bearer-token is empty", EnvVars: []string{"JINKO_BEARER_TOKEN_FILE"}},
		&cli.StringFlag{Name: "jinko-token-url", Value: "https://smart-global.jinkosolar.com/oauth2-s/oauth/token", Usage: "Jinko OAuth token refresh endpoint", EnvVars: []string{"JINKO_TOKEN_URL"}},
		&cli.StringFlag{Name: "jinko-refresh-token", Usage: "Jinko OAuth refresh token", EnvVars: []string{"JINKO_REFRESH_TOKEN"}},
		&cli.StringFlag{Name: "jinko-refresh-token-file", Usage: "Read Jinko OAuth refresh token from a file when jinko-refresh-token is empty", EnvVars: []string{"JINKO_REFRESH_TOKEN_FILE"}},
		&cli.StringFlag{Name: "jinko-token-state-file", Usage: "Private writable file used to load and persist rotated Jinko OAuth token state; required for automatic refresh", EnvVars: []string{"JINKO_TOKEN_STATE_FILE"}},
		&cli.DurationFlag{Name: "jinko-refresh-before", Value: 5 * time.Minute, Usage: "Refresh the Jinko bearer token this long before expiry", EnvVars: []string{"JINKO_REFRESH_BEFORE"}},
		&cli.StringFlag{Name: "jinko-system", Value: "JinKO", Usage: "Jinko OAuth system form field", EnvVars: []string{"JINKO_SYSTEM"}},
		&cli.StringFlag{Name: "jinko-area", Value: "FOREIGN_1", Usage: "Jinko OAuth area form field", EnvVars: []string{"JINKO_AREA"}},
		&cli.StringFlag{Name: "jinko-origin-id", Usage: "Optional Jinko OAuth origin_id form field", EnvVars: []string{"JINKO_ORIGIN_ID"}},
		&cli.StringFlag{Name: "jinko-cookie", Usage: "Optional Jinko cookie header if bearer-only is not enough", EnvVars: []string{"JINKO_COOKIE"}},
		&cli.StringFlag{Name: "jinko-cookie-file", Usage: "Read Jinko cookie header from a file when jinko-cookie is empty", EnvVars: []string{"JINKO_COOKIE_FILE"}},
		&cli.StringFlag{Name: "jinko-user-agent", Value: "jinko-exporter/1.0", Usage: "Optional Jinko HTTP user-agent", EnvVars: []string{"JINKO_USER_AGENT"}},
		&cli.DurationFlag{Name: "jinko-request-jitter-max", Value: 5 * time.Second, Usage: "Maximum random delay added before each Jinko request", EnvVars: []string{"JINKO_REQUEST_JITTER_MAX"}},
		&cli.DurationFlag{Name: "jinko-token-alert-window", Value: 24 * time.Hour, Usage: "Send an alert when the Jinko bearer token expires within this window", EnvVars: []string{"JINKO_TOKEN_ALERT_WINDOW"}},

		&cli.StringFlag{Name: "solarman-base-url", Value: "https://globalapi.solarmanpv.com", Usage: "Solarman OpenAPI base URL", EnvVars: []string{"SOLARMAN_BASE_URL"}},
		&cli.StringFlag{Name: "solarman-api-version", Value: "v1.0", Usage: "Solarman OpenAPI version", EnvVars: []string{"SOLARMAN_API_VERSION"}},
		&cli.StringFlag{Name: "solarman-language", Value: "en", Usage: "Solarman request language", EnvVars: []string{"SOLARMAN_LANGUAGE"}},
		&cli.DurationFlag{Name: "solarman-timeout", Value: 20 * time.Second, Usage: "Solarman HTTP timeout", EnvVars: []string{"SOLARMAN_TIMEOUT"}},
		&cli.BoolFlag{Name: "solarman-insecure-skip-verify", Value: false, Usage: "Skip TLS certificate verification for Solarman HTTPS requests; insecure", EnvVars: []string{"SOLARMAN_INSECURE_SKIP_VERIFY"}},
		&cli.BoolFlag{Name: "solarman-canonical-jinko-metrics", Value: false, Usage: "Limit Solarman output to the shared Jinko metric dictionary; known points are always canonicalized and this defaults to metrics-drop-source-label when unset", EnvVars: []string{"SOLARMAN_CANONICAL_JINKO_METRICS"}},
		&cli.IntFlag{Name: "solarman-yearly-request-limit", Value: 0, Usage: "Solarman yearly API request limit used to pace requests; 0 disables pacing", EnvVars: []string{"SOLARMAN_YEARLY_REQUEST_LIMIT"}},
		&cli.DurationFlag{Name: "solarman-discovery-refresh-interval", Value: 24 * time.Hour, Usage: "How often Solarman device discovery may refresh; 0 caches discovery forever", EnvVars: []string{"SOLARMAN_DISCOVERY_REFRESH_INTERVAL"}},
		&cli.StringFlag{Name: "solarman-app-id", Usage: "Solarman OpenAPI appId", EnvVars: []string{"SOLARMAN_APP_ID"}},
		&cli.StringFlag{Name: "solarman-app-secret", Usage: "Solarman OpenAPI appSecret", EnvVars: []string{"SOLARMAN_APP_SECRET"}},
		&cli.StringFlag{Name: "solarman-app-secret-file", Usage: "Read Solarman app secret from a file when solarman-app-secret is empty", EnvVars: []string{"SOLARMAN_APP_SECRET_FILE"}},
		&cli.StringFlag{Name: "solarman-email", Usage: "Solarman account email", EnvVars: []string{"SOLARMAN_EMAIL"}},
		&cli.StringFlag{Name: "solarman-password", Usage: "Solarman account password", EnvVars: []string{"SOLARMAN_PASSWORD"}},
		&cli.StringFlag{Name: "solarman-password-file", Usage: "Read Solarman password from a file when solarman-password is empty", EnvVars: []string{"SOLARMAN_PASSWORD_FILE"}},
		&cli.StringFlag{Name: "solarman-password-sha256", Usage: "Precomputed Solarman password SHA256 hex", EnvVars: []string{"SOLARMAN_PASSWORD_SHA256"}},
		&cli.StringFlag{Name: "solarman-password-sha256-file", Usage: "Read precomputed Solarman password SHA256 hex from a file when solarman-password-sha256 is empty", EnvVars: []string{"SOLARMAN_PASSWORD_SHA256_FILE"}},
		&cli.StringFlag{Name: "solarman-device-sn", Usage: "Solarman device serial number; skips discovery when set", EnvVars: []string{"SOLARMAN_DEVICE_SN"}},
		&cli.Int64Flag{Name: "solarman-station-id", Usage: "Optional Solarman station ID for device discovery", EnvVars: []string{"SOLARMAN_STATION_ID"}},

		&cli.StringFlag{Name: "modbus-host", Usage: "Private literal IPv4 address of the Solarman V5 logger", EnvVars: []string{"MODBUS_HOST"}},
		&cli.IntFlag{Name: "modbus-port", Value: 8899, Usage: "Modbus TCP/logger port", EnvVars: []string{"MODBUS_PORT"}},
		&cli.StringFlag{Name: "modbus-logger-serial", Usage: "Logger serial needed by Solarman V5-over-TCP devices", EnvVars: []string{"MODBUS_LOGGER_SERIAL"}},
		&cli.StringFlag{Name: "modbus-device-sn", Usage: "Inverter serial used for snapshot identity; required with multiple priority sources so Prometheus device labels stay stable", EnvVars: []string{"MODBUS_DEVICE_SN"}},
		&cli.UintFlag{Name: "modbus-unit-id", Value: 1, Usage: "Modbus unit/slave ID", EnvVars: []string{"MODBUS_UNIT_ID"}},
		&cli.DurationFlag{Name: "modbus-timeout", Value: 5 * time.Second, Usage: "Modbus timeout", EnvVars: []string{"MODBUS_TIMEOUT"}},

		&cli.BoolFlag{Name: "shelly-grid-load-enabled", Value: false, Usage: "Append grid-load metrics from a Shelly Pro 3EM meter to the selected inverter source", EnvVars: []string{"SHELLY_GRID_LOAD_ENABLED"}},
		&cli.StringFlag{Name: "shelly-grid-load-url", Usage: "Shelly Pro 3EM base URL, for example http://192.0.2.50", EnvVars: []string{"SHELLY_GRID_LOAD_URL"}},
		&cli.IntFlag{Name: "shelly-grid-load-em-id", Value: 0, Usage: "Shelly EM component id used for grid-load measurements", EnvVars: []string{"SHELLY_GRID_LOAD_EM_ID"}},
		&cli.DurationFlag{Name: "shelly-grid-load-timeout", Value: 5 * time.Second, Usage: "Shelly HTTP timeout", EnvVars: []string{"SHELLY_GRID_LOAD_TIMEOUT"}},
	}
}

func FromCLI(c *cli.Context) (Config, error) {
	mqttPassword, err := secretValue(c, "mqtt-password", "mqtt-password-file")
	if err != nil {
		return Config{}, err
	}
	smtpPassword, err := secretValue(c, "smtp-password", "smtp-password-file")
	if err != nil {
		return Config{}, err
	}
	jinkoBearerToken, err := secretValue(c, "jinko-bearer-token", "jinko-bearer-token-file")
	if err != nil {
		return Config{}, err
	}
	jinkoRefreshToken, err := secretValue(c, "jinko-refresh-token", "jinko-refresh-token-file")
	if err != nil {
		return Config{}, err
	}
	jinkoCookie, err := secretValue(c, "jinko-cookie", "jinko-cookie-file")
	if err != nil {
		return Config{}, err
	}
	solarmanAppSecret, err := secretValue(c, "solarman-app-secret", "solarman-app-secret-file")
	if err != nil {
		return Config{}, err
	}
	solarmanPassword, err := secretValue(c, "solarman-password", "solarman-password-file")
	if err != nil {
		return Config{}, err
	}
	solarmanPasswordSHA256, err := secretValue(c, "solarman-password-sha256", "solarman-password-sha256-file")
	if err != nil {
		return Config{}, err
	}

	dropSourceLabel := c.Bool("metrics-drop-source-label")
	mqttQOS := c.Int("mqtt-qos")
	// Validate the CLI integer before narrowing it to the byte used by the MQTT
	// library. Otherwise negative values and values above 255 can wrap into an
	// apparently valid QoS.
	if mqttQOS < 0 || mqttQOS > 2 {
		return Config{}, fmt.Errorf("mqtt-qos must be 0, 1, or 2")
	}

	cfg := Config{
		Source:                 strings.ToLower(strings.TrimSpace(c.String("source"))),
		SourcePriority:         normalizeSourceList(c.String("source-priority")),
		ListenAddress:          c.String("listen"),
		MetricsPath:            c.String("metrics-path"),
		PollInterval:           c.Duration("poll-interval"),
		LogLevel:               c.String("log-level"),
		MetricPrefix:           strings.TrimSpace(c.String("metric-prefix")),
		DropSourceLabel:        dropSourceLabel,
		ProjectFailoverMetrics: boolWithLegacyDefault(c, "source-project-failover-metrics", dropSourceLabel),
		MQTT: MQTTConfig{
			Enabled:            c.Bool("mqtt-enabled"),
			Broker:             strings.TrimSpace(c.String("mqtt-broker")),
			ClientID:           c.String("mqtt-client-id"),
			Username:           c.String("mqtt-username"),
			Password:           mqttPassword,
			TopicPrefix:        c.String("mqtt-topic-prefix"),
			DiscoveryPrefix:    c.String("mqtt-discovery-prefix"),
			DeviceName:         c.String("mqtt-device-name"),
			DeviceID:           c.String("mqtt-device-id"),
			QOS:                byte(mqttQOS),
			Retain:             c.Bool("mqtt-retain"),
			Timeout:            c.Duration("mqtt-timeout"),
			InsecureSkipVerify: c.Bool("mqtt-insecure-skip-verify"),
		},
		Alerts: AlertConfig{
			Enabled:                  c.Bool("alerts-enabled"),
			NotifyRecovery:           c.Bool("alerts-notify-recovery"),
			Cooldown:                 c.Duration("alerts-cooldown"),
			Timeout:                  c.Duration("smtp-timeout"),
			SMTPHost:                 c.String("smtp-host"),
			SMTPPort:                 c.Int("smtp-port"),
			SMTPUsername:             c.String("smtp-username"),
			SMTPPassword:             smtpPassword,
			SMTPFromEmail:            c.String("smtp-from-email"),
			SMTPFromName:             c.String("smtp-from-name"),
			SMTPToEmails:             normalizeList(c.StringSlice("smtp-to-email")),
			SMTPUseTLS:               c.Bool("smtp-use-tls"),
			SMTPStartTLS:             c.Bool("smtp-starttls"),
			NoSuccessfulPollWindow:   c.Duration("alert-no-successful-poll-window"),
			GridDownVoltageThreshold: c.Float64("alert-grid-down-voltage-threshold"),
			BatterySOCLowThreshold:   c.Float64("alert-battery-soc-low-threshold"),
			HighTemperatureThreshold: c.Float64("alert-high-temperature-threshold"),
		},
		Jinko: JinkoConfig{
			URL:                c.String("jinko-url"),
			TokenURL:           strings.TrimSpace(c.String("jinko-token-url")),
			Timeout:            c.Duration("jinko-timeout"),
			InsecureSkipVerify: c.Bool("jinko-insecure-skip-verify"),
			RetryAttempts:      c.Int("jinko-retry-attempts"),
			RetryBackoff:       c.Duration("jinko-retry-backoff"),
			DeviceID:           c.Int64("jinko-device-id"),
			SiteID:             c.Int64("jinko-site-id"),
			Language:           c.String("jinko-language"),
			NeedRealtimeData:   c.Bool("jinko-need-realtime"),
			BearerToken:        jinkoBearerToken,
			RefreshToken:       jinkoRefreshToken,
			RefreshTokenFile:   strings.TrimSpace(c.String("jinko-refresh-token-file")),
			TokenStateFile:     strings.TrimSpace(c.String("jinko-token-state-file")),
			RefreshBefore:      c.Duration("jinko-refresh-before"),
			System:             strings.TrimSpace(c.String("jinko-system")),
			Area:               strings.TrimSpace(c.String("jinko-area")),
			OriginID:           strings.TrimSpace(c.String("jinko-origin-id")),
			Cookie:             jinkoCookie,
			UserAgent:          c.String("jinko-user-agent"),
			RequestJitterMax:   c.Duration("jinko-request-jitter-max"),
			TokenAlertWindow:   c.Duration("jinko-token-alert-window"),
		},
		Solarman: SolarmanConfig{
			BaseURL:                  c.String("solarman-base-url"),
			APIVersion:               c.String("solarman-api-version"),
			Language:                 c.String("solarman-language"),
			Timeout:                  c.Duration("solarman-timeout"),
			InsecureSkipVerify:       c.Bool("solarman-insecure-skip-verify"),
			CanonicalJinkoMetrics:    boolWithLegacyDefault(c, "solarman-canonical-jinko-metrics", dropSourceLabel),
			YearlyRequestLimit:       c.Int("solarman-yearly-request-limit"),
			DiscoveryRefreshInterval: c.Duration("solarman-discovery-refresh-interval"),
			AppID:                    c.String("solarman-app-id"),
			AppSecret:                solarmanAppSecret,
			Email:                    c.String("solarman-email"),
			Password:                 solarmanPassword,
			PasswordSHA256:           solarmanPasswordSHA256,
			DeviceSN:                 c.String("solarman-device-sn"),
			StationID:                c.Int64("solarman-station-id"),
		},
		Modbus: ModbusConfig{
			Host:         strings.TrimSpace(c.String("modbus-host")),
			Port:         c.Int("modbus-port"),
			LoggerSerial: strings.TrimSpace(c.String("modbus-logger-serial")),
			DeviceSN:     strings.TrimSpace(c.String("modbus-device-sn")),
			UnitID:       c.Uint("modbus-unit-id"),
			Timeout:      c.Duration("modbus-timeout"),
		},
		ShellyGridLoad: ShellyGridLoadConfig{
			Enabled: c.Bool("shelly-grid-load-enabled"),
			BaseURL: c.String("shelly-grid-load-url"),
			EMID:    c.Int("shelly-grid-load-em-id"),
			Timeout: c.Duration("shelly-grid-load-timeout"),
		},
	}

	if cfg.MetricPrefix == "" {
		cfg.MetricPrefix = "solar"
	}
	if len(cfg.Alerts.SMTPToEmails) == 0 && cfg.Alerts.SMTPFromEmail != "" {
		cfg.Alerts.SMTPToEmails = []string{cfg.Alerts.SMTPFromEmail}
	}
	if cfg.PollInterval <= 0 {
		return Config{}, fmt.Errorf("poll interval must be > 0")
	}
	if cfg.Source == "" {
		return Config{}, fmt.Errorf("source is required")
	}
	if len(cfg.SourcePriority) == 0 {
		cfg.SourcePriority = []string{cfg.Source}
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if err := validateListenAddress(cfg.ListenAddress); err != nil {
		return err
	}
	if err := validateMetricsPath(cfg.MetricsPath); err != nil {
		return err
	}
	if strings.Trim(strings.TrimSpace(cfg.MetricPrefix), "_") == "" {
		return fmt.Errorf("metric-prefix is required")
	}
	if !metricPrefixPattern.MatchString(cfg.MetricPrefix) {
		return fmt.Errorf("metric-prefix must match [A-Za-z_][A-Za-z0-9_]*")
	}

	if cfg.MQTT.Enabled {
		if strings.TrimSpace(cfg.MQTT.Broker) == "" {
			return fmt.Errorf("mqtt-broker is required when MQTT is enabled")
		}
		if err := validateMQTTBrokerURL(cfg.MQTT.Broker); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.MQTT.ClientID) == "" {
			return fmt.Errorf("mqtt-client-id is required when MQTT is enabled")
		}
		if _, err := NormalizeMQTTTopicPrefix(cfg.MQTT.TopicPrefix); err != nil {
			return fmt.Errorf("mqtt-topic-prefix %w", err)
		}
		if _, err := NormalizeMQTTTopicPrefix(cfg.MQTT.DiscoveryPrefix); err != nil {
			return fmt.Errorf("mqtt-discovery-prefix %w", err)
		}
		if cfg.MQTT.QOS > 2 {
			return fmt.Errorf("mqtt-qos must be 0, 1, or 2")
		}
		if cfg.MQTT.Timeout <= 0 {
			return fmt.Errorf("mqtt-timeout must be > 0 when MQTT is enabled")
		}
		if len(cfg.SourcePriority) > 1 && strings.TrimSpace(cfg.MQTT.DeviceID) == "" {
			return fmt.Errorf("mqtt-device-id is required with multiple priority sources so retained Home Assistant state keeps one stable device identity")
		}
	}

	if cfg.Alerts.Enabled {
		if strings.TrimSpace(cfg.Alerts.SMTPHost) == "" {
			return fmt.Errorf("smtp-host is required when alerts are enabled")
		}
		if cfg.Alerts.SMTPPort < 1 || cfg.Alerts.SMTPPort > 65535 {
			return fmt.Errorf("smtp-port must be between 1 and 65535 when alerts are enabled")
		}
		if strings.TrimSpace(cfg.Alerts.SMTPFromEmail) == "" {
			return fmt.Errorf("smtp-from-email is required when alerts are enabled")
		}
		if len(cfg.Alerts.SMTPToEmails) == 0 {
			return fmt.Errorf("smtp-to-email or smtp-from-email is required when alerts are enabled")
		}
		if cfg.Alerts.SMTPUseTLS && cfg.Alerts.SMTPStartTLS {
			return fmt.Errorf("smtp-use-tls and smtp-starttls cannot both be enabled")
		}
		if cfg.Alerts.Timeout <= 0 {
			return fmt.Errorf("smtp-timeout must be > 0 when alerts are enabled")
		}
		if cfg.Alerts.Cooldown <= 0 {
			return fmt.Errorf("alerts-cooldown must be > 0 when alerts are enabled")
		}
		if cfg.Alerts.NoSuccessfulPollWindow < 0 {
			return fmt.Errorf("alert-no-successful-poll-window must be >= 0")
		}
		if !isFinite(cfg.Alerts.GridDownVoltageThreshold) || cfg.Alerts.GridDownVoltageThreshold < 0 {
			return fmt.Errorf("alert-grid-down-voltage-threshold must be finite and >= 0")
		}
		if !isFinite(cfg.Alerts.BatterySOCLowThreshold) || cfg.Alerts.BatterySOCLowThreshold < 0 || cfg.Alerts.BatterySOCLowThreshold >= 100 {
			return fmt.Errorf("alert-battery-soc-low-threshold must be finite, >= 0, and < 100")
		}
		if !isFinite(cfg.Alerts.HighTemperatureThreshold) || cfg.Alerts.HighTemperatureThreshold < 0 {
			return fmt.Errorf("alert-high-temperature-threshold must be finite and >= 0")
		}
	}

	if cfg.ShellyGridLoad.Enabled {
		if strings.TrimSpace(cfg.ShellyGridLoad.BaseURL) == "" {
			return fmt.Errorf("shelly-grid-load-url is required when shelly-grid-load-enabled is set")
		}
		if err := ValidateShellyGridLoadURL(cfg.ShellyGridLoad.BaseURL); err != nil {
			return err
		}
		if cfg.ShellyGridLoad.EMID < 0 {
			return fmt.Errorf("shelly-grid-load-em-id must be >= 0")
		}
		if cfg.ShellyGridLoad.Timeout <= 0 {
			return fmt.Errorf("shelly-grid-load-timeout must be > 0 when Shelly grid-load collection is enabled")
		}
	}

	seenSources := make(map[string]struct{}, len(cfg.SourcePriority))
	for _, sourceName := range cfg.SourcePriority {
		if _, ok := seenSources[sourceName]; ok {
			return fmt.Errorf("duplicate source %q in source-priority", sourceName)
		}
		seenSources[sourceName] = struct{}{}

		if err := validateSourceConfig(cfg, sourceName); err != nil {
			return err
		}
	}
	if len(cfg.SourcePriority) > 1 {
		if _, hasModbus := seenSources["modbus"]; hasModbus && strings.TrimSpace(cfg.Modbus.DeviceSN) == "" {
			return fmt.Errorf("modbus-device-sn is required with multiple priority sources so Prometheus device_sn stays stable across failover")
		}
	}
	return nil
}

func boolWithLegacyDefault(c *cli.Context, name string, legacyDefault bool) bool {
	if c.IsSet(name) {
		return c.Bool(name)
	}
	return legacyDefault
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("listen address must be host:port or :port: %w", err)
	}
	if port == "" {
		return fmt.Errorf("listen address port is required")
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return fmt.Errorf("listen address port %q is invalid: %w", port, err)
	}
	if strings.Contains(host, "/") {
		return fmt.Errorf("listen address host %q is invalid", host)
	}
	return nil
}

func validateMetricsPath(pattern string) (err error) {
	if !strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("metrics-path must start with /")
	}
	if pattern == "/healthz" || pattern == "/readyz" {
		return fmt.Errorf("metrics-path must not collide with reserved endpoint %s", pattern)
	}

	// Go's ServeMux rejects malformed patterns by panicking. Validate with the
	// same parser at startup so bad operator input remains a normal config error.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("metrics-path is not a valid HTTP ServeMux pattern: %v", recovered)
		}
	}()
	http.NewServeMux().Handle(pattern, http.NotFoundHandler())
	if strings.ContainsAny(pattern, "{}") {
		return fmt.Errorf("metrics-path must be an exact path without ServeMux wildcards")
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// NormalizeMQTTTopicPrefix validates an operator-supplied topic prefix and
// returns the canonical slash-delimited form used by the publisher. MQTT topic
// names cannot contain filter wildcards or NUL, and empty interior levels are
// rejected so retained topics cannot silently collapse onto another prefix.
func NormalizeMQTTTopicPrefix(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", fmt.Errorf("is required when MQTT is enabled")
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("must contain valid UTF-8")
	}
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("must not contain NUL")
	}
	if strings.ContainsAny(value, "+#") {
		return "", fmt.Errorf("must not contain MQTT wildcards + or #")
	}

	parts := strings.Split(value, "/")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("must not contain empty topic levels")
		}
		parts[i] = part
	}
	return strings.Join(parts, "/"), nil
}

func validateMQTTBrokerURL(rawURL string) error {
	broker := strings.TrimSpace(rawURL)
	endpoint, err := url.Parse(broker)
	if err != nil || !endpoint.IsAbs() || endpoint.Opaque != "" {
		return fmt.Errorf("mqtt-broker must be a valid absolute URL")
	}
	switch endpoint.Scheme {
	case "tcp", "tls", "ssl":
	default:
		return fmt.Errorf("mqtt-broker scheme must be tcp, tls, or ssl")
	}
	if endpoint.User != nil {
		return fmt.Errorf("mqtt-broker URL must not contain user information; use mqtt-username and mqtt-password instead")
	}
	if endpoint.Hostname() == "" {
		return fmt.Errorf("mqtt-broker URL hostname is required")
	}
	portText := endpoint.Port()
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("mqtt-broker URL must include a port between 1 and 65535")
	}
	if endpoint.Path != "" || endpoint.RawPath != "" {
		return fmt.Errorf("mqtt-broker URL must not contain a path")
	}
	if endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(broker, "?") {
		return fmt.Errorf("mqtt-broker URL must not contain a query")
	}
	if endpoint.Fragment != "" || endpoint.RawFragment != "" || strings.Contains(broker, "#") {
		return fmt.Errorf("mqtt-broker URL must not contain a fragment")
	}
	return nil
}

// ValidateShellyGridLoadURL applies the same URL boundary at configuration
// parsing and at direct client construction. Errors intentionally never echo
// the configured value because a rejected URL may contain credentials.
func ValidateShellyGridLoadURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	endpoint, err := url.Parse(rawURL)
	if err != nil || !endpoint.IsAbs() || endpoint.Opaque != "" || endpoint.Hostname() == "" {
		return fmt.Errorf("shelly-grid-load-url must be a valid absolute HTTP or HTTPS URL")
	}
	switch strings.ToLower(endpoint.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("shelly-grid-load-url scheme must be http or https")
	}
	if endpoint.User != nil {
		return fmt.Errorf("shelly-grid-load-url must not contain user information")
	}
	if endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(rawURL, "?") {
		return fmt.Errorf("shelly-grid-load-url must not contain a query")
	}
	if endpoint.Fragment != "" || endpoint.RawFragment != "" || strings.Contains(rawURL, "#") {
		return fmt.Errorf("shelly-grid-load-url must not contain a fragment")
	}
	if strings.HasSuffix(endpoint.Host, ":") {
		return fmt.Errorf("shelly-grid-load-url contains an invalid port")
	}
	if portText := endpoint.Port(); portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("shelly-grid-load-url port must be between 1 and 65535")
		}
	}
	return nil
}

func validateSourceConfig(cfg Config, sourceName string) error {
	switch sourceName {
	case "jinko":
		if cfg.Jinko.Timeout <= 0 {
			return fmt.Errorf("jinko-timeout must be > 0")
		}
		if err := validateSecureJinkoURL(cfg.Jinko.URL); err != nil {
			return err
		}
		if cfg.Jinko.RetryAttempts <= 0 {
			return fmt.Errorf("jinko-retry-attempts must be > 0")
		}
		if cfg.Jinko.RetryBackoff < 0 {
			return fmt.Errorf("jinko-retry-backoff must be >= 0")
		}
		if cfg.Jinko.RequestJitterMax < 0 {
			return fmt.Errorf("jinko-request-jitter-max must be >= 0")
		}
		if cfg.Jinko.RequestJitterMax == time.Duration(1<<63-1) {
			return fmt.Errorf("jinko-request-jitter-max is too large")
		}
		if cfg.Jinko.TokenAlertWindow < 0 {
			return fmt.Errorf("jinko-token-alert-window must be >= 0")
		}
		if cfg.Jinko.DeviceID == 0 {
			return fmt.Errorf("jinko-device-id is required")
		}
		if cfg.Jinko.SiteID == 0 {
			return fmt.Errorf("jinko-site-id is required")
		}
		hasBearerToken := strings.TrimSpace(cfg.Jinko.BearerToken) != ""
		hasRefreshToken := strings.TrimSpace(cfg.Jinko.RefreshToken) != ""
		hasTokenStateFile := strings.TrimSpace(cfg.Jinko.TokenStateFile) != ""
		if !hasBearerToken && !hasRefreshToken && !hasTokenStateFile {
			return fmt.Errorf("jinko-bearer-token, jinko-refresh-token, or jinko-token-state-file is required")
		}
		if hasRefreshToken && !hasTokenStateFile {
			return fmt.Errorf("jinko-token-state-file is required when jinko-refresh-token is configured")
		}
		if hasTokenStateFile && strings.TrimSpace(cfg.Jinko.RefreshTokenFile) != "" && sameConfiguredFile(cfg.Jinko.TokenStateFile, cfg.Jinko.RefreshTokenFile) {
			return fmt.Errorf("jinko-token-state-file must not be the same file as jinko-refresh-token-file")
		}
		if hasTokenStateFile {
			if err := validateJinkoTokenStateFile(cfg.Jinko.TokenStateFile, hasBearerToken || hasRefreshToken); err != nil {
				return err
			}
		}
		if hasRefreshToken || hasTokenStateFile {
			if strings.TrimSpace(cfg.Jinko.TokenURL) == "" {
				return fmt.Errorf("jinko-token-url is required when Jinko token refresh is configured")
			}
			if err := validateSecureTokenURL(cfg.Jinko.TokenURL); err != nil {
				return err
			}
			if cfg.Jinko.RefreshBefore < 0 {
				return fmt.Errorf("jinko-refresh-before must be >= 0")
			}
			if strings.TrimSpace(cfg.Jinko.System) == "" {
				return fmt.Errorf("jinko-system is required when Jinko token refresh is configured")
			}
			if strings.TrimSpace(cfg.Jinko.Area) == "" {
				return fmt.Errorf("jinko-area is required when Jinko token refresh is configured")
			}
		}
	case "solarman":
		if cfg.Solarman.Timeout <= 0 {
			return fmt.Errorf("solarman-timeout must be > 0")
		}
		if err := validateSecureSolarmanBaseURL(cfg.Solarman.BaseURL); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.Solarman.APIVersion) == "" {
			return fmt.Errorf("solarman-api-version is required")
		}
		if !solarmanAPIVersionPattern.MatchString(cfg.Solarman.APIVersion) {
			return fmt.Errorf("solarman-api-version must be one safe URL path segment")
		}
		if cfg.Solarman.YearlyRequestLimit < 0 {
			return fmt.Errorf("solarman-yearly-request-limit must be >= 0")
		}
		if cfg.Solarman.DiscoveryRefreshInterval < 0 {
			return fmt.Errorf("solarman-discovery-refresh-interval must be >= 0")
		}
		if cfg.Solarman.AppID == "" || cfg.Solarman.AppSecret == "" {
			return fmt.Errorf("solarman-app-id and solarman-app-secret are required")
		}
		if cfg.Solarman.Email == "" {
			return fmt.Errorf("solarman-email is required")
		}
		if cfg.Solarman.Password == "" && cfg.Solarman.PasswordSHA256 == "" {
			return fmt.Errorf("solarman-password or solarman-password-sha256 is required")
		}
	case "modbus":
		if strings.TrimSpace(cfg.Modbus.Host) == "" {
			return fmt.Errorf("modbus-host is required")
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(cfg.Modbus.Host))
		if err != nil || !ip.Is4() {
			return fmt.Errorf("modbus-host must be a literal IPv4 address")
		}
		if !ip.IsPrivate() {
			return fmt.Errorf("modbus-host must be a private literal IPv4 address")
		}
		if cfg.Modbus.Port < 1 || cfg.Modbus.Port > 65535 {
			return fmt.Errorf("modbus-port must be between 1 and 65535")
		}
		serial, err := strconv.ParseUint(strings.TrimSpace(cfg.Modbus.LoggerSerial), 10, 32)
		if err != nil || serial == 0 {
			return fmt.Errorf("modbus-logger-serial must be a non-zero decimal uint32")
		}
		if cfg.Modbus.UnitID != 1 {
			return fmt.Errorf("modbus-unit-id must be 1 for the locked read-only Deye P3 profile")
		}
		if cfg.Modbus.Timeout <= 0 || cfg.Modbus.Timeout > 30*time.Second {
			return fmt.Errorf("modbus-timeout must be > 0 and <= 30s")
		}
		if cfg.PollInterval < time.Minute {
			return fmt.Errorf("poll-interval must be >= 1m when the modbus source is enabled")
		}
	default:
		return fmt.Errorf("unknown source %q", sourceName)
	}
	return nil
}

func validateSecureTokenURL(rawURL string) error {
	return validateSecureHTTPSURL("jinko-token-url", rawURL)
}

func validateSecureJinkoURL(rawURL string) error {
	return validateSecureHTTPSURL("jinko-url", rawURL)
}

func validateSecureSolarmanBaseURL(rawURL string) error {
	return validateSecureHTTPSURL("solarman-base-url", rawURL)
}

func validateSecureHTTPSURL(field string, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	endpoint, err := url.Parse(rawURL)
	if err != nil || !endpoint.IsAbs() || endpoint.Opaque != "" || endpoint.Hostname() == "" || !strings.EqualFold(endpoint.Scheme, "https") {
		return fmt.Errorf("%s must be an absolute HTTPS URL", field)
	}
	if endpoint.User != nil {
		return fmt.Errorf("%s must be an absolute HTTPS URL without user information", field)
	}
	if endpoint.RawQuery != "" || endpoint.ForceQuery || strings.Contains(rawURL, "?") {
		return fmt.Errorf("%s must not contain a query", field)
	}
	if endpoint.Fragment != "" || endpoint.RawFragment != "" || strings.Contains(rawURL, "#") {
		return fmt.Errorf("%s must not contain a fragment", field)
	}
	return nil
}

func validateJinkoTokenStateFile(path string, allowMissing bool) error {
	file, err := os.Open(path)
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("jinko-token-state-file must be an existing regular file when it is the only token source: %w", err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat jinko-token-state-file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("jinko-token-state-file must be a regular file when it is the only token source")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxJinkoTokenStateBytes+1))
	if err != nil {
		return fmt.Errorf("read jinko-token-state-file: %w", err)
	}
	if len(raw) > maxJinkoTokenStateBytes {
		return fmt.Errorf("jinko-token-state-file exceeds %d bytes", maxJinkoTokenStateBytes)
	}
	var state struct {
		AccessToken             string    `json:"access_token"`
		RefreshToken            string    `json:"refresh_token"`
		ExpiresAt               time.Time `json:"expires_at"`
		UpdatedAt               time.Time `json:"updated_at"`
		RefreshOutcomeUncertain bool      `json:"refresh_outcome_uncertain"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode jinko-token-state-file: %w", err)
	}
	if strings.TrimSpace(state.AccessToken) == "" && strings.TrimSpace(state.RefreshToken) == "" {
		return fmt.Errorf("jinko-token-state-file contains neither access_token nor refresh_token")
	}
	return nil
}

func sameConfiguredFile(first string, second string) bool {
	firstAbs, firstErr := filepath.Abs(filepath.Clean(strings.TrimSpace(first)))
	secondAbs, secondErr := filepath.Abs(filepath.Clean(strings.TrimSpace(second)))
	if firstErr == nil && secondErr == nil {
		if firstAbs == secondAbs || (runtime.GOOS == "windows" && strings.EqualFold(firstAbs, secondAbs)) {
			return true
		}
	}
	firstInfo, firstStatErr := os.Stat(first)
	secondInfo, secondStatErr := os.Stat(second)
	return firstStatErr == nil && secondStatErr == nil && os.SameFile(firstInfo, secondInfo)
}

func secretValue(c *cli.Context, valueName string, fileName string) (string, error) {
	value := c.String(valueName)
	if strings.TrimSpace(value) != "" {
		return value, nil
	}

	// Secret files are read once at startup; direct values intentionally win so
	// operators can override a mounted secret without changing the file.
	path := strings.TrimSpace(c.String(fileName))
	if path == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileName, err)
	}
	return strings.TrimRight(string(raw), "\r\n"), nil
}

func normalizeSourceList(value string) []string {
	values := normalizeList([]string{value})
	for i, item := range values {
		values[i] = strings.ToLower(item)
	}
	return values
}

func normalizeList(values []string) []string {
	var normalized []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				normalized = append(normalized, item)
			}
		}
	}
	return normalized
}
