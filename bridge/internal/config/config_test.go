package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v2"
)

func TestRedactedMasksSecrets(t *testing.T) {
	cfg := Config{
		Source:        "jinko",
		ListenAddress: ":9876",
		MetricPrefix:  "solar",
		Alerts: AlertConfig{
			SMTPHost:     "smtp.example.test",
			SMTPUsername: "alerts@example.test",
			SMTPPassword: "smtp-secret",
		},
		MQTT: MQTTConfig{
			Broker:   "tcp://broker.example.test:1883",
			Username: "mqtt-user",
			Password: "mqtt-secret",
		},
		Jinko: JinkoConfig{
			URL:         "https://jinko.example.test/detail",
			BearerToken: "jinko-token",
			Cookie:      "jinko-cookie",
		},
		Solarman: SolarmanConfig{
			BaseURL:        "https://solarman.example.test",
			AppID:          "app-id",
			AppSecret:      "solarman-secret",
			Email:          "solar@example.test",
			Password:       "solarman-password",
			PasswordSHA256: "solarman-password-sha",
		},
	}

	raw, err := json.Marshal(cfg.Redacted())
	if err != nil {
		t.Fatalf("Marshal(Redacted()) error = %v", err)
	}
	text := string(raw)

	for _, secret := range []string{
		"smtp-secret",
		"mqtt-secret",
		"jinko-token",
		"jinko-cookie",
		"solarman-secret",
		"solarman-password",
		"solarman-password-sha",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted config contains secret %q in %s", secret, text)
		}
	}

	for _, expected := range []string{":9876", "solar", "smtp.example.test", "mqtt-user", "app-id"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("redacted config lost non-secret value %q in %s", expected, text)
		}
	}
}

func TestFromCLIReadsSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	writeSecret := func(name string, value string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
		return path
	}

	t.Setenv("MQTT_PASSWORD_FILE", writeSecret("mqtt", "mqtt-secret"))
	t.Setenv("SMTP_PASSWORD_FILE", writeSecret("smtp", "smtp-secret"))
	t.Setenv("JINKO_BEARER_TOKEN_FILE", writeSecret("jinko-token", "jinko-secret"))
	t.Setenv("JINKO_COOKIE_FILE", writeSecret("jinko-cookie", "jinko-cookie"))
	t.Setenv("SOLARMAN_APP_SECRET_FILE", writeSecret("solarman-secret", "solarman-secret"))
	t.Setenv("SOLARMAN_PASSWORD_SHA256_FILE", writeSecret("solarman-pass-sha", "solarman-sha"))

	cfg := mustConfigFromArgs(t,
		"--source-priority", "jinko,solarman",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--solarman-app-id", "app-id",
		"--solarman-email", "solar@example.test",
		"--solarman-device-sn", "DEVICE_SN",
		"--mqtt-enabled",
		"--alerts-enabled",
		"--smtp-host", "smtp.example.test",
		"--smtp-from-email", "alerts@example.test",
	)

	if cfg.MQTT.Password != "mqtt-secret" {
		t.Fatalf("MQTT.Password = %q, want mqtt-secret", cfg.MQTT.Password)
	}
	if cfg.Alerts.SMTPPassword != "smtp-secret" {
		t.Fatalf("Alerts.SMTPPassword = %q, want smtp-secret", cfg.Alerts.SMTPPassword)
	}
	if cfg.Jinko.BearerToken != "jinko-secret" {
		t.Fatalf("Jinko.BearerToken = %q, want jinko-secret", cfg.Jinko.BearerToken)
	}
	if cfg.Jinko.Cookie != "jinko-cookie" {
		t.Fatalf("Jinko.Cookie = %q, want jinko-cookie", cfg.Jinko.Cookie)
	}
	if cfg.Solarman.AppSecret != "solarman-secret" {
		t.Fatalf("Solarman.AppSecret = %q, want solarman-secret", cfg.Solarman.AppSecret)
	}
	if cfg.Solarman.PasswordSHA256 != "solarman-sha" {
		t.Fatalf("Solarman.PasswordSHA256 = %q, want solarman-sha", cfg.Solarman.PasswordSHA256)
	}
}

func TestFromCLIDirectSecretValueTakesPrecedenceOverFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jinko-token")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "direct-secret",
		"--jinko-bearer-token-file", path,
	)

	if cfg.Jinko.BearerToken != "direct-secret" {
		t.Fatalf("Jinko.BearerToken = %q, want direct-secret", cfg.Jinko.BearerToken)
	}
}

