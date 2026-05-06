package model

import "time"

type Metric struct {
	Group string  `json:"group"`
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Unit  string  `json:"unit,omitempty"`
	Value float64 `json:"value"`
}

type Snapshot struct {
	Source      string            `json:"source"`
	DeviceSN    string            `json:"device_sn,omitempty"`
	ParentSN    string            `json:"parent_sn,omitempty"`
	DeviceID    string            `json:"device_id,omitempty"`
	SiteID      string            `json:"site_id,omitempty"`
	CollectedAt time.Time         `json:"collected_at"`
	Metrics     []Metric          `json:"metrics"`
	Meta        map[string]string `json:"meta,omitempty"`
}
