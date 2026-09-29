package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
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
		HomeAssistant: HomeAssistantConfig{
			BaseURL:       "https://ha.example.test",
			Token:         "home-assistant-secret",
			NotifyService: "mobile_app_phone",
		},
		MQTT: MQTTConfig{
			Broker:   "tcp://broker.example.test:1883",
			Username: "mqtt-user",
			Password: "mqtt-secret",
		},
		Jinko: JinkoConfig{
			URL:          "https://jinko.example.test/detail",
			BearerToken:  "jinko-token",
			RefreshToken: "jinko-refresh-token",
			Cookie:       "jinko-cookie",
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
		"home-assistant-secret",
		"mqtt-secret",
		"jinko-token",
		"jinko-refresh-token",
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
	refreshTokenFile := writeSecret("jinko-refresh-token", "jinko-refresh-secret")
	t.Setenv("JINKO_REFRESH_TOKEN_FILE", refreshTokenFile)
	t.Setenv("JINKO_COOKIE_FILE", writeSecret("jinko-cookie", "jinko-cookie"))
	t.Setenv("SOLARMAN_APP_SECRET_FILE", writeSecret("solarman-secret", "solarman-secret"))
	t.Setenv("SOLARMAN_PASSWORD_SHA256_FILE", writeSecret("solarman-pass-sha", "solarman-sha"))

	cfg := mustConfigFromArgs(t,
		"--source-priority", "jinko,solarman",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-token-state-file", filepath.Join(dir, "jinko-token-state.json"),
		"--solarman-app-id", "app-id",
		"--solarman-email", "solar@example.test",
		"--solarman-device-sn", "DEVICE_SN",
		"--mqtt-enabled",
		"--mqtt-device-id", "SYNTHETIC_INVERTER_001",
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
	if cfg.Jinko.RefreshToken != "jinko-refresh-secret" {
		t.Fatalf("Jinko.RefreshToken = %q, want jinko-refresh-secret", cfg.Jinko.RefreshToken)
	}
	if cfg.Jinko.RefreshTokenFile != refreshTokenFile {
		t.Fatalf("Jinko.RefreshTokenFile = %q, want %q", cfg.Jinko.RefreshTokenFile, refreshTokenFile)
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
	refreshPath := filepath.Join(dir, "jinko-refresh-token")
	if err := os.WriteFile(refreshPath, []byte("file-refresh-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "direct-secret",
		"--jinko-bearer-token-file", path,
		"--jinko-refresh-token", "direct-refresh-secret",
		"--jinko-refresh-token-file", refreshPath,
		"--jinko-token-state-file", filepath.Join(dir, "jinko-token-state.json"),
	)

	if cfg.Jinko.BearerToken != "direct-secret" {
		t.Fatalf("Jinko.BearerToken = %q, want direct-secret", cfg.Jinko.BearerToken)
	}
	if cfg.Jinko.RefreshToken != "direct-refresh-secret" {
		t.Fatalf("Jinko.RefreshToken = %q, want direct-refresh-secret", cfg.Jinko.RefreshToken)
	}
}

func TestFromCLIValidatesMQTTBrokerURL(t *testing.T) {
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--mqtt-enabled",
	}

	valid := []string{
		"tcp://broker.example.test:1883",
		"tls://broker.example.test:8883",
		"ssl://127.0.0.1:8883",
		"tcp://[2001:db8::1]:1883",
		"TCP://broker.example.test:1883",
	}
	for _, broker := range valid {
		t.Run("accept "+broker, func(t *testing.T) {
			cfg := mustConfigFromArgs(t, append(base, "--mqtt-broker", broker)...)
			if cfg.MQTT.Broker != broker {
				t.Fatalf("MQTT.Broker = %q, want %q", cfg.MQTT.Broker, broker)
			}
		})
	}

	invalid := []struct {
		name   string
		broker string
	}{
		{name: "relative address", broker: "broker.example.test:1883"},
		{name: "unsupported HTTP scheme", broker: "http://broker.example.test:1883"},
		{name: "unsupported WebSocket scheme", broker: "ws://broker.example.test:1883"},
		{name: "missing hostname", broker: "tcp://:1883"},
		{name: "missing port", broker: "tcp://broker.example.test"},
		{name: "empty port", broker: "tcp://broker.example.test:"},
		{name: "zero port", broker: "tcp://broker.example.test:0"},
		{name: "port above uint16", broker: "tcp://broker.example.test:65536"},
		{name: "nonnumeric port", broker: "tcp://broker.example.test:mqtt"},
		{name: "root path", broker: "tcp://broker.example.test:1883/"},
		{name: "nonempty path", broker: "tcp://broker.example.test:1883/mqtt"},
		{name: "query", broker: "tcp://broker.example.test:1883?client=bridge"},
		{name: "empty query", broker: "tcp://broker.example.test:1883?"},
		{name: "fragment", broker: "tcp://broker.example.test:1883#bridge"},
		{name: "empty fragment", broker: "tcp://broker.example.test:1883#"},
	}
	for _, tt := range invalid {
		t.Run("reject "+tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(base, "--mqtt-broker", tt.broker)...)
			if err == nil || !strings.Contains(err.Error(), "mqtt-broker") {
				t.Fatalf("error = %v, want safe mqtt-broker rejection", err)
			}
		})
	}
}

func TestFromCLIConfiguresMQTTDiscoveryStateFileAndPrimarySource(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "mqtt-discovery-state.json")
	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--source-priority", "SOLARMAN,JINKO",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "opaque-jinko-token",
		"--solarman-app-id", "app-id",
		"--solarman-app-secret", "opaque-solarman-secret",
		"--solarman-email", "solar@example.test",
		"--solarman-password-sha256", "opaque-password-digest",
		"--solarman-device-sn", "SYNTHETIC_INVERTER_001",
		"--mqtt-enabled",
		"--mqtt-device-id", "SYNTHETIC_INVERTER_001",
		"--mqtt-discovery-state-file", " "+statePath+" ",
	)

	if cfg.MQTT.DiscoveryStateFile != statePath {
		t.Fatalf("MQTT.DiscoveryStateFile = %q, want %q", cfg.MQTT.DiscoveryStateFile, statePath)
	}
	if cfg.MQTT.PrimarySource != "solarman" {
		t.Fatalf("MQTT.PrimarySource = %q, want first normalized source-priority entry solarman", cfg.MQTT.PrimarySource)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("config parsing created or could stat missing discovery state file: %v", err)
	}
}

