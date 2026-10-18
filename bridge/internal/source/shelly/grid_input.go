package shelly

import (
	"context"
	"fmt"
	"strings"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

var _ source.Source = (*GridInputClient)(nil)

// GridInputClient reads the upstream meter before the stabilizers. It reuses
// the validated RPC decoder, but never shares readings or identity with the
// separate parallel/grid-load meter.
type GridInputClient struct {
	meter *GridLoadClient
}

func NewGridInputClient(cfg config.ShellyGridInputConfig) (*GridInputClient, error) {
	if err := config.ValidateShellyGridInputURL(cfg.BaseURL); err != nil {
		return nil, err
	}
	if cfg.EMID < 0 {
		return nil, fmt.Errorf("shelly grid-input EM component id must be >= 0")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("shelly grid-input timeout must be > 0")
	}
	meter, err := NewGridLoadClient(cfg)
	if err != nil {
		return nil, err
	}
	return &GridInputClient{meter: meter}, nil
}

func (*GridInputClient) Name() string { return "shelly_grid_input" }

func (c *GridInputClient) Fetch(ctx context.Context) (*model.Snapshot, error) {
	snapshot, err := c.meter.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	snapshot.Source = c.Name()
	for i := range snapshot.Metrics {
		metric := &snapshot.Metrics[i]
		metric.Group = "grid_input"
		metric.Name = strings.Replace(metric.Name, "Grid Load ", "Grid Input ", 1)
		if metric.Key == "internal_temperature" {
			metric.Name = "Shelly Grid Input Internal Temperature"
		}
	}
	snapshot.Meta = map[string]string{
		"shelly_grid_input_url":   snapshot.Meta["shelly_grid_load_url"],
		"shelly_grid_input_em_id": snapshot.Meta["shelly_grid_load_em_id"],
	}
	return snapshot, nil
}
