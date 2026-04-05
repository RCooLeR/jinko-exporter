package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
)

type Config struct {
	Source        string
	ListenAddress string
	MetricsPath   string
	PollInterval  time.Duration
	LogLevel      string
	MetricPrefix  string
	Alerts        AlertConfig
	Jinko         JinkoConfig
	Solarman      SolarmanConfig
	Modbus        ModbusConfig
}

type AlertConfig struct {
	Enabled                  bool
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

type JinkoConfig struct {
	URL              string
	Timeout          time.Duration
	DeviceID         int64
	SiteID           int64
	Language         string
	NeedRealtimeData bool
	BearerToken      string
	Cookie           string
	UserAgent        string
	RequestJitterMax time.Duration
	TokenAlertWindow time.Duration
}

type SolarmanConfig struct {
	BaseURL        string
	APIVersion     string
	Language       string
	Timeout        time.Duration
	AppID          string
	AppSecret      string
	Email          string
	Password       string
	PasswordSHA256 string
	DeviceSN       string
	StationID      int64
}

type ModbusConfig struct {
	Host         string
	Port         int
	LoggerSerial string
	UnitID       uint
	Timeout      time.Duration
}

func Flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "source", Value: "jinko", Usage: "Data source: jinko, solarman, modbus", EnvVars: []string{"EXPORTER_SOURCE"}},
		&cli.StringFlag{Name: "listen", Value: ":9876", Usage: "HTTP listen address", EnvVars: []string{"EXPORTER_LISTEN"}},
		&cli.StringFlag{Name: "metrics-path", Value: "/metrics", Usage: "Prometheus metrics path", EnvVars: []string{"EXPORTER_METRICS_PATH"}},
		&cli.DurationFlag{Name: "poll-interval", Value: 60 * time.Second, Usage: "Polling interval", EnvVars: []string{"EXPORTER_POLL_INTERVAL"}},
		&cli.StringFlag{Name: "log-level", Value: "info", Usage: "zerolog level", EnvVars: []string{"EXPORTER_LOG_LEVEL"}},
		&cli.StringFlag{Name: "metric-prefix", Value: "solar", Usage: "Metric name prefix", EnvVars: []string{"EXPORTER_METRIC_PREFIX"}},
		&cli.BoolFlag{Name: "alerts-enabled", Value: false, Usage: "Enable outbound alert delivery", EnvVars: []string{"ALERTS_ENABLED"}},
		&cli.DurationFlag{Name: "alerts-cooldown", Value: 6 * time.Hour, Usage: "Minimum interval between repeated alerts with the same key", EnvVars: []string{"ALERTS_COOLDOWN"}},
		&cli.DurationFlag{Name: "smtp-timeout", Value: 15 * time.Second, Usage: "SMTP dial/send timeout", EnvVars: []string{"SMTP_TIMEOUT"}},
		&cli.StringFlag{Name: "smtp-host", Usage: "SMTP server hostname", EnvVars: []string{"SMTP_HOST"}},
		&cli.IntFlag{Name: "smtp-port", Value: 587, Usage: "SMTP server port", EnvVars: []string{"SMTP_PORT"}},
		&cli.StringFlag{Name: "smtp-username", Usage: "SMTP username", EnvVars: []string{"SMTP_USERNAME"}},
		&cli.StringFlag{Name: "smtp-password", Usage: "SMTP password", EnvVars: []string{"SMTP_PASSWORD"}},
		&cli.StringFlag{Name: "smtp-from-email", Usage: "Alert sender email address", EnvVars: []string{"SMTP_FROM_EMAIL"}},
		&cli.StringFlag{Name: "smtp-from-name", Usage: "Alert sender display name", EnvVars: []string{"SMTP_FROM_NAME"}},
		&cli.StringSliceFlag{Name: "smtp-to-email", Usage: "Alert recipient email address; repeat or comma-separate to add more than one", EnvVars: []string{"SMTP_TO_EMAILS"}},
		&cli.BoolFlag{Name: "smtp-use-tls", Value: false, Usage: "Use implicit TLS for SMTP connections", EnvVars: []string{"SMTP_USE_TLS"}},
		&cli.BoolFlag{Name: "smtp-starttls", Value: true, Usage: "Use STARTTLS when the SMTP server supports it", EnvVars: []string{"SMTP_STARTTLS"}},
		&cli.DurationFlag{Name: "alert-no-successful-poll-window", Value: 0, Usage: "Optional alert when no successful poll occurs within this time window; 0 disables it", EnvVars: []string{"ALERT_NO_SUCCESSFUL_POLL_WINDOW"}},
		&cli.Float64Flag{Name: "alert-grid-down-voltage-threshold", Value: 20, Usage: "Alert when all available grid phase voltages are at or below this threshold", EnvVars: []string{"ALERT_GRID_DOWN_VOLTAGE_THRESHOLD"}},
		&cli.Float64Flag{Name: "alert-battery-soc-low-threshold", Value: 0, Usage: "Optional battery SOC alert threshold in percent; 0 disables it", EnvVars: []string{"ALERT_BATTERY_SOC_LOW_THRESHOLD"}},
		&cli.Float64Flag{Name: "alert-high-temperature-threshold", Value: 0, Usage: "Optional temperature alert threshold in C; 0 disables it", EnvVars: []string{"ALERT_HIGH_TEMPERATURE_THRESHOLD"}},

		&cli.StringFlag{Name: "jinko-url", Value: "https://smart-global.jinkosolar.com/device-s/device/v3/detail", Usage: "Jinko detail endpoint", EnvVars: []string{"JINKO_URL"}},
		&cli.DurationFlag{Name: "jinko-timeout", Value: 20 * time.Second, Usage: "Jinko HTTP timeout", EnvVars: []string{"JINKO_TIMEOUT"}},
		&cli.Int64Flag{Name: "jinko-device-id", Usage: "Jinko deviceId request field", EnvVars: []string{"JINKO_DEVICE_ID"}},
		&cli.Int64Flag{Name: "jinko-site-id", Usage: "Jinko siteId request field", EnvVars: []string{"JINKO_SITE_ID"}},
		&cli.StringFlag{Name: "jinko-language", Value: "en", Usage: "Jinko request language", EnvVars: []string{"JINKO_LANGUAGE"}},
		&cli.BoolFlag{Name: "jinko-need-realtime", Value: true, Usage: "Jinko needRealTimeDataFlag", EnvVars: []string{"JINKO_NEED_REALTIME_DATA"}},
		&cli.StringFlag{Name: "jinko-bearer-token", Usage: "Jinko bearer token copied from the browser session", EnvVars: []string{"JINKO_BEARER_TOKEN"}},
		&cli.StringFlag{Name: "jinko-cookie", Usage: "Optional Jinko cookie header if bearer-only is not enough", EnvVars: []string{"JINKO_COOKIE"}},
		&cli.StringFlag{Name: "jinko-user-agent", Value: "jinko-exporter/1.0", Usage: "Optional Jinko HTTP user-agent", EnvVars: []string{"JINKO_USER_AGENT"}},
		&cli.DurationFlag{Name: "jinko-request-jitter-max", Value: 5 * time.Second, Usage: "Maximum random delay added before each Jinko request", EnvVars: []string{"JINKO_REQUEST_JITTER_MAX"}},
		&cli.DurationFlag{Name: "jinko-token-alert-window", Value: 24 * time.Hour, Usage: "Send an alert when the Jinko bearer token expires within this window", EnvVars: []string{"JINKO_TOKEN_ALERT_WINDOW"}},

		&cli.StringFlag{Name: "solarman-base-url", Value: "https://globalapi.solarmanpv.com", Usage: "Solarman OpenAPI base URL", EnvVars: []string{"SOLARMAN_BASE_URL"}},
		&cli.StringFlag{Name: "solarman-api-version", Value: "v1.0", Usage: "Solarman OpenAPI version", EnvVars: []string{"SOLARMAN_API_VERSION"}},
		&cli.StringFlag{Name: "solarman-language", Value: "en", Usage: "Solarman request language", EnvVars: []string{"SOLARMAN_LANGUAGE"}},
		&cli.DurationFlag{Name: "solarman-timeout", Value: 20 * time.Second, Usage: "Solarman HTTP timeout", EnvVars: []string{"SOLARMAN_TIMEOUT"}},
		&cli.StringFlag{Name: "solarman-app-id", Usage: "Solarman OpenAPI appId", EnvVars: []string{"SOLARMAN_APP_ID"}},
		&cli.StringFlag{Name: "solarman-app-secret", Usage: "Solarman OpenAPI appSecret", EnvVars: []string{"SOLARMAN_APP_SECRET"}},
		&cli.StringFlag{Name: "solarman-email", Usage: "Solarman account email", EnvVars: []string{"SOLARMAN_EMAIL"}},
		&cli.StringFlag{Name: "solarman-password", Usage: "Solarman account password", EnvVars: []string{"SOLARMAN_PASSWORD"}},
		&cli.StringFlag{Name: "solarman-password-sha256", Usage: "Precomputed Solarman password SHA256 hex", EnvVars: []string{"SOLARMAN_PASSWORD_SHA256"}},
		&cli.StringFlag{Name: "solarman-device-sn", Usage: "Solarman device serial number; skips discovery when set", EnvVars: []string{"SOLARMAN_DEVICE_SN"}},
		&cli.Int64Flag{Name: "solarman-station-id", Usage: "Optional Solarman station ID for device discovery", EnvVars: []string{"SOLARMAN_STATION_ID"}},

		&cli.StringFlag{Name: "modbus-host", Usage: "Modbus logger/inverter host", EnvVars: []string{"MODBUS_HOST"}},
		&cli.IntFlag{Name: "modbus-port", Value: 8899, Usage: "Modbus TCP/logger port", EnvVars: []string{"MODBUS_PORT"}},
		&cli.StringFlag{Name: "modbus-logger-serial", Usage: "Logger serial needed by Solarman V5-over-TCP devices", EnvVars: []string{"MODBUS_LOGGER_SERIAL"}},
		&cli.UintFlag{Name: "modbus-unit-id", Value: 1, Usage: "Modbus unit/slave ID", EnvVars: []string{"MODBUS_UNIT_ID"}},
		&cli.DurationFlag{Name: "modbus-timeout", Value: 5 * time.Second, Usage: "Modbus timeout", EnvVars: []string{"MODBUS_TIMEOUT"}},
	}
}

