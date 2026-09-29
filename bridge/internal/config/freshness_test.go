package config

import (
	"strings"
	"testing"
	"time"
)

func TestCloudMaxDataAgeConfiguration(t *testing.T) {
	base := []string{
		"--source-priority", "jinko,solarman",
		"--jinko-device-id", "100", "--jinko-site-id", "200",
		"--jinko-bearer-token", "synthetic-token",
		"--solarman-app-id", "app-id", "--solarman-app-secret", "synthetic-secret",
		"--solarman-email", "operator@example.test", "--solarman-password", "synthetic-password",
	}

	t.Run("safe defaults", func(t *testing.T) {
		t.Setenv("JINKO_MAX_DATA_AGE", "")
		t.Setenv("SOLARMAN_MAX_DATA_AGE", "")
		cfg := mustConfigFromArgs(t, base...)
		if cfg.Jinko.MaxDataAge != 15*time.Minute || cfg.Solarman.MaxDataAge != 15*time.Minute {
			t.Fatalf("default data ages = %s/%s, want 15m/15m", cfg.Jinko.MaxDataAge, cfg.Solarman.MaxDataAge)
		}
	})
	t.Run("environment and CLI precedence", func(t *testing.T) {
		t.Setenv("JINKO_MAX_DATA_AGE", "10m")
		t.Setenv("SOLARMAN_MAX_DATA_AGE", "20m")
		cfg := mustConfigFromArgs(t, base...)
		if cfg.Jinko.MaxDataAge != 10*time.Minute || cfg.Solarman.MaxDataAge != 20*time.Minute {
			t.Fatalf("environment data ages = %s/%s", cfg.Jinko.MaxDataAge, cfg.Solarman.MaxDataAge)
		}
		args := append(append([]string(nil), base...), "--jinko-max-data-age", "5m", "--solarman-max-data-age", "7m")
		cfg = mustConfigFromArgs(t, args...)
		if cfg.Jinko.MaxDataAge != 5*time.Minute || cfg.Solarman.MaxDataAge != 7*time.Minute {
			t.Fatalf("CLI data ages = %s/%s", cfg.Jinko.MaxDataAge, cfg.Solarman.MaxDataAge)
		}
	})
	for _, flag := range []string{"jinko-max-data-age", "solarman-max-data-age"} {
		for _, value := range []string{"0s", "-1s"} {
			t.Run(flag+"/"+value, func(t *testing.T) {
				args := append(append([]string(nil), base...), "--"+flag, value)
				err := runConfigFromArgs(t, args...)
				if err == nil || !strings.Contains(err.Error(), flag+" must be > 0") {
					t.Fatalf("error = %v, want positive age validation for %s", err, flag)
				}
			})
		}
	}
}
