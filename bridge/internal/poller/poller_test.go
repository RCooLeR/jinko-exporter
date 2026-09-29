package poller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPollOnceRecoversSourcePanic(t *testing.T) {
	state := NewState("panic-source")
	runner := NewRunner(panicSource{}, time.Hour, state, nil, config.AlertConfig{})

	runner.pollOnce(context.Background())

	status := state.Status()
	if status.Up {
		t.Fatal("up = true, want false after panic")
	}
	if status.ErrorCount != 1 {
		t.Fatalf("errorCount = %d, want 1", status.ErrorCount)
	}
	if !strings.Contains(status.LastError, "poll panic: boom") {
		t.Fatalf("lastError = %q, want panic details", status.LastError)
	}
}

func TestPollSuccessUsesWallClockSeparateFromCollectedAt(t *testing.T) {
	state := NewState("stale-source")
	collectedAt := time.Now().Add(-24 * time.Hour)
	runner := NewRunner(staticSource{snapshot: &model.Snapshot{
		Source:      "stale-source",
		CollectedAt: collectedAt,
		Metrics:     []model.Metric{{Key: "OK", Value: 1}},
	}}, time.Hour, state, nil, config.AlertConfig{})

	startedAt := time.Now()
	runner.pollOnce(context.Background())

	status := state.Status()
	if !status.Up {
		t.Fatal("up = false, want successful poll")
	}
	if status.SuccessCount != 1 {
		t.Fatalf("successCount = %d, want 1", status.SuccessCount)
	}
	if !status.LastSourceSuccessAt.Equal(collectedAt) {
		t.Fatalf("lastSourceSuccessAt = %s, want collectedAt %s", status.LastSourceSuccessAt, collectedAt)
	}
	if status.LastPollSuccessAt.Before(startedAt) || time.Since(status.LastPollSuccessAt) > time.Second {
		t.Fatalf("lastPollSuccessAt = %s, want recent wall-clock time", status.LastPollSuccessAt)
	}
	if !state.Ready(time.Minute) {
		t.Fatal("Ready(time.Minute) = false, want true after recent poll")
	}

	state.mu.Lock()
	state.lastPollSuccessAt = time.Now().Add(-time.Hour)
	state.mu.Unlock()
	if state.Ready(time.Minute) {
		t.Fatal("Ready(time.Minute) = true, want stale readiness when max age is exceeded")
	}
}

func TestNoSuccessfulPollMonitorFiresWhileSourceHangs(t *testing.T) {
	notifier := &recordingNotifier{delivered: make(chan string, 1)}
	manager := alert.NewManager(notifier, time.Hour)
	state := NewState("hanging-source")
	runner := NewRunner(
		hangingSource{},
		time.Hour,
		state,
		manager,
		config.AlertConfig{NoSuccessfulPollWindow: 20 * time.Millisecond},
	)
	ctx := t.Context()

	go runner.Run(ctx)

	select {
	case subject := <-notifier.delivered:
		if !strings.Contains(subject, "No successful poll") {
			t.Fatalf("subject = %q, want no-successful-poll alert", subject)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no-successful-poll alert did not fire while source was hanging")
	}
}

func TestNoSuccessfulPollMonitorInterval(t *testing.T) {
	if got := noSuccessfulPollMonitorInterval(time.Millisecond); got != 10*time.Millisecond {
		t.Fatalf("small window interval = %s, want 10ms", got)
	}
	if got := noSuccessfulPollMonitorInterval(40 * time.Millisecond); got != 20*time.Millisecond {
		t.Fatalf("normal window interval = %s, want 20ms", got)
	}
	if got := noSuccessfulPollMonitorInterval(10 * time.Minute); got != time.Minute {
		t.Fatalf("large window interval = %s, want 1m", got)
	}
}

type panicSource struct{}

func (panicSource) Name() string {
	return "panic-source"
}

func (panicSource) Fetch(context.Context) (*model.Snapshot, error) {
	panic("boom")
}

type hangingSource struct{}

func (hangingSource) Name() string {
	return "hanging-source"
}

type staticSource struct {
	snapshot *model.Snapshot
}

func (s staticSource) Name() string {
	return "stale-source"
}

func (s staticSource) Fetch(context.Context) (*model.Snapshot, error) {
	return s.snapshot, nil
}

func (hangingSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type recordingNotifier struct {
	delivered chan string
}

func (n *recordingNotifier) Notify(_ context.Context, subject string, _ string) error {
	n.delivered <- subject
	return nil
}
