package source

import (
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityUPSPowerKeepsCanonicalIdentityIndependentValuesAndSourceTime(t *testing.T) {
	metrics := []model.Metric{
		{Group: "consumption", Key: "C_P_L1", Name: "Load  Power L1", Unit: "W", Value: 0},
		{Group: "consumption", Key: "C_P_L2", Name: "Load  Power L2", Unit: "W", Value: 32768},
		{Group: "consumption", Key: "C_P_L3", Name: "Load  Power L3", Unit: "W", Value: 65535},
		{Group: "ups", Key: "UPS_P", Name: "UPS Load Power", Unit: "W", Value: 321},
		{Group: "consumption", Key: "LPP_A", Name: "Load phase power A", Unit: "W", Value: -5},
		{Group: "consumption", Key: "LPP_B", Name: "Load phase power B", Unit: "W", Value: 10},
		{Group: "consumption", Key: "LPP_C", Name: "Load phase power C", Unit: "W", Value: 20},
		{Group: "consumption", Key: "E_Puse_t1", Name: "Total Consumption Power", Unit: "W", Value: 26},
	}
	for _, fallbackName := range []string{"jinko", "solarman"} {
		t.Run(fallbackName, func(t *testing.T) {
			primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
				Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Unix(10, 0).UTC(),
			}}
			fallback := &stubSource{name: fallbackName, snapshot: &model.Snapshot{
				Source: fallbackName, DeviceSN: primary.snapshot.DeviceSN, CollectedAt: time.Unix(20, 0).UTC(),
			}}
			for _, metric := range metrics {
				native := metric
				native.Value = 1 // Different prior values expose accidental stale-data reuse.
				primary.snapshot.Metrics = append(primary.snapshot.Metrics, native)
				metric.Group, metric.Name, metric.Unit = "other", "cloud label", "watts"
				fallback.snapshot.Metrics = append(fallback.snapshot.Metrics, metric)
			}
			priority := NewPriority([]Source{primary, fallback}, true)
			if _, err := priority.Fetch(t.Context()); err != nil || fallback.calls != 0 {
				t.Fatalf("healthy native fetch = %v; cloud calls = %d", err, fallback.calls)
			}
			primary.err = errors.New("native read unavailable")
			for _, missing := range []int{0, 3, 4} {
				fallback.snapshot.Metrics = nil
				fallback.snapshot.CollectedAt = time.Unix(int64(20+missing), 0).UTC()
				for _, metric := range metrics[missing:] {
					metric.Group, metric.Name, metric.Unit = "other", "cloud label", "watts"
					fallback.snapshot.Metrics = append(fallback.snapshot.Metrics, metric)
				}
				got, err := priority.Fetch(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if got.Source != fallbackName || !got.CollectedAt.Equal(fallback.snapshot.CollectedAt) || len(got.Metrics) != len(metrics)-missing {
					t.Fatalf("fallback identity/time/count changed: %#v", got)
				}
				for i, want := range metrics[missing:] {
					if got.Metrics[i] != want {
						t.Fatalf("fallback metric[%d] = %#v, want independent canonical %#v", i, got.Metrics[i], want)
					}
				}
			}
		})
	}
}
