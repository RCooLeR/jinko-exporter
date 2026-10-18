package prom

import (
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
)

func TestCollectorPreservesExternalCTLabelsUnitsAndSignedValues(t *testing.T) {
	metrics := []model.Metric{
		{Group: "grid", Key: "CT1_P_E", Name: "External\u00a0CT1\u00a0Power", Unit: "W", Value: -101},
		{Group: "grid", Key: "CT2_P_E", Name: "External\u00a0CT2\u00a0Power", Unit: "W", Value: 98},
		{Group: "grid", Key: "CT3_P_E", Name: "External\u00a0CT3\u00a0Power", Unit: "W", Value: 0},
		{Group: "grid", Key: "CT_T_E", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W", Value: -3},
		{Group: "grid", Key: "JINKO_MODBUS_R613_EXTERNAL_CT_L1_CURRENT", Name: "External CT Current L1", Unit: "A", Value: -0.57},
		{Group: "grid", Key: "JINKO_MODBUS_R614_EXTERNAL_CT_L2_CURRENT", Name: "External CT Current L2", Unit: "A", Value: 0.62},
		{Group: "grid", Key: "JINKO_MODBUS_R615_EXTERNAL_CT_L3_CURRENT", Name: "External CT Current L3", Unit: "A", Value: 0},
	}
	wantByKey := make(map[string]model.Metric, len(metrics))
	for _, metric := range metrics {
		wantByKey[metric.Key] = metric
	}
	state := poller.NewState("modbus")
	snapshot := &model.Snapshot{
		Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now().UTC(), Metrics: metrics,
	}
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(t.Context())
	waitForSnapshot(t, state)

	for _, dropSource := range []bool{false, true} {
		family := metricFamily(t, gatherCollector(t, NewCollector("solar", state, dropSource)), "solar_metric")
		if len(family.GetMetric()) != len(metrics) {
			t.Fatalf("dropSource=%v: metric count = %d, want %d", dropSource, len(family.GetMetric()), len(metrics))
		}
		for _, got := range family.GetMetric() {
			labels := metricLabels(got)
			want, ok := wantByKey[labels["key"]]
			if !ok {
				t.Fatalf("unexpected metric key %q", labels["key"])
			}
			if labels["device_sn"] != snapshot.DeviceSN || labels["group"] != want.Group ||
				labels["name"] != want.Name || labels["unit"] != want.Unit {
				t.Fatalf("metric %s labels = %#v, want %#v", want.Key, labels, want)
			}
			if source, present := labels["source"]; present == dropSource || (!dropSource && source != "modbus") {
				t.Fatalf("dropSource=%v: source label = %q (present=%v)", dropSource, source, present)
			}
			if value := got.GetGauge().GetValue(); value != want.Value {
				t.Fatalf("metric %s value = %v, want signed value %v", want.Key, value, want.Value)
			}
		}
	}
}
