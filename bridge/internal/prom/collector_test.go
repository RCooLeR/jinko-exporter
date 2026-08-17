package prom

import (
	"context"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollectorSkipsInvalidMetricValues(t *testing.T) {
	state := poller.NewState("jinko")
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "OK", Name: "OK Metric", Unit: "W", Value: 123},
			{Group: "electric", Key: "NAN", Name: "NaN Metric", Unit: "W", Value: math.NaN()},
			{Group: "electric", Key: "INF", Name: "Inf Metric", Unit: "W", Value: math.Inf(1)},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(ctx)
	waitForSnapshot(t, state)

	registry := prometheus.NewRegistry()
	if err := registry.Register(NewCollector("solar", state, false)); err != nil {
		t.Fatalf("Register collector error = %v", err)
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	seenOK := false
	for _, family := range families {
		if family.GetName() != "solar_metric" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := metricLabels(metric)
			switch labels["key"] {
			case "OK":
				seenOK = true
			case "NAN", "INF":
				t.Fatalf("invalid metric key %q was exported", labels["key"])
			}
		}
	}
	if !seenOK {
		t.Fatal("valid metric OK was not exported")
	}
}

func TestCollectorDeduplicatesIdenticalMetricLabels(t *testing.T) {
	for _, dropSourceLabel := range []bool{false, true} {
		t.Run("drop_source_label="+map[bool]string{false: "false", true: "true"}[dropSourceLabel], func(t *testing.T) {
			state := poller.NewState("jinko")
			snapshot := &model.Snapshot{
				Source:      "jinko",
				DeviceSN:    "ABC123",
				CollectedAt: time.Unix(1775145150, 0).UTC(),
				Metrics: []model.Metric{
					{Group: "electric", Key: "DUP", Name: "Duplicate", Unit: "W", Value: 123},
					{Group: "electric", Key: "DUP", Name: "Duplicate", Unit: "W", Value: 456},
					{Group: "electric", Key: "OTHER", Name: "Other", Unit: "W", Value: 789},
				},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
			go runner.Run(ctx)
			waitForSnapshot(t, state)

			family := metricFamily(t, gatherCollector(t, NewCollector("solar", state, dropSourceLabel)), "solar_metric")
			if got := len(family.GetMetric()); got != 2 {
				t.Fatalf("solar_metric count = %d, want 2 unique label sets", got)
			}
			for _, metric := range family.GetMetric() {
				if metricLabels(metric)["key"] == "DUP" && metric.GetGauge().GetValue() != 123 {
					t.Fatalf("deduplicated value = %v, want first value 123", metric.GetGauge().GetValue())
				}
			}
		})
	}
}

func TestCollectorPreservesCanonicalOutputPowerLabels(t *testing.T) {
	state := poller.NewState("modbus")
	snapshot := &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "INV_O_P_T", Name: "Total Inverter Output Power", Unit: "W", Value: 2854},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(ctx)
	waitForSnapshot(t, state)

	families := gatherCollector(t, NewCollector("solar", state, false))
	family := metricFamily(t, families, "solar_metric")
	if len(family.GetMetric()) != 1 {
		t.Fatalf("solar_metric count = %d, want 1", len(family.GetMetric()))
	}
	metric := family.GetMetric()[0]
	wantLabels := map[string]string{
		"source": "modbus", "device_sn": "SYNTHETIC_INV_001", "group": "electric",
		"key": "INV_O_P_T", "name": "Total Inverter Output Power", "unit": "W",
	}
	for key, want := range wantLabels {
		if got := metricLabels(metric)[key]; got != want {
			t.Fatalf("label %s = %q, want %q", key, got, want)
		}
	}
	if got := metric.GetGauge().GetValue(); got != 2854 {
		t.Fatalf("output power = %v, want 2854", got)
	}
}

