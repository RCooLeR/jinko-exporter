package source

import (
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityBMSMirrorsDoNotInheritInverterValuesOnFallback(t *testing.T) {
	// The primary names/units are deliberately distinct from cloud display
	// labels: projection must preserve identity but use current cloud values.
	keys := []string{"BMS_C_V", "BMS_D_C_L", "BMS_B_V1", "BMS_SOC", "BMST"}
	primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Unix(10, 0)}}
	for _, key := range keys {
		primary.snapshot.Metrics = append(primary.snapshot.Metrics, model.Metric{Group: "bms", Key: key, Name: key, Value: 1})
	}
	primary.snapshot.Metrics = append(primary.snapshot.Metrics, model.Metric{Group: "battery", Key: "B_V1", Name: "Battery Voltage", Unit: "V", Value: 615.6})
	cloud := &stubSource{name: "jinko", snapshot: &model.Snapshot{Source: "jinko", DeviceSN: primary.snapshot.DeviceSN, CollectedAt: time.Unix(20, 0), Metrics: []model.Metric{
		{Group: "other", Key: "BMS_B_V1", Name: "cloud", Value: 616.2},
		{Group: "other", Key: "B_V1", Name: "cloud inverter", Value: 616.6},
	}}}
	priority := NewPriority([]Source{primary, cloud}, true)
	if _, err := priority.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	primary.err = errors.New("Modbus unavailable")
	got, err := priority.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "jinko" || !got.CollectedAt.Equal(cloud.snapshot.CollectedAt) || len(got.Metrics) != 2 {
		t.Fatalf("changed current fallback: %+v", got)
	}
	if got.Metrics[0].Key != "BMS_B_V1" || got.Metrics[0].Group != "bms" || got.Metrics[0].Value != 616.2 || got.Metrics[1].Value != 616.6 {
		t.Fatalf("BMS/inverter values merged: %v", got.Metrics)
	}
}