func TestFromEnvironmentConfiguresMQTTDiscoveryStateFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "mqtt-discovery-state.json")
	for key, value := range map[string]string{
		"EXPORTER_SOURCE_PRIORITY":  "jinko",
		"MQTT_ENABLED":              "true",
		"MQTT_DEVICE_ID":            "SYNTHETIC_INVERTER_001",
		"MQTT_DISCOVERY_STATE_FILE": statePath,
	} {
		t.Setenv(key, value)
	}

	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "opaque-jinko-token",
	)
	if cfg.MQTT.DiscoveryStateFile != statePath {
		t.Fatalf("MQTT.DiscoveryStateFile = %q, want %q", cfg.MQTT.DiscoveryStateFile, statePath)
	}
	if cfg.MQTT.PrimarySource != "jinko" {
		t.Fatalf("MQTT.PrimarySource = %q, want jinko", cfg.MQTT.PrimarySource)
	}
	if cfg.MQTT.DeviceID != "SYNTHETIC_INVERTER_001" {
		t.Fatalf("MQTT.DeviceID = %q, want environment value", cfg.MQTT.DeviceID)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("config parsing created or could stat missing discovery state file: %v", err)
	}
}

func TestFromCLIValidatesMQTTDiscoveryStateFileTarget(t *testing.T) {
	dir := t.TempDir()
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "opaque-jinko-token",
		"--mqtt-enabled",
	}
	tests := []struct {
		name     string
		path     string
		deviceID bool
		wantErr  string
	}{
		{
			name:    "explicit stable device ID is required",
			path:    filepath.Join(dir, "state-without-device.json"),
			wantErr: "mqtt-device-id is required when mqtt-discovery-state-file is configured",
		},
		{
			name:     "parent directory must already exist",
			path:     filepath.Join(dir, "missing-parent", "state.json"),
			deviceID: true,
			wantErr:  "mqtt-discovery-state-file parent directory must exist",
		},
		{
			name:     "existing directory is not a regular state file",
			path:     dir,
			deviceID: true,
			wantErr:  "mqtt-discovery-state-file must be a regular file when it exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string{}, base...), "--mqtt-discovery-state-file", tt.path)
			if tt.deviceID {
				args = append(args, "--mqtt-device-id", "SYNTHETIC_INVERTER_001")
			}
			err := runConfigFromArgs(t, args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestFromCLIRejectsMQTTDiscoveryStateSecretPathAliasesWithoutDisclosure(t *testing.T) {
	dir := t.TempDir()
	const stateName = "private-owned-state-sentinel.json"
	statePath := filepath.Join(dir, stateName)
	if err := os.WriteFile(statePath, []byte("opaque-file-value\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	aliasPath := dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + stateName
	if aliasPath == statePath {
		t.Fatal("test path alias unexpectedly equals the canonical path")
	}

	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "opaque-jinko-token",
		"--mqtt-enabled",
		"--mqtt-device-id", "SYNTHETIC_INVERTER_001",
		"--mqtt-discovery-state-file", statePath,
	}
	secretFileFlags := []string{
		"--mqtt-password-file",
		"--smtp-password-file",
		"--homeassistant-token-file",
		"--jinko-bearer-token-file",
		"--jinko-refresh-token-file",
		"--jinko-token-state-file",
		"--jinko-cookie-file",
		"--solarman-app-secret-file",
		"--solarman-password-file",
		"--solarman-password-sha256-file",
	}
	const wantError = "mqtt-discovery-state-file must be separate from configured secret and token-state files"
	for _, flag := range secretFileFlags {
		t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) {
			args := append(append([]string{}, base...), flag, aliasPath)
			err := runConfigFromArgs(t, args...)
			if err == nil {
				t.Fatalf("path alias through %s was accepted", flag)
			}
			if err.Error() != wantError {
				t.Fatalf("error = %q, want path-free alias rejection", err)
			}
			if strings.Contains(err.Error(), statePath) || strings.Contains(err.Error(), aliasPath) || strings.Contains(err.Error(), stateName) {
				t.Fatalf("alias rejection exposed the configured path: %q", err)
			}
		})
	}
}

func TestMQTTDiscoveryStateFileRemainsOptionalForSingleSource(t *testing.T) {
	t.Setenv("MQTT_DISCOVERY_STATE_FILE", "")
	t.Setenv("MQTT_DEVICE_ID", "")
	t.Setenv("EXPORTER_SOURCE_PRIORITY", "")
	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "opaque-jinko-token",
		"--mqtt-enabled",
	)
	if cfg.MQTT.DiscoveryStateFile != "" {
		t.Fatalf("MQTT.DiscoveryStateFile = %q, want optional empty default", cfg.MQTT.DiscoveryStateFile)
	}
	if cfg.MQTT.DeviceID != "" {
		t.Fatalf("MQTT.DeviceID = %q, want legacy serial-derived identity", cfg.MQTT.DeviceID)
	}
	if cfg.MQTT.PrimarySource != "jinko" {
		t.Fatalf("MQTT.PrimarySource = %q, want single source jinko", cfg.MQTT.PrimarySource)
	}
}

func TestFromCLIRejectsMQTTBrokerUserInfoWithoutExposingCredentials(t *testing.T) {
	const (
		sentinelUser     = "MQTT_SENTINEL_USER"
		sentinelPassword = "MQTT_SENTINEL_PASSWORD"
	)
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--mqtt-enabled",
	}
	brokers := []string{
		"tcp://" + sentinelUser + ":" + sentinelPassword + "@broker.example.test:1883",
		"tcp://" + sentinelUser + ":" + sentinelPassword + "@broker.example.test:not-a-port",
	}
	for _, broker := range brokers {
		err := runConfigFromArgs(t, append(base, "--mqtt-broker", broker)...)
		if err == nil {
			t.Fatalf("credential-bearing MQTT broker %q was accepted", broker)
		}
		errorText := err.Error()
		if strings.Contains(errorText, sentinelUser) || strings.Contains(errorText, sentinelPassword) || strings.Contains(errorText, broker) {
			t.Fatalf("MQTT broker validation error exposed credentials: %q", errorText)
		}
	}
}

