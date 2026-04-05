package prom

import (
	"strings"

	"github.com/RCooLeR/jinko-exporter/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	state *poller.State

	upDesc           *prometheus.Desc
	lastUpdateDesc   *prometheus.Desc
	pollDurationDesc *prometheus.Desc
	errorCountDesc   *prometheus.Desc
	valueDesc        *prometheus.Desc
}

func NewCollector(prefix string, state *poller.State) *Collector {
	prefix = strings.Trim(prefix, "_")
	return &Collector{
		state: state,
		upDesc: prometheus.NewDesc(
			prefix+"_up",
			"1 if the last poll for the device succeeded, 0 otherwise.",
			[]string{"source", "device_sn"},
			nil,
		),
		lastUpdateDesc: prometheus.NewDesc(
			prefix+"_last_update_timestamp_seconds",
			"Unix timestamp of the last successful upstream update.",
			[]string{"source", "device_sn"},
			nil,
		),
		pollDurationDesc: prometheus.NewDesc(
			prefix+"_poll_duration_seconds",
			"Duration of the last source poll in seconds.",
			[]string{"source"},
			nil,
		),
		errorCountDesc: prometheus.NewDesc(
			prefix+"_request_errors_total",
			"Total number of poll errors.",
			[]string{"source"},
			nil,
		),
		valueDesc: prometheus.NewDesc(
			prefix+"_metric",
			"Numeric solar metric values from the selected source.",
			[]string{"source", "device_sn", "group", "key", "name", "unit"},
			nil,
		),
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.upDesc
	ch <- c.lastUpdateDesc
	ch <- c.pollDurationDesc
	ch <- c.errorCountDesc
	ch <- c.valueDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	snapshot, lastDuration, _, lastSuccessAt, up, errorCount := c.state.Snapshot()
	sourceName := "unknown"
	deviceSN := "unknown"
	if snapshot != nil {
		sourceName = snapshot.Source
		if snapshot.DeviceSN != "" {
			deviceSN = snapshot.DeviceSN
		}
	}

	upValue := 0.0
	if up {
		upValue = 1
	}
	ch <- prometheus.MustNewConstMetric(c.upDesc, prometheus.GaugeValue, upValue, sourceName, deviceSN)
	ch <- prometheus.MustNewConstMetric(c.pollDurationDesc, prometheus.GaugeValue, lastDuration.Seconds(), sourceName)
	ch <- prometheus.MustNewConstMetric(c.errorCountDesc, prometheus.CounterValue, float64(errorCount), sourceName)
	if !lastSuccessAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.lastUpdateDesc, prometheus.GaugeValue, float64(lastSuccessAt.Unix()), sourceName, deviceSN)
	}

	if snapshot == nil {
		return
	}

	// Keep the public metric surface generic across all sources; source-specific field names stay in labels.
	for _, metric := range snapshot.Metrics {
		ch <- prometheus.MustNewConstMetric(
			c.valueDesc,
			prometheus.GaugeValue,
			metric.Value,
			snapshot.Source,
			deviceSN,
			metric.Group,
			metric.Key,
			metric.Name,
			metric.Unit,
		)
	}
}
