package source

import (
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityExternalCTFallbackPreservesPowerButDoesNotInventCurrents(t *testing.T) {
	canonicalPower := []model.Metric{
		{Group: "grid", Key: "CT1_P_E", Name: "External\u00a0CT1\u00a0Power", Unit: "W", Value: 101},
		{Group: "grid", Key: "CT2_P_E", Name: "External\u00a0CT2\u00a0Power", Unit: "W", Value: 98},
		{Group: "grid", Key: "CT3_P_E", Name: "External\u00a0CT3\u00a0Power", Unit: "W", Value: 172},
		{Group: "grid", Key: "CT_T_E", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W", Value: 371},
	}
	primaryMetrics := append([]model.Metric{}, canonicalPower...)
	primaryMetrics = append(primaryMetrics,
		model.Metric{Group: "grid", Key: "JINKO_MODBUS_R613_EXTERNAL_CT_L1_CURRENT", Name: "External CT Current L1", Unit: "A", Value: 0.57},
		model.Metric{Group: "grid", Key: "JINKO_MODBUS_R614_EXTERNAL_CT_L2_CURRENT", Name: "External CT Current L2", Unit: "A", Value: 0.62},
		model.Metric{Group: "grid", Key: "JINKO_MODBUS_R615_EXTERNAL_CT_L3_CURRENT", Name: "External CT Current L3", Unit: "A", Value: 1},
	)
	primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
		Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Unix(10, 0).UTC(), Metrics: primaryMetrics,
	}}
	for _, fallbackName := range []string{"jinko", "solarman"} {
		t.Run(fallbackName, func(t *testing.T) {
			primary.err = nil
			fallback := &stubSource{name: fallbackName, snapshot: &model.Snapshot{
				Source: fallbackName, DeviceSN: primary.snapshot.DeviceSN, CollectedAt: time.Unix(20, 0).UTC(),
			}}
			for _, metric := range canonicalPower {
				fallback.snapshot.Metrics = append(fallback.snapshot.Metrics, model.Metric{
					Group: "other", Key: metric.Key, Name: "Cloud " + metric.Key, Unit: "watts", Value: -metric.Value,
				})
			}
			priority := NewPriority([]Source{primary, fallback}, true)
			if _, err := priority.Fetch(t.Context()); err != nil {
				t.Fatal(err)
			}
			if fallback.calls != 0 {
				t.Fatal("healthy native CT telemetry fetched cloud data")
			}
			primary.err = errors.New("native CT read failed")
			snapshot, err := priority.Fetch(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Source != fallbackName || !snapshot.CollectedAt.Equal(fallback.snapshot.CollectedAt) {
				t.Fatalf("fallback source/time = %q/%v, want %q/%v", snapshot.Source, snapshot.CollectedAt, fallbackName, fallback.snapshot.CollectedAt)
			}
			if len(snapshot.Metrics) != len(canonicalPower) {
				t.Fatalf("fallback metrics = %#v, want only four available CT powers, no synthesized currents", snapshot.Metrics)
			}
			for i, metric := range snapshot.Metrics {
				want := canonicalPower[i]
				want.Value = -want.Value
				if metric != want {
					t.Fatalf("fallback metric[%d] = %#v, want %#v", i, metric, want)
				}
			}
		})
	}
}
