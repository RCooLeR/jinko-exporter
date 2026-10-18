package prom

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	dto "github.com/prometheus/client_model/go"
)

func TestCollectorMetadataInfoPreservesRawValuesAndLabelModes(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "source_label", true: "source_label_dropped"}[dropSource], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				want := metadataValues()
				meta := maps.Clone(want)
				for key, value := range map[string]string{
					"base_url": "https://example.invalid/private", "password": "private-secret",
					"access_token": "private-token", "profile": "readonly-v1", "arbitrary": "00AF",
					"shelly_grid_load_url": "http://shelly.invalid", "Protocol_Version_Hex": "ABCD",
				} {
					meta[key] = value
				}
				state := poller.NewState("modbus")
				snapshot := &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now(), Meta: meta}
				go poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{}).Run(ctx)
				synctest.Wait()
				families := gatherCollector(t, NewCollector("_custom_", state, dropSource))
				family := metricFamily(t, families, "custom_device_metadata_info")
				if family.GetType() != dto.MetricType_GAUGE {
					t.Fatalf("metadata family type = %v, want gauge", family.GetType())
				}
				assertMetadataInfo(t, family, want, "modbus", "SYNTHETIC_INV_001", dropSource)
				assertGridFamilyAbsent(t, families, "custom_metric")
			})
		})
	}
}

func TestCollectorMetadataInfoOmitsMissingAndInvalidValues(t *testing.T) {
	for _, meta := range []map[string]string{
		nil,
		{},
		{"protocol_version_hex": "00af", "control_firmware_words": "000A", "communication_firmware_words": "000A-00BC-0DEG", "bootloader_version_hex": "000A", "assistant_version_hex": "FF ", "battery_input_code": "5", "base_url": "https://example.invalid"},
		{"protocol_version_hex": "", "control_firmware_words": "000A/00BC", "communication_firmware_words": "000A-00BC", "bootloader_version_hex": "F", "assistant_version_hex": "ＦＦ", "battery_input_code": "00"},
	} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			state := poller.NewState("modbus")
			snapshot := &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now(), Meta: meta}
			go poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{}).Run(ctx)
			synctest.Wait()
			assertGridFamilyAbsent(t, gatherCollector(t, NewCollector("solar", state, false)), "solar_device_metadata_info")
		})
	}
}

func TestMetadataInfoValueValidation(t *testing.T) {
	tests := []struct {
		key     string
		valid   []string
		invalid []string
	}{
		{"protocol_version_hex", []string{"0000", "00AF", "FFFF"}, []string{"", "000", "00000", "00af", "00AG", " 00A", "0xAF", "00AF\n"}},
		{"control_firmware_words", []string{"0000-0000", "000A-BCDE"}, []string{"000A", "000A-BCDE-0000", "000A:BCDE", "000A-bcde", "000-ABCDE"}},
		{"communication_firmware_words", []string{"0000-0000-0000", "000A-BCDE-00FF"}, []string{"000A-BCDE", "000A-BCDE-00FF-0000", "000A-BCDE-00ff", "000A--BCD-00FF"}},
		{"bootloader_version_hex", []string{"00", "0A", "FF"}, []string{"", "0", "000", "0a", "0G", " FF"}},
		{"assistant_version_hex", []string{"00", "0B", "EF"}, []string{"", "F", "00F", "ef", "EF ", "ＦＦ"}},
		{"battery_input_code", []string{"0", "1", "2", "3", "4"}, []string{"", "5", "-1", "00", "+1", "1.0", " 1", "1\n", "１"}},
		{"private", nil, []string{"", "00AF", "https://example.invalid"}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			for _, value := range tt.valid {
				if !validMetadataInfoValue(tt.key, value) {
					t.Errorf("validMetadataInfoValue(%q, %q) rejected valid raw value", tt.key, value)
				}
			}
			for _, value := range tt.invalid {
				if validMetadataInfoValue(tt.key, value) {
					t.Errorf("validMetadataInfoValue(%q, %q) accepted malformed value", tt.key, value)
				}
			}
		})
	}
}

