package prom

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/buildinfo"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
)

type Collector struct {
	state           *poller.State
	dropSourceLabel bool
	gridLoadState   *poller.State
	gridInputState  *poller.State
	deviceSNSeed    string

	upDesc                       *prometheus.Desc
	buildInfoDesc                *prometheus.Desc
	pollSuccessDesc              *prometheus.Desc
	pollCountDesc                *prometheus.Desc
	lastUpdateDesc               *prometheus.Desc
	dataAgeDesc                  *prometheus.Desc
	lastPollSuccessDesc          *prometheus.Desc
	lastSourceSyncDesc           *prometheus.Desc
	pollDurationDesc             *prometheus.Desc
	errorCountDesc               *prometheus.Desc
	valueDesc                    *prometheus.Desc
	metadataInfoDesc             *prometheus.Desc
	gridLoadUpDesc               *prometheus.Desc
	gridLoadDataAgeDesc          *prometheus.Desc
	gridLoadLastUpdateDesc       *prometheus.Desc
	gridLoadLastPollSuccessDesc  *prometheus.Desc
	gridInputUpDesc              *prometheus.Desc
	gridInputDataAgeDesc         *prometheus.Desc
	gridInputLastUpdateDesc      *prometheus.Desc
	gridInputLastPollSuccessDesc *prometheus.Desc
}

type Option func(*Collector)

// WithGridLoad adds an independently polled Shelly stream. Its values retain
// the existing inverter identity labels, while health and freshness have their
// own metric families and never change the inverter's status.
func WithGridLoad(state *poller.State, deviceSN string) Option {
	return func(c *Collector) {
		c.gridLoadState = state
		c.deviceSNSeed = strings.TrimSpace(deviceSN)
	}
}

// WithGridInput adds the independently polled meter before the stabilizers.
func WithGridInput(state *poller.State, deviceSN string) Option {
	return func(c *Collector) { c.gridInputState = state; c.deviceSNSeed = strings.TrimSpace(deviceSN) }
}

