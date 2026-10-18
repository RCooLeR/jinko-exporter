package source

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityMetadataFollowsSelectedSourceAcrossFallbackAndRecovery(t *testing.T) {
	for _, project := range []bool{false, true} {
		t.Run(map[bool]string{false: "unprojected", true: "projected"}[project], func(t *testing.T) {
			initialMeta := map[string]string{
				"protocol_version_hex": "00AF", "control_firmware_words": "000A-BCDE",
				"communication_firmware_words": "000A-BCDE-00FF", "bootloader_version_hex": "0B",
				"assistant_version_hex": "EF", "battery_input_code": "0",
			}
			newSnapshot := func(name string, timestamp int64, meta map[string]string) *model.Snapshot {
				return &model.Snapshot{
					Source: name, DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Unix(timestamp, 0).UTC(), Meta: meta,
					Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: float64(timestamp)}},
				}
			}
			modbus := &stubSource{name: "modbus", snapshot: newSnapshot("modbus", 10, initialMeta)}
			jinko := &stubSource{name: "jinko", snapshot: newSnapshot("jinko", 20, map[string]string{"cloud_firmware": "0104", "cloud_source": "jinko"})}
			solarman := &stubSource{name: "solarman", snapshot: newSnapshot("solarman", 30, map[string]string{"cloud_firmware": "00AF", "cloud_source": "solarman"})}
			priority := NewPriority([]Source{modbus, jinko, solarman}, project)
			assertSelected := func(want *model.Snapshot) {
				t.Helper()
				got, err := priority.Fetch(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if got.Source != want.Source || !got.CollectedAt.Equal(want.CollectedAt) || got.DeviceSN != want.DeviceSN {
					t.Fatalf("selected source/time/identity = %q/%v/%q, want %q/%v/%q", got.Source, got.CollectedAt, got.DeviceSN, want.Source, want.CollectedAt, want.DeviceSN)
				}
				if !reflect.DeepEqual(got.Meta, want.Meta) {
					t.Fatalf("%s metadata = %#v, want exactly selected source metadata %#v", want.Source, got.Meta, want.Meta)
				}
				if len(got.Metrics) != 1 || got.Metrics[0].Value != want.Metrics[0].Value {
					t.Fatalf("%s numeric telemetry = %#v, want current selected-source value", want.Source, got.Metrics)
				}
				if want.Source != "modbus" {
					for key := range initialMeta {
						if value, exists := got.Meta[key]; exists {
							t.Fatalf("%s fallback inherited stale Modbus %s = %q", want.Source, key, value)
						}
					}
				}
			}

			assertSelected(modbus.snapshot)
			if jinko.calls != 0 || solarman.calls != 0 {
				t.Fatal("healthy Modbus fetched a cloud source")
			}
			modbus.err = errors.New("Modbus unavailable")
			assertSelected(jinko.snapshot)
			if jinko.calls != 1 || solarman.calls != 0 {
				t.Fatalf("Jinko fallback calls = %d/%d, want 1/0", jinko.calls, solarman.calls)
			}
			jinko.err = errors.New("Jinko unavailable")
			assertSelected(solarman.snapshot)
			if jinko.calls != 2 || solarman.calls != 1 {
				t.Fatalf("Solarman fallback calls = %d/%d, want 2/1", jinko.calls, solarman.calls)
			}

			modbus.err = nil
			modbus.snapshot = newSnapshot("modbus", 40, map[string]string{
				"protocol_version_hex": "BEEF", "control_firmware_words": "000B-CDEF",
				"communication_firmware_words": "000C-DEFA-00AB", "bootloader_version_hex": "0C",
				"assistant_version_hex": "AB", "battery_input_code": "4",
			})
			assertSelected(modbus.snapshot)
			if jinko.calls != 2 || solarman.calls != 1 {
				t.Fatal("recovered Modbus fetched a cloud source")
			}
		})
	}
}
