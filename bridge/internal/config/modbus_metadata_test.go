package config

import (
	"os"
	"testing"
)

func TestModbusMetadataDefaultAndExplicitOverrides(t *testing.T) {
	base := []string{"--source", "modbus", "--modbus-host", "192.168.50.25", "--modbus-logger-serial", "305419896"}
	t.Setenv("MODBUS_METADATA_ENABLED", "")
	// Unset (rather than empty) is required to exercise the CLI default;
	// urfave/cli treats an explicitly empty boolean environment value as false.
	if err := os.Unsetenv("MODBUS_METADATA_ENABLED"); err != nil {
		t.Fatal(err)
	}
	if cfg := mustConfigFromArgs(t, base...); !cfg.Modbus.MetadataEnabled {
		t.Fatal("validated metadata reads disabled by default")
	}
	if cfg := mustConfigFromArgs(t, append(base, "--modbus-metadata-enabled")...); !cfg.Modbus.MetadataEnabled {
		t.Fatal("explicit CLI opt-in ignored")
	}
	t.Setenv("MODBUS_METADATA_ENABLED", "true")
	if cfg := mustConfigFromArgs(t, base...); !cfg.Modbus.MetadataEnabled {
		t.Fatal("explicit environment opt-in ignored")
	}
	if cfg := mustConfigFromArgs(t, append(base, "--modbus-metadata-enabled=false")...); cfg.Modbus.MetadataEnabled {
		t.Fatal("CLI false did not override environment opt-in")
	}
	t.Setenv("MODBUS_METADATA_ENABLED", "false")
	if cfg := mustConfigFromArgs(t, base...); cfg.Modbus.MetadataEnabled {
		t.Fatal("environment false enabled metadata reads")
	}
	t.Setenv("MODBUS_METADATA_ENABLED", "maybe")
	if err := runConfigFromArgs(t, base...); err == nil {
		t.Fatal("invalid metadata opt-in accepted")
	}
}
