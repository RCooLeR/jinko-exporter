package prom

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/RCooLeR/jinko-exporter/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeSource struct {
	name     string
	snapshot *model.Snapshot
}

func (s fakeSource) Name() string {
	return s.name
}

func (s fakeSource) Fetch(context.Context) (*model.Snapshot, error) {
	return s.snapshot, nil
}

func TestCollectorKeepsSourceLabelAndAddsLastSourceSync(t *testing.T) {
	state := newPolledState(t)
	registry := prometheus.NewRegistry()
	if err := registry.Register(NewCollector("solar", state, false)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	expected := `
# HELP solar_last_source_sync_timestamp_seconds Unix timestamp of the last successful upstream update by source.
# TYPE solar_last_source_sync_timestamp_seconds gauge
solar_last_source_sync_timestamp_seconds{source="jinko"} 1.7760084e+09
# HELP solar_metric Numeric solar metric values from the selected source.
# TYPE solar_metric gauge
solar_metric{device_sn="dev-1",group="grid",key="grid_voltage",name="Grid voltage",source="jinko",unit="V"} 230
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "solar_last_source_sync_timestamp_seconds", "solar_metric"); err != nil {
		t.Fatalf("GatherAndCompare() error = %v", err)
	}
}

func TestCollectorDropsSourceLabelExceptLastSourceSync(t *testing.T) {
	state := newPolledState(t)
	registry := prometheus.NewRegistry()
	if err := registry.Register(NewCollector("solar", state, true)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	expected := `
# HELP solar_last_source_sync_timestamp_seconds Unix timestamp of the last successful upstream update by source.
# TYPE solar_last_source_sync_timestamp_seconds gauge
solar_last_source_sync_timestamp_seconds{source="jinko"} 1.7760084e+09
# HELP solar_metric Numeric solar metric values from the selected source.
# TYPE solar_metric gauge
solar_metric{device_sn="dev-1",group="grid",key="grid_voltage",name="Grid voltage",unit="V"} 230
`

	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "solar_last_source_sync_timestamp_seconds", "solar_metric"); err != nil {
		t.Fatalf("GatherAndCompare() error = %v", err)
	}
}

func newPolledState(t *testing.T) *poller.State {
	t.Helper()

	state := poller.NewState("jinko")
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "dev-1",
		CollectedAt: time.Date(2026, 4, 12, 15, 40, 0, 0, time.UTC),
		Metrics: []model.Metric{
			{
				Group: "grid",
				Key:   "grid_voltage",
				Name:  "Grid voltage",
				Unit:  "V",
				Value: 230,
			},
		},
	}

	src := fakeSource{name: "jinko", snapshot: snapshot}
	runner := poller.NewRunner(src, time.Hour, state, nil, config.AlertConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()

	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for poller snapshot")
		case <-ticker.C:
			if state.HasSnapshot() {
				cancel()
				<-done
				return state
			}
		}
	}
}