func TestFromCLIValidatesMQTTQOSBeforeNarrowingToByte(t *testing.T) {
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--mqtt-enabled",
	}

	for _, qos := range []string{"-1", "3", "256", "257"} {
		t.Run(qos, func(t *testing.T) {
			err := runConfigFromArgs(t, append(base, "--mqtt-qos="+qos)...)
			if err == nil || !strings.Contains(err.Error(), "mqtt-qos must be 0, 1, or 2") {
				t.Fatalf("error = %v, want MQTT QoS range rejection", err)
			}
		})
	}

	for _, qos := range []string{"0", "1", "2"} {
		t.Run("accept "+qos, func(t *testing.T) {
			cfg := mustConfigFromArgs(t, append(base, "--mqtt-qos="+qos)...)
			want := byte(qos[0] - '0')
			if cfg.MQTT.QOS != want {
				t.Fatalf("MQTT.QOS = %d, want %d", cfg.MQTT.QOS, want)
			}
		})
	}
}

func TestFromCLIRejectsUnsafeMQTTTopicPrefixes(t *testing.T) {
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--mqtt-enabled",
	}
	tests := []struct {
		name  string
		flag  string
		value string
	}{
		{name: "topic wildcard plus", flag: "--mqtt-topic-prefix", value: "bridge/+/state"},
		{name: "topic wildcard hash", flag: "--mqtt-topic-prefix", value: "bridge/#"},
		{name: "topic NUL", flag: "--mqtt-topic-prefix", value: "bridge/\x00/state"},
		{name: "topic empty interior level", flag: "--mqtt-topic-prefix", value: "bridge//state"},
		{name: "topic whitespace-only level", flag: "--mqtt-topic-prefix", value: "bridge/ /state"},
		{name: "discovery wildcard", flag: "--mqtt-discovery-prefix", value: "homeassistant/#"},
		{name: "discovery empty interior level", flag: "--mqtt-discovery-prefix", value: "homeassistant//sensor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(base, tt.flag, tt.value)...)
			if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(tt.flag, "--")) {
				t.Fatalf("error = %v, want %s rejection", err, tt.flag)
			}
		})
	}
}

func TestSecureUpstreamURLValidationDoesNotExposeRejectedValues(t *testing.T) {
	const sentinel = "UPSTREAM_URL_SENTINEL"
	validators := []struct {
		name     string
		validate func(string) error
	}{
		{name: "jinko detail", validate: validateSecureJinkoURL},
		{name: "jinko token", validate: validateSecureTokenURL},
		{name: "solarman base", validate: validateSecureSolarmanBaseURL},
	}
	invalidURLs := []string{
		"https://" + sentinel + ":password@example.test/path",
		"https://" + sentinel + ":bad%zz@example.test/path",
		"https://example.test/path?token=" + sentinel,
		"https://example.test/path?",
		"https://example.test/path#" + sentinel,
		"https://example.test/path#",
	}

	for _, validator := range validators {
		t.Run(validator.name, func(t *testing.T) {
			for _, rawURL := range invalidURLs {
				err := validator.validate(rawURL)
				if err == nil {
					t.Fatalf("validator accepted %q", rawURL)
				}
				if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), rawURL) {
					t.Fatalf("validation error exposed rejected URL data: %q", err)
				}
			}
			if err := validator.validate("https://example.test/safe/path"); err != nil {
				t.Fatalf("validator rejected a safe HTTPS URL: %v", err)
			}
		})
	}
}

func TestFromCLIJinkoTokenModes(t *testing.T) {
	t.Run("legacy bearer only keeps refresh defaults", func(t *testing.T) {
		cfg := mustConfigFromArgs(t,
			"--source", "jinko",
			"--jinko-device-id", "100",
			"--jinko-site-id", "200",
			"--jinko-bearer-token", "access-token",
		)

		if cfg.Jinko.TokenURL != "https://smart-global.jinkosolar.com/oauth2-s/oauth/token" {
			t.Fatalf("Jinko.TokenURL = %q", cfg.Jinko.TokenURL)
		}
		if cfg.Jinko.RefreshBefore != 5*time.Minute {
			t.Fatalf("Jinko.RefreshBefore = %s, want 5m", cfg.Jinko.RefreshBefore)
		}
		if cfg.Jinko.System != "JinKO" {
			t.Fatalf("Jinko.System = %q, want JinKO", cfg.Jinko.System)
		}
		if cfg.Jinko.Area != "FOREIGN_1" {
			t.Fatalf("Jinko.Area = %q, want FOREIGN_1", cfg.Jinko.Area)
		}
	})

	t.Run("refresh token can bootstrap without bearer", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "new-token-state.json")
		cfg := mustConfigFromArgs(t,
			"--source", "jinko",
			"--jinko-device-id", "100",
			"--jinko-site-id", "200",
			"--jinko-refresh-token", "refresh-token",
			"--jinko-token-state-file", statePath,
			"--jinko-token-url", "https://auth.example.test/oauth/token",
			"--jinko-refresh-before", "2m",
			"--jinko-system", "custom-system",
			"--jinko-area", "custom-area",
			"--jinko-origin-id", "origin-123",
		)

		if cfg.Jinko.BearerToken != "" || cfg.Jinko.RefreshToken != "refresh-token" {
			t.Fatalf("Jinko bearer/refresh token = %q/%q", cfg.Jinko.BearerToken, cfg.Jinko.RefreshToken)
		}
		if cfg.Jinko.TokenURL != "https://auth.example.test/oauth/token" {
			t.Fatalf("Jinko.TokenURL = %q", cfg.Jinko.TokenURL)
		}
		if cfg.Jinko.TokenStateFile != statePath {
			t.Fatalf("Jinko.TokenStateFile = %q, want %q", cfg.Jinko.TokenStateFile, statePath)
		}
		if cfg.Jinko.RefreshBefore != 2*time.Minute {
			t.Fatalf("Jinko.RefreshBefore = %s, want 2m", cfg.Jinko.RefreshBefore)
		}
		if cfg.Jinko.System != "custom-system" || cfg.Jinko.Area != "custom-area" || cfg.Jinko.OriginID != "origin-123" {
			t.Fatalf("Jinko refresh form fields = %q/%q/%q", cfg.Jinko.System, cfg.Jinko.Area, cfg.Jinko.OriginID)
		}
	})

	t.Run("persisted access token can bootstrap without inline secrets", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "jinko-token-state.json")
		if err := os.WriteFile(statePath, []byte(`{"access_token":"state-access"}`+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		cfg := mustConfigFromArgs(t,
			"--source", "jinko",
			"--jinko-device-id", "100",
			"--jinko-site-id", "200",
			"--jinko-token-state-file", statePath,
		)

		if cfg.Jinko.TokenStateFile != statePath {
			t.Fatalf("Jinko.TokenStateFile = %q, want %q", cfg.Jinko.TokenStateFile, statePath)
		}
	})

	t.Run("persisted refresh token can bootstrap without inline secrets", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "jinko-token-state.json")
		if err := os.WriteFile(statePath, []byte(`{"refresh_token":"state-refresh"}`+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		cfg := mustConfigFromArgs(t,
			"--source", "jinko",
			"--jinko-device-id", "100",
			"--jinko-site-id", "200",
			"--jinko-token-state-file", statePath,
		)
		if cfg.Jinko.TokenStateFile != statePath {
			t.Fatalf("Jinko.TokenStateFile = %q, want %q", cfg.Jinko.TokenStateFile, statePath)
		}
	})
}

