package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestFailedPollPreservesLastAcceptedSnapshotAndDisablesReadiness(t *testing.T) {
	accepted := &model.Snapshot{
		Source: "solarman", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now().Add(-5 * time.Minute),
		Metrics: []model.Metric{{Key: "DP1", Value: 2230}},
	}
	src := &changingSource{snapshot: accepted}
	state := NewState(src.Name())
	observer := &freshnessObserver{}
	runner := NewRunner(src, time.Minute, state, nil, config.AlertConfig{}, observer)
	runner.pollOnce(t.Context())
	before := state.Status()
	if !state.Ready(time.Hour) || !before.Up {
		t.Fatal("successful initial poll is not ready/up")
	}

	// An error must take precedence even if a source also returns a payload.
	src.snapshot = &model.Snapshot{Source: "solarman", CollectedAt: time.Now()}
	src.err = errors.New("device is offline")
	runner.pollOnce(t.Context())
	runner.pollOnce(t.Context())
	after := state.Status()
	if after.Up || state.Ready(time.Hour) || state.Ready(0) {
		t.Fatal("failed poll must immediately clear up/readiness, even with a recent accepted snapshot")
	}
	if after.Snapshot != accepted || !after.LastSourceSuccessAt.Equal(before.LastSourceSuccessAt) || !after.LastPollSuccessAt.Equal(before.LastPollSuccessAt) {
		t.Fatalf("failed polls replaced accepted data or success timestamps: before=%+v after=%+v", before, after)
	}
	if after.SuccessCount != 1 || after.ErrorCount != 2 || after.LastError != "device is offline" {
		t.Fatalf("status after offline polls = %+v, want 1 success, 2 errors, offline reason", after)
	}
	if observer.successes != 1 || observer.failures != 2 || observer.lastErrorCount != 2 {
		t.Fatalf("observer = %+v, want one accepted snapshot and two failures", observer)
	}
}

type changingSource struct {
	snapshot *model.Snapshot
	err      error
}

func (*changingSource) Name() string { return "solarman" }
func (s *changingSource) Fetch(context.Context) (*model.Snapshot, error) {
	return s.snapshot, s.err
}

type freshnessObserver struct {
	successes      int
	failures       int
	lastErrorCount uint64
}

func (o *freshnessObserver) OnPollSuccess(*model.Snapshot, time.Duration) error {
	o.successes++
	return nil
}

func (o *freshnessObserver) OnPollFailure(_ string, _ error, _ time.Duration, count uint64) error {
	o.failures++
	o.lastErrorCount = count
	return nil
}
