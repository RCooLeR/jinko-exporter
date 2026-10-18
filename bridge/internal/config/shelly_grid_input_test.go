package config

import (
	"strings"
	"testing"
	"time"
)

func TestShellyGridInputDefaultsDisabledAndIndependent(t *testing.T) {
	t.Setenv("SHELLY_GRID_INPUT_ENABLED", "false")
	t.Setenv("SHELLY_GRID_INPUT_URL", "")
	t.Setenv("SHELLY_GRID_INPUT_EM_ID", "0")
	t.Setenv("SHELLY_GRID_INPUT_TIMEOUT", "5s")
	cfg := mustConfigFromArgs(t, gridInputSourceArgs()...)
	if cfg.ShellyGridInput.Enabled || cfg.ShellyGridInput.BaseURL != "" || cfg.ShellyGridInput.EMID != 0 || cfg.ShellyGridInput.Timeout != 5*time.Second {
		t.Fatalf("unexpected upstream defaults: %+v", cfg.ShellyGridInput)
	}
}

func TestShellyGridInputCLIAndEnvironmentStaySeparateFromGridLoad(t *testing.T) {
	t.Setenv("SHELLY_GRID_INPUT_ENABLED", "true")
	t.Setenv("SHELLY_GRID_INPUT_URL", "https://input.example.test")
	t.Setenv("SHELLY_GRID_INPUT_EM_ID", "2")
	t.Setenv("SHELLY_GRID_INPUT_TIMEOUT", "4s")
	args := append(gridInputSourceArgs(),
		"--shelly-grid-load-enabled", "--shelly-grid-load-url", "http://load.example.test",
		"--shelly-grid-load-em-id", "1", "--shelly-grid-load-timeout", "3s")
	cfg := mustConfigFromArgs(t, args...)
	wantInput := ShellyGridInputConfig{Enabled: true, BaseURL: "https://input.example.test", EMID: 2, Timeout: 4 * time.Second}
	wantLoad := ShellyGridLoadConfig{Enabled: true, BaseURL: "http://load.example.test", EMID: 1, Timeout: 3 * time.Second}
	if cfg.ShellyGridInput != wantInput || cfg.ShellyGridLoad != wantLoad {
		t.Fatalf("meter settings crossed: input=%+v load=%+v", cfg.ShellyGridInput, cfg.ShellyGridLoad)
	}
	cfg = mustConfigFromArgs(t, append(args, "--shelly-grid-input-enabled=false", "--shelly-grid-input-url", "http://192.0.2.60", "--shelly-grid-input-em-id", "4", "--shelly-grid-input-timeout", "2s")...)
	if cfg.ShellyGridInput.Enabled || cfg.ShellyGridInput.BaseURL != "http://192.0.2.60" || cfg.ShellyGridInput.EMID != 4 || cfg.ShellyGridInput.Timeout != 2*time.Second || cfg.ShellyGridLoad != wantLoad {
		t.Fatalf("explicit upstream flags did not remain independent: input=%+v load=%+v", cfg.ShellyGridInput, cfg.ShellyGridLoad)
	}
}

func TestShellyGridInputValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing URL", nil, "shelly-grid-input-url is required"},
		{"negative ID", []string{"--shelly-grid-input-url", "http://input.example.test", "--shelly-grid-input-em-id", "-1"}, "shelly-grid-input-em-id must be >= 0"},
		{"zero timeout", []string{"--shelly-grid-input-url", "http://input.example.test", "--shelly-grid-input-timeout", "0s"}, "shelly-grid-input-timeout must be > 0"},
		{"MQTT identity", []string{"--shelly-grid-input-url", "http://input.example.test", "--mqtt-enabled", "--mqtt-device-id", ""}, "mqtt-device-id is required with Shelly grid-input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runConfigFromArgs(t, append(append(gridInputSourceArgs(), "--shelly-grid-input-enabled"), tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
		})
	}
}

func TestShellyGridInputRejectsUnsafeURLsWithoutEchoingSecrets(t *testing.T) {
	const secret = "INPUT_SECRET_MUST_NOT_APPEAR"
	for _, raw := range []string{
		"ftp://input.example.test", "http://" + secret + ":password@input.example.test",
		"http://input.example.test?token=" + secret, "http://input.example.test#" + secret,
		"http://input.example.test:65536", "http://input.example.test:", "relative/path",
	} {
		err := runConfigFromArgs(t, append(gridInputSourceArgs(), "--shelly-grid-input-enabled", "--shelly-grid-input-url", raw)...)
		if err == nil || !strings.Contains(err.Error(), "shelly-grid-input-url") || strings.Contains(err.Error(), secret) {
			t.Fatalf("unsafe upstream URL accepted or exposed: %v", err)
		}
	}
}

func gridInputSourceArgs() []string {
	return []string{"--source", "jinko", "--jinko-device-id", "100", "--jinko-site-id", "200", "--jinko-bearer-token", "synthetic-token"}
}
