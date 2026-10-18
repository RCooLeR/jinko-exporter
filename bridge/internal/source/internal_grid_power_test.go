package source

import (
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityInternalGridPowerKeepsCanonicalIdentityAndIndependentValues(t *testing.T) {
	metrics := []model.Metric{
		{Group: "grid", Key: "GS_A", Name: "Internal L1 Power", Unit: "W", Value: -98},
		{Group: "grid", Key: "GS_B", Name: "Internal L2 Power", Unit: "W", Value: 0},
		{Group: "grid", Key: "GS_C", Name: "Internal L3 Power", Unit: "W", Value: 148},
		{Group: "grid", Key: "GS_T", Name: "Internal Power", Unit: "W", Value: 49},
		{Group: "grid", Key: "CT_T_E", Name: "Total\u00a0External\u00a0CT\u00a0Power", Unit: "W", Value: 371},
		{Group: "grid", Key: "PG_Pt1", Name: "Total Grid Power", Unit: "W", Value: 316},
	}
	for _, fallbackName := range []string{"jinko", "solarman"} {
		t.Run(fallbackName, func(t *testing.T) {
			primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
				Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", Metrics: metrics,
			}}
			fallback := &stubSource{name: fallbackName, snapshot: &model.Snapshot{
				Source: fallbackName, DeviceSN: primary.snapshot.DeviceSN, CollectedAt: time.Unix(20, 0).UTC(),
			}}
			for _, metric := range metrics {
				metric.Group, metric.Name, metric.Unit = "other", "cloud label", "watts"
				fallback.snapshot.Metrics = append(fallback.snapshot.Metrics, metric)
			}
			priority := NewPriority([]Source{primary, fallback}, true)
			if _, err := priority.Fetch(t.Context()); err != nil || fallback.calls != 0 {
				t.Fatalf("healthy native fetch = %v; cloud calls = %d", err, fallback.calls)
			}
			primary.err = errors.New("native read unavailable")
			got, err := priority.Fetch(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if got.Source != fallbackName || !got.CollectedAt.Equal(fallback.snapshot.CollectedAt) || len(got.Metrics) != len(metrics) {
				t.Fatalf("fallback identity/time/metric count changed: %#v", got)
			}
			for i, want := range metrics {
				if got.Metrics[i] != want {
					t.Fatalf("fallback metric[%d] = %#v, want independent canonical %#v", i, got.Metrics[i], want)
				}
			}
		})
	}
}
