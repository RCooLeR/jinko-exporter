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

	_, _, lastError, _, up, errorCount := state.Snapshot()
	if up {
		t.Fatal("up = true, want false after panic")
	}
	if errorCount != 1 {
		t.Fatalf("errorCount = %d, want 1", errorCount)
	}
	if !strings.Contains(lastError, "poll panic: boom") {
		t.Fatalf("lastError = %q, want panic details", lastError)
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