func NewCollector(prefix string, state *poller.State, dropSourceLabel bool, options ...Option) *Collector {
	prefix = strings.Trim(prefix, "_")
	sourceLabels := []string{"source"}
	deviceLabels := []string{"source", "device_sn"}
	valueLabels := []string{"source", "device_sn", "group", "key", "name", "unit"}
	metadataLabels := []string{"source", "device_sn", "key", "value"}
	if dropSourceLabel {
		sourceLabels = nil
		deviceLabels = []string{"device_sn"}
		valueLabels = []string{"device_sn", "group", "key", "name", "unit"}
		metadataLabels = metadataLabels[1:]
	}
	pollCountLabels := append(append([]string{}, sourceLabels...), "result")

	collector := &Collector{
		state:           state,
		dropSourceLabel: dropSourceLabel,
		upDesc: prometheus.NewDesc(
			prefix+"_up",
			"1 if the last poll for the device succeeded, 0 otherwise.",
			deviceLabels,
			nil,
		),
		buildInfoDesc: prometheus.NewDesc(
			prefix+"_build_info",
			"Build information for the exporter.",
			[]string{"version", "commit", "date"},
			nil,
		),
		pollSuccessDesc: prometheus.NewDesc(
			prefix+"_poll_success",
			"1 if the last source poll succeeded, 0 otherwise.",
			sourceLabels,
			nil,
		),
		pollCountDesc: prometheus.NewDesc(
			prefix+"_polls_total",
			"Total number of polls by result.",
			pollCountLabels,
			nil,
		),
		lastUpdateDesc: prometheus.NewDesc(
			prefix+"_last_update_timestamp_seconds",
			"Unix timestamp of the last successful upstream update.",
			deviceLabels,
			nil,
		),
		dataAgeDesc: prometheus.NewDesc(
			prefix+"_data_age_seconds",
			"Age in seconds of the last accepted upstream collection timestamp, including while the device is unavailable.",
			deviceLabels,
			nil,
		),
		lastPollSuccessDesc: prometheus.NewDesc(
			prefix+"_last_poll_success_timestamp_seconds",
			"Unix timestamp when the exporter last completed a successful poll.",
			sourceLabels,
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
		metadataInfoDesc: prometheus.NewDesc(
			prefix+"_device_metadata_info",
			"Allowlisted lossless device metadata; 1 while the latest inverter poll is successful.",
			metadataLabels,
			nil,
		),
		gridLoadUpDesc: prometheus.NewDesc(
			prefix+"_grid_load_up",
			"1 if the last independent Shelly grid-load poll succeeded, 0 otherwise.",
			deviceLabels,
			nil,
		),
		gridLoadDataAgeDesc: prometheus.NewDesc(
			prefix+"_grid_load_data_age_seconds",
			"Age in seconds of the last accepted Shelly grid-load measurement, including while the meter is unavailable.",
			deviceLabels,
			nil,
		),
		gridLoadLastUpdateDesc: prometheus.NewDesc(
			prefix+"_grid_load_last_update_timestamp_seconds",
			"Unix timestamp of the last accepted Shelly grid-load measurement.",
			deviceLabels,
			nil,
		),
		gridLoadLastPollSuccessDesc: prometheus.NewDesc(
			prefix+"_grid_load_last_poll_success_timestamp_seconds",
			"Unix timestamp when the exporter last completed a successful independent Shelly grid-load poll.",
			deviceLabels,
			nil,
		),
	}
	collector.gridInputUpDesc = prometheus.NewDesc(prefix+"_grid_input_up", "1 if the last independent upstream Shelly poll succeeded, 0 otherwise.", deviceLabels, nil)
	collector.gridInputDataAgeDesc = prometheus.NewDesc(prefix+"_grid_input_data_age_seconds", "Age in seconds of the last accepted upstream Shelly measurement, including while unavailable.", deviceLabels, nil)
	collector.gridInputLastUpdateDesc = prometheus.NewDesc(prefix+"_grid_input_last_update_timestamp_seconds", "Unix timestamp of the last accepted upstream Shelly measurement.", deviceLabels, nil)
	collector.gridInputLastPollSuccessDesc = prometheus.NewDesc(prefix+"_grid_input_last_poll_success_timestamp_seconds", "Unix timestamp when the exporter last completed a successful independent upstream Shelly poll.", deviceLabels, nil)
	for _, option := range options {
		if option != nil {
			option(collector)
		}
	}
	return collector
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.upDesc
	ch <- c.buildInfoDesc
	ch <- c.pollSuccessDesc
	ch <- c.pollCountDesc
	ch <- c.lastUpdateDesc
	ch <- c.dataAgeDesc
	ch <- c.lastPollSuccessDesc
	ch <- c.lastSourceSyncDesc
	ch <- c.pollDurationDesc
	ch <- c.errorCountDesc
	ch <- c.valueDesc
	ch <- c.metadataInfoDesc
	if c.gridLoadState != nil {
		ch <- c.gridLoadUpDesc
		ch <- c.gridLoadDataAgeDesc
		ch <- c.gridLoadLastUpdateDesc
		ch <- c.gridLoadLastPollSuccessDesc
	}
	if c.gridInputState != nil {
		ch <- c.gridInputUpDesc
		ch <- c.gridInputDataAgeDesc
		ch <- c.gridInputLastUpdateDesc
		ch <- c.gridInputLastPollSuccessDesc
	}
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	status := c.state.Status()
	snapshot := status.Snapshot
	sourceName := strings.TrimSpace(status.SourceName)
	if sourceName == "" {
		sourceName = "unknown"
	}
	deviceSN := c.deviceSNSeed
	if deviceSN == "" {
		deviceSN = "unknown"
	}
	if snapshot != nil {
		sourceName = snapshot.Source
		if snapshot.DeviceSN != "" {
			deviceSN = snapshot.DeviceSN
		}
	}

	upValue := 0.0
	if status.Up {
		upValue = 1
	}
	ch <- prometheus.MustNewConstMetric(c.buildInfoDesc, prometheus.GaugeValue, 1, buildinfo.Version, buildinfo.Commit, buildinfo.Date)
	ch <- prometheus.MustNewConstMetric(c.upDesc, prometheus.GaugeValue, upValue, c.deviceLabelValues(sourceName, deviceSN)...)
	ch <- prometheus.MustNewConstMetric(c.pollSuccessDesc, prometheus.GaugeValue, upValue, c.sourceLabelValues(sourceName)...)
	ch <- prometheus.MustNewConstMetric(c.pollCountDesc, prometheus.CounterValue, float64(status.SuccessCount), c.pollCountLabelValues(sourceName, "success")...)
	ch <- prometheus.MustNewConstMetric(c.pollCountDesc, prometheus.CounterValue, float64(status.ErrorCount), c.pollCountLabelValues(sourceName, "error")...)
	ch <- prometheus.MustNewConstMetric(c.pollDurationDesc, prometheus.GaugeValue, status.LastPollDuration.Seconds(), c.sourceLabelValues(sourceName)...)
	ch <- prometheus.MustNewConstMetric(c.errorCountDesc, prometheus.CounterValue, float64(status.ErrorCount), c.sourceLabelValues(sourceName)...)
	if !status.LastSourceSuccessAt.IsZero() {
		syncTimestamp := float64(status.LastSourceSuccessAt.Unix())
		ch <- prometheus.MustNewConstMetric(c.lastUpdateDesc, prometheus.GaugeValue, syncTimestamp, c.deviceLabelValues(sourceName, deviceSN)...)
		ch <- prometheus.MustNewConstMetric(c.dataAgeDesc, prometheus.GaugeValue, max(0, time.Since(status.LastSourceSuccessAt).Seconds()), c.deviceLabelValues(sourceName, deviceSN)...)
		ch <- prometheus.MustNewConstMetric(c.lastSourceSyncDesc, prometheus.GaugeValue, syncTimestamp, sourceName)
	}
	if !status.LastPollSuccessAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(c.lastPollSuccessDesc, prometheus.GaugeValue, float64(status.LastPollSuccessAt.Unix()), c.sourceLabelValues(sourceName)...)
	}

	// The independent grid-load stream must still be exported on cold start
	// and after inverter failures, including while an inverter Fetch is blocked.
	seenValueLabels := make(map[string]struct{})
	if snapshot != nil {
		c.collectValues(ch, snapshot.Metrics, sourceName, deviceSN, "", seenValueLabels)
		if status.Up {
			c.collectMetadataInfo(ch, snapshot.Meta, sourceName, deviceSN)
		}
	}
	c.collectMeter(ch, c.gridLoadState, "grid_load", sourceName, deviceSN, [4]*prometheus.Desc{c.gridLoadUpDesc, c.gridLoadDataAgeDesc, c.gridLoadLastUpdateDesc, c.gridLoadLastPollSuccessDesc}, seenValueLabels)
	c.collectMeter(ch, c.gridInputState, "grid_input", sourceName, deviceSN, [4]*prometheus.Desc{c.gridInputUpDesc, c.gridInputDataAgeDesc, c.gridInputLastUpdateDesc, c.gridInputLastPollSuccessDesc}, seenValueLabels)
}

