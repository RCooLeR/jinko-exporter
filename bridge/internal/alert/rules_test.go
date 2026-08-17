package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestEvaluateSnapshotGridDownAndCooldown(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{GridDownVoltageThreshold: 20}
	snapshot := testSnapshot(
		model.Metric{Group: "grid", Key: "G_V_L1", Name: "Grid Voltage L1", Unit: "V", Value: 0},
		model.Metric{Group: "grid", Key: "G_V_L2", Name: "Grid Voltage L2", Unit: "V", Value: 12},
		model.Metric{Group: "grid", Key: "G_V_L3", Name: "Grid Voltage L3", Unit: "V", Value: 20},
	)

	EvaluateSnapshot(context.Background(), manager, cfg, snapshot)
	EvaluateSnapshot(context.Background(), manager, cfg, snapshot)

	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count = %d, want 1 due to cooldown", got)
	}
	if !strings.Contains(notifier.events[0].subject, "Grid down detected") {
		t.Fatalf("subject = %q, want grid-down alert", notifier.events[0].subject)
	}
}

func TestEvaluateSnapshotGridDownRequiresAllAvailableVoltagesBelowThreshold(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{GridDownVoltageThreshold: 20}
	snapshot := testSnapshot(
		model.Metric{Group: "grid", Key: "G_V_L1", Name: "Grid Voltage L1", Unit: "V", Value: 0},
		model.Metric{Group: "grid", Key: "G_V_L2", Name: "Grid Voltage L2", Unit: "V", Value: 230},
	)

	EvaluateSnapshot(context.Background(), manager, cfg, snapshot)

	if got := len(notifier.events); got != 0 {
		t.Fatalf("alert count = %d, want 0", got)
	}
}

func TestEvaluateSnapshotBatterySOCAndHighTemperature(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		BatterySOCLowThreshold:   30,
		HighTemperatureThreshold: 60,
	}
	snapshot := testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 25},
		model.Metric{Group: "temperature", Key: "AC_T", Name: "AC Temperature", Unit: "C", Value: 61},
	)

	EvaluateSnapshot(context.Background(), manager, cfg, snapshot)

	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count = %d, want 2", got)
	}
	if !strings.Contains(notifier.events[0].subject, "Battery SOC low") {
		t.Fatalf("first subject = %q, want battery SOC alert", notifier.events[0].subject)
	}
	if !strings.Contains(notifier.events[1].subject, "High temperature") {
		t.Fatalf("second subject = %q, want high temperature alert", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotRecoveryNotificationsAreOptIn(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		BatterySOCLowThreshold: 30,
		NotifyRecovery:         false,
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 25},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 36},
	))

	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count = %d, want only initial alert when recovery notification is disabled", got)
	}
}

func TestEvaluateSnapshotBatterySOCRecoveryUsesHysteresis(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		BatterySOCLowThreshold: 30,
		NotifyRecovery:         true,
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 30},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 34.9},
	))
	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count before hysteresis recovery = %d, want 1", got)
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 35},
	))
	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count after hysteresis recovery = %d, want 2", got)
	}
	if !strings.Contains(notifier.events[1].subject, "Battery SOC recovered") {
		t.Fatalf("recovery subject = %q, want battery recovery", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotBatterySOCHighThresholdCanRecoverAtFullCharge(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		BatterySOCLowThreshold: 98,
		NotifyRecovery:         true,
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 98},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 99},
	))
	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count below capped recovery threshold = %d, want 1", got)
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 100},
	))
	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count at full-charge recovery = %d, want 2", got)
	}
	if !strings.Contains(notifier.events[1].subject, "Battery SOC recovered") ||
		!strings.Contains(notifier.events[1].body, "Recovery Threshold: 100.00 %") {
		t.Fatalf("recovery event = %#v, want capped 100%% recovery", notifier.events[1])
	}
}