func TestCollectorMetadataInfoLifecycleWithHealthyGridLoad(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "source_label", true: "source_label_dropped"}[dropSource], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				inverter := poller.NewState("modbus")
				gridLoad := poller.NewState("shelly_grid_load")
				gridSnapshot := gridLoadSnapshot(time.Now(), 300)
				// Even allowlisted-looking metadata from Shelly cannot establish inverter metadata.
				gridSnapshot.Meta = metadataValues()
				go poller.NewRunner(staticSource{snapshot: gridSnapshot}, time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
				synctest.Wait()
				collector := NewCollector("solar", inverter, dropSource, WithGridLoad(gridLoad, "SYNTHETIC_INV_001"))
				families := gatherCollector(t, collector)
				assertGridGauge(t, families, "solar_up", 0)
				assertGridGauge(t, families, "solar_grid_load_up", 1)
				assertGridFamilyAbsent(t, families, "solar_device_metadata_info")

				next := &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now(), Meta: metadataValues(), Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 2230}}}
				var nextErr error
				// Scrapes can let the fake clock advance and trigger another poll.
				// Guard the fixture response pair independently of synctest.Wait.
				var nextMu sync.Mutex
				go poller.NewRunner(gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) {
					nextMu.Lock()
					defer nextMu.Unlock()
					return next, nextErr
				}}, time.Minute, inverter, nil, config.AlertConfig{}).Run(ctx)
				synctest.Wait()
				families = gatherCollector(t, collector)
				assertMetadataInfo(t, metricFamily(t, families, "solar_device_metadata_info"), metadataValues(), "modbus", "SYNTHETIC_INV_001", dropSource)

				nextMu.Lock()
				nextErr = errors.New("inverter unavailable")
				nextMu.Unlock()
				time.Sleep(time.Minute)
				synctest.Wait()
				families = gatherCollector(t, collector)
				assertGridGauge(t, families, "solar_up", 0)
				assertGridGauge(t, families, "solar_poll_success", 0)
				assertGridGauge(t, families, "solar_grid_load_up", 1)
				assertGridMetric(t, families, "electric", "DP1", 2230)
				assertGridFamilyAbsent(t, families, "solar_device_metadata_info")

				nextMu.Lock()
				nextErr = nil
				next = &model.Snapshot{Source: "jinko", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now(), Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 2240}}}
				nextMu.Unlock()
				time.Sleep(time.Minute)
				synctest.Wait()
				families = gatherCollector(t, collector)
				assertGridGauge(t, families, "solar_up", 1)
				assertGridMetric(t, families, "electric", "DP1", 2240)
				assertGridFamilyAbsent(t, families, "solar_device_metadata_info")

				nextMu.Lock()
				next = &model.Snapshot{Source: "modbus", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now(), Meta: map[string]string{"protocol_version_hex": "BEEF", "battery_input_code": "4"}}
				nextMu.Unlock()
				time.Sleep(time.Minute)
				synctest.Wait()
				families = gatherCollector(t, collector)
				assertMetadataInfo(t, metricFamily(t, families, "solar_device_metadata_info"), next.Meta, "modbus", "SYNTHETIC_INV_001", dropSource)
			})
		})
	}
}

func metadataValues() map[string]string {
	return map[string]string{
		"protocol_version_hex": "00AF", "control_firmware_words": "000A-BCDE",
		"communication_firmware_words": "000A-BCDE-00FF", "bootloader_version_hex": "0B",
		"assistant_version_hex": "EF", "battery_input_code": "0",
	}
}

func assertMetadataInfo(t *testing.T, family *dto.MetricFamily, want map[string]string, source, serial string, dropSource bool) {
	t.Helper()
	if got := len(family.GetMetric()); got != len(want) || got > 6 {
		t.Fatalf("metadata sample count = %d, want %d (at most six)", got, len(want))
	}
	seen := make(map[string]bool)
	for _, metric := range family.GetMetric() {
		labels := metricLabels(metric)
		value, ok := want[labels["key"]]
		if !ok || seen[labels["key"]] {
			t.Fatalf("unexpected or duplicate metadata key %q", labels["key"])
		}
		seen[labels["key"]] = true
		wantLabels := map[string]string{"device_sn": serial, "key": labels["key"], "value": value}
		if !dropSource {
			wantLabels["source"] = source
		}
		if !reflect.DeepEqual(labels, wantLabels) || metric.GetGauge().GetValue() != 1 {
			t.Errorf("metadata sample = %v / %v, want %v / 1", labels, metric.GetGauge().GetValue(), wantLabels)
		}
	}
}
