package modbus

import (
	"context"
	"fmt"

	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/RCooLeR/jinko-exporter/internal/source"
)

var _ source.Source = (*Client)(nil)

type Client struct {
	cfg config.ModbusConfig
}

func New(cfg config.ModbusConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Name() string {
	return "modbus"
}

func (c *Client) Fetch(context.Context) (*model.Snapshot, error) {
	return nil, fmt.Errorf("modbus source is not implemented yet; waiting for protocol/register documentation for host=%s port=%d logger_serial=%s unit_id=%d", c.cfg.Host, c.cfg.Port, c.cfg.LoggerSerial, c.cfg.UnitID)
}
