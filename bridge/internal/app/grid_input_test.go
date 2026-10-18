package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/shelly"
)

type blockedMeterTestSource struct{}

func (blockedMeterTestSource) Name() string { return "blocked" }
func (blockedMeterTestSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestEitherShellyPollsIndependentlyWhenInverterAndOtherMeterBlock(t *testing.T) {
	for _, upstream := range []bool{false, true} {
		t.Run(map[bool]string{false: "downstream", true: "upstream"}[upstream], func(t *testing.T) {
			var calls atomic.Int32
			meter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/rpc/EM.GetStatus":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 0, "total_act_power": calls.Add(1), "a_voltage": 230})
				case "/rpc/EMData.GetStatus":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 0, "total_act": 1000})
				case "/rpc/Temperature.GetStatus":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 0, "tC": 45})
				default:
					t.Errorf("unexpected RPC %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer meter.Close()
			cfg := config.ShellyGridLoadConfig{Enabled: true, BaseURL: meter.URL, Timeout: time.Second}
			var live source.Source
			var err error
			group := "grid_load"
			if upstream {
				live, err = shelly.NewGridInputClient(cfg)
				group = "grid_input"
			} else {
				live, err = shelly.NewGridLoadClient(cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			inverterState, otherState, liveState := poller.NewState("modbus"), poller.NewState("other_meter"), poller.NewState(live.Name())
			updates := &gridPollUpdates{snapshots: make(chan *model.Snapshot, 8)}
			ctx, cancel := context.WithCancel(t.Context())
			done := startPollRunners(ctx,
				poller.NewRunner(blockedMeterTestSource{}, 20*time.Millisecond, inverterState, nil, config.AlertConfig{}),
				poller.NewRunner(blockedMeterTestSource{}, 20*time.Millisecond, otherState, nil, config.AlertConfig{}),
				poller.NewRunner(live, 20*time.Millisecond, liveState, nil, config.AlertConfig{}, updates))
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("shutdown failed to join all three runners")
				}
			}()
			var previous time.Time
			for range 2 {
				select {
				case snapshot := <-updates.snapshots:
					if !snapshot.CollectedAt.After(previous) {
						t.Fatal("meter timestamp did not advance")
					}
					previous = snapshot.CollectedAt
					for _, metric := range snapshot.Metrics {
						if metric.Group != group {
							t.Fatal("meter crossed stream identity")
						}
					}
				case <-time.After(2 * time.Second):
					t.Fatal("blocked peer prevented independent meter updates")
				}
			}
			if inverterState.Status().Up || otherState.Status().Up {
				t.Fatal("fresh meter marked blocked peer up")
			}
		})
	}
}
