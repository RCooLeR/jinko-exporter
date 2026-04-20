package hamqtt

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
)

func TestDiscoveryMessagesAndStatePayload(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		DeviceID:    "100000001",
		SiteID:      "200000001",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Meta: map[string]string{
			"base_url": "https://example.invalid",
		},
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
			{Group: "electric", Key: "Etdy_ge1", Name: "Daily Production (Active)", Unit: "kWh", Value: 18.6},
			{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 82},
			{Group: "temperature", Key: "BMST", Name: "BMS Temperature", Unit: "\u2103", Value: 31.5},
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 1},
		},
	}

	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatalf("discoveryMessages() error = %v", err)
	}

	power := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_electric_dp1/config")
	if power["device_class"] != "power" || power["state_class"] != "measurement" || power["unit_of_measurement"] != "W" {
		t.Fatalf("unexpected power discovery payload: %#v", power)
	}

	energy := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_electric_etdy_ge1/config")
	if energy["device_class"] != "energy" || energy["state_class"] != "total_increasing" || energy["unit_of_measurement"] != "kWh" {
		t.Fatalf("unexpected energy discovery payload: %#v", energy)
	}

	battery := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_battery_b_left_cap1/config")
	if battery["device_class"] != "battery" || battery["state_class"] != "measurement" || battery["unit_of_measurement"] != "%" {
		t.Fatalf("unexpected battery discovery payload: %#v", battery)
	}

	temperature := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_temperature_bmst/config")
	if temperature["device_class"] != "temperature" || temperature["state_class"] != "measurement" || temperature["unit_of_measurement"] != "\u00b0C" {
		t.Fatalf("unexpected temperature discovery payload: %#v", temperature)
	}

	fault := decodeDiscovery(t, messages, "homeassistant/binary_sensor/abc123_alert_l_b_f_f_active/config")
	if fault["device_class"] != "problem" || fault["entity_category"] != "diagnostic" {
		t.Fatalf("unexpected fault binary discovery payload: %#v", fault)
	}

	meta := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_meta_base_url/config")
	if meta["entity_category"] != "diagnostic" {
		t.Fatalf("unexpected meta discovery payload: %#v", meta)
	}

	state := publisher.buildStatePayload(snapshot, 1500*time.Millisecond)
	if state.MetricCount != len(snapshot.Metrics) {
		t.Fatalf("MetricCount = %d, want %d", state.MetricCount, len(snapshot.Metrics))
	}
	if state.AlertCount != 1 || !state.AlertsActive {
		t.Fatalf("alert state = count %d active %v, want count 1 active true", state.AlertCount, state.AlertsActive)
	}
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 1840 {
		t.Fatalf("electric_dp1 = %v, want 1840", got)
	}
	if got := state.AlertMetrics["alert_l_b_f_f"]; got != 1 {
		t.Fatalf("alert_l_b_f_f = %v, want 1", got)
	}
	if state.PollDurationSeconds != 1.5 {
		t.Fatalf("PollDurationSeconds = %v, want 1.5", state.PollDurationSeconds)
	}
	if state.Meta["base_url"] != "https://example.invalid" {
		t.Fatalf("Meta[base_url] = %q, want https://example.invalid", state.Meta["base_url"])
	}
}

func decodeDiscovery(t *testing.T, messages []discoveryMessage, topic string) map[string]any {
	t.Helper()
	for _, msg := range messages {
		if msg.topic != topic {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(msg.payload), &payload); err != nil {
			t.Fatalf("decode discovery payload for %s: %v", topic, err)
		}
		return payload
	}
	t.Fatalf("topic %s not found", topic)
	return nil
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
