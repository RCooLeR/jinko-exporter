package source

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityFirstSuccessWins(t *testing.T) {
	primary := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "PRIMARY")}
	fallback := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "FALLBACK")}
	priority := NewPriority([]Source{primary, fallback}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "jinko" || primary.calls != 1 || fallback.calls != 0 {
		t.Fatalf("snapshot source=%q primary calls=%d fallback calls=%d", snapshot.Source, primary.calls, fallback.calls)
	}
}

func TestPriorityFallsBackAfterFailure(t *testing.T) {
	primary := &stubSource{name: "jinko", err: errors.New("cloud down")}
	fallback := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "FALLBACK")}
	priority := NewPriority([]Source{primary, fallback}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "solarman" || primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("snapshot source=%q primary calls=%d fallback calls=%d", snapshot.Source, primary.calls, fallback.calls)
	}
}

func TestPriorityAllFail(t *testing.T) {
	priority := NewPriority([]Source{
		&stubSource{name: "jinko", err: errors.New("cloud down")},
		&stubSource{name: "solarman", err: errors.New("quota down")},
	}, false)

	_, err := priority.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "all priority sources failed") {
		t.Fatalf("Fetch() error = %v, want all-failed error", err)
	}
}

func TestPriorityProjectsFallbackMetricsToPrimarySurface(t *testing.T) {
	primary := &stubSource{name: "jinko", snapshot: &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "PRIMARY_SN",
		CollectedAt: time.Unix(10, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 100},
		},
	}}
	fallback := &stubSource{name: "solarman", snapshot: &model.Snapshot{
		Source:      "solarman",
		DeviceSN:    "FALLBACK_SN",
		CollectedAt: time.Unix(20, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "other", Key: "DP1", Name: "Fallback PV1", Unit: "watts", Value: 200},
			{Group: "other", Key: "EXTRA", Name: "Extra", Unit: "W", Value: 300},
		},
	}}
	priority := NewPriority([]Source{primary, fallback}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("cloud down")

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	if snapshot.DeviceSN != "PRIMARY_SN" {
		t.Fatalf("DeviceSN = %q, want PRIMARY_SN", snapshot.DeviceSN)
	}
	if len(snapshot.Metrics) != 1 {
		t.Fatalf("metric count = %d, want 1", len(snapshot.Metrics))
	}
	metric := snapshot.Metrics[0]
	if metric.Group != "electric" || metric.Key != "DP1" || metric.Name != "DC Power PV1" || metric.Unit != "W" || metric.Value != 200 {
		t.Fatalf("projected metric = %#v", metric)
	}
}

type stubSource struct {
	name     string
	snapshot *model.Snapshot
	err      error
	calls    int
}

func (s *stubSource) Name() string {
	return s.name
}

func (s *stubSource) Fetch(context.Context) (*model.Snapshot, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshot, nil
}

func testSourceSnapshot(sourceName string, deviceSN string) *model.Snapshot {
	return &model.Snapshot{
		Source:      sourceName,
		DeviceSN:    deviceSN,
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 100},
		},
	}
}
