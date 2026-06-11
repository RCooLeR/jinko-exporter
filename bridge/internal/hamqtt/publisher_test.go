package hamqtt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	mqtt "github.com/eclipse/paho.mqtt.golang"
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

func TestDiscoveryPayloadContracts(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		QOS:             1,
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
			{Group: "grid", Key: "E_B_TO", Name: "Total Energy Buy", Unit: "kWh", Value: 1234},
		},
	}

	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatalf("discoveryMessages() error = %v", err)
	}

	power := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_electric_dp1/config")
	assertDiscoveryContract(t, power, map[string]any{
		"unique_id":                   "abc123_electric_dp1",
		"state_topic":                 "jinko-exporter/abc123/state",
		"availability_topic":          "jinko-exporter/availability",
		"payload_available":           "online",
		"payload_not_available":       "offline",
		"qos":                         float64(1),
		"device_class":                "power",
		"state_class":                 "measurement",
		"unit_of_measurement":         "W",
		"suggested_display_precision": nil,
	})

	energy := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_grid_e_b_to/config")
	assertDiscoveryContract(t, energy, map[string]any{
		"unique_id":           "abc123_grid_e_b_to",
		"device_class":        "energy",
		"state_class":         "total_increasing",
		"unit_of_measurement": "kWh",
	})

	pollUp := decodeDiscovery(t, messages, "homeassistant/binary_sensor/abc123_poll_up/config")
	assertDiscoveryContract(t, pollUp, map[string]any{
		"unique_id":             "abc123_poll_up",
		"availability_topic":    "jinko-exporter/availability",
		"payload_available":     "online",
		"payload_not_available": "offline",
		"device_class":          "connectivity",
		"entity_category":       "diagnostic",
	})

	deviceBlock, ok := power["device"].(map[string]any)
	if !ok {
		t.Fatalf("device block = %#v, want object", power["device"])
	}
	if deviceBlock["manufacturer"] != "Jinko" {
		t.Fatalf("manufacturer = %#v, want Jinko", deviceBlock["manufacturer"])
	}
}

func TestStatePayloadMarksPreviouslyDiscoveredMissingMetricsNull(t *testing.T) {
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

	firstSnapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Now().UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
			{Group: "battery", Key: "B_left_cap1", Name: "SoC", Unit: "%", Value: 82},
		},
	}
	device := publisher.device(firstSnapshot)
	if _, err := publisher.discoveryMessages(firstSnapshot, device, publisher.stateTopic(device.ID)); err != nil {
		t.Fatalf("discoveryMessages() error = %v", err)
	}

	nextSnapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Now().UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1000},
		},
	}
	state := publisher.buildStatePayload(nextSnapshot, time.Second)

	if got := derefFloat(state.Metrics["electric_dp1"]); got != 1000 {
		t.Fatalf("electric_dp1 = %v, want 1000", got)
	}
	if state.Metrics["battery_b_left_cap1"] != nil {
		t.Fatalf("battery_b_left_cap1 = %v, want nil for missing metric", *state.Metrics["battery_b_left_cap1"])
	}
}

func TestStartDoesNotFailWithoutBroker(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://127.0.0.1:1",
		ClientID:        "test-no-broker",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	defer publisher.Close()

	startedAt := time.Now()
	if err := publisher.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("Start() took %s, want it to return quickly without a broker", elapsed)
	}
}

func TestPollCallbacksDoNotFailWithoutBroker(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://127.0.0.1:1",
		ClientID:        "test-no-broker-callbacks",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	defer publisher.Close()

	if err := publisher.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Now().UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
		},
	}
	if err := publisher.OnPollSuccess(snapshot, time.Millisecond); err != nil {
		t.Fatalf("OnPollSuccess() error = %v", err)
	}
	if err := publisher.OnPollFailure("jinko", errClientNotConnected, time.Millisecond, 1); err != nil {
		t.Fatalf("OnPollFailure() error = %v", err)
	}
}