func TestFromCLIRejectsInvalidJinkoTokenModes(t *testing.T) {
	baseArgs := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
	}
	missingStatePath := filepath.Join(t.TempDir(), "missing-token-state.json")
	stateDirectory := t.TempDir()
	invalidStateDir := t.TempDir()
	emptyStatePath := filepath.Join(invalidStateDir, "empty.json")
	malformedStatePath := filepath.Join(invalidStateDir, "malformed.json")
	invalidTimestampStatePath := filepath.Join(invalidStateDir, "invalid-timestamp.json")
	invalidUncertaintyStatePath := filepath.Join(invalidStateDir, "invalid-uncertainty.json")
	if err := os.WriteFile(emptyStatePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(empty state) error = %v", err)
	}
	if err := os.WriteFile(malformedStatePath, []byte("{\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(malformed state) error = %v", err)
	}
	if err := os.WriteFile(invalidTimestampStatePath, []byte(`{"access_token":"bearer-token","expires_at":"not-a-time"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(invalid timestamp state) error = %v", err)
	}
	if err := os.WriteFile(invalidUncertaintyStatePath, []byte(`{"access_token":"bearer-token","refresh_outcome_uncertain":"not-a-bool"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(invalid uncertainty state) error = %v", err)
	}
	newStatePath := filepath.Join(t.TempDir(), "new-token-state.json")
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "no token source",
			wantErr: "jinko-bearer-token, jinko-refresh-token, or jinko-token-state-file is required",
		},
		{
			name:    "detail endpoint must use TLS",
			args:    []string{"--jinko-bearer-token", "bearer-token", "--jinko-url", "http://jinko.example.test/detail"},
			wantErr: "jinko-url must be an absolute HTTPS URL",
		},
		{
			name:    "sole state file is missing",
			args:    []string{"--jinko-token-state-file", missingStatePath},
			wantErr: "jinko-token-state-file must be an existing regular file",
		},
		{
			name:    "sole state path is a directory",
			args:    []string{"--jinko-token-state-file", stateDirectory},
			wantErr: "jinko-token-state-file must be a regular file",
		},
		{
			name:    "sole state file is empty",
			args:    []string{"--jinko-token-state-file", emptyStatePath},
			wantErr: "contains neither access_token nor refresh_token",
		},
		{
			name:    "sole state file is malformed",
			args:    []string{"--jinko-token-state-file", malformedStatePath},
			wantErr: "decode jinko-token-state-file",
		},
		{
			name:    "existing malformed state is not overwritten by a bearer bootstrap",
			args:    []string{"--jinko-bearer-token", "bearer-token", "--jinko-token-state-file", malformedStatePath},
			wantErr: "decode jinko-token-state-file",
		},
		{
			name:    "state file rejects malformed optional timestamps",
			args:    []string{"--jinko-token-state-file", invalidTimestampStatePath},
			wantErr: "decode jinko-token-state-file",
		},
		{
			name:    "state file rejects malformed uncertainty marker",
			args:    []string{"--jinko-token-state-file", invalidUncertaintyStatePath},
			wantErr: "decode jinko-token-state-file",
		},
		{
			name:    "existing directory is not accepted as a state target with refresh bootstrap",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", stateDirectory},
			wantErr: "jinko-token-state-file must be a regular file",
		},
		{
			name:    "state file cannot alias refresh bootstrap file",
			args:    []string{"--jinko-refresh-token-file", malformedStatePath, "--jinko-token-state-file", malformedStatePath},
			wantErr: "must not be the same file",
		},
		{
			name:    "refresh token requires durable state",
			args:    []string{"--jinko-refresh-token", "refresh-token"},
			wantErr: "jinko-token-state-file is required",
		},
		{
			name:    "refresh endpoint empty",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-token-url="},
			wantErr: "jinko-token-url is required",
		},
		{
			name:    "refresh endpoint must use TLS",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-token-url", "http://auth.example.test/oauth/token"},
			wantErr: "absolute HTTPS URL",
		},
		{
			name:    "refresh endpoint rejects embedded credentials",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-token-url", "https://user:password@auth.example.test/oauth/token"},
			wantErr: "without user information",
		},
		{
			name:    "negative token alert window",
			args:    []string{"--jinko-token-alert-window", "-1s"},
			wantErr: "jinko-token-alert-window must be >= 0",
		},
		{
			name:    "negative refresh window",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-refresh-before", "-1s"},
			wantErr: "jinko-refresh-before must be >= 0",
		},
		{
			name:    "refresh system empty",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-system="},
			wantErr: "jinko-system is required",
		},
		{
			name:    "refresh area empty",
			args:    []string{"--jinko-refresh-token", "refresh-token", "--jinko-token-state-file", newStatePath, "--jinko-area="},
			wantErr: "jinko-area is required",
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

func TestFromCLIRejectsUnsafeJinkoRequestJitter(t *testing.T) {
	baseArgs := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
	}
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "negative", value: "-1ns", want: "must be >= 0"},
		{name: "overflowing inclusive random bound", value: time.Duration(1<<63 - 1).String(), want: "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(baseArgs, "--jinko-request-jitter-max", tt.value)...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFromCLIRejectsUnsafeSolarmanConfiguration(t *testing.T) {
	baseArgs := []string{
		"--source", "solarman",
		"--solarman-app-id", "app-id",
		"--solarman-app-secret", "secret",
		"--solarman-email", "solar@example.test",
		"--solarman-password-sha256", "sha",
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "zero timeout", args: []string{"--solarman-timeout=0s"}, wantErr: "solarman-timeout must be > 0"},
		{name: "base URL requires TLS", args: []string{"--solarman-base-url", "http://solarman.example.test"}, wantErr: "absolute HTTPS URL"},
		{name: "base URL rejects credentials", args: []string{"--solarman-base-url", "https://user:password@solarman.example.test"}, wantErr: "without user information"},
		{name: "API version required", args: []string{"--solarman-api-version="}, wantErr: "solarman-api-version is required"},
		{name: "API version rejects slash", args: []string{"--solarman-api-version", "v1/metrics"}, wantErr: "safe URL path segment"},
		{name: "API version rejects query", args: []string{"--solarman-api-version", "v1?admin=true"}, wantErr: "safe URL path segment"},
		{name: "API version rejects fragment", args: []string{"--solarman-api-version", "v1#fragment"}, wantErr: "safe URL path segment"},
		{name: "API version rejects dot traversal", args: []string{"--solarman-api-version", "../v2"}, wantErr: "safe URL path segment"},
		{name: "API version rejects encoded traversal", args: []string{"--solarman-api-version", "v1%2F..%2Fv2"}, wantErr: "safe URL path segment"},
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
		"--shelly-grid-load-url", "http://192.0.2.50",
		"--shelly-grid-load-em-id", "1",
		"--shelly-grid-load-timeout", "3s",
	)

	if !cfg.ShellyGridLoad.Enabled {
		t.Fatal("ShellyGridLoad.Enabled = false, want true")
	}
	if cfg.ShellyGridLoad.BaseURL != "http://192.0.2.50" {
		t.Fatalf("ShellyGridLoad.BaseURL = %q", cfg.ShellyGridLoad.BaseURL)
	}
	if cfg.ShellyGridLoad.EMID != 1 {
		t.Fatalf("ShellyGridLoad.EMID = %d, want 1", cfg.ShellyGridLoad.EMID)
	}
	if cfg.ShellyGridLoad.Timeout != 3*time.Second {
		t.Fatalf("ShellyGridLoad.Timeout = %s, want 3s", cfg.ShellyGridLoad.Timeout)
	}
}

