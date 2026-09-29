package hamqtt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPersistentDiscoveryRestartRestoresSchemaAndPublishesMissingNulls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	first, _ := newPersistentPublisher(t, path)
	primary := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800},
		model.Metric{Group: "battery", Key: "SOC", Name: "Battery SoC", Unit: "%", Value: 75},
	)
	primary.Meta = map[string]string{"profile": "readonly-v1"}
	if err := first.OnPollSuccess(primary, time.Second); err != nil {
		t.Fatal(err)
	}

	restarted, client := newPersistentPublisher(t, path)
	if len(restarted.cachedDiscovery) != 0 {
		t.Fatalf("startup cached discovery count = %d, want no generic pre-poll replay", len(restarted.cachedDiscovery))
	}
	partial := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 900},
	)
	if err := restarted.OnPollSuccess(partial, time.Second); err != nil {
		t.Fatal(err)
	}

	if !client.hasTopic("homeassistant/sensor/stable_inverter_battery_soc/config", true, "get('battery_soc')") {
		t.Fatalf("restored battery discovery was not regenerated: %#v", client.messages)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 900 {
		t.Fatalf("electric_dp1 = %v, want 900", got)
	}
	if value, ok := state.Metrics["battery_soc"]; !ok || value != nil {
		t.Fatalf("battery_soc = %#v (present=%v), want persisted missing null", value, ok)
	}
}

func TestPersistentDiscoveryColdFallbackCannotEstablishOrdinarySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	fallback := discoverySnapshot("jinko",
		model.Metric{Group: "electric", Key: "CLOUD_ONLY", Name: "Cloud Only", Unit: "W", Value: 42},
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Lithium Battery Fault", Value: 0},
	)
	if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}

	if client.hasTopic("homeassistant/sensor/stable_inverter_electric_cloud_only/config", true, "") {
		t.Fatalf("cold fallback established ordinary discovery: %#v", client.messages)
	}
	if !client.hasTopic("homeassistant/sensor/stable_inverter_alert_l_b_f_f/config", true, "") ||
		!client.hasTopic("homeassistant/binary_sensor/stable_inverter_jinko_warning_alarm_fault_active/config", true, "get('alert_domain') == 'jinko'") {
		t.Fatalf("cold fallback alert schema was not retained: %#v", client.messages)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if _, exists := state.Metrics["electric_cloud_only"]; exists {
		t.Fatalf("cold fallback-only ordinary metric leaked into MQTT state: %#v", state.Metrics)
	}
	if got := derefFloat(state.Metrics["alert_l_b_f_f"]); got != 0 {
		t.Fatalf("zero alert value = %v, want numeric zero", got)
	}

	stored := readDiscoveryStateForTest(t, path, publisher.discoveryState.Binding)
	if len(stored.OrdinaryMetrics) != 0 || len(stored.AlertMetrics["jinko"]) != 1 {
		t.Fatalf("cold fallback manifest ownership = ordinary %#v alerts %#v", stored.OrdinaryMetrics, stored.AlertMetrics)
	}
}

func TestPersistentDiscoveryOnlyOrdinaryColdFallbackPersistsEmptyOwnershipBeforePublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	fallback := discoverySnapshot("jinko",
		model.Metric{Group: "electric", Key: "CLOUD_ONLY", Name: "Cloud Only", Unit: "W", Value: 42},
	)
	if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}
	stored := readDiscoveryStateForTest(t, path, publisher.discoveryState.Binding)
	if len(stored.OrdinaryMetrics) != 0 || len(stored.GridLoadMetrics) != 0 || len(stored.AlertMetrics) != 0 {
		t.Fatalf("ordinary cold fallback changed durable ownership: %#v", stored)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if len(state.Metrics) != 0 {
		t.Fatalf("ordinary cold fallback MQTT metrics = %#v, want empty", state.Metrics)
	}
}