func TestFromCLIIndependentFailoverMetricFlagsKeepLegacyDefaults(t *testing.T) {
	baseArgs := []string{
		"--source-priority", "jinko,solarman",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--solarman-app-id", "app-id",
		"--solarman-app-secret", "secret",
		"--solarman-email", "solar@example.test",
		"--solarman-password-sha256", "sha",
	}

	tests := []struct {
		name                string
		args                []string
		wantDropSourceLabel bool
		wantProjectFailover bool
		wantCanonical       bool
	}{
		{
			name:                "default legacy false",
			wantDropSourceLabel: false,
			wantProjectFailover: false,
			wantCanonical:       false,
		},
		{
			name:                "legacy drop source label keeps coupled defaults",
			args:                []string{"--metrics-drop-source-label"},
			wantDropSourceLabel: true,
			wantProjectFailover: true,
			wantCanonical:       true,
		},
		{
			name:                "explicit flags can enable behavior independently",
			args:                []string{"--source-project-failover-metrics", "--solarman-canonical-jinko-metrics"},
			wantDropSourceLabel: false,
			wantProjectFailover: true,
			wantCanonical:       true,
		},
		{
			name:                "explicit flags override legacy true",
			args:                []string{"--metrics-drop-source-label", "--source-project-failover-metrics=false", "--solarman-canonical-jinko-metrics=false"},
			wantDropSourceLabel: true,
			wantProjectFailover: false,
			wantCanonical:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustConfigFromArgs(t, append(baseArgs, tt.args...)...)

			if cfg.DropSourceLabel != tt.wantDropSourceLabel {
				t.Fatalf("DropSourceLabel = %v, want %v", cfg.DropSourceLabel, tt.wantDropSourceLabel)
			}
			if cfg.ProjectFailoverMetrics != tt.wantProjectFailover {
				t.Fatalf("ProjectFailoverMetrics = %v, want %v", cfg.ProjectFailoverMetrics, tt.wantProjectFailover)
			}
			if cfg.Solarman.CanonicalJinkoMetrics != tt.wantCanonical {
				t.Fatalf("Solarman.CanonicalJinkoMetrics = %v, want %v", cfg.Solarman.CanonicalJinkoMetrics, tt.wantCanonical)
			}
		})
	}
}

func TestFromCLIShellyGridLoadConfig(t *testing.T) {
	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--shelly-grid-load-enabled",
		"--shelly-grid-load-url", "http://192.168.120.50",
		"--shelly-grid-load-em-id", "1",
		"--shelly-grid-load-timeout", "3s",
	)

	if !cfg.ShellyGridLoad.Enabled {
		t.Fatal("ShellyGridLoad.Enabled = false, want true")
	}
	if cfg.ShellyGridLoad.BaseURL != "http://192.168.120.50" {
		t.Fatalf("ShellyGridLoad.BaseURL = %q", cfg.ShellyGridLoad.BaseURL)
	}
	if cfg.ShellyGridLoad.EMID != 1 {
		t.Fatalf("ShellyGridLoad.EMID = %d, want 1", cfg.ShellyGridLoad.EMID)
	}
	if cfg.ShellyGridLoad.Timeout != 3*time.Second {
		t.Fatalf("ShellyGridLoad.Timeout = %s, want 3s", cfg.ShellyGridLoad.Timeout)
	}
}

func TestFromCLISecretFileReadError(t *testing.T) {
	err := runConfigFromArgs(
		t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token-file", filepath.Join(t.TempDir(), "missing"),
	)
	if err == nil || !strings.Contains(err.Error(), "read jinko-bearer-token-file") {
		t.Fatalf("error = %v, want file read error", err)
	}
}

func TestFromCLIRejectsInvalidServerConfig(t *testing.T) {
	baseArgs := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "bad listen address",
			args:    []string{"--listen", "9876"},
			wantErr: "listen address",
		},
		{
			name:    "bad metrics path",
			args:    []string{"--metrics-path", "metrics"},
			wantErr: "metrics-path must start with /",
		},
		{
			name:    "empty metric prefix",
			args:    []string{"--metric-prefix", "___"},
			wantErr: "metric-prefix is required",
		},
		{
			name:    "shelly enabled without url",
			args:    []string{"--shelly-grid-load-enabled"},
			wantErr: "shelly-grid-load-url is required",
		},
		{
			name:    "bad shelly timeout",
			args:    []string{"--shelly-grid-load-enabled", "--shelly-grid-load-url", "http://shelly.test", "--shelly-grid-load-timeout", "0s"},
			wantErr: "shelly-grid-load-timeout must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(baseArgs, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func mustConfigFromArgs(t *testing.T, args ...string) Config {
	t.Helper()
	var cfg Config
	err := runConfigApp(t, append(args, "--poll-interval", time.Minute.String()), func(parsed Config) {
		cfg = parsed
	})
	if err != nil {
		t.Fatalf("config FromCLI error = %v", err)
	}
	return cfg
}

func runConfigFromArgs(t *testing.T, args ...string) error {
	t.Helper()
	return runConfigApp(t, args, nil)
}

func runConfigApp(t *testing.T, args []string, capture func(Config)) error {
	t.Helper()
	app := &cli.App{
		Name:  "test",
		Flags: Flags(),
		Action: func(ctx *cli.Context) error {
			cfg, err := FromCLI(ctx)
			if err != nil {
				return err
			}
			if capture != nil {
				capture(cfg)
			}
			return nil
		},
	}
	return app.Run(append([]string{"test"}, args...))
}