func TestCollectorExportsBuildInfoAndPollSuccess(t *testing.T) {
	state := poller.NewState("jinko")
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics:     []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 123}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(ctx)
	waitForSnapshot(t, state)

	families := gatherCollector(t, NewCollector("solar", state, false))

	buildInfo := metricFamily(t, families, "solar_build_info")
	if got := len(buildInfo.GetMetric()); got != 1 {
		t.Fatalf("build_info metric count = %d, want 1", got)
	}
	buildLabels := metricLabels(buildInfo.GetMetric()[0])
	for _, label := range []string{"version", "commit", "date"} {
		if buildLabels[label] == "" {
			t.Fatalf("build_info label %q is empty in %#v", label, buildLabels)
		}
	}
	if got := buildInfo.GetMetric()[0].GetGauge().GetValue(); got != 1 {
		t.Fatalf("build_info value = %v, want 1", got)
	}

	pollSuccess := metricFamily(t, families, "solar_poll_success")
	if got := len(pollSuccess.GetMetric()); got != 1 {
		t.Fatalf("poll_success metric count = %d, want 1", got)
	}
	pollLabels := metricLabels(pollSuccess.GetMetric()[0])
	if pollLabels["source"] != "jinko" {
		t.Fatalf("poll_success source label = %q, want jinko", pollLabels["source"])
	}
	if got := pollSuccess.GetMetric()[0].GetGauge().GetValue(); got != 1 {
		t.Fatalf("poll_success value = %v, want 1", got)
	}

	pollsTotal := metricFamily(t, families, "solar_polls_total")
	if got := counterValueForLabels(t, pollsTotal, map[string]string{"source": "jinko", "result": "success"}); got != 1 {
		t.Fatalf("polls_total success = %v, want 1", got)
	}
	if got := counterValueForLabels(t, pollsTotal, map[string]string{"source": "jinko", "result": "error"}); got != 0 {
		t.Fatalf("polls_total error = %v, want 0", got)
	}

	lastPollSuccess := metricFamily(t, families, "solar_last_poll_success_timestamp_seconds")
	if got := len(lastPollSuccess.GetMetric()); got != 1 {
		t.Fatalf("last_poll_success metric count = %d, want 1", got)
	}
	lastPollSuccessLabels := metricLabels(lastPollSuccess.GetMetric()[0])
	if lastPollSuccessLabels["source"] != "jinko" {
		t.Fatalf("last_poll_success source label = %q, want jinko", lastPollSuccessLabels["source"])
	}
	if got := lastPollSuccess.GetMetric()[0].GetGauge().GetValue(); got <= 0 {
		t.Fatalf("last_poll_success value = %v, want positive timestamp", got)
	}
}

func TestCollectorDropSourceLabelDropsPollSuccessSourceLabel(t *testing.T) {
	state := poller.NewState("jinko")
	snapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "ABC123",
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics:     []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 123}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := poller.NewRunner(staticSource{snapshot: snapshot}, time.Hour, state, nil, config.AlertConfig{})
	go runner.Run(ctx)
	waitForSnapshot(t, state)

	families := gatherCollector(t, NewCollector("solar", state, true))

	pollSuccess := metricFamily(t, families, "solar_poll_success")
	if got := metricLabelNames(pollSuccess.GetMetric()[0]); len(got) != 0 {
		t.Fatalf("poll_success labels = %v, want none when source label is dropped", got)
	}

	pollsTotal := metricFamily(t, families, "solar_polls_total")
	if got := metricLabelNames(pollsTotal.GetMetric()[0]); len(got) != 1 || got[0] != "result" {
		t.Fatalf("polls_total labels = %v, want only result when source label is dropped", got)
	}

	lastPollSuccess := metricFamily(t, families, "solar_last_poll_success_timestamp_seconds")
	if got := metricLabelNames(lastPollSuccess.GetMetric()[0]); len(got) != 0 {
		t.Fatalf("last_poll_success labels = %v, want none when source label is dropped", got)
	}
}

type staticSource struct {
	snapshot *model.Snapshot
}

func (staticSource) Name() string {
	return "jinko"
}

func (s staticSource) Fetch(context.Context) (*model.Snapshot, error) {
	return s.snapshot, nil
}

func waitForSnapshot(t *testing.T, state *poller.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state.HasSnapshot() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("state did not receive snapshot")
}

func gatherCollector(t *testing.T, collector prometheus.Collector) []*dto.MetricFamily {
	t.Helper()
	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		t.Fatalf("Register collector error = %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}
	return families
}

func metricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

func metricLabels(metric *dto.Metric) map[string]string {
	labels := make(map[string]string)
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}

func metricLabelNames(metric *dto.Metric) []string {
	names := make([]string, 0, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		names = append(names, label.GetName())
	}
	sort.Strings(names)
	return names
}

func counterValueForLabels(t *testing.T, family *dto.MetricFamily, labels map[string]string) float64 {
	t.Helper()
	for _, metric := range family.GetMetric() {
		labelsByName := metricLabels(metric)
		matches := true
		for name, want := range labels {
			if labelsByName[name] != want {
				matches = false
				break
			}
		}
		if matches {
			return metric.GetCounter().GetValue()
		}
	}
	t.Fatalf("metric family %q has no metric with labels %#v", family.GetName(), labels)
	return 0
}