func TestPersistentDiscoveryPrimaryThenFallbackAndRestartKeepsCanonicalUnion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	if err := publisher.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 500},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	client.clear()

	fallback := discoverySnapshot("jinko",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 450},
		model.Metric{Group: "electric", Key: "CLOUD_ONLY", Name: "Cloud Only", Unit: "W", Value: 99},
		model.Metric{Group: "alert", Key: "L_B_A_F", Name: "Lithium Battery Alarm", Value: 1},
	)
	if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 450 {
		t.Fatalf("canonical fallback value = %v, want 450", got)
	}
	if _, exists := state.Metrics["electric_cloud_only"]; exists {
		t.Fatalf("fallback-only value leaked after primary schema: %#v", state.Metrics)
	}

	restarted, restartedClient := newPersistentPublisher(t, path)
	secondFallback := discoverySnapshot("solarman",
		model.Metric{Group: "electric", Key: "SOLARMAN_ONLY", Name: "Solarman Only", Unit: "W", Value: 7},
	)
	if err := restarted.OnPollSuccess(secondFallback, time.Second); err != nil {
		t.Fatal(err)
	}
	if !restartedClient.hasTopic("homeassistant/sensor/stable_inverter_electric_dp1/config", true, "") ||
		!restartedClient.hasTopic("homeassistant/binary_sensor/stable_inverter_jinko_warning_alarm_fault_active/config", true, "") {
		t.Fatalf("restart did not regenerate complete primary/alert union: %#v", restartedClient.messages)
	}
	restartedState := publishedState(t, restartedClient, "jinko-exporter/stable_inverter/state")
	if value, ok := restartedState.Metrics["electric_dp1"]; !ok || value != nil {
		t.Fatalf("missing primary metric after restart = %#v (present=%v), want null", value, ok)
	}
	if _, exists := restartedState.Metrics["electric_solarman_only"]; exists {
		t.Fatalf("second cold fallback extended ordinary schema: %#v", restartedState.Metrics)
	}
}

func TestPersistentDiscoveryGridLoadIsIndependentMonotonicEnrichment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	fallback := discoverySnapshot("jinko",
		model.Metric{Group: "grid_load", Key: "total_power", Name: "Grid Load Total Power", Unit: "W", Value: 1200},
		model.Metric{Group: "electric", Key: "CLOUD_ONLY", Name: "Cloud Only", Unit: "W", Value: 1},
	)
	if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}
	if !client.hasTopic("homeassistant/sensor/stable_inverter_grid_load_total_power/config", true, "") {
		t.Fatalf("fallback grid_load enrichment was not discovered: %#v", client.messages)
	}

	restarted, restartedClient := newPersistentPublisher(t, path)
	primary := discoverySnapshot("modbus",
		model.Metric{Group: "grid_load", Key: "l1_power", Name: "Grid Load L1 Power", Unit: "W", Value: 400},
	)
	if err := restarted.OnPollSuccess(primary, time.Second); err != nil {
		t.Fatal(err)
	}
	for _, topic := range []string{
		"homeassistant/sensor/stable_inverter_grid_load_total_power/config",
		"homeassistant/sensor/stable_inverter_grid_load_l1_power/config",
	} {
		if !restartedClient.hasTopic(topic, true, "") {
			t.Fatalf("grid_load union topic %q missing: %#v", topic, restartedClient.messages)
		}
	}
}

