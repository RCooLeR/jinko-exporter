package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/solarman"
)

func TestPriorityRejectsCachedSolarmanDataAndUsesValidLaterSource(t *testing.T) {
	for _, failure := range []string{"stale", "offline"} {
		t.Run(failure, func(t *testing.T) {
			cloud := newFreshnessSolarmanClient(t, failure)
			accepted := &model.Snapshot{
				Source: "jinko", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: time.Now().UTC(),
				Metrics: []model.Metric{{Group: "electric", Key: "DP1", Value: 0}},
			}
			fallback := &freshnessFallback{snapshot: accepted}
			priority := source.NewPriority([]source.Source{cloud, fallback}, false)
			snapshot, err := priority.Fetch(t.Context())
			if err != nil {
				t.Fatalf("Fetch() = %v, want valid later fallback", err)
			}
			if snapshot != accepted || fallback.calls != 1 {
				t.Fatalf("invalid Solarman data won priority: snapshot=%+v fallback calls=%d", snapshot, fallback.calls)
			}
		})
	}
}

func TestPriorityAllSourcesFailWhenSolarmanReturnsCachedData(t *testing.T) {
	for _, failure := range []string{"stale", "offline"} {
		t.Run(failure, func(t *testing.T) {
			cloud := newFreshnessSolarmanClient(t, failure)
			primary := &freshnessFallback{err: errors.New("local source unavailable")}
			priority := source.NewPriority([]source.Source{primary, cloud}, false)
			state := poller.NewState(priority.Name())
			observer := &freshnessPollResult{done: make(chan error, 1)}
			runner := poller.NewRunner(priority, time.Hour, state, nil, config.AlertConfig{}, observer)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runnerDone := make(chan struct{})
			go func() {
				defer close(runnerDone)
				runner.Run(ctx)
			}()
			select {
			case err := <-observer.done:
				if err == nil || !strings.Contains(err.Error(), failure) {
					t.Errorf("poll result = %v, want %s rejection", err, failure)
				}
			case <-time.After(5 * time.Second):
				t.Error("poll did not finish")
			}
			cancel()
			<-runnerDone
			status := state.Status()
			if status.Up || status.Snapshot != nil || status.SuccessCount != 0 || status.ErrorCount != 1 {
				t.Fatalf("cached cloud data counted as a successful poll: %+v", status)
			}
			if !status.LastSourceSuccessAt.IsZero() || !status.LastPollSuccessAt.IsZero() || state.Ready(time.Hour) {
				t.Fatalf("cached cloud data advanced successful timestamps/readiness: %+v", status)
			}
		})
	}
}

func newFreshnessSolarmanClient(t *testing.T, failure string) *solarman.Client {
	t.Helper()
	collectedAt := time.Now().UTC().Truncate(time.Second)
	deviceState := 1
	if failure == "stale" {
		collectedAt = collectedAt.Add(-48 * time.Hour)
	} else {
		deviceState = 3
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/account/v1.0/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "access_token": "synthetic-token", "token_type": "Bearer", "expires_in": "3600",
			})
		case "/device/v1.0/currentData":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "deviceState": deviceState, "collectionTime": collectedAt.Unix(),
				"dataList": []map[string]any{{"key": "DP1", "name": "DC Power PV1", "unit": "W", "value": "2230"}},
			})
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return solarman.New(config.SolarmanConfig{
		BaseURL: server.URL, APIVersion: "v1.0", AppID: "synthetic-app", AppSecret: "synthetic-secret",
		Email: "synthetic@example.invalid", Password: "synthetic-password", DeviceSN: "SYNTHETIC_INV_001",
		Timeout: time.Second, MaxDataAge: 15 * time.Minute,
	}, nil)
}

type freshnessFallback struct {
	snapshot *model.Snapshot
	err      error
	calls    int
}

func (*freshnessFallback) Name() string { return "jinko" }

func (s *freshnessFallback) Fetch(context.Context) (*model.Snapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

type freshnessPollResult struct {
	done chan error
}

func (o *freshnessPollResult) OnPollSuccess(*model.Snapshot, time.Duration) error {
	o.done <- nil
	return nil
}

func (o *freshnessPollResult) OnPollFailure(_ string, err error, _ time.Duration, _ uint64) error {
	o.done <- err
	return nil
}
