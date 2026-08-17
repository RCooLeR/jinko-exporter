package source

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestEnrichedAppendsExtraMetrics(t *testing.T) {
	primary := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "PRIMARY")}
	extra := &stubSource{name: "shelly_grid_load", snapshot: &model.Snapshot{
		Source:      "shelly_grid_load",
		CollectedAt: time.Unix(20, 0).UTC(),
		Meta:        map[string]string{"shelly_grid_load_url": "http://shelly.test"},
		Metrics: []model.Metric{
			{Group: "grid_load", Key: "total_power", Name: "Grid Load Total Power", Unit: "W", Value: 1200},
		},
	}}
	enriched := NewEnriched(primary, extra)

	snapshot, err := enriched.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "jinko" {
		t.Fatalf("Source = %q, want jinko", snapshot.Source)
	}
	if len(snapshot.Metrics) != 2 {
		t.Fatalf("metric count = %d, want 2", len(snapshot.Metrics))
	}
	if got := snapshot.Metrics[1]; got.Group != "grid_load" || got.Key != "total_power" || got.Value != 1200 {
		t.Fatalf("enriched metric = %#v", got)
	}
	if snapshot.Meta["shelly_grid_load_url"] != "http://shelly.test" {
		t.Fatalf("shelly meta not copied: %#v", snapshot.Meta)
	}
}

func TestEnrichedKeepsPrimarySnapshotWhenExtraFails(t *testing.T) {
	primary := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "PRIMARY")}
	extra := &stubSource{name: "shelly_grid_load", err: errors.New("shelly offline")}
	enriched := NewEnriched(primary, extra)

	snapshot, err := enriched.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "jinko" || len(snapshot.Metrics) != 1 {
		t.Fatalf("snapshot = %#v, want primary snapshot", snapshot)
	}
}

func TestEnrichedFlattensAndDeduplicatesBackgroundMaintainers(t *testing.T) {
	shared := newBackgroundStub("jinko")
	extra := newBackgroundStub("shelly_grid_load")
	priority := NewPriority([]Source{shared}, false)
	enriched := NewEnriched(priority, shared, extra)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		enriched.RunBackground(ctx)
		close(done)
	}()

	waitStarted(t, shared.started)
	waitStarted(t, extra.started)
	if shared.backgroundCalls.Load() != 1 || extra.backgroundCalls.Load() != 1 {
		t.Fatalf("background calls shared/extra=%d/%d, want 1/1", shared.backgroundCalls.Load(), extra.backgroundCalls.Load())
	}
	cancel()
	waitStarted(t, done)
}