func TestPersistentDiscoveryKeepsPerSourceAlertsInCompleteReconnectCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	if err := publisher.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "alert", Key: "R553", Name: "Modbus Warning", Value: 0},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := publisher.OnPollSuccess(discoverySnapshot("jinko",
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Jinko Fault", Value: 1},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	client.clear()
	publisher.onConnect(client)
	for _, topic := range []string{
		"homeassistant/binary_sensor/stable_inverter_modbus_warning_alarm_fault_active/config",
		"homeassistant/binary_sensor/stable_inverter_jinko_warning_alarm_fault_active/config",
		"homeassistant/binary_sensor/stable_inverter_alert_r553_active/config",
		"homeassistant/binary_sensor/stable_inverter_alert_l_b_f_f_active/config",
	} {
		if !client.hasTopic(topic, true, "") {
			t.Fatalf("complete reconnect cache omitted %q: %#v", topic, client.messages)
		}
	}
}

func TestPersistentDiscoveryReloadRegeneratesSourceScopedAlertAvailability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	first, _ := newPersistentPublisher(t, path)
	if err := first.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "alert", Key: "R553", Name: "Modbus Warning", Value: 0},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := first.OnPollSuccess(discoverySnapshot("jinko",
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Jinko Fault", Value: 1},
	), time.Second); err != nil {
		t.Fatal(err)
	}

	restarted, client := newPersistentPublisher(t, path)
	// A current Modbus zero must make the Modbus aggregate and R553 entities
	// available, while the retained Jinko aggregate and metric entities become
	// unavailable through their state-topic availability condition.
	if err := restarted.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "alert", Key: "R553", Name: "Modbus Warning", Value: 0},
	), time.Second); err != nil {
		t.Fatal(err)
	}

	assertScopedAvailability := func(topic, template string) {
		t.Helper()
		payload := decodeDiscovery(t, restarted.cachedDiscovery, topic)
		if got := payload["availability_mode"]; got != "all" {
			t.Fatalf("%s availability_mode = %#v, want all", topic, got)
		}
		if _, exists := payload["availability_topic"]; exists {
			t.Fatalf("%s retained legacy availability_topic: %#v", topic, payload)
		}
		availability, ok := payload["availability"].([]any)
		if !ok || len(availability) != 2 {
			t.Fatalf("%s availability = %#v, want exact two-condition list", topic, payload["availability"])
		}
		global, ok := availability[0].(map[string]any)
		if !ok || global["topic"] != "jinko-exporter/availability" || global["payload_available"] != "online" || global["payload_not_available"] != "offline" {
			t.Fatalf("%s global availability = %#v", topic, availability[0])
		}
		scoped, ok := availability[1].(map[string]any)
		if !ok || scoped["topic"] != "jinko-exporter/stable_inverter/state" || scoped["value_template"] != template || scoped["payload_available"] != "online" || scoped["payload_not_available"] != "offline" {
			t.Fatalf("%s scoped availability = %#v", topic, availability[1])
		}
	}
	assertScopedAvailability(
		"homeassistant/binary_sensor/stable_inverter_modbus_warning_alarm_fault_active/config",
		"{{ 'online' if value_json.get('alert_domain') == 'modbus' and value_json.get('alerts_known', false) else 'offline' }}",
	)
	assertScopedAvailability(
		"homeassistant/binary_sensor/stable_inverter_jinko_warning_alarm_fault_active/config",
		"{{ 'online' if value_json.get('alert_domain') == 'jinko' and value_json.get('alerts_known', false) else 'offline' }}",
	)
	assertScopedAvailability(
		"homeassistant/sensor/stable_inverter_alert_r553/config",
		"{{ 'online' if value_json.get('alert_domain') == 'modbus' and 'alert_r553' in value_json.get('alert_metrics', {}) else 'offline' }}",
	)
	assertScopedAvailability(
		"homeassistant/binary_sensor/stable_inverter_alert_l_b_f_f_active/config",
		"{{ 'online' if value_json.get('alert_domain') == 'jinko' and 'alert_l_b_f_f' in value_json.get('alert_metrics', {}) else 'offline' }}",
	)

	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if state.AlertDomain != "modbus" || !state.AlertsKnown || state.AlertsActive || state.AlertMetrics["alert_r553"] != 0 {
		t.Fatalf("restarted Modbus state = %#v, want a known explicit zero", state)
	}
	if _, exists := state.AlertMetrics["alert_l_b_f_f"]; exists {
		t.Fatalf("inactive Jinko metric leaked into current alert domain: %#v", state.AlertMetrics)
	}

	client.clear()
	if err := restarted.OnPollSuccess(discoverySnapshot("jinko",
		model.Metric{Group: "alert", Key: "L_B_F_F", Name: "Jinko Fault", Value: 0},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if state.AlertDomain != "jinko" || !state.AlertsKnown || state.AlertsActive || state.AlertMetrics["alert_l_b_f_f"] != 0 {
		t.Fatalf("source-switched Jinko state = %#v, want a known explicit zero", state)
	}
	if _, exists := state.AlertMetrics["alert_r553"]; exists {
		t.Fatalf("inactive Modbus metric leaked after source switch: %#v", state.AlertMetrics)
	}

	client.clear()
	restarted.onConnect(client)
	for _, topic := range []string{
		"homeassistant/binary_sensor/stable_inverter_modbus_warning_alarm_fault_active/config",
		"homeassistant/binary_sensor/stable_inverter_jinko_warning_alarm_fault_active/config",
		"homeassistant/sensor/stable_inverter_alert_r553/config",
		"homeassistant/binary_sensor/stable_inverter_alert_l_b_f_f_active/config",
	} {
		if !client.hasTopic(topic, true, `"availability_mode":"all"`) {
			t.Fatalf("reconnect did not replay regenerated availability payload for %s: %#v", topic, client.messages)
		}
	}
}

func TestPersistentDiscoveryReloadRepublishesAllAbsentJinkoAlertTemplatesSafely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	first, _ := newPersistentPublisher(t, path)
	jinkoAlerts := []model.Metric{
		{Group: "alert", Key: "L_B_A_F", Name: "Lithium Battery Alarm", Value: 0},
		{Group: "alert", Key: "L_B_F_F", Name: "Lithium Battery Fault", Value: 0},
		{Group: "alert", Key: "L_B_A_F2", Name: "Lithium Battery Alarm 2", Value: 0},
		{Group: "alert", Key: "L_B_F_F2", Name: "Lithium Battery Fault 2", Value: 0},
	}
	if err := first.OnPollSuccess(discoverySnapshot("jinko", jinkoAlerts...), time.Second); err != nil {
		t.Fatal(err)
	}

	restarted, client := newPersistentPublisher(t, path)
	if err := restarted.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "alert", Key: "R553", Name: "Modbus Warning", Value: 0},
	), time.Second); err != nil {
		t.Fatal(err)
	}

	for _, stateKey := range []string{"alert_l_b_a_f", "alert_l_b_f_f", "alert_l_b_a_f2", "alert_l_b_f_f2"} {
		topic := "homeassistant/binary_sensor/stable_inverter_" + stateKey + "_active/config"
		payloads := client.payloadsForTopic(topic)
		if len(payloads) == 0 || payloads[len(payloads)-1] == "" {
			t.Fatalf("persisted absent Jinko config %s was not retained-republished: %#v", topic, payloads)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloads[len(payloads)-1]), &payload); err != nil {
			t.Fatalf("decode %s: %v", topic, err)
		}
		template, _ := payload["value_template"].(string)
		if !strings.Contains(template, "'"+stateKey+"' in value_json.get('alert_metrics', {})") ||
			!strings.Contains(template, "|float(0)") || strings.Contains(template, "|float !=") ||
			!strings.Contains(template, "else none") {
			t.Fatalf("migrated %s value_template = %q, want absent-safe float conversion", topic, template)
		}
		availability, ok := payload["availability"].([]any)
		if !ok || len(availability) != 2 {
			t.Fatalf("migrated %s availability = %#v", topic, payload["availability"])
		}
		scoped, _ := availability[1].(map[string]any)
		availabilityTemplate, _ := scoped["value_template"].(string)
		if !strings.Contains(availabilityTemplate, "get('alert_domain') == 'jinko'") ||
			!strings.Contains(availabilityTemplate, "'"+stateKey+"' in value_json.get('alert_metrics', {})") {
			t.Fatalf("migrated %s scoped availability = %q", topic, availabilityTemplate)
		}
	}

	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	for _, stateKey := range []string{"alert_l_b_a_f", "alert_l_b_f_f", "alert_l_b_a_f2", "alert_l_b_f_f2"} {
		if _, exists := state.AlertMetrics[stateKey]; exists {
			t.Fatalf("absent Jinko metric %s leaked into Modbus state: %#v", stateKey, state.AlertMetrics)
		}
	}
}

