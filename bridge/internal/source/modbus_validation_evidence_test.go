package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func validationSnapshot() *model.Snapshot {
	snapshot := correlationModbusSnapshot(ModbusAlertSignature{})
	snapshot.Metrics = append(snapshot.Metrics, model.Metric{Group: "bms", Key: "BMS_SOC", Unit: "%", Value: 100})
	start := time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	window := func(offset time.Duration) model.ModbusReadWindow {
		return model.ModbusReadWindow{StartedAt: start.Add(offset), FinishedAt: start.Add(offset + time.Millisecond)}
	}
	snapshot.CollectedAt = start.Add(time.Second)
	snapshot.ModbusEvidence = &model.ModbusEvidence{
		StartedAt: start, FinishedAt: snapshot.CollectedAt,
		BMSWords:  [9]uint16{6480, 0, 0, 22, 100, 6152, 1, 1220, 0},
		BMSWindow: window(time.Millisecond), RelayWindow: window(3 * time.Millisecond),
		AlertWindow: window(5 * time.Millisecond), RunStateWindow: window(7 * time.Millisecond),
	}
	return snapshot
}

func newValidationTestObserver(now *time.Time, records *[]modbusValidationRecord) *modbusValidationObserver {
	return &modbusValidationObserver{now: func() time.Time { return *now }, emit: func(record modbusValidationRecord) {
		*records = append(*records, record)
	}}
}

func TestModbusValidationEvidenceMilestonesPreserveRawWords(t *testing.T) {
	now := time.Now()
	var records []modbusValidationRecord
	observer := newValidationTestObserver(&now, &records)
	snapshot := validationSnapshot()
	observer.observe(snapshot)
	observer.observe(snapshot)
	if len(records) != 1 || !slices.Equal(records[0].Reasons, []string{"first_complete_sample"}) {
		t.Fatalf("initial dedup = %#v", records)
	}
	snapshot.ModbusEvidence.BMSWords[6] = 0xffff
	observer.observe(snapshot)
	if len(records) != 2 || !slices.Equal(records[1].Reasons, []string{"r216_first_high_bit"}) {
		t.Fatalf("FFFF milestone = %#v", records)
	}
	snapshot.ModbusEvidence.BMSWords[6] = 0xfffe
	observer.observe(snapshot)
	if len(records) != 3 || !slices.Equal(records[2].Reasons, []string{"r216_first_high_bit_non_ffff"}) {
		t.Fatalf("FFFF consumed later useful high-bit evidence: %#v", records)
	}
	for _, index := range []int{1, 2, 8} {
		snapshot.ModbusEvidence.BMSWords[index] = 0xffff
	}
	observer.observe(snapshot)
	for _, index := range []int{1, 2, 8} {
		snapshot.ModbusEvidence.BMSWords[index] = 123
	}
	observer.observe(snapshot)
	if len(records) != 5 || len(records[3].Reasons) != 3 || len(records[4].Reasons) != 3 {
		t.Fatalf("limit milestones = %#v", records)
	}
	for range 10 {
		observer.observe(snapshot)
	}
	if len(records) != 5 || records[1].Evidence.BMSWords[6] != 0xffff || records[2].Evidence.BMSWords[6] != 0xfffe {
		t.Fatal("raw evidence changed, was decoded, or repeated")
	}
	for index, record := range records {
		if record.CaptureIndex != uint64(index+1) {
			t.Fatal("capture sequence is not deterministic")
		}
		for _, reason := range record.Reasons {
			if strings.Contains(reason, "negative") {
				t.Fatal("raw high bit is not proven negative current")
			}
		}
	}
	// Changing unrelated BMS measurements cannot flood the diagnostic log.
	snapshot.ModbusEvidence.BMSWords[5]++
	observer.observe(snapshot)
	if len(records) != 5 {
		t.Fatal("ordinary voltage movement created a record")
	}
}