func FromCLI(c *cli.Context) (Config, error) {
	cfg := Config{
		Source:        strings.ToLower(strings.TrimSpace(c.String("source"))),
		ListenAddress: c.String("listen"),
		MetricsPath:   c.String("metrics-path"),
		PollInterval:  c.Duration("poll-interval"),
		LogLevel:      c.String("log-level"),
		MetricPrefix:  strings.TrimSpace(c.String("metric-prefix")),
		Alerts: AlertConfig{
			Enabled:                  c.Bool("alerts-enabled"),
			Cooldown:                 c.Duration("alerts-cooldown"),
			Timeout:                  c.Duration("smtp-timeout"),
			SMTPHost:                 c.String("smtp-host"),
			SMTPPort:                 c.Int("smtp-port"),
			SMTPUsername:             c.String("smtp-username"),
			SMTPPassword:             c.String("smtp-password"),
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
			URL:              c.String("jinko-url"),
			Timeout:          c.Duration("jinko-timeout"),
			DeviceID:         c.Int64("jinko-device-id"),
			SiteID:           c.Int64("jinko-site-id"),
			Language:         c.String("jinko-language"),
			NeedRealtimeData: c.Bool("jinko-need-realtime"),
			BearerToken:      c.String("jinko-bearer-token"),
			Cookie:           c.String("jinko-cookie"),
			UserAgent:        c.String("jinko-user-agent"),
			RequestJitterMax: c.Duration("jinko-request-jitter-max"),
			TokenAlertWindow: c.Duration("jinko-token-alert-window"),
		},
		Solarman: SolarmanConfig{
			BaseURL:        c.String("solarman-base-url"),
			APIVersion:     c.String("solarman-api-version"),
			Language:       c.String("solarman-language"),
			Timeout:        c.Duration("solarman-timeout"),
			AppID:          c.String("solarman-app-id"),
			AppSecret:      c.String("solarman-app-secret"),
			Email:          c.String("solarman-email"),
			Password:       c.String("solarman-password"),
			PasswordSHA256: c.String("solarman-password-sha256"),
			DeviceSN:       c.String("solarman-device-sn"),
			StationID:      c.Int64("solarman-station-id"),
		},
		Modbus: ModbusConfig{
			Host:         c.String("modbus-host"),
			Port:         c.Int("modbus-port"),
			LoggerSerial: c.String("modbus-logger-serial"),
			UnitID:       c.Uint("modbus-unit-id"),
			Timeout:      c.Duration("modbus-timeout"),
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
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if cfg.Alerts.Enabled {
		if strings.TrimSpace(cfg.Alerts.SMTPHost) == "" {
			return fmt.Errorf("smtp-host is required when alerts are enabled")
		}
		if cfg.Alerts.SMTPPort <= 0 {
			return fmt.Errorf("smtp-port must be > 0 when alerts are enabled")
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
		if cfg.Alerts.GridDownVoltageThreshold < 0 {
			return fmt.Errorf("alert-grid-down-voltage-threshold must be >= 0")
		}
		if cfg.Alerts.BatterySOCLowThreshold < 0 || cfg.Alerts.BatterySOCLowThreshold > 100 {
			return fmt.Errorf("alert-battery-soc-low-threshold must be between 0 and 100")
		}
		if cfg.Alerts.HighTemperatureThreshold < 0 {
			return fmt.Errorf("alert-high-temperature-threshold must be >= 0")
		}
	}

	switch cfg.Source {
	case "jinko":
		if cfg.Jinko.DeviceID == 0 {
			return fmt.Errorf("jinko-device-id is required")
		}
		if cfg.Jinko.SiteID == 0 {
			return fmt.Errorf("jinko-site-id is required")
		}
		if strings.TrimSpace(cfg.Jinko.BearerToken) == "" {
			return fmt.Errorf("jinko-bearer-token is required")
		}
	case "solarman":
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
		if cfg.Modbus.Host == "" {
			return fmt.Errorf("modbus-host is required")
		}
	default:
		return fmt.Errorf("unknown source %q", cfg.Source)
	}
	return nil
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
