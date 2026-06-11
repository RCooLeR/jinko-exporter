package jinko

import (
	"os"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestParseDetailResponseFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/jinko_detail_response.json")
	if err != nil {
		t.Fatalf("ReadFile fixture error = %v", err)
	}

	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		t.Fatalf("ParseDetailResponse() error = %v", err)
	}

	if snapshot.DeviceSN != "DEVICE_SN_EXAMPLE" {
		t.Fatalf("DeviceSN = %q, want DEVICE_SN_EXAMPLE", snapshot.DeviceSN)
	}
	if snapshot.ParentSN != "LOGGER_SN_EXAMPLE" {
		t.Fatalf("ParentSN = %q, want LOGGER_SN_EXAMPLE", snapshot.ParentSN)
	}
	if snapshot.DeviceID != "100000001" || snapshot.SiteID != "200000001" {
		t.Fatalf("ids = device %q site %q, want 100000001/200000001", snapshot.DeviceID, snapshot.SiteID)
	}
	wantCollectedAt := time.Unix(1775145150, 0).UTC()
	if !snapshot.CollectedAt.Equal(wantCollectedAt) {
		t.Fatalf("CollectedAt = %s, want %s", snapshot.CollectedAt, wantCollectedAt)
	}

	metrics := metricsByKey(snapshot.Metrics)
	assertMetric(t, metrics["DP1"], "electric", "DC Power PV1", "W", 290)
	assertMetric(t, metrics["S_P_T"], "electric", "Total Solar Power", "W", 540)
	assertMetric(t, metrics["PG_Pt1"], "grid", "Total Grid Power", "W", 13)
	assertMetric(t, metrics["B_left_cap1"], "battery", "SoC", "%", 100)
	assertMetric(t, metrics["BMST"], "bms", "BMS Temperature", "\u2103", 13)
}

func TestParseDetailResponseSkipsMalformedAndFallsBackPerField(t *testing.T) {
	raw := []byte(`{
		"deviceId": 1,
		"siteId": 2,
		"paramCategoryList": [{
			"name": "Grid",
			"tag": "grid",
			"fieldList": [
				{"key": "Total Grid Power", "storageName": "PG_Pt1", "orgValue": "not numeric", "value": "1,25", "unit": "W"},
				{"key": "Bad", "storageName": "BAD", "orgValue": "offline", "value": "still offline", "unit": "W"},
				{"key": "Thousands", "storageName": "THOUSANDS", "orgValue": "1,250", "unit": "W"}
			]
		}]
	}`)

	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		t.Fatalf("ParseDetailResponse() error = %v", err)
	}
	metrics := metricsByKey(snapshot.Metrics)

	assertMetric(t, metrics["PG_Pt1"], "grid", "Total Grid Power", "W", 1.25)
	assertMetric(t, metrics["THOUSANDS"], "grid", "Thousands", "W", 1250)
	if _, ok := metrics["BAD"]; ok {
		t.Fatalf("BAD metric should have been skipped")
	}
}

func TestParseDetailResponseMalformedJSON(t *testing.T) {
	if _, err := ParseDetailResponse([]byte(`{"paramCategoryList":`)); err == nil {
		t.Fatal("ParseDetailResponse() error = nil, want malformed JSON error")
	}
}

func TestParseNumberHandlesCommonSeparators(t *testing.T) {
	tests := map[string]float64{
		"1,25":     1.25,
		"1,250":    1250,
		"1,250.50": 1250.50,
		"1 250,50": 1250.50,
	}

	for input, want := range tests {
		got, ok := parseNumber(input)
		if !ok || got != want {
			t.Fatalf("parseNumber(%q) = %v, %v; want %v, true", input, got, ok, want)
		}
	}
}

func metricsByKey(metrics []model.Metric) map[string]model.Metric {
	index := make(map[string]model.Metric, len(metrics))
	for _, metric := range metrics {
		index[metric.Key] = metric
	}
	return index
}

func assertMetric(t *testing.T, metric model.Metric, group, name, unit string, value float64) {
	t.Helper()
	if metric.Group != group || metric.Name != name || metric.Unit != unit || metric.Value != value {
		t.Fatalf("metric %#v, want group=%q name=%q unit=%q value=%v", metric, group, name, unit, value)
	}
}
