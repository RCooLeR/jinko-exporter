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
	if !strings.Contains(notifier.events[1].subject, "alarm metrics recovered") {
		t.Fatalf("recovery subject = %q, want alarm recovery", notifier.events[1].subject)
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