func TestOnConnectRepublishesCachedState(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
		},
	}

	if err := publisher.OnPollSuccess(snapshot, 250*time.Millisecond); err != nil {
		t.Fatalf("OnPollSuccess() error = %v", err)
	}
	client.clear()

	publisher.onConnect(client)

	if !client.hasTopic("homeassistant/sensor/abc123_electric_dp1/config", true, "") {
		t.Fatalf("cached discovery was not republished: %#v", client.messages)
	}
	if !client.hasTopic("jinko-exporter/abc123/state", true, `"up":true`) {
		t.Fatalf("cached state was not republished: %#v", client.messages)
	}
	if !client.hasTopic("jinko-exporter/availability", true, availabilityOnline) {
		t.Fatalf("online availability was not republished: %#v", client.messages)
	}
}

func TestOnConnectKeepsOfflineAfterPollFailure(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
		},
	}
	if err := publisher.OnPollSuccess(snapshot, 250*time.Millisecond); err != nil {
		t.Fatalf("OnPollSuccess() error = %v", err)
	}
	if err := publisher.OnPollFailure("jinko", errClientNotConnected, time.Second, 1); err != nil {
		t.Fatalf("OnPollFailure() error = %v", err)
	}
	client.clear()

	publisher.onConnect(client)

	if !client.hasTopic("jinko-exporter/abc123/state", true, `"up":true`) {
		t.Fatalf("cached state was not republished while offline: %#v", client.messages)
	}
	if !client.hasTopic("jinko-exporter/availability", true, availabilityOffline) {
		t.Fatalf("offline availability was not preserved after reconnect: %#v", client.messages)
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

func assertDiscoveryContract(t *testing.T, payload map[string]any, expected map[string]any) {
	t.Helper()
	for key, want := range expected {
		if want == nil {
			if _, ok := payload[key]; ok {
				t.Fatalf("%s = %#v, want absent", key, payload[key])
			}
			continue
		}
		if got := payload[key]; got != want {
			t.Fatalf("%s = %#v, want %#v in payload %#v", key, got, want, payload)
		}
	}
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func newPublisherWithRecordingClient(t *testing.T) (*Publisher, *recordingMQTTClient) {
	t.Helper()
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
	client := &recordingMQTTClient{open: true}
	publisher.client = client
	return publisher, client
}

type publishedMQTTMessage struct {
	topic  string
	retain bool
	body   string
}

type recordingMQTTClient struct {
	open     bool
	messages []publishedMQTTMessage
}

func (c *recordingMQTTClient) IsConnected() bool {
	return c.open
}

func (c *recordingMQTTClient) IsConnectionOpen() bool {
	return c.open
}

func (c *recordingMQTTClient) Connect() mqtt.Token {
	return staticMQTTToken{}
}

func (c *recordingMQTTClient) Disconnect(uint) {
	c.open = false
}

func (c *recordingMQTTClient) Publish(topic string, _ byte, retained bool, payload any) mqtt.Token {
	body := ""
	switch value := payload.(type) {
	case string:
		body = value
	case []byte:
		body = string(value)
	default:
		raw, _ := json.Marshal(value)
		body = string(raw)
	}
	c.messages = append(c.messages, publishedMQTTMessage{topic: topic, retain: retained, body: body})
	return staticMQTTToken{}
}

func (c *recordingMQTTClient) Subscribe(string, byte, mqtt.MessageHandler) mqtt.Token {
	return staticMQTTToken{}
}

func (c *recordingMQTTClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return staticMQTTToken{}
}

func (c *recordingMQTTClient) Unsubscribe(...string) mqtt.Token {
	return staticMQTTToken{}
}

func (c *recordingMQTTClient) AddRoute(string, mqtt.MessageHandler) {}

func (c *recordingMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.NewOptionsReader(mqtt.NewClientOptions())
}

func (c *recordingMQTTClient) clear() {
	c.messages = nil
}

func (c *recordingMQTTClient) hasTopic(topic string, retain bool, contains string) bool {
	for _, msg := range c.messages {
		if msg.topic != topic || msg.retain != retain {
			continue
		}
		if contains == "" || strings.Contains(msg.body, contains) {
			return true
		}
	}
	return false
}

type staticMQTTToken struct{}

func (staticMQTTToken) Wait() bool {
	return true
}

func (staticMQTTToken) WaitTimeout(time.Duration) bool {
	return true
}

func (staticMQTTToken) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (staticMQTTToken) Error() error {
	return nil
}
