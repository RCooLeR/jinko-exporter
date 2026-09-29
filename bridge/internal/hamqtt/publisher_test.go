package hamqtt

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestNewPublisherValidatesAndNormalizesTopicPrefixes(t *testing.T) {
	base := config.MQTTConfig{
		Broker:          "tcp://127.0.0.1:1883",
		ClientID:        "topic-prefix-test",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
	}

	invalid := []struct {
		name      string
		topic     string
		discovery string
	}{
		{name: "topic wildcard", topic: "bridge/+"},
		{name: "topic NUL", topic: "bridge/\x00/state"},
		{name: "topic empty level", topic: "bridge//state"},
		{name: "discovery wildcard", discovery: "homeassistant/#"},
		{name: "discovery empty level", discovery: "homeassistant//sensor"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			if tt.topic != "" {
				cfg.TopicPrefix = tt.topic
			}
			if tt.discovery != "" {
				cfg.DiscoveryPrefix = tt.discovery
			}
			if publisher, err := NewPublisher(cfg); err == nil {
				publisher.Close()
				t.Fatal("NewPublisher() error = nil, want unsafe topic-prefix rejection")
			}
		})
	}

	cfg := base
	cfg.TopicPrefix = " / bridge / state / "
	cfg.DiscoveryPrefix = "/ homeassistant /"
	publisher, err := NewPublisher(cfg)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	defer publisher.Close()
	if publisher.topicPrefix != "bridge/state" || publisher.discoveryPrefix != "homeassistant" {
		t.Fatalf("normalized prefixes = %q/%q, want bridge/state and homeassistant", publisher.topicPrefix, publisher.discoveryPrefix)
	}
}

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
		ParentSN:    "PARENT123",
		DeviceID:    "100000001",
		SiteID:      "200000001",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Meta: map[string]string{
			"base_url": "https://example.invalid",
		},
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
			{Group: "electric", Key: "INV_O_P_T", Name: "Total Inverter Output Power", Unit: "W", Value: 2854},
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
	outputPower := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_electric_inv_o_p_t/config")
	if outputPower["name"] != "Total Inverter Output Power" || outputPower["device_class"] != "power" || outputPower["state_class"] != "measurement" || outputPower["unit_of_measurement"] != "W" {
		t.Fatalf("unexpected inverter output-power discovery payload: %#v", outputPower)
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
	wantFaultTemplate := "{{ 'ON' if 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) and value_json.get('alert_metrics', {}).get('alert_l_b_f_f')|float(0) != 0 else 'OFF' if 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) else none }}"
	if template, _ := fault["value_template"].(string); template != wantFaultTemplate {
		t.Fatalf("fault binary template = %q, want defaulted, missing-safe conversion %q", template, wantFaultTemplate)
	}
	jinkoAggregate := decodeDiscovery(t, messages, "homeassistant/binary_sensor/abc123_jinko_warning_alarm_fault_active/config")
	if template, _ := jinkoAggregate["value_template"].(string); !strings.Contains(template, "value_json.get('alert_domain') == 'jinko'") || !strings.Contains(template, "else 'OFF'") {
		t.Fatalf("Jinko aggregate template is not source-domain-safe: %#v", jinkoAggregate)
	}

	meta := decodeDiscovery(t, messages, "homeassistant/sensor/abc123_meta_base_url/config")
	if meta["entity_category"] != "diagnostic" {
		t.Fatalf("unexpected meta discovery payload: %#v", meta)
	}
	if template, _ := meta["value_template"].(string); template != "{{ value_json.get('meta', {}).get('base_url') }}" {
		t.Fatalf("meta template = %q, want nested missing-safe lookup", template)
	}

	state := publisher.buildStatePayload(snapshot, 1500*time.Millisecond)
	if got := [4]string{state.DeviceSN, state.ParentSN, state.DeviceID, state.SiteID}; got != [4]string{"ABC123", "PARENT123", "100000001", "200000001"} {
		t.Fatalf("identity = %#v, want complete Jinko identity", got)
	}
	if state.MetricCount != len(snapshot.Metrics) {
		t.Fatalf("MetricCount = %d, want %d", state.MetricCount, len(snapshot.Metrics))
	}
	if state.AlertCount != 1 || !state.AlertsKnown || !state.AlertsActive {
		t.Fatalf("alert state = count %d known %v active %v, want count 1 known/active true", state.AlertCount, state.AlertsKnown, state.AlertsActive)
	}
	if state.AlertDomain != "jinko" {
		t.Fatalf("AlertDomain = %q, want jinko", state.AlertDomain)
	}
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 1840 {
		t.Fatalf("electric_dp1 = %v, want 1840", got)
	}
	if got := derefFloat(state.Metrics["electric_inv_o_p_t"]); got != 2854 {
		t.Fatalf("electric_inv_o_p_t = %v, want 2854", got)
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

func TestAlertDiscoveryPayloadsUseExactEntityScopedAvailability(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "alert-availability-contract",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &model.Snapshot{
		Source:   "jinko",
		DeviceSN: "ALERT001",
		Metrics: []model.Metric{
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 0},
		},
	}
	device := publisher.device(snapshot)
	stateTopic := publisher.stateTopic(device.ID)
	messages, err := publisher.discoveryMessages(snapshot, device, stateTopic)
	if err != nil {
		t.Fatal(err)
	}

	globalAvailability := map[string]any{
		"payload_available":     "online",
		"payload_not_available": "offline",
		"topic":                 "jinko-exporter/availability",
	}
	metricAvailabilityTemplate := "{{ 'online' if value_json.get('alert_domain') == 'jinko' and 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) else 'offline' }}"
	devicePayload := map[string]any{
		"identifiers":   []string{"jinko_exporter_alert001"},
		"manufacturer":  "Jinko",
		"model":         "Solar inverter via jinko-exporter",
		"name":          "Jinko Inverter ALERT001",
		"serial_number": "ALERT001",
	}
	exact := []struct {
		topic string
		want  map[string]any
	}{
		{
			topic: "homeassistant/binary_sensor/alert001_jinko_warning_alarm_fault_active/config",
			want: map[string]any{
				"availability": []map[string]any{
					globalAvailability,
					{
						"payload_available":     "online",
						"payload_not_available": "offline",
						"topic":                 stateTopic,
						"value_template":        "{{ 'online' if value_json.get('alert_domain') == 'jinko' and value_json.get('alerts_known', false) else 'offline' }}",
					},
				},
				"availability_mode": "all",
				"device":            devicePayload,
				"device_class":      "problem",
				"name":              "Warning/Alarm/Fault Active (jinko)",
				"payload_off":       "OFF",
				"payload_on":        "ON",
				"qos":               0,
				"state_topic":       stateTopic,
				"unique_id":         "alert001_jinko_warning_alarm_fault_active",
				"value_template":    "{{ 'ON' if value_json.get('alert_domain') == 'jinko' and value_json.get('alerts_known', false) and value_json.get('alerts_active', false) else 'OFF' }}",
			},
		},
		{
			topic: "homeassistant/sensor/alert001_alert_l_b_f_f/config",
			want: map[string]any{
				"availability": []map[string]any{
					globalAvailability,
					{
						"payload_available":     "online",
						"payload_not_available": "offline",
						"topic":                 stateTopic,
						"value_template":        metricAvailabilityTemplate,
					},
				},
				"availability_mode": "all",
				"device":            devicePayload,
				"entity_category":   "diagnostic",
				"icon":              "mdi:alert-circle",
				"name":              "Lithium battery fault flag",
				"qos":               0,
				"state_topic":       stateTopic,
				"unique_id":         "alert001_alert_l_b_f_f",
				"value_template":    "{{ value_json.get('alert_metrics', {}).get('alert_l_b_f_f') }}",
			},
		},
		{
			topic: "homeassistant/binary_sensor/alert001_alert_l_b_f_f_active/config",
			want: map[string]any{
				"availability": []map[string]any{
					globalAvailability,
					{
						"payload_available":     "online",
						"payload_not_available": "offline",
						"topic":                 stateTopic,
						"value_template":        metricAvailabilityTemplate,
					},
				},
				"availability_mode": "all",
				"device":            devicePayload,
				"device_class":      "problem",
				"entity_category":   "diagnostic",
				"name":              "Lithium battery fault flag Active",
				"payload_off":       "OFF",
				"payload_on":        "ON",
				"qos":               0,
				"state_topic":       stateTopic,
				"unique_id":         "alert001_alert_l_b_f_f_active",
				"value_template":    "{{ 'ON' if 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) and value_json.get('alert_metrics', {}).get('alert_l_b_f_f')|float(0) != 0 else 'OFF' if 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) else none }}",
			},
		},
	}
	for _, tc := range exact {
		t.Run(tc.topic, func(t *testing.T) {
			raw := discoveryPayload(t, messages, tc.topic)
			var got map[string]any
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatal(err)
			}
			wantRaw, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err := json.Unmarshal(wantRaw, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("discovery payload mismatch\ngot:  %s\nwant: %s", raw, wantRaw)
			}
		})
	}

	ordinary := decodeDiscovery(t, messages, "homeassistant/sensor/alert001_alert_l_b_f_f/config")
	if _, exists := ordinary["availability_topic"]; exists {
		t.Fatal("source-scoped alert retained the single global availability form")
	}
}