func (c *Collector) collectMeter(ch chan<- prometheus.Metric, state *poller.State, group, sourceName, deviceSN string, desc [4]*prometheus.Desc, seenValueLabels map[string]struct{}) {
	if state == nil {
		return
	}
	status := state.Status()
	up := 0.0
	if status.Up {
		up = 1
	}
	labelValues := c.deviceLabelValues(sourceName, deviceSN)
	ch <- prometheus.MustNewConstMetric(desc[0], prometheus.GaugeValue, up, labelValues...)
	if !status.LastSourceSuccessAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(desc[2], prometheus.GaugeValue, float64(status.LastSourceSuccessAt.Unix()), labelValues...)
		ch <- prometheus.MustNewConstMetric(desc[1], prometheus.GaugeValue, max(0, time.Since(status.LastSourceSuccessAt).Seconds()), labelValues...)
	}
	if !status.LastPollSuccessAt.IsZero() {
		ch <- prometheus.MustNewConstMetric(desc[3], prometheus.GaugeValue, float64(status.LastPollSuccessAt.Unix()), labelValues...)
	}
	if status.Snapshot != nil {
		c.collectValues(ch, status.Snapshot.Metrics, sourceName, deviceSN, group, seenValueLabels)
	}
}

func (c *Collector) collectValues(ch chan<- prometheus.Metric, metrics []model.Metric, sourceName, deviceSN, streamGroup string, seenValueLabels map[string]struct{}) {
	// A malformed or projected snapshot can contain the same logical point more
	// than once. Prometheus rejects an entire scrape when a collector emits two
	// samples with identical labels, so keep the first sample in snapshot order.
	// This applies even when the source label is retained.
	for _, metric := range metrics {
		group := strings.ToLower(strings.TrimSpace(metric.Group))
		if streamGroup != "" && group != streamGroup || streamGroup == "" && (c.gridLoadState != nil && group == "grid_load" || c.gridInputState != nil && group == "grid_input") {
			continue
		}
		if !isFinite(metric.Value) {
			continue
		}
		labelValues := []string{sourceName, deviceSN, metric.Group, metric.Key, metric.Name, metric.Unit}
		if c.dropSourceLabel {
			labelValues = labelValues[1:]
		}
		labelSignature := labelsSignature(labelValues)
		if _, ok := seenValueLabels[labelSignature]; ok {
			continue
		}
		seenValueLabels[labelSignature] = struct{}{}

		ch <- prometheus.MustNewConstMetric(c.valueDesc, prometheus.GaugeValue, metric.Value, labelValues...)
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (c *Collector) sourceLabelValues(sourceName string) []string {
	if c.dropSourceLabel {
		return nil
	}
	return []string{sourceName}
}

func (c *Collector) pollCountLabelValues(sourceName string, result string) []string {
	return append(c.sourceLabelValues(sourceName), result)
}

func (c *Collector) deviceLabelValues(sourceName, deviceSN string) []string {
	if c.dropSourceLabel {
		return []string{deviceSN}
	}
	return []string{sourceName, deviceSN}
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
