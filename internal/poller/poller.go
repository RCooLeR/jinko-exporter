package poller

import (
	"context"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/alert"
	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/RCooLeR/jinko-exporter/internal/source"
	"github.com/rs/zerolog/log"
)

type State struct {
	mu sync.RWMutex

	sourceName       string
	snapshot         *model.Snapshot
	lastPollDuration time.Duration
	lastError        string
	lastSuccessAt    time.Time
	up               bool
	errorCount       uint64
}

func NewState(sourceName string) *State {
	return &State{sourceName: sourceName}
}

func (s *State) HasSnapshot() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot != nil
}

func (s *State) Snapshot() (*model.Snapshot, time.Duration, string, time.Time, bool, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot, s.lastPollDuration, s.lastError, s.lastSuccessAt, s.up, s.errorCount
}

type Runner struct {
	src       source.Source
	interval  time.Duration
	state     *State
	alerts    *alert.Manager
	alertCfg  config.AlertConfig
	startedAt time.Time
}

func NewRunner(src source.Source, interval time.Duration, state *State, alerts *alert.Manager, alertCfg config.AlertConfig) *Runner {
	return &Runner{
		src:       src,
		interval:  interval,
		state:     state,
		alerts:    alerts,
		alertCfg:  alertCfg,
		startedAt: time.Now(),
	}
}

func (r *Runner) Run(ctx context.Context) {
	r.pollOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce(ctx)
		}
	}
}

func (r *Runner) pollOnce(ctx context.Context) {
	start := time.Now()
	snapshot, err := r.src.Fetch(ctx)
	duration := time.Since(start)
	if err != nil {
		var lastSuccessAt time.Time
		var lastError string

		r.state.mu.Lock()
		r.state.lastPollDuration = duration
		r.state.up = false
		r.state.errorCount++
		r.state.lastError = err.Error()
		lastSuccessAt = r.state.lastSuccessAt
		lastError = r.state.lastError
		r.state.mu.Unlock()

		log.Error().Err(err).Str("source", r.src.Name()).Dur("duration", duration).Msg("poll failed")
		alert.EvaluateNoSuccessfulPoll(ctx, r.alerts, r.alertCfg, r.src.Name(), r.startedAt, lastSuccessAt, lastError)
		return
	}

	r.state.mu.Lock()
	r.state.snapshot = snapshot
	r.state.lastPollDuration = duration
	r.state.lastSuccessAt = snapshot.CollectedAt
	r.state.lastError = ""
	r.state.up = true
	r.state.mu.Unlock()

	alert.EvaluateSnapshot(ctx, r.alerts, r.alertCfg, snapshot)
	log.Info().Str("source", snapshot.Source).Int("metric_count", len(snapshot.Metrics)).Dur("duration", duration).Msg("poll succeeded")
}