func TestAlertStateDistinguishesZeroNonzeroMissingAndGlobalOffline(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	zero := &model.Snapshot{
		Source:   "modbus",
		DeviceSN: "ALERT001",
		Metrics: []model.Metric{
			{Group: "alert", Key: "R553", Name: "Modbus warning word", Value: 0},
		},
	}
	if err := publisher.OnPollSuccess(zero, time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, "jinko-exporter/alert001/state")
	if !state.AlertsKnown || state.AlertsActive || state.AlertCount != 0 || state.AlertMetrics["alert_r553"] != 0 {
		t.Fatalf("zero alert state = %#v, want known clear with explicit zero", state)
	}

	nonzero := *zero
	nonzero.Metrics = []model.Metric{{Group: "alert", Key: "R553", Name: "Modbus warning word", Value: 4}}
	if err := publisher.OnPollSuccess(&nonzero, time.Second); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, "jinko-exporter/alert001/state")
	if !state.AlertsKnown || !state.AlertsActive || state.AlertCount != 1 || state.AlertMetrics["alert_r553"] != 4 {
		t.Fatalf("nonzero alert state = %#v, want known active value 4", state)
	}

	missing := *zero
	missing.Metrics = []model.Metric{{Group: "alert", Key: "R553", Name: "Modbus warning word", Value: math.NaN()}}
	if err := publisher.OnPollSuccess(&missing, time.Second); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, "jinko-exporter/alert001/state")
	if state.AlertsKnown || state.AlertsActive || len(state.AlertMetrics) != 0 {
		t.Fatalf("non-finite alert state = %#v, want source entity unavailable", state)
	}

	client.clear()
	if err := publisher.OnPollFailure("modbus", errors.New("synthetic poll failure"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if payloads := client.payloadsForTopic("jinko-exporter/availability"); len(payloads) != 1 || payloads[0] != "offline" {
		t.Fatalf("global availability after failure = %#v, want offline", payloads)
	}
}

func TestNonFiniteAlertValuesPublishAsUnknown(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye warning word", Value: math.NaN()},
			{Group: "alert", Key: "DEYE_MODBUS_R554_WARNING_WORD_2_RAW", Name: "Deye warning word", Value: math.Inf(1)},
		},
	}

	if err := publisher.OnPollSuccess(snapshot, time.Second); err != nil {
		t.Fatalf("OnPollSuccess() error = %v", err)
	}

	stateTopic := "jinko-exporter/synthetic_inv_001/state"
	var state statePayload
	found := false
	for _, message := range client.messages {
		if message.topic != stateTopic {
			continue
		}
		if err := json.Unmarshal([]byte(message.body), &state); err != nil {
			t.Fatalf("decode published state: %v", err)
		}
		found = true
	}
	if !found {
		t.Fatalf("state topic %q was not published: %#v", stateTopic, client.messages)
	}
	if state.AlertsKnown || state.AlertsActive || state.AlertCount != 0 || len(state.AlertMetrics) != 0 {
		t.Fatalf("non-finite alert state = known=%v active=%v count=%d metrics=%#v, want unknown/inactive/empty", state.AlertsKnown, state.AlertsActive, state.AlertCount, state.AlertMetrics)
	}
	for _, key := range []string{"alert_deye_modbus_r553_warning_word_1_raw", "alert_deye_modbus_r554_warning_word_2_raw"} {
		value, ok := state.Metrics[key]
		if !ok || value != nil {
			t.Fatalf("metrics[%q] = %#v (present=%v), want present null", key, value, ok)
		}
	}
}