func TestPersistentDiscoverySharedAlertKeyRequiresIdenticalSemantics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, client := newPersistentPublisher(t, path)
	shared := model.Metric{Group: "alert", Key: "SHARED_FLAG", Name: "Shared Fault Flag", Value: 0}
	if err := publisher.OnPollSuccess(discoverySnapshot("jinko", shared), time.Second); err != nil {
		t.Fatal(err)
	}
	shared.Value = 1
	if err := publisher.OnPollSuccess(discoverySnapshot("solarman", shared), time.Second); err != nil {
		t.Fatalf("identical shared alert semantics were rejected: %v", err)
	}
	payload := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_alert_shared_flag/config")
	availability, ok := payload["availability"].([]any)
	if !ok || len(availability) != 2 {
		t.Fatalf("shared alert availability = %#v", payload["availability"])
	}
	scoped := availability[1].(map[string]any)
	wantTemplate := "{{ 'online' if value_json.get('alert_domain') in ['jinko', 'solarman'] and 'alert_shared_flag' in value_json.get('alert_metrics', {}) else 'offline' }}"
	if scoped["value_template"] != wantTemplate {
		t.Fatalf("shared alert ownership template = %#v, want %q", scoped["value_template"], wantTemplate)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	client.clear()
	ambiguous := shared
	ambiguous.Name = "Different Fault Meaning"
	if err := publisher.OnPollSuccess(discoverySnapshot("other_cloud", ambiguous), time.Second); err == nil {
		t.Fatal("different shared-key semantics were accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected ambiguous alert ownership changed the durable manifest")
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline {
		t.Fatalf("ambiguous alert rejection availability = %#v, want offline", payloads)
	}
}

func TestPersistentDiscoveryPrimaryMetadataUnionFiltersFallbackMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt-discovery-state.json")
	publisher, _ := newPersistentPublisher(t, path)
	primary := discoverySnapshot("modbus")
	primary.Meta = map[string]string{"profile": "v1", "firmware": "1.2.3"}
	if err := publisher.OnPollSuccess(primary, time.Second); err != nil {
		t.Fatal(err)
	}

	restarted, client := newPersistentPublisher(t, path)
	fallback := discoverySnapshot("jinko")
	fallback.Meta = map[string]string{"profile": "cloud-profile", "cloud_url": "https://example.invalid"}
	if err := restarted.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}
	if !client.hasTopic("homeassistant/sensor/stable_inverter_meta_profile/config", true, "") ||
		!client.hasTopic("homeassistant/sensor/stable_inverter_meta_firmware/config", true, "") {
		t.Fatalf("primary metadata discovery union was not restored: %#v", client.messages)
	}
	if client.hasTopic("homeassistant/sensor/stable_inverter_meta_cloud_url/config", true, "") {
		t.Fatalf("fallback metadata established discovery ownership: %#v", client.messages)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	if state.Meta["profile"] != "cloud-profile" {
		t.Fatalf("known canonical metadata value = %q, want fallback value for known key", state.Meta["profile"])
	}
	if _, exists := state.Meta["cloud_url"]; exists {
		t.Fatalf("fallback-only metadata leaked into state: %#v", state.Meta)
	}
}

