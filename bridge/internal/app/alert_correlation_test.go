package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

func TestAlertCorrelationNotificationContracts(t *testing.T) {
	notificationTag := "synthetic-device-tag"
	signature := source.ModbusAlertSignature{
		R553: 1,
		R554: 2,
		R555: 3,
		R556: 4,
		R557: 5,
		R558: 0xffff,
	}
	tests := []struct {
		name        string
		event       source.AlertCorrelationEvent
		wantTitle   string
		wantMessage []string
	}{
		{
			name:      "detected is immediate and explicitly pending",
			event:     source.AlertCorrelationEvent{Kind: source.AlertCorrelationDetected, Signature: signature},
			wantTitle: "Inverter warning/fault detected",
			wantMessage: []string{
				"R553=1 (0x0001)",
				"R558=65535 (0xFFFF)",
				"correlation is running",
			},
		},
		{
			name:      "recovery requires an all-zero complete Modbus observation",
			event:     source.AlertCorrelationEvent{Kind: source.AlertCorrelationRecovered},
			wantTitle: "Inverter warning/fault cleared",
			wantMessage: []string{
				"complete Modbus snapshot",
				"R553-R558 as zero",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			notification, err := alertCorrelationNotification(test.event, notificationTag)
			if err != nil {
				t.Fatalf("alertCorrelationNotification() error = %v", err)
			}
			if notification.Title != test.wantTitle {
				t.Fatalf("title = %q, want %q", notification.Title, test.wantTitle)
			}
			for _, fragment := range test.wantMessage {
				if !strings.Contains(notification.Message, fragment) {
					t.Fatalf("message %q does not contain %q", notification.Message, fragment)
				}
			}
			if notification.Data["tag"] != notificationTag || len(notification.Data) != 1 {
				t.Fatalf("notification data = %#v, want one stable tag", notification.Data)
			}
			if strings.Contains(notification.Message, "raw upstream body") || strings.Contains(notification.Message, "untrusted-source") {
				t.Fatalf("message reflected untrusted correlation data: %q", notification.Message)
			}
		})
	}

	if _, err := alertCorrelationNotification(source.AlertCorrelationEvent{Kind: "unsupported"}, notificationTag); err == nil {
		t.Fatal("unsupported event error = nil")
	}
}

func TestFormatCorrelationStatusesUsesOnlyClosedVocabulary(t *testing.T) {
	got := formatCorrelationStatuses([]source.AlertCorrelationSourceSummary{
		{Source: "jinko", Status: source.AlertCorrelationSourceOK},
		{Source: "solarman", Status: source.AlertCorrelationSourceTimedOut},
		{Source: "untrusted-source", Status: source.AlertCorrelationSourceStatus("raw upstream body")},
	})
	if got != "Jinko=ok, Solarman=timeout" {
		t.Fatalf("statuses = %q", got)
	}
	if strings.Contains(got, "raw upstream body") || strings.Contains(got, "untrusted-source") {
		t.Fatalf("statuses reflected untrusted input: %q", got)
	}
}

func TestHomeAssistantCorrelationCallbackDeliversExactNotificationAndPropagatesFailure(t *testing.T) {
	deliveryErr := errors.New("synthetic delivery failure")
	sender := &recordingHomeAssistantSender{err: deliveryErr}
	event := source.AlertCorrelationEvent{
		Kind:       source.AlertCorrelationDetected,
		ObservedAt: time.Unix(1_700_000_000, 123).UTC(),
		Signature:  source.ModbusAlertSignature{R553: 7},
	}

	err := homeAssistantCorrelationCallback(sender, "synthetic-device-tag")(context.Background(), event)
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("callback error = %v, want delivery error", err)
	}
	if len(sender.notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(sender.notifications))
	}
	if !strings.Contains(sender.notifications[0].Message, "R553=7 (0x0007)") {
		t.Fatalf("notification = %#v", sender.notifications[0])
	}
}

func TestHomeAssistantCorrelationCallbackLogsCompletionWithoutSecondPush(t *testing.T) {
	sender := &recordingHomeAssistantSender{}
	callback := homeAssistantCorrelationCallback(sender, "synthetic-device-tag")
	if err := callback(context.Background(), source.AlertCorrelationEvent{
		Kind:       source.AlertCorrelationComplete,
		ObservedAt: time.Unix(1_700_000_000, 0).UTC(),
		Signature:  source.ModbusAlertSignature{R553: 1},
		Sources: []source.AlertCorrelationSourceSummary{
			{Source: "jinko", Status: source.AlertCorrelationSourceOK},
			{Source: "solarman", Status: source.AlertCorrelationSourceOK},
		},
	}); err != nil {
		t.Fatalf("completion callback error = %v", err)
	}
	if len(sender.notifications) != 0 {
		t.Fatalf("completion notifications = %d, want 0", len(sender.notifications))
	}
}