func TestModbusValidationEvidenceSettledTransitionsAndBudget(t *testing.T) {
	now := time.Now()
	var records []modbusValidationRecord
	observer := newValidationTestObserver(&now, &records)
	snapshot := validationSnapshot()
	observer.observe(snapshot)
	snapshot.ModbusEvidence.RelayWords[0] = 0x8001
	now = now.Add(59 * time.Second)
	observer.observe(snapshot)
	if len(records) != 1 {
		t.Fatal("transition ignored minimum interval")
	}
	now = now.Add(time.Second)
	observer.observe(snapshot)
	if len(records) != 2 || records[1].Evidence.RelayWords[0] != 0x8001 {
		t.Fatal("settled transition was lost during rate limiting")
	}
	// Evidence must own arrays independently from both the snapshot and sink.
	records[1].Evidence.RelayWords[0] = 99
	observer.observe(snapshot)
	if len(records) != 2 {
		t.Fatal("sink mutation changed deduplication state")
	}
	for index := 1; index < validationChangeBudget; index++ {
		now = now.Add(time.Minute)
		snapshot.ModbusEvidence.RelayWords[1] = uint16(index)
		observer.observe(snapshot)
	}
	if len(records) != 1+validationChangeBudget || !slices.Contains(records[len(records)-1].Reasons, "status_change_budget_exhausted") {
		t.Fatalf("budget/marker = %d / %#v", len(records), records[len(records)-1])
	}
	for index := range 100 {
		now = now.Add(time.Minute)
		snapshot.ModbusEvidence.RelayWords[1] = uint16(index + 100)
		observer.observe(snapshot)
	}
	if len(records) != 1+validationChangeBudget {
		t.Fatal("transition budget was not terminal")
	}
	snapshot.ModbusEvidence.BMSWords[6] = 0xfffe
	observer.observe(snapshot)
	if len(records) != 2+validationChangeBudget || slices.Contains(records[len(records)-1].Reasons, "status_change_budget_exhausted") {
		t.Fatal("transition budget consumed independent BMS milestones or duplicated marker")
	}
	restarted := newValidationTestObserver(&now, &records)
	restarted.observe(snapshot)
	if records[len(records)-1].CaptureIndex != 1 || !slices.Contains(records[len(records)-1].Reasons, "first_complete_sample") {
		t.Fatal("new observer must start a fresh bounded session")
	}
}

func TestModbusValidationEvidenceRejectsInvalidSidecarsWithoutConsumingMilestones(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Snapshot)
	}{
		{"cloud", func(s *model.Snapshot) { s.Source = "solarman" }},
		{"missing", func(s *model.Snapshot) { s.ModbusEvidence = nil }},
		{"zero start", func(s *model.Snapshot) { s.ModbusEvidence.StartedAt = time.Time{} }},
		{"reversed fetch", func(s *model.Snapshot) { s.ModbusEvidence.StartedAt = s.CollectedAt.Add(time.Second) }},
		{"different snapshot", func(s *model.Snapshot) { s.CollectedAt = s.CollectedAt.Add(time.Second) }},
		{"long window", func(s *model.Snapshot) { s.ModbusEvidence.StartedAt = s.CollectedAt.Add(-31 * time.Second) }},
		{"reversed block", func(s *model.Snapshot) { s.ModbusEvidence.BMSWindow.FinishedAt = s.ModbusEvidence.StartedAt }},
		{"overlapping blocks", func(s *model.Snapshot) { s.ModbusEvidence.RelayWindow = s.ModbusEvidence.BMSWindow }},
		{"after finish", func(s *model.Snapshot) { s.ModbusEvidence.RunStateWindow.FinishedAt = s.CollectedAt.Add(time.Second) }},
		{"alert mismatch", func(s *model.Snapshot) { s.ModbusEvidence.AlertWords[0] = 1 }},
		{"missing metrics", func(s *model.Snapshot) { s.Metrics = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			var records []modbusValidationRecord
			observer := newValidationTestObserver(&now, &records)
			snapshot := validationSnapshot()
			test.mutate(snapshot)
			observer.observe(nil)
			observer.observe(snapshot)
			if len(records) != 0 || observer.seen != 0 {
				t.Fatal("invalid evidence logged or consumed a milestone")
			}
			observer.observe(validationSnapshot())
			if len(records) != 1 {
				t.Fatal("invalid observation blocked next valid capture")
			}
		})
	}
}