func TestPersistentDiscoveryRejectsUnsafeStateWithoutOverwrite(t *testing.T) {
	basePath := filepath.Join(t.TempDir(), "state.json")
	base, _ := newPersistentPublisher(t, basePath)
	if err := base.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 1},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	validRaw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		raw  []byte
		cfg  func(config.MQTTConfig) config.MQTTConfig
	}{
		{name: "zero length", raw: nil},
		{name: "corrupt", raw: []byte("{")},
		{name: "unknown field", raw: []byte(`{"version":1,"unknown":true}`)},
		{name: "duplicate field", raw: []byte(`{"version":1,"version":1}`)},
		{name: "unsupported version", raw: []byte(strings.Replace(string(validRaw), `"version":1`, `"version":2`, 1))},
		{name: "oversize", raw: []byte(strings.Repeat("x", maxDiscoveryStateBytes+1))},
		{name: "binding mismatch", raw: validRaw, cfg: func(cfg config.MQTTConfig) config.MQTTConfig {
			cfg.DeviceID = "other_inverter"
			return cfg
		}},
		{name: "primary source mismatch", raw: validRaw, cfg: func(cfg config.MQTTConfig) config.MQTTConfig {
			cfg.PrimarySource = "jinko"
			return cfg
		}},
		{name: "topic prefix mismatch", raw: validRaw, cfg: func(cfg config.MQTTConfig) config.MQTTConfig {
			cfg.TopicPrefix = "other-bridge"
			return cfg
		}},
		{name: "discovery prefix mismatch", raw: validRaw, cfg: func(cfg config.MQTTConfig) config.MQTTConfig {
			cfg.DiscoveryPrefix = "other-homeassistant"
			return cfg
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, tt.raw, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := persistentMQTTConfig(path)
			if tt.cfg != nil {
				cfg = tt.cfg(cfg)
			}
			if publisher, err := NewPublisher(cfg); err == nil {
				publisher.Close()
				t.Fatal("NewPublisher() error = nil, want fail-closed state rejection")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(tt.raw) {
				t.Fatal("rejected discovery state was overwritten")
			}
		})
	}
}