func TestFromCLIRejectsUnsafeShellyGridLoadURLWithoutExposingSecrets(t *testing.T) {
	const sentinel = "SHELLY_CONFIG_SENTINEL"
	base := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--shelly-grid-load-enabled",
	}
	urls := []string{
		"ftp://shelly.example.test",
		"http://" + sentinel + ":password@shelly.example.test",
		"http://shelly.example.test?token=" + sentinel,
		"http://shelly.example.test#" + sentinel,
		"http://shelly.example.test:65536",
	}

	for _, rawURL := range urls {
		t.Run(rawURL, func(t *testing.T) {
			err := runConfigFromArgs(t, append(base, "--shelly-grid-load-url", rawURL)...)
			if err == nil || !strings.Contains(err.Error(), "shelly-grid-load-url") {
				t.Fatalf("error = %v, want strict Shelly URL rejection", err)
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("Shelly URL validation error exposed a secret: %q", err)
			}
		})
	}
}

func TestFromCLISecretFileReadError(t *testing.T) {
	tests := []struct {
		name     string
		fileFlag string
	}{
		{name: "bearer token", fileFlag: "--jinko-bearer-token-file"},
		{name: "refresh token", fileFlag: "--jinko-refresh-token-file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(
				t,
				"--source", "jinko",
				"--jinko-device-id", "100",
				"--jinko-site-id", "200",
				tt.fileFlag, filepath.Join(t.TempDir(), "missing"),
			)
			want := "read " + strings.TrimPrefix(tt.fileFlag, "--")
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestFromCLIModbusLockedReadOnlyProfile(t *testing.T) {
	cfg := mustConfigFromArgs(t,
		"--source", "modbus",
		"--modbus-host", " 192.168.50.25 ",
		"--modbus-port", "8899",
		"--modbus-logger-serial", " 305419896 ",
		"--modbus-device-sn", " INVERTER_SN_EXAMPLE ",
		"--modbus-unit-id", "1",
		"--modbus-timeout", "5s",
	)

	if cfg.Modbus.Host != "192.168.50.25" || cfg.Modbus.Port != 8899 {
		t.Fatalf("Modbus endpoint = %q:%d", cfg.Modbus.Host, cfg.Modbus.Port)
	}
	if cfg.Modbus.LoggerSerial != "305419896" || cfg.Modbus.DeviceSN != "INVERTER_SN_EXAMPLE" {
		t.Fatalf("Modbus identities = %q/%q", cfg.Modbus.LoggerSerial, cfg.Modbus.DeviceSN)
	}
	if cfg.Modbus.UnitID != 1 || cfg.Modbus.Timeout != 5*time.Second {
		t.Fatalf("Modbus unit/timeout = %d/%s", cfg.Modbus.UnitID, cfg.Modbus.Timeout)
	}
}

func TestFromCLIRejectsUnsafeModbusConfiguration(t *testing.T) {
	base := []string{
		"--source", "modbus",
		"--modbus-host", "192.168.50.25",
		"--modbus-logger-serial", "305419896",
		"--poll-interval", "1m",
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing host", args: []string{"--modbus-host="}, wantErr: "modbus-host is required"},
		{name: "hostname would use DNS", args: []string{"--modbus-host", "logger.local"}, wantErr: "literal IPv4"},
		{name: "IPv6 is not part of first profile", args: []string{"--modbus-host", "::1"}, wantErr: "literal IPv4"},
		{name: "IPv4-mapped IPv6", args: []string{"--modbus-host", "::ffff:192.168.50.25"}, wantErr: "literal IPv4"},
		{name: "public IPv4", args: []string{"--modbus-host", "1.1.1.1"}, wantErr: "private literal IPv4"},
		{name: "zero port", args: []string{"--modbus-port", "0"}, wantErr: "modbus-port"},
		{name: "large port", args: []string{"--modbus-port", "65536"}, wantErr: "modbus-port"},
		{name: "missing logger serial", args: []string{"--modbus-logger-serial="}, wantErr: "modbus-logger-serial"},
		{name: "negative logger serial", args: []string{"--modbus-logger-serial", "-1"}, wantErr: "modbus-logger-serial"},
		{name: "overflow logger serial", args: []string{"--modbus-logger-serial", "4294967296"}, wantErr: "modbus-logger-serial"},
		{name: "different unit", args: []string{"--modbus-unit-id", "2"}, wantErr: "modbus-unit-id must be 1"},
		{name: "zero timeout", args: []string{"--modbus-timeout", "0s"}, wantErr: "modbus-timeout"},
		{name: "excessive timeout", args: []string{"--modbus-timeout", "31s"}, wantErr: "modbus-timeout"},
		{name: "aggressive poll", args: []string{"--poll-interval", "59s"}, wantErr: "poll-interval must be >= 1m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(base, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestFromCLIAcceptsModbusJinkoSolarmanPriorityInExactOrder(t *testing.T) {
	base := []string{
		"--source-priority", "modbus,jinko,solarman",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--solarman-app-id", "app-id",
		"--solarman-app-secret", "secret",
		"--solarman-email", "solar@example.test",
		"--solarman-password-sha256", "sha256-password",
		"--solarman-device-sn", "INVERTER_SN_EXAMPLE",
		"--modbus-host", "192.168.50.25",
		"--modbus-logger-serial", "305419896",
		"--poll-interval", "1m",
		"--modbus-device-sn", "INVERTER_SN_EXAMPLE",
	}
	if err := runConfigFromArgs(t, base[:len(base)-2]...); err == nil || !strings.Contains(err.Error(), "modbus-device-sn is required with multiple priority sources") {
		t.Fatalf("error = %v, want stable mixed-source Prometheus identity rejection", err)
	}
	cfg := mustConfigFromArgs(t, base...)
	want := []string{"modbus", "jinko", "solarman"}
	if len(cfg.SourcePriority) != len(want) {
		t.Fatalf("SourcePriority = %#v, want %#v", cfg.SourcePriority, want)
	}
	for i := range want {
		if cfg.SourcePriority[i] != want[i] {
			t.Fatalf("SourcePriority = %#v, want %#v", cfg.SourcePriority, want)
		}
	}

	t.Run("MQTT requires an explicit stable mixed-source device ID", func(t *testing.T) {
		mqttArgs := append(append([]string{}, base...),
			"--mqtt-enabled",
			"--mqtt-broker", "tcp://mqtt.example.test:1883",
			"--mqtt-client-id", "mixed-source-test",
		)
		err := runConfigFromArgs(t, mqttArgs...)
		if err == nil || !strings.Contains(err.Error(), "mqtt-device-id is required with multiple priority sources") {
			t.Fatalf("error = %v, want stable mixed-source MQTT identity rejection", err)
		}
		cfg := mustConfigFromArgs(t, append(mqttArgs, "--mqtt-device-id", "SYNTHETIC_INVERTER_001")...)
		if cfg.MQTT.DeviceID != "SYNTHETIC_INVERTER_001" {
			t.Fatalf("MQTT.DeviceID = %q", cfg.MQTT.DeviceID)
		}
	})
}

func TestFromCLIReadsExactMixedPriorityFromEnvironment(t *testing.T) {
	for key, value := range map[string]string{
		"EXPORTER_SOURCE_PRIORITY": "modbus,jinko,solarman",
		"EXPORTER_POLL_INTERVAL":   "60s",
		"MODBUS_HOST":              "192.168.50.25",
		"MODBUS_LOGGER_SERIAL":     "305419896",
		"MODBUS_DEVICE_SN":         "INVERTER_SN_EXAMPLE",
		"JINKO_DEVICE_ID":          "100",
		"JINKO_SITE_ID":            "200",
		"JINKO_BEARER_TOKEN":       "opaque-test-token",
		"SOLARMAN_APP_ID":          "app-id",
		"SOLARMAN_APP_SECRET":      "secret",
		"SOLARMAN_EMAIL":           "solar@example.test",
		"SOLARMAN_PASSWORD_SHA256": "sha256-password",
		"SOLARMAN_DEVICE_SN":       "INVERTER_SN_EXAMPLE",
	} {
		t.Setenv(key, value)
	}

	cfg := mustConfigFromArgs(t)
	want := []string{"modbus", "jinko", "solarman"}
	if len(cfg.SourcePriority) != len(want) {
		t.Fatalf("SourcePriority = %#v, want %#v", cfg.SourcePriority, want)
	}
	for i := range want {
		if cfg.SourcePriority[i] != want[i] {
			t.Fatalf("SourcePriority = %#v, want %#v", cfg.SourcePriority, want)
		}
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
			name:    "metrics path collides with health",
			args:    []string{"--metrics-path", "/healthz"},
			wantErr: "reserved endpoint /healthz",
		},
		{
			name:    "metrics path collides with readiness",
			args:    []string{"--metrics-path", "/readyz"},
			wantErr: "reserved endpoint /readyz",
		},
		{
			name:    "metrics path cannot be a wildcard",
			args:    []string{"--metrics-path", "/{metrics}"},
			wantErr: "exact path without ServeMux wildcards",
		},
		{
			name:    "malformed ServeMux metrics path",
			args:    []string{"--metrics-path", "/{"},
			wantErr: "valid HTTP ServeMux pattern",
		},
		{
			name:    "empty metric prefix",
			args:    []string{"--metric-prefix", "___"},
			wantErr: "metric-prefix is required",
		},
		{
			name:    "metric prefix cannot start with a digit",
			args:    []string{"--metric-prefix", "1solar"},
			wantErr: "metric-prefix must match",
		},
		{
			name:    "metric prefix rejects hyphen",
			args:    []string{"--metric-prefix", "bad-prefix"},
			wantErr: "metric-prefix must match",
		},
		{
			name:    "metric prefix rejects colon",
			args:    []string{"--metric-prefix", "bad:prefix"},
			wantErr: "metric-prefix must match",
		},
		{
			name:    "metric prefix rejects whitespace",
			args:    []string{"--metric-prefix", "bad prefix"},
			wantErr: "metric-prefix must match",
		},
		{
			name:    "metric prefix rejects control characters",
			args:    []string{"--metric-prefix", "bad\nprefix"},
			wantErr: "metric-prefix must match",
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

func TestFromCLIRejectsUnsafeAlertConfiguration(t *testing.T) {
	baseArgs := []string{
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--alerts-enabled",
		"--smtp-host", "smtp.example.test",
		"--smtp-from-email", "alerts@example.test",
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "SMTP port above uint16", args: []string{"--smtp-port", "65536"}, wantErr: "between 1 and 65535"},
		{name: "grid threshold NaN", args: []string{"--alert-grid-down-voltage-threshold", "NaN"}, wantErr: "finite and >= 0"},
		{name: "grid threshold infinity", args: []string{"--alert-grid-down-voltage-threshold", "+Inf"}, wantErr: "finite and >= 0"},
		{name: "battery threshold NaN", args: []string{"--alert-battery-soc-low-threshold", "NaN"}, wantErr: "finite, >= 0, and < 100"},
		{name: "battery threshold infinity", args: []string{"--alert-battery-soc-low-threshold", "+Inf"}, wantErr: "finite, >= 0, and < 100"},
		{name: "battery threshold cannot recover at 100", args: []string{"--alert-battery-soc-low-threshold", "100"}, wantErr: "finite, >= 0, and < 100"},
		{name: "temperature threshold NaN", args: []string{"--alert-high-temperature-threshold", "NaN"}, wantErr: "finite and >= 0"},
		{name: "temperature threshold infinity", args: []string{"--alert-high-temperature-threshold", "+Inf"}, wantErr: "finite and >= 0"},
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

func TestFromCLIModbusAlertCorrelationConfig(t *testing.T) {
	cfg := mustConfigFromArgs(t, validModbusAlertCorrelationArgs()...)
	if !cfg.ModbusAlertCorrelation.Enabled {
		t.Fatal("ModbusAlertCorrelation.Enabled = false, want true")
	}
	if cfg.ModbusAlertCorrelation.Cooldown != 6*time.Hour || cfg.ModbusAlertCorrelation.JobTimeout != 45*time.Second {
		t.Fatalf("Modbus correlation timing = %s/%s", cfg.ModbusAlertCorrelation.Cooldown, cfg.ModbusAlertCorrelation.JobTimeout)
	}
	if cfg.HomeAssistant.BaseURL != "https://ha.example.test" {
		t.Fatalf("HomeAssistant.BaseURL = %q", cfg.HomeAssistant.BaseURL)
	}
	if cfg.HomeAssistant.Token != "ha-token" {
		t.Fatalf("HomeAssistant.Token = %q, want parsed token", cfg.HomeAssistant.Token)
	}
	if cfg.HomeAssistant.NotifyService != "mobile_app_operator_phone" {
		t.Fatalf("HomeAssistant.NotifyService = %q, want normalized service segment", cfg.HomeAssistant.NotifyService)
	}
	if cfg.HomeAssistant.Timeout != 10*time.Second {
		t.Fatalf("HomeAssistant.Timeout = %s", cfg.HomeAssistant.Timeout)
	}
}

func TestFromCLIAllowsExplicitInternalHTTPHomeAssistant(t *testing.T) {
	for _, baseURL := range []string{
		"http://192.168.100.2:8123",
		"http://127.0.0.1:8123",
		"http://[::1]:8123",
		"http://homeassistant:8123",
		"http://ha-core:8123",
	} {
		t.Run(baseURL, func(t *testing.T) {
			args := append(validModbusAlertCorrelationArgs(),
				"--homeassistant-url", baseURL,
				"--homeassistant-allow-insecure-http",
			)
			cfg := mustConfigFromArgs(t, args...)
			if !cfg.HomeAssistant.AllowInsecureHTTP {
				t.Fatal("HomeAssistant.AllowInsecureHTTP = false")
			}
		})
	}
}

func TestFromCLIRejectsUnsafeModbusAlertCorrelationConfig(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "modbus not first", args: []string{"--source-priority", "jinko,modbus,solarman"}, wantErr: "must start with modbus"},
		{name: "solarman absent", args: []string{"--source-priority", "modbus,jinko"}, wantErr: "must contain jinko and solarman"},
		{name: "zero cooldown", args: []string{"--modbus-alert-correlation-cooldown", "0s"}, wantErr: "cooldown must be > 0"},
		{name: "zero job timeout", args: []string{"--modbus-alert-correlation-job-timeout", "0s"}, wantErr: "job-timeout must be > 0"},
		{name: "http not opted in", args: []string{"--homeassistant-url", "http://192.168.100.2:8123"}, wantErr: "must use HTTPS"},
		{name: "public http IP", args: []string{"--homeassistant-url", "http://203.0.113.20:8123", "--homeassistant-allow-insecure-http"}, wantErr: "private/loopback IP"},
		{name: "http FQDN", args: []string{"--homeassistant-url", "http://ha.internal.example:8123", "--homeassistant-allow-insecure-http"}, wantErr: "single-label internal hostname"},
		{name: "URL user info", args: []string{"--homeassistant-url", "https://user:secret@ha.example.test"}, wantErr: "must not contain user information"},
		{name: "URL path", args: []string{"--homeassistant-url", "https://ha.example.test/api"}, wantErr: "must not contain a path"},
		{name: "URL query", args: []string{"--homeassistant-url", "https://ha.example.test?token=secret"}, wantErr: "must not contain a query"},
		{name: "URL fragment", args: []string{"--homeassistant-url", "https://ha.example.test#secret"}, wantErr: "must not contain a fragment"},
		{name: "generic notify service", args: []string{"--jinko-ha-notify-service", "notify.persistent_notification"}, wantErr: "must be mobile_app_*"},
		{name: "unsafe service path", args: []string{"--jinko-ha-notify-service", "mobile_app_phone/../../restart"}, wantErr: "must be mobile_app_*"},
		{name: "zero HA timeout", args: []string{"--homeassistant-timeout", "0s"}, wantErr: "homeassistant-timeout must be > 0"},
		{name: "empty token", args: []string{"--homeassistant-token", ""}, wantErr: "must contain one non-empty ASCII bearer token"},
		{name: "token control", args: []string{"--homeassistant-token", "secret\nvalue"}, wantErr: "must contain one non-empty ASCII bearer token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(validModbusAlertCorrelationArgs(), tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestFromCLIHomeAssistantTokenFileIsExclusiveBoundedAndRedacted(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "ha-token")
	if err := os.WriteFile(tokenPath, []byte("file-ha-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}

	args := validModbusAlertCorrelationArgs()
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--homeassistant-token" {
			args = append(args[:i], args[i+2:]...)
			break
		}
	}
	cfg := mustConfigFromArgs(t, append(args, "--homeassistant-token-file", tokenPath)...)
	if cfg.HomeAssistant.Token != "file-ha-token" {
		t.Fatalf("HomeAssistant.Token = %q", cfg.HomeAssistant.Token)
	}
	redacted, err := json.Marshal(cfg.Redacted())
	if err != nil {
		t.Fatalf("Marshal(Redacted()) error = %v", err)
	}
	if strings.Contains(string(redacted), "file-ha-token") {
		t.Fatalf("redacted config exposed Home Assistant token: %s", redacted)
	}

	err = runConfigFromArgs(t, append(validModbusAlertCorrelationArgs(), "--homeassistant-token-file", tokenPath)...)
	if err == nil || !strings.Contains(err.Error(), "cannot both be configured") {
		t.Fatalf("direct plus file error = %v", err)
	}

	sentinel := "PRIVATE_PATH_SENTINEL"
	badPath := filepath.Join(dir, sentinel)
	if err := os.WriteFile(badPath, make([]byte, maxHomeAssistantTokenBytes+1), 0o600); err != nil {
		t.Fatalf("WriteFile(oversized) error = %v", err)
	}
	err = runConfigFromArgs(t, "--homeassistant-token-file", badPath)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized token-file error = %v", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("token-file error exposed path: %v", err)
	}

	err = runConfigFromArgs(t, "--homeassistant-token-file", dir)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory token-file error = %v", err)
	}
}

func TestFromCLIRejectsHomeAssistantTokenFileSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "token-link")
	if err := os.WriteFile(target, []byte("PRIVATE_TOKEN_SENTINEL\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable in this test environment: %v", err)
	}
	err := runConfigFromArgs(t, "--homeassistant-token-file", link)
	if err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("symlink token-file error = %v", err)
	}
	if strings.Contains(err.Error(), target) || strings.Contains(err.Error(), link) || strings.Contains(err.Error(), "PRIVATE_TOKEN_SENTINEL") {
		t.Fatalf("symlink rejection exposed private input: %v", err)
	}
}

func TestFromCLIHomeAssistantSettingsRequireExplicitOptIn(t *testing.T) {
	cfg := mustConfigFromArgs(t,
		"--source", "jinko",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "token",
		"--homeassistant-url", "https://ha.example.test",
		"--homeassistant-token", "ha-token",
		"--jinko-ha-notify-service", "mobile_app_phone",
	)
	if cfg.ModbusAlertCorrelation.Enabled {
		t.Fatal("Modbus correlation enabled without explicit opt-in")
	}
}

func validModbusAlertCorrelationArgs() []string {
	return []string{
		"--source-priority", "modbus,jinko,solarman",
		"--poll-interval", "1m",
		"--modbus-host", "192.168.50.25",
		"--modbus-logger-serial", "305419896",
		"--modbus-device-sn", "INVERTER_SN_EXAMPLE",
		"--jinko-device-id", "100",
		"--jinko-site-id", "200",
		"--jinko-bearer-token", "jinko-token",
		"--solarman-app-id", "app-id",
		"--solarman-app-secret", "app-secret",
		"--solarman-email", "solar@example.test",
		"--solarman-password-sha256", "password-sha",
		"--solarman-device-sn", "INVERTER_SN_EXAMPLE",
		"--modbus-alert-correlation-enabled",
		"--homeassistant-url", "https://ha.example.test",
		"--homeassistant-token", "ha-token",
		"--jinko-ha-notify-service", "notify.mobile_app_operator_phone",
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
	app := &cli.Command{
		Name:  "test",
		Flags: Flags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			cfg, err := FromCLI(cmd)
			if err != nil {
				return err
			}
			if capture != nil {
				capture(cfg)
			}
			return nil
		},
	}
	return app.Run(context.Background(), append([]string{"test"}, args...))
}
