package jinko

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestCloudMetadataPreservesEightExplicitStringsAndLegacyMetrics(t *testing.T) {
	fields := []field{
		{StorageName: "PTCv1", OrgValue: "0104", Value: "display protocol"},
		{StorageName: "MAIN", OrgValue: "3001-1087-1C08"},
		{StorageName: "HMI", OrgValue: "2002-2017"},
		{StorageName: "CBAMSV", OrgValue: "1C"},
		{StorageName: "CBAVM", OrgValue: "08"},
		{StorageName: "L_B_H_V_N", OrgValue: "FFFF"},
		{StorageName: "L_B_H_V_N2", OrgValue: "0000"},
		{StorageName: "BAT_FAC", OrgValue: "JINKOBSS_HV"},
		{StorageName: "DP1", OrgValue: "290", Unit: "W"},
		{StorageName: "UNKNOWN_NUMBER", OrgValue: "42"},
	}
	snapshot := parseMetadataFields(t, fields)
	want := map[string]string{
		"cloud_protocol_version":          "0104",
		"cloud_main_firmware":             "3001-1087-1C08",
		"cloud_hmi_firmware":              "2002-2017",
		"cloud_assistant_version":         "1C",
		"cloud_bootloader_version":        "08",
		"cloud_battery_hardware_version":  "FFFF",
		"cloud_battery2_hardware_version": "0000",
		"cloud_battery_manufacturer":      "JINKOBSS_HV",
	}
	if !reflect.DeepEqual(snapshot.Meta, want) {
		t.Fatalf("metadata = %#v, want %#v", snapshot.Meta, want)
	}
	metrics := metricsByKey(snapshot.Metrics)
	// Text metadata is additive. Even numeric-shaped version fields and
	// unknown numeric fields retain the previous metric behavior.
	if len(metrics) != 5 || len(snapshot.Metrics) != 5 {
		t.Fatalf("metrics = %#v, want five legacy numeric metrics", snapshot.Metrics)
	}
	assertMetric(t, metrics["PTCv1"], "version", "Protocol Version", "", 104)
	assertMetric(t, metrics["DP1"], "electric", "DC Power PV1", "W", 290)
	for key, wantValue := range map[string]float64{"CBAVM": 8, "L_B_H_V_N2": 0, "UNKNOWN_NUMBER": 42} {
		if got := metrics[key].Value; got != wantValue {
			t.Fatalf("legacy %s = %v, want %v", key, got, wantValue)
		}
	}
}

func TestCloudMetadataSanitizedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/jinko_detail_response.json")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"cloud_protocol_version":          "0104",
		"cloud_main_firmware":             "3001-1087-1C08",
		"cloud_hmi_firmware":              "2002-2017",
		"cloud_battery_hardware_version":  "FFFF",
		"cloud_battery2_hardware_version": "FFFF",
		"cloud_battery_manufacturer":      "JINKOBSS_HV",
	}
	if !reflect.DeepEqual(snapshot.Meta, want) {
		t.Fatalf("fixture metadata = %#v, want %#v", snapshot.Meta, want)
	}
	assertMetric(t, metricsByKey(snapshot.Metrics)["PTCv1"], "version", "Protocol Version", "", 104)
}

func TestCloudMetadataTextValidationAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, original, display, want string
	}{
		{name: "original wins", original: "0104", display: "other", want: "0104"},
		{name: "empty original falls back", display: "00AF", want: "00AF"},
		{name: "preserve spaces", original: " 0104 ", want: " 0104 "},
		{name: "empty"},
		{name: "whitespace", original: "   "},
		{name: "blank original not replaced", original: "   ", display: "valid"},
		{name: "byte limit", original: strings.Repeat("a", 128), want: strings.Repeat("a", 128)},
		{name: "too long", original: strings.Repeat("a", 129), display: "valid"},
		{name: "unicode byte limit", original: strings.Repeat("é", 64), want: strings.Repeat("é", 64)},
		{name: "unicode too long", original: strings.Repeat("é", 65)},
		{name: "NUL", original: "01\x0004", display: "valid"},
		{name: "tab", original: "\t", display: "valid"},
		{name: "newline", original: "0104\n"},
		{name: "DEL", original: "0104\x7f"},
		{name: "C1 control", original: "0104\u0085"},
		{name: "invalid display", display: "0104\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := parseMetadataFields(t, []field{{StorageName: "PTCv1", OrgValue: tc.original, Value: tc.display}})
			got, exists := snapshot.Meta["cloud_protocol_version"]
			if got != tc.want || exists != (tc.want != "") {
				t.Fatalf("metadata = %q/%v, want %q/%v", got, exists, tc.want, tc.want != "")
			}
		})
	}
}

func TestCloudMetadataUsesOnlyExactStorageNames(t *testing.T) {
	snapshot := parseMetadataFields(t, []field{
		{Key: "MAIN", OrgValue: "3001-1087-1C08"},
		{Key: "Protocol Version", StorageName: "UNKNOWN", OrgValue: "0104"},
		{StorageName: "main", OrgValue: "firmware"},
		{StorageName: " MAIN ", OrgValue: "firmware"},
		{StorageName: "SN1", OrgValue: "SYNTHETIC_INV_001"},
		{StorageName: "LBVN", OrgValue: "0000"},
	})
	if snapshot.Meta != nil {
		t.Fatalf("unexpected metadata from unallowlisted storage names: %#v", snapshot.Meta)
	}
}

func TestCloudMetadataDuplicateConflictIsOrderIndependent(t *testing.T) {
	for _, values := range [][]string{
		{"0104", "0104"},
		{"0104", "0105"},
		{"0105", "0104"},
		{"0104", "0105", "0104"},
		{"0104", "\x00", "0104"},
		{"\x00", "0104"},
	} {
		t.Run(strings.Join(values, "/"), func(t *testing.T) {
			fields := []field{{StorageName: "BAT_FAC", OrgValue: "JINKOBSS_HV"}}
			for _, value := range values {
				fields = append(fields, field{StorageName: "PTCv1", OrgValue: value})
			}
			snapshot := parseMetadataFields(t, fields)
			want := map[string]string{"cloud_battery_manufacturer": "JINKOBSS_HV"}
			if len(values) == 2 && values[0] == values[1] {
				want["cloud_protocol_version"] = values[0]
			}
			if !reflect.DeepEqual(snapshot.Meta, want) {
				t.Fatalf("metadata = %#v, want %#v", snapshot.Meta, want)
			}
		})
	}
}

func TestCloudMetadataNonStringJSONIsNeverCoerced(t *testing.T) {
	for _, property := range []string{"orgValue", "value"} {
		for _, value := range []string{`104`, `true`, `{}`, `[]`} {
			raw := []byte(`{"paramCategoryList":[{"fieldList":[{"storageName":"PTCv1","` + property + `":` + value + `}]}]}`)
			if snapshot, err := ParseDetailResponse(raw); err == nil || snapshot != nil {
				t.Fatalf("non-string %s %s accepted", property, value)
			}
		}
	}
	for _, raw := range []string{
		`{"paramCategoryList":[{"fieldList":[{"storageName":"PTCv1"}]}]}`,
		`{"paramCategoryList":[{"fieldList":[{"storageName":"PTCv1","orgValue":null,"value":null}]}]}`,
	} {
		snapshot, err := ParseDetailResponse([]byte(raw))
		if err != nil || snapshot.Meta != nil {
			t.Fatalf("missing/null metadata must remain absent, got %#v, %v", snapshot, err)
		}
	}
}

func parseMetadataFields(t *testing.T, fields []field) *model.Snapshot {
	t.Helper()
	raw, err := json.Marshal(detailResponse{Categories: []category{{Tag: "basic", FieldList: fields}}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