func TestPersistentDiscoveryRequiresExplicitStableOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	tests := []struct {
		name   string
		mutate func(*config.MQTTConfig)
	}{
		{name: "device ID", mutate: func(cfg *config.MQTTConfig) { cfg.DeviceID = "" }},
		{name: "primary source", mutate: func(cfg *config.MQTTConfig) { cfg.PrimarySource = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := persistentMQTTConfig(path)
			tt.mutate(&cfg)
			if publisher, err := NewPublisher(cfg); err == nil {
				publisher.Close()
				t.Fatal("NewPublisher() error = nil, want explicit ownership requirement")
			}
		})
	}

	legacy := persistentMQTTConfig("")
	legacy.DeviceID = ""
	legacy.PrimarySource = ""
	publisher, err := NewPublisher(legacy)
	if err != nil {
		t.Fatalf("legacy in-memory mode unexpectedly rejected: %v", err)
	}
	publisher.Close()
}

func TestPersistentDiscoveryAtomicWriteAndPersistBeforePublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	publisher, _ := newPersistentPublisher(t, path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("NewPublisher created state before Start/listener bind: %v", err)
	}
	connectStarted := make(chan struct{})
	publisher.client.(*recordingMQTTClient).connectStarted = connectStarted
	if err := publisher.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connectStarted:
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Connect began before ownership manifest was durable: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not begin MQTT Connect")
	}
	publisher.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".jinko-mqtt-discovery-state-") {
			t.Fatalf("atomic state temp file was not removed: %s", entry.Name())
		}
	}

	missingParentPath := filepath.Join(t.TempDir(), "missing", "state.json")
	failing, client := newPersistentPublisher(t, missingParentPath)
	err = failing.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "generator", Key: "TOTAL_POWER", Name: "Generator Total Power", Unit: "W", Value: 0},
	), time.Second)
	if err == nil {
		t.Fatal("OnPollSuccess() error = nil, want state persistence failure")
	}
	if client.messageCount() != 0 || len(failing.cachedDiscovery) != 0 {
		t.Fatalf("persistence failure mutated/published schema: messages=%#v cache=%#v", client.messages, failing.cachedDiscovery)
	}

	startFailing, startClient := newPersistentPublisher(t, filepath.Join(t.TempDir(), "missing", "start-state.json"))
	if err := startFailing.Start(); err == nil {
		t.Fatal("Start() error = nil, want ownership-state persistence failure")
	}
	if startClient.connectCallCount() != 0 || startClient.messageCount() != 0 {
		t.Fatalf("failed Start connected or published before durable ownership: connects=%d messages=%#v", startClient.connectCallCount(), startClient.messages)
	}
}