func TestAlertCorrelationIDIsStableAndSignatureSpecific(t *testing.T) {
	observedAt := time.Unix(1_700_000_000, 123).UTC()
	first := alertCorrelationID(observedAt, source.ModbusAlertSignature{R553: 1})
	if len(first) != 24 || first != alertCorrelationID(observedAt, source.ModbusAlertSignature{R553: 1}) {
		t.Fatalf("stable correlation ID = %q", first)
	}
	if first == alertCorrelationID(observedAt, source.ModbusAlertSignature{R553: 2}) {
		t.Fatal("different signature produced the same correlation ID")
	}
	if first == alertCorrelationID(observedAt.Add(time.Nanosecond), source.ModbusAlertSignature{R553: 1}) {
		t.Fatal("different observation time produced the same correlation ID")
	}
}

func TestModbusAlertNotificationTagIsStablePrivateAndDeviceSpecific(t *testing.T) {
	first := modbusAlertNotificationTag("SYNTHETIC_INVERTER_A", "synthetic-secret-a")
	if first != modbusAlertNotificationTag(" SYNTHETIC_INVERTER_A ", "synthetic-secret-a") {
		t.Fatalf("tag is not stable after trimming: %q", first)
	}
	if first == modbusAlertNotificationTag("SYNTHETIC_INVERTER_B", "synthetic-secret-a") {
		t.Fatal("different devices produced the same notification tag")
	}
	if first == modbusAlertNotificationTag("SYNTHETIC_INVERTER_A", "synthetic-secret-b") {
		t.Fatal("different notification secrets produced the same tag")
	}
	if strings.Contains(first, "SYNTHETIC") || len(first) != len(modbusAlertNotificationTagPrefix)+16 {
		t.Fatalf("tag exposes identity or has wrong length: %q", first)
	}
}

func TestBuildSourceConfiguresModbusAlertCorrelationWithoutNetworkActivity(t *testing.T) {
	cfg := correlationBuildConfig()
	src, err := buildSource(cfg, nil)
	if err != nil {
		t.Fatalf("buildSource() error = %v", err)
	}
	if src.Name() != "modbus,jinko,solarman" {
		t.Fatalf("source name = %q", src.Name())
	}
	if _, ok := src.(source.BackgroundMaintainer); !ok {
		t.Fatalf("source type %T does not expose background correlation lifecycle", src)
	}
	priority, ok := src.(*source.Priority)
	if !ok {
		t.Fatalf("source type = %T, want *source.Priority", src)
	}
	if err := priority.ConfigureAlertCorrelation(source.AlertCorrelationConfig{
		JobTimeout: time.Second,
		Notify:     func(context.Context, source.AlertCorrelationEvent) error { return nil },
	}); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("second ConfigureAlertCorrelation() error = %v", err)
	}
}

func TestBuildSourceRejectsIncompleteCorrelationSourceSet(t *testing.T) {
	cfg := correlationBuildConfig()
	cfg.SourcePriority = []string{"modbus"}
	if _, err := buildSource(cfg, nil); err == nil || !strings.Contains(err.Error(), "Jinko") {
		t.Fatalf("buildSource() error = %v, want incomplete correlation source set", err)
	}
}

func correlationBuildConfig() config.Config {
	return config.Config{
		SourcePriority: []string{"modbus", "jinko", "solarman"},
		ModbusAlertCorrelation: config.ModbusAlertCorrelationConfig{
			Enabled:    true,
			Cooldown:   6 * time.Hour,
			JobTimeout: 45 * time.Second,
		},
		HomeAssistant: config.HomeAssistantConfig{
			BaseURL:       "https://ha.example.test",
			Token:         "synthetic-ha-token",
			NotifyService: "mobile_app_operator_phone",
			Timeout:       time.Second,
		},
		Jinko: config.JinkoConfig{
			URL:           "https://jinko.example.test/detail",
			Timeout:       time.Second,
			RetryAttempts: 1,
			DeviceID:      100,
			SiteID:        200,
			BearerToken:   "synthetic-jinko-token",
		},
		Solarman: config.SolarmanConfig{
			BaseURL:    "https://solarman.example.test",
			APIVersion: "v1.0",
			Timeout:    time.Second,
			AppID:      "synthetic-app-id",
			AppSecret:  "synthetic-app-secret",
			Email:      "operator@example.test",
			Password:   "synthetic-password",
			DeviceSN:   "SYNTHETIC_INVERTER",
		},
		Modbus: config.ModbusConfig{
			Host:         "192.168.50.25",
			Port:         8899,
			LoggerSerial: "305419896",
			DeviceSN:     "SYNTHETIC_INVERTER",
			UnitID:       1,
			Timeout:      time.Second,
		},
	}
}

type recordingHomeAssistantSender struct {
	notifications []alert.HomeAssistantNotification
	err           error
}

func (s *recordingHomeAssistantSender) Send(_ context.Context, notification alert.HomeAssistantNotification) error {
	s.notifications = append(s.notifications, notification)
	return s.err
}
