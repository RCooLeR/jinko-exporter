package prom

import (
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestCollectorBMSMirrorsRemainDistinctFromInverterBattery(t *testing.T) {
	snapshot := &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now()}
	want := map[string]model.Metric{}
	for key, value := range map[string]float64{"BMS_C_V": 648, "BMS_D_C_L": 22, "BMS_B_V1": 615.2, "BMS_SOC": 100, "BMST": 22,
		"B_V1": 615.6, "B_left_cap1": 99, "B_T1": 23} {
		metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: key, Value: value})
		if !ok {
			t.Fatalf("missing canonical %s", key)
		}
		want[key] = metric
		snapshot.Metrics = append(snapshot.Metrics, metric)
	}
	state := poller.NewState("modbus")
	go poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{}).Run(t.Context())
	waitForSnapshot(t, state)
	for _, dropSource := range []bool{false, true} {
		family := metricFamily(t, gatherCollector(t, NewCollector("solar", state, dropSource)), "solar_metric")
		seen := map[string]bool{}
		for _, sample := range family.GetMetric() {
			labels := metricLabels(sample)
			metric, ok := want[labels["key"]]
			if !ok || seen[metric.Key] {
				t.Fatalf("unknown/duplicate metric %v", labels)
			}
			seen[metric.Key] = true
			if labels["group"] != metric.Group || labels["unit"] != metric.Unit || labels["name"] != metric.Name || sample.GetGauge().GetValue() != metric.Value {
				t.Fatalf("changed BMS/inverter data: %v", labels)
			}
			if source, found := labels["source"]; found == dropSource || (!dropSource && source != "modbus") {
				t.Fatalf("source labels = %v", labels)
			}
		}
		if len(seen) != len(want) {
			t.Fatalf("published %d of %d", len(seen), len(want))
		}
	}
}