func TestPersistentDiscoveryRejectsSameStateKeySchemaMutationBeforePublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	publisher, client := newPersistentPublisher(t, path)
	first := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 100},
	)
	if err := publisher.OnPollSuccess(first, time.Second); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeCache := publisher.cachedDiscoverySig
	beforeState := append([]byte(nil), publisher.cachedState...)
	client.clear()

	changed := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "Different Meaning", Unit: "kWh", Value: 200},
	)
	if err := publisher.OnPollSuccess(changed, time.Second); err == nil {
		t.Fatal("OnPollSuccess() error = nil, want schema-mutation rejection")
	} else if !strings.Contains(err.Error(), "changed schema") {
		t.Fatalf("OnPollSuccess() error = %q, want original schema-mutation error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || publisher.cachedDiscoverySig != beforeCache || string(publisher.cachedState) != string(beforeState) {
		t.Fatalf("rejected schema mutation changed durable/runtime cache: file_changed=%v discovery_cache_changed=%v state_cache_changed=%v", string(after) != string(before), publisher.cachedDiscoverySig != beforeCache, string(publisher.cachedState) != string(beforeState))
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline || client.messageCount() != 1 {
		t.Fatalf("schema rejection MQTT messages = %#v, want one retained offline availability", client.messages)
	}
	if !client.hasTopic(publisher.availabilityTopic, true, availabilityOffline) {
		t.Fatalf("schema rejection did not replace retained online availability: %#v", client.messages)
	}
	publisher.mu.Lock()
	lastAvailability := publisher.lastAvailability
	publisher.mu.Unlock()
	if lastAvailability != availabilityOffline {
		t.Fatalf("last availability = %q, want offline after schema rejection", lastAvailability)
	}

	client.clear()
	publisher.onConnect(client)
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline {
		t.Fatalf("reconnect availability = %#v, want offline only", payloads)
	}
	state := publishedState(t, client, publisher.cachedStateTopic)
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 100 {
		t.Fatalf("reconnect replayed metric value %v, want last accepted value 100", got)
	}
	if client.hasTopic("homeassistant/sensor/stable_inverter_electric_dp1/config", true, "Different Meaning") {
		t.Fatalf("reconnect replayed rejected schema: %#v", client.messages)
	}
}

func TestPersistentDiscoveryRetriesOfflineAfterFailedAvailabilityPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	publisher, client := newPersistentPublisher(t, path)
	accepted := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 100},
	)
	if err := publisher.OnPollSuccess(accepted, time.Second); err != nil {
		t.Fatal(err)
	}

	rejected := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "Different Meaning", Unit: "kWh", Value: 200},
	)
	client.clear()
	client.failNextPublishes(errors.New("synthetic offline publish failure"))
	if err := publisher.OnPollSuccess(rejected, time.Second); err == nil {
		t.Fatal("first OnPollSuccess() error = nil, want schema rejection")
	} else if !strings.Contains(err.Error(), "changed schema") {
		t.Fatalf("first OnPollSuccess() error = %q, want original schema rejection", err)
	}
	publisher.mu.Lock()
	firstAvailability := publisher.lastAvailability
	firstPending := publisher.offlinePublishPending
	publisher.mu.Unlock()
	if firstAvailability != availabilityOffline || !firstPending {
		t.Fatalf("after failed offline publish availability=%q pending=%v, want offline/true", firstAvailability, firstPending)
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline {
		t.Fatalf("first offline publish attempts = %#v, want one offline attempt", payloads)
	}

	client.clear()
	if err := publisher.OnPollSuccess(rejected, time.Second); err == nil {
		t.Fatal("second OnPollSuccess() error = nil, want schema rejection")
	} else if !strings.Contains(err.Error(), "changed schema") {
		t.Fatalf("second OnPollSuccess() error = %q, want original schema rejection", err)
	}
	publisher.mu.Lock()
	secondAvailability := publisher.lastAvailability
	secondPending := publisher.offlinePublishPending
	publisher.mu.Unlock()
	if secondAvailability != availabilityOffline || secondPending {
		t.Fatalf("after offline retry availability=%q pending=%v, want offline/false", secondAvailability, secondPending)
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline || client.messageCount() != 1 {
		t.Fatalf("offline retry messages = %#v, want one successful retained offline publish", client.messages)
	}

	client.clear()
	publisher.onConnect(client)
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline {
		t.Fatalf("reconnect availability = %#v, want offline only", payloads)
	}
	state := publishedState(t, client, publisher.cachedStateTopic)
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 100 {
		t.Fatalf("reconnect replayed metric value %v, want last accepted value 100", got)
	}
}

func TestPersistentDiscoveryPersistenceFailureMarksPriorOnlineOffline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	publisher, client := newPersistentPublisher(t, path)
	if err := publisher.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 100},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeDiscoveryCache := publisher.cachedDiscoverySig
	beforeState := append([]byte(nil), publisher.cachedState...)
	client.clear()

	// Force the next monotonic schema extension to fail before its atomic
	// manifest commit. The missing parent is deterministic on every platform.
	publisher.discoveryStateFile = filepath.Join(t.TempDir(), "missing", "state.json")
	err = publisher.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 200},
		model.Metric{Group: "generator", Key: "TOTAL_POWER", Name: "Generator Total Power", Unit: "W", Value: 0},
	), time.Second)
	if err == nil {
		t.Fatal("OnPollSuccess() error = nil, want state persistence failure")
	}
	if !strings.Contains(err.Error(), "create temporary MQTT discovery state") {
		t.Fatalf("OnPollSuccess() error = %q, want original persistence error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || publisher.cachedDiscoverySig != beforeDiscoveryCache || string(publisher.cachedState) != string(beforeState) {
		t.Fatalf("persistence failure changed durable/runtime cache: file_changed=%v discovery_cache_changed=%v state_cache_changed=%v", string(after) != string(before), publisher.cachedDiscoverySig != beforeDiscoveryCache, string(publisher.cachedState) != string(beforeState))
	}
	if _, leaked := publisher.discoveryState.OrdinaryMetrics["generator_total_power"]; leaked {
		t.Fatal("failed manifest extension leaked into in-memory ownership")
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline || client.messageCount() != 1 {
		t.Fatalf("persistence failure MQTT messages = %#v, want one retained offline availability", client.messages)
	}
	if !client.hasTopic(publisher.availabilityTopic, true, availabilityOffline) {
		t.Fatalf("persistence failure did not replace retained online availability: %#v", client.messages)
	}
	publisher.mu.Lock()
	lastAvailability := publisher.lastAvailability
	publisher.mu.Unlock()
	if lastAvailability != availabilityOffline {
		t.Fatalf("last availability = %q, want offline after persistence failure", lastAvailability)
	}

	client.clear()
	publisher.onConnect(client)
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline {
		t.Fatalf("reconnect availability = %#v, want offline only", payloads)
	}
	state := publishedState(t, client, publisher.cachedStateTopic)
	if got := derefFloat(state.Metrics["electric_dp1"]); got != 100 {
		t.Fatalf("reconnect replayed metric value %v, want last accepted value 100", got)
	}
	if _, leaked := state.Metrics["generator_total_power"]; leaked {
		t.Fatalf("reconnect replayed uncommitted metric: %#v", state.Metrics)
	}
	if client.hasTopic("homeassistant/sensor/stable_inverter_generator_total_power/config", true, "") {
		t.Fatalf("reconnect replayed uncommitted discovery: %#v", client.messages)
	}
}

func TestPersistentDiscoveryRejectsFallbackConflictWithOwnedPrimarySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	publisher, client := newPersistentPublisher(t, path)
	if err := publisher.OnPollSuccess(discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 100},
	), time.Second); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	client.clear()
	fallback := discoverySnapshot("jinko",
		model.Metric{Group: "electric", Key: "DP1", Name: "Different Meaning", Unit: "kWh", Value: 5},
	)
	if err := publisher.OnPollSuccess(fallback, time.Second); err == nil {
		t.Fatal("OnPollSuccess() error = nil, want conflicting fallback schema rejection")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("conflicting fallback changed durable state")
	}
	if payloads := client.payloadsForTopic(publisher.availabilityTopic); len(payloads) != 1 || payloads[0] != availabilityOffline || client.messageCount() != 1 {
		t.Fatalf("conflicting fallback MQTT messages = %#v, want one retained offline availability", client.messages)
	}
}