func TestEvaluateSnapshotHighTemperatureRecoveryUsesHysteresis(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		HighTemperatureThreshold: 60,
		NotifyRecovery:           true,
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "temperature", Key: "AC_T", Name: "AC Temperature", Unit: "C", Value: 60},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "temperature", Key: "AC_T", Name: "AC Temperature", Unit: "C", Value: 55.1},
	))
	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count before hysteresis recovery = %d, want 1", got)
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "temperature", Key: "AC_T", Name: "AC Temperature", Unit: "C", Value: 55},
	))
	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count after hysteresis recovery = %d, want 2", got)
	}
	if !strings.Contains(notifier.events[1].subject, "Temperature recovered") {
		t.Fatalf("recovery subject = %q, want temperature recovery", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotGridRecovery(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{
		GridDownVoltageThreshold: 20,
		NotifyRecovery:           true,
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "grid", Key: "G_V_L1", Name: "Grid Voltage L1", Unit: "V", Value: 0},
		model.Metric{Group: "grid", Key: "G_V_L2", Name: "Grid Voltage L2", Unit: "V", Value: 0},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "grid", Key: "G_V_L1", Name: "Grid Voltage L1", Unit: "V", Value: 230},
		model.Metric{Group: "grid", Key: "G_V_L2", Name: "Grid Voltage L2", Unit: "V", Value: 0},
	))

	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count = %d, want grid alert and recovery", got)
	}
	if !strings.Contains(notifier.events[1].subject, "Grid restored") {
		t.Fatalf("recovery subject = %q, want grid restored", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotAlarmMetricRecovery(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{NotifyRecovery: true}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 1},
	))
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 0},
	))

	if got := len(notifier.events); got != 2 {
		t.Fatalf("alert count = %d, want alarm alert and recovery", got)
	}
	if !strings.Contains(notifier.events[1].subject, "warning/alarm/fault metrics recovered") {
		t.Fatalf("recovery subject = %q, want alarm recovery", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotAlarmRecoveryIsIsolatedBySource(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{NotifyRecovery: true}

	modbusActive := testSnapshot(
		model.Metric{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye warning word", Value: 1},
	)
	modbusActive.Source = "modbus"
	EvaluateSnapshot(context.Background(), manager, cfg, modbusActive)

	jinkoClear := testSnapshot(
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 0},
	)
	jinkoClear.Source = "jinko"
	EvaluateSnapshot(context.Background(), manager, cfg, jinkoClear)
	if got := len(notifier.events); got != 1 {
		t.Fatalf("cross-source clear delivered %d events, want only the original Modbus alert", got)
	}

	modbusClear := testSnapshot(
		model.Metric{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye warning word", Value: 0},
	)
	modbusClear.Source = "modbus"
	EvaluateSnapshot(context.Background(), manager, cfg, modbusClear)
	if got := len(notifier.events); got != 2 {
		t.Fatalf("same-source clear delivered %d events, want alert and recovery", got)
	}
	if !strings.Contains(notifier.events[1].subject, "recovered") {
		t.Fatalf("same-source recovery subject = %q", notifier.events[1].subject)
	}
}

func TestEvaluateSnapshotModbusRawWarningFaultWordsUseOneAggregateAlert(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{NotifyRecovery: true}
	clear := []model.Metric{
		{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye Inverter Warning Word 1 (Raw U16)"},
		{Group: "alert", Key: "DEYE_MODBUS_R554_WARNING_WORD_2_RAW", Name: "Deye Inverter Warning Word 2 (Raw U16)"},
		{Group: "alert", Key: "DEYE_MODBUS_R555_FAULT_WORD_1_RAW", Name: "Deye Inverter Fault Word 1 (Raw U16)"},
		{Group: "alert", Key: "DEYE_MODBUS_R556_FAULT_WORD_2_RAW", Name: "Deye Inverter Fault Word 2 (Raw U16)"},
		{Group: "alert", Key: "DEYE_MODBUS_R557_FAULT_WORD_3_RAW", Name: "Deye Inverter Fault Word 3 (Raw U16)"},
		{Group: "alert", Key: "DEYE_MODBUS_R558_FAULT_WORD_4_RAW", Name: "Deye Inverter Fault Word 4 (Raw U16)"},
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(clear...))
	if len(notifier.events) != 0 {
		t.Fatalf("all-clear raw words delivered %d events, want 0", len(notifier.events))
	}

	active := append([]model.Metric(nil), clear...)
	active[1].Value = 0x8000
	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(active...))
	if len(notifier.events) != 1 {
		t.Fatalf("one active warning word delivered %d events, want one aggregate event", len(notifier.events))
	}
	for _, want := range []string{"DEYE_MODBUS_R554_WARNING_WORD_2_RAW", "32768.00"} {
		if !strings.Contains(notifier.events[0].body, want) {
			t.Fatalf("aggregate alert body %q does not contain %q", notifier.events[0].body, want)
		}
	}
	if !strings.Contains(notifier.events[0].subject, "warning/alarm/fault metrics active") {
		t.Fatalf("aggregate warning subject = %q", notifier.events[0].subject)
	}

	EvaluateSnapshot(context.Background(), manager, cfg, testSnapshot(clear...))
	if len(notifier.events) != 2 || !strings.Contains(notifier.events[1].subject, "warning/alarm/fault metrics recovered") {
		t.Fatalf("events = %#v, want one aggregate alert followed by recovery", notifier.events)
	}
}

func TestEvaluateSnapshotModbusRunStateIsNotAnAlertMetric(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, 0)
	EvaluateSnapshot(context.Background(), manager, config.AlertConfig{}, testSnapshot(
		model.Metric{
			Group: "status",
			Key:   "DEYE_MODBUS_R500_RUN_STATE",
			Name:  "Deye Inverter Run State Code",
			Value: 4,
		},
	))
	if len(notifier.events) != 0 {
		t.Fatalf("run-state status metric delivered %d alert events, want 0", len(notifier.events))
	}
}

func TestEvaluateNoSuccessfulPoll(t *testing.T) {
	notifier := &recordingNotifier{}
	manager := NewManager(notifier, time.Hour)
	cfg := config.AlertConfig{NoSuccessfulPollWindow: time.Minute}

	EvaluateNoSuccessfulPoll(
		context.Background(),
		manager,
		cfg,
		"jinko",
		time.Now().Add(-2*time.Minute),
		time.Time{},
		"last failure",
	)

	if got := len(notifier.events); got != 1 {
		t.Fatalf("alert count = %d, want 1", got)
	}
	if !strings.Contains(notifier.events[0].body, "last failure") {
		t.Fatalf("body = %q, want last failure details", notifier.events[0].body)
	}
}

type deliveredAlert struct {
	subject string
	body    string
}

type recordingNotifier struct {
	events []deliveredAlert
}

func (n *recordingNotifier) Notify(_ context.Context, subject string, body string) error {
	n.events = append(n.events, deliveredAlert{subject: subject, body: body})
	return nil
}

func testSnapshot(metrics ...model.Metric) *model.Snapshot {
	return &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics:     metrics,
	}
}