func TestMetaDiscoveryTemplateIsMissingSafeAcrossSourceTransition(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "meta-transition-test",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		DeviceID:        "stable_inverter",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	modbus := &model.Snapshot{
		Source:   "modbus",
		DeviceSN: "SYNTHETIC_INV_001",
		Meta:     map[string]string{"profile": "jks-6-20h-ei-readonly-v1"},
	}
	device := publisher.device(modbus)
	messages, err := publisher.discoveryMessages(modbus, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatal(err)
	}
	meta := decodeDiscovery(t, messages, "homeassistant/sensor/stable_inverter_meta_profile/config")
	if got := meta["value_template"]; got != "{{ value_json.get('meta', {}).get('profile') }}" {
		t.Fatalf("meta value_template = %#v, want missing-safe nested lookup", got)
	}

	jinko := &model.Snapshot{Source: "jinko", DeviceSN: "SYNTHETIC_INV_001"}
	raw, err := json.Marshal(publisher.buildStatePayload(jinko, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if _, exists := state["meta"]; exists {
		t.Fatalf("fallback state unexpectedly contains meta: %s", raw)
	}
}

func TestDiscoverySignatureSortsMetadataKeys(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "signature-test",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &model.Snapshot{
		Source:   "modbus",
		DeviceSN: "SYNTHETIC_INV_001",
		Meta: map[string]string{
			"zeta":   "last",
			"alpha":  "first",
			"middle": "middle",
		},
	}
	device := publisher.device(snapshot)
	stateTopic := publisher.stateTopic(device.ID)
	want := publisher.discoverySignature(snapshot, device, stateTopic)
	if alpha, middle, zeta := strings.Index(want, "meta:alpha|"), strings.Index(want, "meta:middle|"), strings.Index(want, "meta:zeta|"); alpha < 0 || !(alpha < middle && middle < zeta) {
		t.Fatalf("metadata signature order = %q, want alpha, middle, zeta", want)
	}
	for range 100 {
		if got := publisher.discoverySignature(snapshot, device, stateTopic); got != want {
			t.Fatalf("metadata signature changed across identical snapshots:\nfirst: %q\nnext:  %q", want, got)
		}
	}
}

func TestModbusRawWarningFaultMetricsHaveUniqueDiscoveryAndCountWords(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test-modbus-alerts",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics := []model.Metric{
		{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye Inverter Warning Word 1 (Raw U16)", Value: 0},
		{Group: "alert", Key: "DEYE_MODBUS_R554_WARNING_WORD_2_RAW", Name: "Deye Inverter Warning Word 2 (Raw U16)", Value: 1},
		{Group: "alert", Key: "DEYE_MODBUS_R555_FAULT_WORD_1_RAW", Name: "Deye Inverter Fault Word 1 (Raw U16)", Value: 0},
		{Group: "alert", Key: "DEYE_MODBUS_R556_FAULT_WORD_2_RAW", Name: "Deye Inverter Fault Word 2 (Raw U16)", Value: 0x8000},
		{Group: "alert", Key: "DEYE_MODBUS_R557_FAULT_WORD_3_RAW", Name: "Deye Inverter Fault Word 3 (Raw U16)", Value: 0xFFFF},
		{Group: "alert", Key: "DEYE_MODBUS_R558_FAULT_WORD_4_RAW", Name: "Deye Inverter Fault Word 4 (Raw U16)", Value: 0},
	}
	snapshot := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INVERTER_001",
		CollectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Metrics:     metrics,
	}
	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got := discoveryPayload(t, messages, "homeassistant/binary_sensor/synthetic_inverter_001_alarm_or_fault_active/config"); got != "" {
		t.Fatalf("legacy cross-source aggregate tombstone payload = %q, want empty", got)
	}
	modbusAggregate := decodeDiscovery(t, messages, "homeassistant/binary_sensor/synthetic_inverter_001_modbus_warning_alarm_fault_active/config")
	if modbusAggregate["name"] != "Warning/Alarm/Fault Active (modbus)" {
		t.Fatalf("Modbus alert discovery name = %q", modbusAggregate["name"])
	}
	if template, _ := modbusAggregate["value_template"].(string); !strings.Contains(template, "value_json.get('alert_domain') == 'modbus'") || !strings.Contains(template, "else 'OFF'") {
		t.Fatalf("Modbus aggregate template is not source-domain-safe: %#v", modbusAggregate)
	}
	alertCount := decodeDiscovery(t, messages, "homeassistant/sensor/synthetic_inverter_001_alert_count/config")
	if alertCount["name"] != "Current Source Active Warning/Alarm/Fault Count" {
		t.Fatalf("alert count discovery name = %q", alertCount["name"])
	}
	if template, _ := alertCount["value_template"].(string); !strings.Contains(template, "value_json.alerts_known") || !strings.Contains(template, "else none") {
		t.Fatalf("alert count template is not source-switch/missing-safe: %#v", alertCount)
	}

	seen := make(map[string]struct{}, len(metrics))
	for _, metric := range metrics {
		stateKey := metricStateKey(metric)
		if _, duplicate := seen[stateKey]; duplicate {
			t.Fatalf("duplicate sanitized state key %q", stateKey)
		}
		seen[stateKey] = struct{}{}
		if strings.Contains(stateKey, "l_b_a_f") || strings.Contains(stateKey, "l_b_f_f") {
			t.Fatalf("Modbus raw word received a Jinko lithium alias: %q", stateKey)
		}

		numericTopic := "homeassistant/sensor/synthetic_inverter_001_" + stateKey + "/config"
		numeric := decodeDiscovery(t, messages, numericTopic)
		if numeric["entity_category"] != "diagnostic" {
			t.Fatalf("numeric discovery for %s = %#v, want diagnostic", stateKey, numeric)
		}
		if _, hasUnit := numeric["unit_of_measurement"]; hasUnit {
			t.Fatalf("numeric discovery for %s unexpectedly has a unit: %#v", stateKey, numeric)
		}

		binaryTopic := "homeassistant/binary_sensor/synthetic_inverter_001_" + stateKey + "_active/config"
		binary := decodeDiscovery(t, messages, binaryTopic)
		if binary["device_class"] != "problem" || binary["entity_category"] != "diagnostic" {
			t.Fatalf("binary discovery for %s = %#v, want diagnostic problem", stateKey, binary)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("unique state keys = %d, want 6", len(seen))
	}

	state := publisher.buildStatePayload(snapshot, time.Second)
	if state.AlertCount != 3 || !state.AlertsKnown || !state.AlertsActive {
		t.Fatalf("alert count/known/active = %d/%v/%v, want 3/true/true", state.AlertCount, state.AlertsKnown, state.AlertsActive)
	}
	if state.AlertDomain != "modbus" {
		t.Fatalf("AlertDomain = %q, want modbus", state.AlertDomain)
	}
	if len(state.AlertMetrics) != 6 {
		t.Fatalf("alert metric count = %d, want all six raw words", len(state.AlertMetrics))
	}
	if got := state.AlertMetrics["alert_deye_modbus_r556_fault_word_2_raw"]; got != 0x8000 {
		t.Fatalf("R556 raw value = %.0f, want 32768", got)
	}
	if got := state.AlertMetrics["alert_deye_modbus_r557_fault_word_3_raw"]; got != 0xFFFF {
		t.Fatalf("R557 raw value = %.0f, want 65535", got)
	}
}

func TestAlertAggregateDiscoveryIsSourceScopedAndLegacyTopicIsDeleted(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	modbus := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INVERTER_001",
		CollectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "alert", Key: "DEYE_MODBUS_R554_WARNING_WORD_2_RAW", Name: "Deye Inverter Warning Word 2 (Raw U16)", Value: 1},
		},
	}
	if err := publisher.OnPollSuccess(modbus, time.Second); err != nil {
		t.Fatalf("Modbus OnPollSuccess() error = %v", err)
	}
	legacyTopic := "homeassistant/binary_sensor/synthetic_inverter_001_alarm_or_fault_active/config"
	if !client.hasTopic(legacyTopic, true, "") {
		t.Fatalf("legacy aggregate tombstone was not retained-published: %#v", client.messages)
	}
	for _, message := range client.messages {
		if message.topic == legacyTopic && message.body != "" {
			t.Fatalf("legacy aggregate tombstone body = %q, want empty", message.body)
		}
	}
	if !client.hasTopic("homeassistant/binary_sensor/synthetic_inverter_001_modbus_warning_alarm_fault_active/config", true, "value_json.get('alert_domain') == 'modbus'") {
		t.Fatalf("source-scoped Modbus aggregate was not published: %#v", client.messages)
	}
	modbusMetricTopic := "homeassistant/binary_sensor/synthetic_inverter_001_alert_deye_modbus_r554_warning_word_2_raw_active/config"
	if !client.hasTopic(modbusMetricTopic, true, "|float(0)") {
		t.Fatalf("retained Modbus metric discovery does not default numeric conversion: %#v", client.messages)
	}

	client.clear()
	jinko := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "SYNTHETIC_INVERTER_001",
		CollectedAt: modbus.CollectedAt.Add(time.Minute),
		Metrics: []model.Metric{
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 0},
		},
	}
	if err := publisher.OnPollSuccess(jinko, time.Second); err != nil {
		t.Fatalf("Jinko OnPollSuccess() error = %v", err)
	}
	if !client.hasTopic("homeassistant/binary_sensor/synthetic_inverter_001_jinko_warning_alarm_fault_active/config", true, "value_json.get('alert_domain') == 'jinko'") {
		t.Fatalf("source switch did not publish a distinct Jinko aggregate: %#v", client.messages)
	}
	var state statePayload
	stateTopic := "jinko-exporter/synthetic_inverter_001/state"
	foundState := false
	for _, message := range client.messages {
		if message.topic != stateTopic {
			continue
		}
		if err := json.Unmarshal([]byte(message.body), &state); err != nil {
			t.Fatalf("decode source-switched state: %v", err)
		}
		foundState = true
	}
	if !foundState || state.AlertDomain != "jinko" || !state.AlertsKnown || state.AlertsActive || state.AlertCount != 0 {
		t.Fatalf("source-switched alert state = found=%v domain=%q known=%v active=%v count=%d", foundState, state.AlertDomain, state.AlertsKnown, state.AlertsActive, state.AlertCount)
	}
	if _, exists := state.AlertMetrics["alert_deye_modbus_r554_warning_word_2_raw"]; exists {
		t.Fatalf("source-switched state unexpectedly retained the Modbus alert metric: %#v", state.AlertMetrics)
	}

	client.clear()
	publisher.onConnect(client)
	if !client.hasTopic(legacyTopic, true, "") {
		t.Fatalf("reconnect did not replay the retained legacy tombstone: %#v", client.messages)
	}
}

