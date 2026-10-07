package hamqtt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestExternalCTDiscoveryAndStatePreserveSignedValuesAndMissingCurrents(t *testing.T) {
	publisher, err := NewPublisher(config.MQTTConfig{
		Broker: "tcp://localhost:1883", ClientID: "external-ct-test",
		TopicPrefix: "jinko-exporter", DiscoveryPrefix: "homeassistant", Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()

	metrics := []model.Metric{
		{Group: "grid", Key: "CT1_P_E", Name: "External\u00a0CT1\u00a0Power", Unit: "W", Value: -101},
		{Group: "grid", Key: "CT2_P_E", Name: "External\u00a0CT2\u00a0Power", Unit: "W", Value: 98},
		{Group: "grid", Key: "CT3_P_E", Name: "External\u00a0CT3\u00a0Power", Unit: "W", Value: 0},
		{Group: "grid", Key: "CT_T_E", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W", Value: -3},
		{Group: "grid", Key: "JINKO_MODBUS_R613_EXTERNAL_CT_L1_CURRENT", Name: "External CT Current L1", Unit: "A", Value: -0.57},
		{Group: "grid", Key: "JINKO_MODBUS_R614_EXTERNAL_CT_L2_CURRENT", Name: "External CT Current L2", Unit: "A", Value: 0.62},
		{Group: "grid", Key: "JINKO_MODBUS_R615_EXTERNAL_CT_L3_CURRENT", Name: "External CT Current L3", Unit: "A", Value: 0},
	}
	snapshot := &model.Snapshot{
		Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now().UTC(), Metrics: metrics,
	}
	device := publisher.device(snapshot)
	messages, err := publisher.discoveryMessages(snapshot, device, publisher.stateTopic(device.ID))
	if err != nil {
		t.Fatal(err)
	}
	state := publisher.buildStatePayload(snapshot, time.Second)
	if state.MetricCount != len(metrics) {
		t.Fatalf("MetricCount = %d, want %d", state.MetricCount, len(metrics))
	}
	for _, metric := range metrics {
		stateKey := "grid_" + strings.ToLower(metric.Key)
		id := "synthetic_inv_001_" + stateKey
		discovery := decodeDiscovery(t, messages, "homeassistant/sensor/"+id+"/config")
		class := "power"
		if metric.Unit == "A" {
			class = "current"
		}
		if discovery["unique_id"] != id || discovery["name"] != metric.Name ||
			discovery["device_class"] != class || discovery["state_class"] != "measurement" ||
			discovery["unit_of_measurement"] != metric.Unit {
			t.Fatalf("discovery for %s = %#v", metric.Key, discovery)
		}
		wantTemplate := "{{ value_json.get('metrics', {}).get('" + stateKey + "') }}"
		if discovery["value_template"] != wantTemplate {
			t.Fatalf("value template for %s = %v, want missing-safe %q", metric.Key, discovery["value_template"], wantTemplate)
		}
		value, ok := state.Metrics[stateKey]
		if !ok || value == nil || *value != metric.Value {
			t.Fatalf("state metric %s = %v, want signed value %v", stateKey, value, metric.Value)
		}
	}

	// Cloud fallback can supply canonical CT power, but not native CT current.
	// An absent current must invalidate the old reading, not become zero or
	// retain the last Modbus value under the new publication timestamp.
	fallback := &model.Snapshot{
		Source: "solarman", DeviceSN: snapshot.DeviceSN, CollectedAt: snapshot.CollectedAt.Add(time.Minute),
		Metrics: []model.Metric{{Group: "grid", Key: "CT_T_E", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W", Value: -20}},
	}
	fallbackState := publisher.buildStatePayload(fallback, time.Second)
	data, err := json.Marshal(fallbackState)
	if err != nil {
		t.Fatal(err)
	}
	var encoded struct {
		Metrics map[string]*float64 `json:"metrics"`
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	for _, metric := range metrics {
		stateKey := "grid_" + strings.ToLower(metric.Key)
		value, ok := encoded.Metrics[stateKey]
		if !ok {
			t.Fatalf("previously discovered metric %s is absent, want explicit null", stateKey)
		}
		if metric.Key == "CT_T_E" {
			if value == nil || *value != -20 {
				t.Fatalf("fallback CT power = %v, want -20", value)
			}
		} else if value != nil {
			t.Fatalf("missing fallback metric %s = %v, want null", stateKey, *value)
		}
	}
	if fallbackState.MetricCount != 1 {
		t.Fatalf("fallback MetricCount = %d, want one actual measurement", fallbackState.MetricCount)
	}
}