func TestPersistentDiscoveryRejectsDerivedTopicCollisionBeforePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	publisher, client := newPersistentPublisher(t, path)
	colliding := discoverySnapshot("modbus",
		model.Metric{Key: "source", Name: "Metric Colliding With Diagnostic", Value: 1},
	)
	if err := publisher.OnPollSuccess(colliding, time.Second); err == nil {
		t.Fatal("OnPollSuccess() error = nil, want derived topic collision rejection")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("colliding schema was persisted: %v", err)
	}
	if client.messageCount() != 0 || len(publisher.cachedDiscovery) != 0 {
		t.Fatalf("colliding schema mutated/published discovery: messages=%#v cache=%#v", client.messages, publisher.cachedDiscovery)
	}
}

func newPersistentPublisher(t *testing.T, path string) (*Publisher, *recordingMQTTClient) {
	t.Helper()
	publisher, err := NewPublisher(persistentMQTTConfig(path))
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	client := &recordingMQTTClient{open: true}
	publisher.client = client
	return publisher, client
}

func persistentMQTTConfig(path string) config.MQTTConfig {
	return config.MQTTConfig{
		Broker:             "tcp://localhost:1883",
		ClientID:           "persistent-discovery-test",
		TopicPrefix:        "jinko-exporter",
		DiscoveryPrefix:    "homeassistant",
		DeviceName:         "Stable Inverter",
		DeviceID:           "stable_inverter",
		Retain:             true,
		Timeout:            time.Second,
		DiscoveryStateFile: path,
		PrimarySource:      "modbus",
	}
}

func discoverySnapshot(source string, metrics ...model.Metric) *model.Snapshot {
	return &model.Snapshot{
		Source:      source,
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		Metrics:     metrics,
	}
}

func publishedState(t *testing.T, client *recordingMQTTClient, topic string) statePayload {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, message := range slices.Backward(client.messages) {

		if message.topic != topic {
			continue
		}
		var state statePayload
		if err := json.Unmarshal([]byte(message.body), &state); err != nil {
			t.Fatalf("decode state topic %s: %v", topic, err)
		}
		return state
	}
	t.Fatalf("state topic %s was not published", topic)
	return statePayload{}
}

func readDiscoveryStateForTest(t *testing.T, path string, binding discoveryStateBinding) discoveryState {
	t.Helper()
	state, exists, err := loadDiscoveryState(path, binding)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("discovery state file does not exist")
	}
	return state
}
