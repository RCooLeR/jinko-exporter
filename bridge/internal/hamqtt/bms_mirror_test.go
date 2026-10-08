package hamqtt

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestBMSMirrorsKeepDiscoveryIdentityAndIndependentValues(t *testing.T) {
	publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
	defer publisher.Close()
	canonical := func(key string, value float64) model.Metric {
		t.Helper()
		metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: key, Value: value})
		if !ok {
			t.Fatalf("missing canonical %s", key)
		}
		return metric
	}
	// Simulate the old alias-backed entities before the BMS mirror upgrade.
	old := discoverySnapshot("modbus", canonical("BMST", 23), canonical("BMS_SOC", 99))
	if err := publisher.OnPollSuccess(old, time.Second); err != nil {
		t.Fatal(err)
	}
	oldDiscovery := map[string]map[string]any{}
	for _, key := range []string{"bmst", "bms_soc"} {
		oldDiscovery[key] = decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_bms_"+key+"/config")
	}
	values := map[string]float64{"BMS_C_V": 648, "BMS_D_C_L": 22, "BMS_B_V1": 615.2, "BMS_SOC": 100, "BMST": 22,
		"B_V1": 615.6, "B_left_cap1": 99, "B_T1": 23}
	var metrics []model.Metric
	for key, value := range values {
		metrics = append(metrics, canonical(key, value))
	}
	for _, sourceName := range []string{"modbus", "jinko", "solarman"} {
		if err := publisher.OnPollSuccess(discoverySnapshot(sourceName, metrics...), time.Second); err != nil {
			t.Fatal(err)
		}
		state := publishedState(t, client, independentStateTopic)
		if state.MetricCount != len(metrics) {
			t.Fatalf("count = %d", state.MetricCount)
		}
		for _, metric := range metrics {
			key := metric.Group + "_" + strings.ToLower(metric.Key)
			if value := state.Metrics[key]; value == nil || *value != metric.Value {
				t.Fatalf("%s changed: %v", key, value)
			}
			discovery := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_"+key+"/config")
			wantUnit := metric.Unit
			if wantUnit == "C" {
				wantUnit = "°C"
			}
			if discovery["unit_of_measurement"] != wantUnit || discovery["name"] != metric.Name {
				t.Fatalf("changed canonical schema %s: %v", key, discovery)
			}
			if original := oldDiscovery[strings.ToLower(metric.Key)]; original != nil && !reflect.DeepEqual(original, discovery) {
				t.Fatalf("mirror upgrade changed existing entity identity/schema %s", key)
			}
		}
		for _, key := range []string{"bms_bms_b_c1", "bms_bms_d_v", "bms_bms_c_c_l", "bms_bms_cc1"} {
			if _, found := state.Metrics[key]; found {
				t.Fatalf("invented unverified field %s", key)
			}
		}
	}
	// Missing BMS values on fallback must not be filled from inverter readings.
	if err := publisher.OnPollSuccess(discoverySnapshot("jinko", canonical("B_V1", 620), canonical("B_T1", 25), canonical("B_left_cap1", 98)), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	for _, key := range []string{"bms_bms_c_v", "bms_bms_d_c_l", "bms_bms_b_v1", "bms_bms_soc", "bms_bmst"} {
		if value, exists := state.Metrics[key]; !exists || value != nil {
			t.Fatalf("missing %s = %v, exists=%v", key, value, exists)
		}
	}
}

func TestParsedCloudMetadataRespectsPersistentPrimaryOwnership(t *testing.T) {
	raw := []byte(`{"deviceSn":"SYNTHETIC_INV_001","paramCategoryList":[{"fieldList":[{"storageName":"PTCv1","orgValue":"0104"},{"storageName":"MAIN","orgValue":"3001-1087-1C08"},{"storageName":"CBAVM","orgValue":"08"}]}]}`)
	for _, primary := range []string{"jinko", "modbus"} {
		t.Run(primary, func(t *testing.T) {
			cfg := persistentMQTTConfig(filepath.Join(t.TempDir(), "discovery.json"))
			cfg.PrimarySource = primary
			publisher, err := NewPublisher(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer publisher.Close()
			client := &recordingMQTTClient{open: true}
			publisher.client = client
			snapshot, err := jinko.ParseDetailResponse(raw)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Source = "jinko"
			snapshot.CollectedAt = time.Now()
			if err := publisher.OnPollSuccess(snapshot, time.Second); err != nil {
				t.Fatal(err)
			}
			state := publishedState(t, client, independentStateTopic)
			if primary == "jinko" {
				if !reflect.DeepEqual(state.Meta, snapshot.Meta) || state.Meta["cloud_protocol_version"] != "0104" || state.Meta["cloud_bootloader_version"] != "08" {
					t.Fatalf("cloud metadata not lossless: %v", state.Meta)
				}
				for key := range snapshot.Meta {
					assertDeviceMetadataDiscovery(t, publisher.cachedDiscovery, key)
				}
			} else if len(state.Meta) != 0 {
				t.Fatalf("fallback established cloud-only metadata: %v", state.Meta)
			}
		})
	}
}
