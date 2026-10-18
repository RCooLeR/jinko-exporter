package prom

import (
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestCollectorInternalGridPowerPreservesDistinctSignedSeries(t *testing.T) {
	values := map[string]float64{"GS_A": -98, "GS_B": 0, "GS_C": 148, "GS_T": 49, "CT_T_E": 371, "PG_Pt1": 316}
	want := make(map[string]model.Metric, len(values))
	snapshot := &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now().UTC()}
	for key, value := range values {
		metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: key, Value: value})
		if !ok {
			t.Fatalf("missing canonical definition for %s", key)
		}
		want[key] = metric
		snapshot.Metrics = append(snapshot.Metrics, metric)
	}
	state := poller.NewState("modbus")
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(t.Context())
	waitForSnapshot(t, state)
	for _, dropSource := range []bool{false, true} {
		family := metricFamily(t, gatherCollector(t, NewCollector("solar", state, dropSource)), "solar_metric")
		seen := make(map[string]bool, len(want))
		for _, sample := range family.GetMetric() {
			labels := metricLabels(sample)
			metric, ok := want[labels["key"]]
			if !ok || seen[metric.Key] {
				t.Fatalf("unknown or duplicate key: %#v", labels)
			}
			seen[metric.Key] = true
			if labels["group"] != "grid" || labels["unit"] != "W" || labels["name"] != metric.Name || labels["device_sn"] != snapshot.DeviceSN || sample.GetGauge().GetValue() != metric.Value {
				t.Fatalf("changed independent series %s: %v / %#v", metric.Key, sample.GetGauge().GetValue(), labels)
			}
			if source, present := labels["source"]; present == dropSource || (!dropSource && source != "modbus") {
				t.Fatalf("dropSource=%v: source=%q present=%v", dropSource, source, present)
			}
		}
		if len(seen) != len(want) {
			t.Fatalf("published %d distinct series, want %d", len(seen), len(want))
		}
	}
}