func TestModbusRunStateIsDiagnosticNumericAndNotAnAlert(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test-modbus-run-state",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	metric := model.Metric{
		Group: "status",
		Key:   "DEYE_MODBUS_R500_RUN_STATE",
		Name:  "Deye Inverter Run State Code",
		Value: 4,
	}
	snapshot := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INVERTER_001",
		CollectedAt: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
		Metrics:     []model.Metric{metric},
	}
	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatal(err)
	}
	stateKey := "status_deye_modbus_r500_run_state"
	numeric := decodeDiscovery(t, messages, "homeassistant/sensor/synthetic_inverter_001_"+stateKey+"/config")
	if numeric["entity_category"] != "diagnostic" {
		t.Fatalf("run-state discovery = %#v, want diagnostic", numeric)
	}
	if _, hasUnit := numeric["unit_of_measurement"]; hasUnit {
		t.Fatalf("run-state discovery unexpectedly has a unit: %#v", numeric)
	}
	for _, message := range messages {
		if message.topic == "homeassistant/binary_sensor/synthetic_inverter_001_"+stateKey+"_active/config" {
			t.Fatalf("run-state unexpectedly created alert binary discovery: %#v", message)
		}
	}
	state := publisher.buildStatePayload(snapshot, time.Second)
	if state.AlertCount != 0 || state.AlertsKnown || state.AlertsActive || len(state.AlertMetrics) != 0 {
		t.Fatalf("run-state alert state = count %d known %v active %v metrics %#v, want unknown/no alert semantics", state.AlertCount, state.AlertsKnown, state.AlertsActive, state.AlertMetrics)
	}
	if got := derefFloat(state.Metrics[stateKey]); got != 4 {
		t.Fatalf("run-state value = %.0f, want 4", got)
	}
}

