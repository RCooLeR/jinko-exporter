package prom

import (
	"strconv"
	"strings"

	"github.com/RCooLeR/jinko-exporter/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	state           *poller.State
	dropSourceLabel bool

	upDesc             *prometheus.Desc
	lastUpdateDesc     *prometheus.Desc
	lastSourceSyncDesc *prometheus.Desc
	pollDurationDesc   *prometheus.Desc
	errorCountDesc     *prometheus.Desc
	valueDesc          *prometheus.Desc
}

func NewCollector(prefix string, state *poller.State, dropSourceLabel bool) *Collector {
	prefix = strings.Trim(prefix, "_")
	sourceLabels := []string{"source"}
	deviceLabels := []string{"source", "device_sn"}
	valueLabels := []string{"source", "device_sn", "group", "key", "name", "unit"}
	if dropSourceLabel {
		sourceLabels = nil
		deviceLabels = []string{"device_sn"}
		valueLabels = []string{"device_sn", "group", "key", "name", "unit"}
	}

	return &Collector{
		state:           state,
		dropSourceLabel: dropSourceLabel,
		upDesc: prometheus.NewDesc(
			prefix+"_up",
			"1 if the last poll for the device succeeded, 0 otherwise.",
			deviceLabels,
			nil,
		),
		lastUpdateDesc: prometheus.NewDesc(
			prefix+"_last_update_timestamp_seconds",
			"Unix timestamp of the last successful upstream update.",
			deviceLabels,
			nil,
		),
		lastSourceSyncDesc: prometheus.NewDesc(
			prefix+"_last_source_sync_timestamp_seconds",
			"Unix timestamp of the last successful upstream update by source.",
			[]string{"source"},
			nil,
		),
		pollDurationDesc: prometheus.NewDesc(
			prefix+"_poll_duration_seconds",
			"Duration of the last source poll in seconds.",
			sourceLabels,
			nil,
		),
		errorCountDesc: prometheus.NewDesc(
			prefix+"_request_errors_total",
			"Total number of poll errors.",
			sourceLabels,
			nil,
		),
		valueDesc: prometheus.NewDesc(
			prefix+"_metric",
			"Numeric solar metric values from the selected source.",
			valueLabels,
			nil,
		),
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.upDesc
	ch <- c.lastUpdateDesc
	ch <- c.lastSourceSyncDesc
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
	ch <- prometheus.MustNewConstMetric(c.upDesc, prometheus.GaugeValue, upValue, c.deviceLabelValues(sourceName, deviceSN)...)
	ch <- prometheus.MustNewConstMetric(c.pollDurationDesc, prometheus.GaugeValue, lastDuration.Seconds(), c.sourceLabelValues(sourceName)...)
	ch <- prometheus.MustNewConstMetric(c.errorCountDesc, prometheus.CounterValue, float64(errorCount), c.sourceLabelValues(sourceName)...)
	if !lastSuccessAt.IsZero() {
		syncTimestamp := float64(lastSuccessAt.Unix())
		ch <- prometheus.MustNewConstMetric(c.lastUpdateDesc, prometheus.GaugeValue, syncTimestamp, c.deviceLabelValues(sourceName, deviceSN)...)
		ch <- prometheus.MustNewConstMetric(c.lastSourceSyncDesc, prometheus.GaugeValue, syncTimestamp, sourceName)
	}

	if snapshot == nil {
		return
	}

	if !c.dropSourceLabel {
		for _, metric := range snapshot.Metrics {
			ch <- prometheus.MustNewConstMetric(
				c.valueDesc,
				prometheus.GaugeValue,
				metric.Value,
				sourceName,
				deviceSN,
				metric.Group,
				metric.Key,
				metric.Name,
				metric.Unit,
			)
		}
		return
	}

	// Label dropping can collapse source-specific metrics into the same Prometheus series.
	seenValueLabels := make(map[string]struct{}, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		labelValues := []string{deviceSN, metric.Group, metric.Key, metric.Name, metric.Unit}
		labelSignature := labelsSignature(labelValues)
		if _, ok := seenValueLabels[labelSignature]; ok {
			continue
		}
		seenValueLabels[labelSignature] = struct{}{}

		ch <- prometheus.MustNewConstMetric(c.valueDesc, prometheus.GaugeValue, metric.Value, labelValues...)
	}
}

func (c *Collector) sourceLabelValues(sourceName string) []string {
	if c.dropSourceLabel {
		return nil
	}
	return []string{sourceName}
}

func (c *Collector) deviceLabelValues(sourceName, deviceSN string) []string {
	if c.dropSourceLabel {
		return []string{deviceSN}
	}
	return []string{sourceName, deviceSN}
}

func (c *Collector) valueLabelValues(sourceName, deviceSN, group, key, name, unit string) []string {
	if c.dropSourceLabel {
		return []string{deviceSN, group, key, name, unit}
	}
	return []string{sourceName, deviceSN, group, key, name, unit}
}

func labelsSignature(values []string) string {
	var b strings.Builder
	for _, value := range values {
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteByte(':')
		b.WriteString(value)
		b.WriteByte('|')
	}
	return b.String()
}
