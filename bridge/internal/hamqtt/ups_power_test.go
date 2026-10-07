package hamqtt

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestPersistentUPSPowerKeepsCanonicalDiscoveryAndIndependentUnsignedState(t *testing.T) {
	publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
	defer publisher.Close()
	// Synthetic transport-contract values: the total intentionally is not the
	// phase sum, and high unsigned low words must never become negative watts.
	definitions := []struct {
		key, group, name string
		value            float64
	}{
		{"C_P_L1", "consumption", "Load  Power L1", 0},
		{"C_P_L2", "consumption", "Load  Power L2", 32768},
		{"C_P_L3", "consumption", "Load  Power L3", 65535},
		{"UPS_P", "ups", "UPS Load Power", 321},
		{"LPP_A", "consumption", "Load phase power A", -5},
		{"LPP_B", "consumption", "Load phase power B", 10},
		{"LPP_C", "consumption", "Load phase power C", 20},
		{"E_Puse_t1", "consumption", "Total Consumption Power", 26},
	}
	var metrics []model.Metric
	for _, definition := range definitions {
		metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: definition.key, Value: definition.value})
		if !ok || metric.Group != definition.group || metric.Name != definition.name || metric.Unit != "W" {
			t.Fatalf("canonical metric %s = %#v", definition.key, metric)
		}
		metrics = append(metrics, metric)
	}
	initialDiscovery := make(map[string]map[string]any)
	for _, source := range []string{"modbus", "jinko", "solarman"} {
		if err := publisher.OnPollSuccess(discoverySnapshot(source, metrics...), time.Second); err != nil {
			t.Fatal(err)
		}
		state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
		if state.Source != source || state.MetricCount != len(metrics) {
			t.Fatalf("source/count = %s/%d, want %s/%d", state.Source, state.MetricCount, source, len(metrics))
		}
		for _, definition := range definitions {
			key := definition.group + "_" + strings.ToLower(definition.key)
			id := "stable_inverter_" + key
			discovery := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/"+id+"/config")
			if discovery["unique_id"] != id || discovery["name"] != definition.name || discovery["device_class"] != "power" || discovery["state_class"] != "measurement" || discovery["unit_of_measurement"] != "W" {
				t.Fatalf("changed discovery for %s: %#v", key, discovery)
			}
			if original, found := initialDiscovery[key]; found {
				if !reflect.DeepEqual(discovery, original) {
					t.Fatalf("source %s changed discovery for %s: %#v", source, key, discovery)
				}
			} else {
				initialDiscovery[key] = discovery
			}
			if value := state.Metrics[key]; value == nil || *value != definition.value {
				t.Fatalf("%s state = %v, want independent value %v", key, value, definition.value)
			}
		}
	}
	// Presence of direct-load phases must not fill absent UPS phases. In the
	// second snapshot even the UPS total is absent, despite direct-load total.
	for _, missing := range []int{3, 4} {
		if err := publisher.OnPollSuccess(discoverySnapshot("solarman", metrics[missing:]...), time.Second); err != nil {
			t.Fatal(err)
		}
		state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
		for _, definition := range definitions[:missing] {
			key := definition.group + "_" + strings.ToLower(definition.key)
			if value, present := state.Metrics[key]; !present || value != nil {
				t.Fatalf("missing %s = %v (present=%v), want explicit null", key, value, present)
			}
		}
		for _, definition := range definitions[missing:] {
			key := definition.group + "_" + strings.ToLower(definition.key)
			if value := state.Metrics[key]; value == nil || *value != definition.value {
				t.Fatalf("remaining %s state = %v, want %v", key, value, definition.value)
			}
		}
	}
}