func TestStatePayloadSerializesEmptyIdentityFields(t *testing.T) {
	publisher, _ := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "solarman",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(publisher.buildStatePayload(snapshot, time.Second))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	assertIdentityJSON(t, raw, map[string]string{
		"device_sn": "SYNTHETIC_INV_001",
		"parent_sn": "",
		"device_id": "",
		"site_id":   "",
	})
}

func TestIdentityDiscoveryTemplatesAreMissingSafeAndStable(t *testing.T) {
	publisher, _ := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "solarman",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 321},
		},
	}
	device := publisher.device(snapshot)
	stateTopic := publisher.stateTopic(device.ID)
	messages, err := publisher.discoveryMessages(snapshot, device, stateTopic)
	if err != nil {
		t.Fatalf("discoveryMessages() error = %v", err)
	}

	tests := []struct {
		stateKey      string
		name          string
		valueTemplate string
	}{
		{stateKey: "parent_sn", name: "Parent Serial", valueTemplate: "{{ value_json.get('parent_sn', '') }}"},
		{stateKey: "device_id", name: "Device ID", valueTemplate: "{{ value_json.get('device_id', '') }}"},
		{stateKey: "site_id", name: "Site ID", valueTemplate: "{{ value_json.get('site_id', '') }}"},
	}
	for _, tc := range tests {
		t.Run(tc.stateKey, func(t *testing.T) {
			topic := "homeassistant/sensor/synthetic_inv_001_" + tc.stateKey + "/config"
			payload := decodeDiscovery(t, messages, topic)
			assertDiscoveryContract(t, payload, map[string]any{
				"name":           tc.name,
				"unique_id":      "synthetic_inv_001_" + tc.stateKey,
				"state_topic":    "jinko-exporter/synthetic_inv_001/state",
				"value_template": tc.valueTemplate,
			})
		})
	}

	metric := decodeDiscovery(t, messages, "homeassistant/sensor/synthetic_inv_001_electric_dp1/config")
	assertDiscoveryContract(t, metric, map[string]any{
		"unique_id":      "synthetic_inv_001_electric_dp1",
		"state_topic":    "jinko-exporter/synthetic_inv_001/state",
		"value_template": "{{ value_json.get('metrics', {}).get('electric_dp1') }}",
	})
}

