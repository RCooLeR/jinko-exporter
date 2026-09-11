package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/prom"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/shelly"
	"github.com/prometheus/client_golang/prometheus"
)

func TestShellyKeepsPollingWhileInverterRequestIsBlocked(t *testing.T) {
	primaryStarted := make(chan struct{}, 1)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case primaryStarted <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer primary.Close()
	var meterCalls atomic.Int32
	meter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/EM.GetStatus":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 0, "total_act_power": 100 + meterCalls.Add(1), "a_voltage": 230})
		case "/rpc/EMData.GetStatus":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 0, "total_act": 1000})
		default:
			t.Errorf("unexpected Shelly RPC %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer meter.Close()
	cfg := config.Config{
		SourcePriority: []string{"jinko"},
		Jinko:          config.JinkoConfig{URL: primary.URL, Timeout: time.Minute, RetryAttempts: 1, BearerToken: "synthetic-token"},
		ShellyGridLoad: config.ShellyGridLoadConfig{Enabled: true, BaseURL: meter.URL, Timeout: time.Second},
	}
	inverter, err := buildInverterSource(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	grid, err := shelly.NewGridLoadClient(cfg.ShellyGridLoad)
	if err != nil {
		t.Fatal(err)
	}
	inverterState, gridState := poller.NewState(inverter.Name()), poller.NewState(grid.Name())
	updates := &gridPollUpdates{snapshots: make(chan *model.Snapshot, 8)}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := startPollRunners(ctx,
		poller.NewRunner(inverter, 20*time.Millisecond, inverterState, nil, config.AlertConfig{}),
		poller.NewRunner(grid, 20*time.Millisecond, gridState, nil, config.AlertConfig{}, updates),
	)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("shutdown did not join both independent pollers")
		}
	}()
	select {
	case <-primaryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("inverter request did not start")
	}
	var previous *model.Snapshot
	for range 2 {
		select {
		case snapshot := <-updates.snapshots:
			if previous != nil && !snapshot.CollectedAt.After(previous.CollectedAt) {
				t.Fatal("Shelly collection time did not advance independently")
			}
			previous = snapshot
		case <-time.After(2 * time.Second):
			t.Fatal("blocked inverter prevented fresh Shelly polling")
		}
	}
	if status := inverterState.Status(); status.Up || status.SuccessCount != 0 || status.Snapshot != nil || !status.LastSourceSuccessAt.IsZero() {
		t.Fatalf("Shelly invented a successful inverter poll: %+v", status)
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(prom.NewCollector("solar", inverterState, true, prom.WithGridLoad(gridState, "SYNTHETIC_INV_001")))
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	foundGrid := false
	for _, family := range families {
		switch family.GetName() {
		case "solar_grid_load_up":
			foundGrid = family.GetMetric()[0].GetGauge().GetValue() == 1
		case "solar_up":
			if family.GetMetric()[0].GetGauge().GetValue() != 0 {
				t.Fatal("inverter was marked up by a grid-load update")
			}
		}
	}
	if !foundGrid {
		t.Fatal("fresh Shelly data is not independently available in Prometheus")
	}
}

func TestConfiguredInverterSerialUsesOnlyConfiguredPrioritySources(t *testing.T) {
	cfg := config.Config{
		SourcePriority: []string{"modbus", "jinko", "solarman"},
		Modbus:         config.ModbusConfig{DeviceSN: " PRIMARY "},
		Solarman:       config.SolarmanConfig{DeviceSN: "FALLBACK"},
	}
	if got := configuredInverterSerial(cfg); got != "PRIMARY" {
		t.Fatalf("serial = %q", got)
	}
	cfg.SourcePriority = []string{"jinko", "solarman"}
	if got := configuredInverterSerial(cfg); got != "FALLBACK" {
		t.Fatalf("serial = %q, want enabled fallback serial", got)
	}
	cfg.SourcePriority = []string{"jinko"}
	if got := configuredInverterSerial(cfg); got != "" {
		t.Fatalf("serial = %q, want unknown until inverter provides identity", got)
	}
}

type gridPollUpdates struct {
	snapshots chan *model.Snapshot
}

func (o *gridPollUpdates) OnPollSuccess(snapshot *model.Snapshot, _ time.Duration) error {
	select {
	case o.snapshots <- snapshot:
	default:
	}
	return nil
}

func (*gridPollUpdates) OnPollFailure(string, error, time.Duration, uint64) error { return nil }