func TestPriorityPassiveEvidenceRequiresExplicitModbusPrimaryAndNeverFetchesCloud(t *testing.T) {
	var nilPriority *Priority
	if nilPriority.EnableModbusValidationEvidence() == nil {
		t.Fatal("nil priority accepted")
	}
	for _, sources := range [][]Source{nil, {nil}, {&stubSource{name: "jinko"}}, {&stubSource{name: "modbus"}, &stubSource{name: "modbus"}}} {
		if NewPriority(sources, false).EnableModbusValidationEvidence() == nil {
			t.Fatal("invalid primary topology accepted")
		}
	}
	now := time.Now()
	var records []modbusValidationRecord
	primary := &stubSource{name: "modbus", snapshot: validationSnapshot()}
	fallback := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "CLOUD")}
	priority := NewPriority([]Source{primary, fallback}, true)
	if _, err := priority.Fetch(context.Background()); err != nil || priority.validationEvidence != nil {
		t.Fatal("default path changed")
	}
	if err := priority.EnableModbusValidationEvidence(); err != nil {
		t.Fatal(err)
	}
	if priority.EnableModbusValidationEvidence() == nil {
		t.Fatal("duplicate activation reset budget")
	}
	priority.validationEvidence = newValidationTestObserver(&now, &records)
	snapshot, err := priority.Fetch(context.Background())
	if err != nil || snapshot != primary.snapshot || len(records) != 1 || primary.calls != 2 || fallback.calls != 0 || len(priority.backgroundMaintainers()) != 0 {
		t.Fatalf("passive acceptance changed: %v / records %d calls %d/%d", err, len(records), primary.calls, fallback.calls)
	}
	primary.err = errors.New("synthetic failure")
	fallback.snapshot = testSourceSnapshot("solarman", "SYNTHETIC_DEVICE")
	fallback.snapshot.Metrics = []model.Metric{{Group: "bms", Key: "BMS_SOC", Unit: "%", Value: 99}}
	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || fallback.calls != 1 || primary.calls != 3 {
		t.Fatal("fallback emitted Modbus evidence")
	}
	primary.err = nil
	primary.snapshot.ModbusEvidence.BMSWords[6] = 0xfffe
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = priority.Fetch(canceled)
	if len(records) != 1 {
		t.Fatal("canceled fetch consumed evidence")
	}
}

func TestPassiveEvidenceLogsNoIdentityOrTransportAndSnapshotJSONHidesIt(t *testing.T) {
	var output bytes.Buffer
	oldLogger := log.Logger
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() { log.Logger = oldLogger })
	snapshot := validationSnapshot()
	snapshot.DeviceSN, snapshot.ParentSN, snapshot.DeviceID, snapshot.SiteID = "secret-device", "secret-logger", "secret-id", "secret-site"
	snapshot.Meta = map[string]string{"secret-url": "private-credential", "profile": "untrusted-profile"}
	observer := &modbusValidationObserver{now: time.Now, emit: logModbusValidationEvidence}
	observer.observe(snapshot)
	for _, forbidden := range []string{"secret-", "private-credential", "untrusted-profile", "TX=", "RX="} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("evidence leaked %q", forbidden)
		}
	}
	var record struct {
		Evidence model.ModbusEvidence `json:"raw_evidence"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil || !reflect.DeepEqual(record.Evidence, *snapshot.ModbusEvidence) {
		t.Fatalf("lossy record: %v %s", err, output.Bytes())
	}
	withEvidence, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ModbusEvidence = nil
	withoutEvidence, _ := json.Marshal(snapshot)
	if !bytes.Equal(withEvidence, withoutEvidence) {
		t.Fatal("internal evidence leaked into snapshot JSON")
	}
}