func TestColdMetricDiscoveryTemplateIsMissingSafeAndZeroIsPublished(t *testing.T) {
	publisher, _ := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "generator", Key: "GEN_TOTAL_POWER", Name: "Generator Total Power", Unit: "W", Value: 0},
		},
	}

	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatalf("discoveryMessages() error = %v", err)
	}
	payload := decodeDiscovery(t, messages, "homeassistant/sensor/synthetic_inv_001_generator_gen_total_power/config")
	if got := payload["value_template"]; got != "{{ value_json.get('metrics', {}).get('generator_gen_total_power') }}" {
		t.Fatalf("metric value_template = %#v, want exact nested missing-safe lookup", got)
	}

	state := publisher.buildStatePayload(snapshot, time.Second)
	value, ok := state.Metrics["generator_gen_total_power"]
	if !ok || value == nil || *value != 0 {
		t.Fatalf("zero metric = %#v (present=%v), want a present numeric zero", value, ok)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var encoded struct {
		Metrics map[string]*float64 `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &encoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	value, ok = encoded.Metrics["generator_gen_total_power"]
	if !ok || value == nil || *value != 0 {
		t.Fatalf("serialized zero metric = %#v (present=%v), want a present numeric zero", value, ok)
	}
}

func TestRepeatedColdFallbackPublishesWarningSafeIdentitySchema(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "solarman",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
	}

	for i := range 2 {
		snapshot.CollectedAt = snapshot.CollectedAt.Add(time.Minute)
		if err := publisher.OnPollSuccess(snapshot, time.Second); err != nil {
			t.Fatalf("OnPollSuccess() publication %d error = %v", i+1, err)
		}
	}

	stateCount := 0
	for _, message := range client.messages {
		if message.topic != "jinko-exporter/synthetic_inv_001/state" {
			continue
		}
		stateCount++
		assertIdentityJSON(t, []byte(message.body), map[string]string{
			"device_sn": "SYNTHETIC_INV_001",
			"parent_sn": "",
			"device_id": "",
			"site_id":   "",
		})
	}
	if stateCount != 2 {
		t.Fatalf("state publication count = %d, want 2", stateCount)
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

func TestStartAndCloseAreIdempotentAndJoinUnavailableConnect(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test-lifecycle",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	connectToken := newControlledMQTTToken()
	connectStarted := make(chan struct{})
	client := &recordingMQTTClient{
		connectToken:   connectToken,
		connectStarted: connectStarted,
	}
	publisher.client = client

	if err := publisher.Start(); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := publisher.Start(); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	select {
	case <-connectStarted:
	case <-time.After(time.Second):
		t.Fatal("initial Connect() did not start")
	}
	if got := client.connectCallCount(); got != 1 {
		t.Fatalf("Connect() calls = %d, want 1 after repeated Start", got)
	}

	closeDone := make(chan struct{})
	go func() {
		publisher.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not abort and join the unavailable initial connection")
	}
	publisher.Close()

	if got := client.disconnectCallCount(); got != 1 {
		t.Fatalf("Disconnect() calls = %d, want 1 after repeated Close", got)
	}
	if !connectToken.isDone() {
		t.Fatal("initial Connect token remains pending after Close")
	}
	publisher.mu.Lock()
	closed := publisher.closed
	availability := publisher.lastAvailability
	publisher.mu.Unlock()
	if !closed || availability != availabilityOffline {
		t.Fatalf("closed = %v, last availability = %q; want true/offline", closed, availability)
	}
	if err := publisher.Start(); err != errPublisherClosed {
		t.Fatalf("Start() after Close error = %v, want %v", err, errPublisherClosed)
	}
}

func TestClosePreventsConnectInitiationAfterCancellation(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker:          "tcp://localhost:1883",
		ClientID:        "test-connect-initiation-race",
		TopicPrefix:     "jinko-exporter",
		DiscoveryPrefix: "homeassistant",
		Retain:          true,
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	disconnectStarted := make(chan struct{})
	client := &recordingMQTTClient{disconnectStarted: disconnectStarted}
	publisher.client = client
	attemptReached := make(chan struct{})
	attemptRelease := make(chan struct{})
	var attemptSignal sync.Once
	publisher.beforeConnectAttempt = func() {
		attemptSignal.Do(func() { close(attemptReached) })
		<-attemptRelease
	}

	if err := publisher.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-attemptReached:
	case <-time.After(time.Second):
		t.Fatal("connection worker did not reach the pre-Connect barrier")
	}

	closeDone := make(chan struct{})
	go func() {
		publisher.Close()
		close(closeDone)
	}()
	select {
	case <-disconnectStarted:
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel and Disconnect while Connect initiation was paused")
	}
	close(attemptRelease)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not join the paused connection worker")
	}

	if got := client.connectCallCount(); got != 0 {
		t.Fatalf("Connect() calls after cancellation/Disconnect = %d, want 0", got)
	}
	if got := client.disconnectCallCount(); got != 1 {
		t.Fatalf("Disconnect() calls = %d, want 1", got)
	}
}

func TestOnConnectRacingCloseLeavesOfflineFinalAndCannotReplayAfterClose(t *testing.T) {
	publisher, client := newPublisherWithRecordingClient(t)
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 1840},
		},
	}
	if err := publisher.OnPollSuccess(snapshot, time.Second); err != nil {
		t.Fatalf("OnPollSuccess() error = %v", err)
	}
	client.clear()

	publishStarted := make(chan struct{})
	publishRelease := make(chan struct{})
	client.blockPublish(publisher.availabilityTopic, availabilityOnline, publishStarted, publishRelease)
	callbackDone := make(chan struct{})
	go func() {
		publisher.onConnect(client)
		close(callbackDone)
	}()
	select {
	case <-publishStarted:
	case <-time.After(time.Second):
		t.Fatal("onConnect did not reach its online availability publish")
	}

	closeDone := make(chan struct{})
	go func() {
		publisher.Close()
		close(closeDone)
	}()
	deadline := time.Now().Add(time.Second)
	for publisher.lifecycleMu.TryLock() {
		publisher.lifecycleMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("Close() did not enter the lifecycle critical section")
		}
		runtime.Gosched()
	}
	close(publishRelease)

	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("onConnect callback did not finish")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not finish after the callback was released")
	}

	availability := client.payloadsForTopic(publisher.availabilityTopic)
	if len(availability) < 2 || availability[len(availability)-2] != availabilityOnline || availability[len(availability)-1] != availabilityOffline {
		t.Fatalf("availability publish order = %#v, want online followed by final offline", availability)
	}
	messageCount := client.messageCount()
	publisher.onConnect(client)
	if got := client.messageCount(); got != messageCount {
		t.Fatalf("post-close onConnect published %d new messages, want 0", got-messageCount)
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
	raw := discoveryPayload(t, messages, topic)
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode discovery payload for %s: %v", topic, err)
	}
	return payload
}

func discoveryPayload(t *testing.T, messages []discoveryMessage, topic string) string {
	t.Helper()
	for _, msg := range messages {
		if msg.topic == topic {
			return msg.payload
		}
	}
	t.Fatalf("topic %s not found", topic)
	return ""
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

func assertIdentityJSON(t *testing.T, raw []byte, expected map[string]string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode state payload: %v", err)
	}
	for key, want := range expected {
		got, ok := payload[key]
		if !ok || got != want {
			t.Fatalf("%s = %#v, present=%v, want %q in payload %#v", key, got, ok, want, payload)
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
	mu sync.Mutex

	open              bool
	messages          []publishedMQTTMessage
	connectToken      mqtt.Token
	connectStarted    chan struct{}
	connectSignal     sync.Once
	connectCalls      int
	disconnectCalls   int
	disconnectStarted chan struct{}
	disconnectSignal  sync.Once

	blockedTopic   string
	blockedBody    string
	publishStarted chan struct{}
	publishRelease <-chan struct{}
	publishSignal  sync.Once
	publishErrors  []error
}

func (c *recordingMQTTClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open
}

func (c *recordingMQTTClient) IsConnectionOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.open
}

func (c *recordingMQTTClient) Connect() mqtt.Token {
	c.mu.Lock()
	c.connectCalls++
	token := c.connectToken
	started := c.connectStarted
	c.mu.Unlock()
	if started != nil {
		c.connectSignal.Do(func() { close(started) })
	}
	if token == nil {
		return staticMQTTToken{}
	}
	return token
}

func (c *recordingMQTTClient) Disconnect(uint) {
	c.mu.Lock()
	c.disconnectCalls++
	c.open = false
	token := c.connectToken
	started := c.disconnectStarted
	c.mu.Unlock()
	if started != nil {
		c.disconnectSignal.Do(func() { close(started) })
	}
	if controlled, ok := token.(*controlledMQTTToken); ok {
		controlled.complete(errClientNotConnected)
	}
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
	c.mu.Lock()
	c.messages = append(c.messages, publishedMQTTMessage{topic: topic, retain: retained, body: body})
	var publishErr error
	if len(c.publishErrors) > 0 {
		publishErr = c.publishErrors[0]
		c.publishErrors = c.publishErrors[1:]
	}
	shouldBlock := topic == c.blockedTopic && body == c.blockedBody && c.publishRelease != nil
	started := c.publishStarted
	release := c.publishRelease
	c.mu.Unlock()
	if shouldBlock {
		if started != nil {
			c.publishSignal.Do(func() { close(started) })
		}
		<-release
	}
	return staticMQTTToken{err: publishErr}
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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = nil
}

func (c *recordingMQTTClient) failNextPublishes(errs ...error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishErrors = append(c.publishErrors, errs...)
}

func (c *recordingMQTTClient) hasTopic(topic string, retain bool, contains string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
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

func (c *recordingMQTTClient) blockPublish(topic, body string, started chan struct{}, release <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blockedTopic = topic
	c.blockedBody = body
	c.publishStarted = started
	c.publishRelease = release
}

func (c *recordingMQTTClient) connectCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectCalls
}

func (c *recordingMQTTClient) disconnectCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disconnectCalls
}

func (c *recordingMQTTClient) messageCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.messages)
}

func (c *recordingMQTTClient) payloadsForTopic(topic string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	payloads := make([]string, 0)
	for _, message := range c.messages {
		if message.topic == topic {
			payloads = append(payloads, message.body)
		}
	}
	return payloads
}

type controlledMQTTToken struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newControlledMQTTToken() *controlledMQTTToken {
	return &controlledMQTTToken{done: make(chan struct{})}
}

func (t *controlledMQTTToken) complete(err error) {
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.mu.Unlock()
		close(t.done)
	})
}

func (t *controlledMQTTToken) isDone() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *controlledMQTTToken) Wait() bool {
	<-t.done
	return true
}

func (t *controlledMQTTToken) WaitTimeout(timeout time.Duration) bool {
	select {
	case <-t.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (t *controlledMQTTToken) Done() <-chan struct{} {
	return t.done
}

func (t *controlledMQTTToken) Error() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

type staticMQTTToken struct {
	err error
}

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

func (t staticMQTTToken) Error() error {
	return t.err
}
