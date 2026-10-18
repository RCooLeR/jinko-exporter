package app

import (
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

func TestSingleModbusSourcePassiveEvidenceUsesAcceptanceWrapperOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := config.Config{
			SourcePriority: []string{"modbus"},
			Modbus:         config.ModbusConfig{Host: "192.168.50.25", Port: 8899, LoggerSerial: "305419896", UnitID: 1, Timeout: time.Second, ValidationEvidenceEnabled: enabled},
		}
		src, err := buildInverterSource(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, wrapped := src.(*source.Priority)
		if wrapped != enabled || src.Name() != "modbus" {
			t.Fatalf("enabled=%t: source=%T name=%q", enabled, src, src.Name())
		}
	}
}

func TestBuildSourceRejectsPassiveEvidenceWithoutModbusPrimary(t *testing.T) {
	cfg := config.Config{SourcePriority: []string{"jinko"}, Modbus: config.ModbusConfig{ValidationEvidenceEnabled: true}}
	if _, err := buildInverterSource(cfg, nil); err == nil {
		t.Fatal("direct app config must also reject non-Modbus primary")
	}
}
