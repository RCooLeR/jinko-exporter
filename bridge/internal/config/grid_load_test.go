package config

import (
	"strings"
	"testing"
)

func TestIndependentShellyMQTTRequiresStableDeviceIdentity(t *testing.T) {
	base := []string{
		"--source", "jinko", "--jinko-device-id", "100", "--jinko-site-id", "200", "--jinko-bearer-token", "synthetic-token",
		"--shelly-grid-load-enabled", "--shelly-grid-load-url", "http://192.0.2.50",
		"--mqtt-enabled", "--mqtt-broker", "tcp://mqtt.example.test:1883",
	}
	t.Setenv("MQTT_DEVICE_ID", "")
	err := runConfigFromArgs(t, base...)
	if err == nil || !strings.Contains(err.Error(), "mqtt-device-id is required with Shelly") {
		t.Fatalf("validation = %v, want stable identity requirement", err)
	}
	cfg := mustConfigFromArgs(t, append(base, "--mqtt-device-id", "synthetic-inverter")...)
	if cfg.MQTT.DeviceID != "synthetic-inverter" {
		t.Fatalf("device identity = %q", cfg.MQTT.DeviceID)
	}
}
