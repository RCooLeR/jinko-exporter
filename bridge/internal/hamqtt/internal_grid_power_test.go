package hamqtt

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestPersistentInternalGridPowerKeepsCanonicalDiscoveryAndIndependentState(t *testing.T) {
	publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
	defer publisher.Close()
	definitions := []struct {
		key, name string
		value     float64
	}{
		{"GS_A", "Internal L1 Power", -98}, {"GS_B", "Internal L2 Power", 0},
		{"GS_C", "Internal L3 Power", 148}, {"GS_T", "Internal Power", 49},
		{"CT_T_E", "Total\u00a0External\u00a0CT\u00a0Power", 371}, {"PG_Pt1", "Total Grid Power", 316},
	}
	var metrics []model.Metric
	for _, definition := range definitions {
		metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: definition.key, Value: definition.value})
		if !ok || metric.Group != "grid" || metric.Name != definition.name || metric.Unit != "W" {
			t.Fatalf("canonical metric %s = %#v", definition.key, metric)
		}
		metrics = append(metrics, metric)
	}
	for _, source := range []string{"modbus", "jinko", "solarman"} {
		if err := publisher.OnPollSuccess(discoverySnapshot(source, metrics...), time.Second); err != nil {
			t.Fatal(err)
		}
		state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
		if state.Source != source || state.MetricCount != len(metrics) {
			t.Fatalf("source/count = %s/%d, want %s/%d", state.Source, state.MetricCount, source, len(metrics))
		}
		for _, definition := range definitions {
			key := "grid_" + strings.ToLower(definition.key)
			id := "stable_inverter_" + key
			discovery := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/"+id+"/config")
			if discovery["unique_id"] != id || discovery["name"] != definition.name || discovery["device_class"] != "power" || discovery["state_class"] != "measurement" || discovery["unit_of_measurement"] != "W" {
				t.Fatalf("changed discovery for %s: %#v", key, discovery)
			}
			if value := state.Metrics[key]; value == nil || *value != definition.value {
				t.Fatalf("%s state = %v, want independent value %v", key, value, definition.value)
			}
		}
	}
	// Selected grid and CT totals must not fill missing internal grid readings.
	if err := publisher.OnPollSuccess(discoverySnapshot("solarman", metrics[4:]...), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, "jinko-exporter/stable_inverter/state")
	for _, definition := range definitions[:4] {
		key := "grid_" + strings.ToLower(definition.key)
		if value, present := state.Metrics[key]; !present || value != nil {
			t.Fatalf("missing %s = %v (present=%v), want explicit null", key, value, present)
		}
	}
}
