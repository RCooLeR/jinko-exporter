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
}

func TestCollectorDropSourceLabelDropsPollSuccessSourceLabel(t *testing.T) {
	state := poller.NewState("jinko")
	families := gatherCollector(t, NewCollector("solar", state, true))

	pollSuccess := metricFamily(t, families, "solar_poll_success")
	if got := metricLabelNames(pollSuccess.GetMetric()[0]); len(got) != 0 {
		t.Fatalf("poll_success labels = %v, want none when source label is dropped", got)
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
