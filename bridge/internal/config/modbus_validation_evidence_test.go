package config

import (
	"os"
	"strings"
	"testing"
)

func TestModbusValidationEvidenceOptIn(t *testing.T) {
	base := []string{"--source", "modbus", "--modbus-host", "192.168.50.25", "--modbus-logger-serial", "305419896"}
	t.Setenv("MODBUS_VALIDATION_EVIDENCE_ENABLED", "")
	if err := os.Unsetenv("MODBUS_VALIDATION_EVIDENCE_ENABLED"); err != nil {
		t.Fatal(err)
	}
	if mustConfigFromArgs(t, base...).Modbus.ValidationEvidenceEnabled {
		t.Fatal("passive evidence must be explicit opt-in")
	}
	if !mustConfigFromArgs(t, append(base, "--modbus-validation-evidence-enabled")...).Modbus.ValidationEvidenceEnabled {
		t.Fatal("CLI opt-in ignored")
	}
	t.Setenv("MODBUS_VALIDATION_EVIDENCE_ENABLED", "true")
	if !mustConfigFromArgs(t, base...).Modbus.ValidationEvidenceEnabled {
		t.Fatal("environment opt-in ignored")
	}
	if mustConfigFromArgs(t, append(base, "--modbus-validation-evidence-enabled=false")...).Modbus.ValidationEvidenceEnabled {
		t.Fatal("CLI false must override environment")
	}
	t.Setenv("MODBUS_VALIDATION_EVIDENCE_ENABLED", "maybe")
	if err := runConfigFromArgs(t, base...); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}

func TestModbusValidationEvidenceRejectsNonModbusPrimary(t *testing.T) {
	for _, priority := range [][]string{nil, {"jinko"}, {"solarman", "modbus"}} {
		// Start with a valid parsed configuration so earlier global validators do
		// not obscure the scope check under test.
		base := mustConfigFromArgs(t, "--source", "modbus", "--modbus-host", "192.168.50.25", "--modbus-logger-serial", "305419896")
		base.SourcePriority = priority
		base.Modbus.ValidationEvidenceEnabled = true
		err := validate(base)
		if err == nil || !strings.Contains(err.Error(), "source-priority") {
			t.Fatalf("priority %v: error=%v", priority, err)
		}
	}
}

func TestModbusValidationEvidenceRequiresVisibleInfoLogs(t *testing.T) {
	for _, level := range []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled", "invalid"} {
		err := runConfigFromArgs(t, "--source", "modbus", "--modbus-host", "192.168.50.25", "--modbus-logger-serial", "305419896", "--modbus-validation-evidence-enabled", "--log-level", level)
		allowed := level == "trace" || level == "debug" || level == "info"
		if allowed && err != nil || !allowed && (err == nil || !strings.Contains(err.Error(), "log-level")) {
			t.Fatalf("level %s: error=%v", level, err)
		}
	}
}
