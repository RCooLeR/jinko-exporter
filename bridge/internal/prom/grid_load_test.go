package prom

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCollectorIndependentGridLoadSurvivesInverterFailure(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "source_label", true: "source_label_dropped"}[dropSource], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				collectedAt := time.Now().Add(-10 * time.Minute)
				inverter := poller.NewState("modbus")
				gridLoad := poller.NewState("shelly_grid_load")
				go poller.NewRunner(&freshnessSource{snapshot: inverterSnapshot(collectedAt)}, time.Minute, inverter, nil, config.AlertConfig{}).Run(ctx)
				go poller.NewRunner(liveGridLoadSource(), time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
				synctest.Wait()

				collector := NewCollector("solar", inverter, dropSource, WithGridLoad(gridLoad, "CONFIGURED_INV_001"))
				families := gatherCollector(t, collector)
				assertGridGauge(t, families, "solar_up", 1)
				assertGridGauge(t, families, "solar_grid_load_up", 1)
				lastPollSuccess := metricFamily(t, families, "solar_last_poll_success_timestamp_seconds").GetMetric()[0].GetGauge().GetValue()

				time.Sleep(2 * time.Minute)
				synctest.Wait()
				families = gatherCollector(t, collector)
				for name, want := range map[string]float64{
					"solar_up": 0, "solar_poll_success": 0, "solar_grid_load_up": 1,
					"solar_data_age_seconds": 720, "solar_grid_load_data_age_seconds": 0,
					"solar_last_update_timestamp_seconds":                 float64(collectedAt.Unix()),
					"solar_last_poll_success_timestamp_seconds":           lastPollSuccess,
					"solar_grid_load_last_update_timestamp_seconds":       float64(time.Now().Unix()),
					"solar_grid_load_last_poll_success_timestamp_seconds": float64(time.Now().Unix()),
				} {
					assertGridGauge(t, families, name, want)
				}
				if got := counterValueForLabels(t, metricFamily(t, families, "solar_polls_total"), map[string]string{"result": "success"}); got != 1 {
					t.Fatalf("inverter successful polls = %v, want 1", got)
				}
				assertGridMetric(t, families, "electric", "DP1", 2230)
				gridMetric := assertGridMetric(t, families, "grid_load", "total_power", 300)
				wantLabels := map[string]string{
					"device_sn": "LEARNED_INV_001", "group": "grid_load", "key": "total_power", "name": "Grid Load Total Power", "unit": "W",
				}
				if !dropSource {
					wantLabels["source"] = "solarman"
				}
				if got := metricLabels(gridMetric); !reflect.DeepEqual(got, wantLabels) {
					t.Fatalf("grid-load labels = %#v, want %#v", got, wantLabels)
				}
				assertGridHealthLabels(t, families)
			})
		})
	}
}

func TestCollectorIndependentGridLoadFailureDoesNotAffectInverter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		collectedAt := time.Now()
		inverter := poller.NewState("modbus")
		gridLoad := poller.NewState("shelly_grid_load")
		go poller.NewRunner(gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) {
			return inverterSnapshot(time.Now()), nil
		}}, time.Minute, inverter, nil, config.AlertConfig{}).Run(ctx)
		go poller.NewRunner(&freshnessSource{snapshot: gridLoadSnapshot(collectedAt, 100)}, time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
		synctest.Wait()
		time.Sleep(2 * time.Minute)
		synctest.Wait()

		families := gatherCollector(t, NewCollector("solar", inverter, true, WithGridLoad(gridLoad, "CONFIGURED_INV_001")))
		for name, want := range map[string]float64{
			"solar_up": 1, "solar_poll_success": 1, "solar_grid_load_up": 0,
			"solar_data_age_seconds": 0, "solar_grid_load_data_age_seconds": 120,
			"solar_last_update_timestamp_seconds":                 float64(time.Now().Unix()),
			"solar_grid_load_last_update_timestamp_seconds":       float64(collectedAt.Unix()),
			"solar_grid_load_last_poll_success_timestamp_seconds": float64(collectedAt.Unix()),
		} {
			assertGridGauge(t, families, name, want)
		}
		assertGridMetric(t, families, "electric", "DP1", 2230)
		assertGridMetric(t, families, "grid_load", "total_power", 100)
	})
}

func TestCollectorIndependentGridLoadColdStartWithBlockedInverter(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "source_label", true: "source_label_dropped"}[dropSource], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				inverter := poller.NewState("modbus")
				gridLoad := poller.NewState("shelly_grid_load")
				go poller.NewRunner(gridTestSource{fetch: func(ctx context.Context) (*model.Snapshot, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				}}, time.Minute, inverter, nil, config.AlertConfig{}).Run(ctx)
				go poller.NewRunner(liveGridLoadSource(), time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
				synctest.Wait()
				time.Sleep(2 * time.Minute)
				synctest.Wait()

				families := gatherCollector(t, NewCollector("solar", inverter, dropSource, WithGridLoad(gridLoad, "CONFIGURED_INV_001")))
				assertGridGauge(t, families, "solar_up", 0)
				assertGridGauge(t, families, "solar_grid_load_up", 1)
				gridMetric := assertGridMetric(t, families, "grid_load", "total_power", 300)
				labels := metricLabels(gridMetric)
				if labels["device_sn"] != "CONFIGURED_INV_001" {
					t.Fatalf("cold-start device serial = %q", labels["device_sn"])
				}
				wantSource := "modbus"
				if dropSource {
					wantSource = ""
				}
				if labels["source"] != wantSource {
					t.Fatalf("cold-start source = %q, want %q", labels["source"], wantSource)
				}
				for _, name := range []string{"solar_data_age_seconds", "solar_last_update_timestamp_seconds", "solar_last_poll_success_timestamp_seconds"} {
					assertGridFamilyAbsent(t, families, name)
				}
				assertGridHealthLabels(t, families)
			})
		})
	}
}

func TestCollectorIndependentGridLoadUnknownSnapshotAndTimestamp(t *testing.T) {
	for _, scenario := range []string{"disabled", "no_snapshot", "unknown_time", "future_time"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var gridLoad *poller.State
				if scenario != "disabled" {
					gridLoad = poller.NewState("shelly_grid_load")
				}
				if scenario == "unknown_time" || scenario == "future_time" {
					collectedAt := time.Time{}
					if scenario == "future_time" {
						collectedAt = time.Now().Add(time.Second)
					}
					go poller.NewRunner(staticSource{snapshot: gridLoadSnapshot(collectedAt, 100)}, time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
					synctest.Wait()
				}
				families := gatherCollector(t, NewCollector("solar", poller.NewState("jinko"), false, WithGridLoad(gridLoad, "")))
				if scenario == "disabled" {
					assertGridFamilyAbsent(t, families, "solar_grid_load_up")
					return
				}
				if scenario == "no_snapshot" {
					assertGridGauge(t, families, "solar_grid_load_up", 0)
					assertGridFamilyAbsent(t, families, "solar_metric")
				}
				if scenario == "future_time" {
					assertGridGauge(t, families, "solar_grid_load_data_age_seconds", 0)
				} else {
					assertGridFamilyAbsent(t, families, "solar_grid_load_data_age_seconds")
					assertGridFamilyAbsent(t, families, "solar_grid_load_last_update_timestamp_seconds")
				}
			})
		})
	}
}

func TestCollectorIndependentGridLoadOwnsOnlyGridLoadValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		inverter := poller.NewState("modbus")
		gridLoad := poller.NewState("shelly_grid_load")
		primarySnapshot := inverterSnapshot(time.Now())
		primarySnapshot.Metrics = append(primarySnapshot.Metrics, gridLoadSnapshot(time.Now(), 999).Metrics...)
		gridSnapshot := gridLoadSnapshot(time.Now(), 100)
		gridSnapshot.Metrics = append(gridSnapshot.Metrics,
			gridLoadSnapshot(time.Now(), 200).Metrics[0],
			model.Metric{Group: "grid_load", Key: "nan", Value: math.NaN()},
			model.Metric{Group: "electric", Key: "INJECTED", Value: 999},
		)
		go poller.NewRunner(staticSource{snapshot: primarySnapshot}, time.Minute, inverter, nil, config.AlertConfig{}).Run(ctx)
		go poller.NewRunner(staticSource{snapshot: gridSnapshot}, time.Minute, gridLoad, nil, config.AlertConfig{}).Run(ctx)
		synctest.Wait()
		families := gatherCollector(t, NewCollector("solar", inverter, false, WithGridLoad(gridLoad, "CONFIGURED_INV_001")))
		assertGridMetric(t, families, "grid_load", "total_power", 100)
		assertGridMetric(t, families, "electric", "DP1", 2230)
		if got := len(metricFamily(t, families, "solar_metric").GetMetric()); got != 2 {
			t.Fatalf("numeric metric count = %d, want only 2 authoritative finite values", got)
		}
	})
}

func TestCollectorIndependentGridLoadConcurrentScrapes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inverter := poller.NewState("modbus")
	gridLoad := poller.NewState("shelly_grid_load")
	go poller.NewRunner(gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) {
		return inverterSnapshot(time.Now()), nil
	}}, time.Millisecond, inverter, nil, config.AlertConfig{}).Run(ctx)
	go poller.NewRunner(liveGridLoadSource(), time.Millisecond, gridLoad, nil, config.AlertConfig{}).Run(ctx)
	waitForSnapshot(t, inverter)
	waitForSnapshot(t, gridLoad)
	registry := prometheus.NewRegistry()
	if err := registry.Register(NewCollector("solar", inverter, false, WithGridLoad(gridLoad, "CONFIGURED_INV_001"))); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	errorsSeen := make(chan error, 4)
	for range 4 {
		workers.Go(func() {
			for range 50 {
				if _, err := registry.Gather(); err != nil {
					errorsSeen <- err
					return
				}
			}
		})
	}
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent scrape failed: %v", err)
	}
}

type gridTestSource struct {
	fetch func(context.Context) (*model.Snapshot, error)
}

func (gridTestSource) Name() string { return "test" }

func (s gridTestSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	if s.fetch == nil {
		return nil, errors.New("test source has no fetch implementation")
	}
	return s.fetch(ctx)
}

func liveGridLoadSource() gridTestSource {
	count := 0
	return gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) {
		count++
		return gridLoadSnapshot(time.Now(), float64(count*100)), nil
	}}
}

func inverterSnapshot(collectedAt time.Time) *model.Snapshot {
	return &model.Snapshot{
		Source: "solarman", DeviceSN: "LEARNED_INV_001", CollectedAt: collectedAt,
		Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 2230}},
	}
}

func gridLoadSnapshot(collectedAt time.Time, value float64) *model.Snapshot {
	return &model.Snapshot{
		Source: "shelly_grid_load", DeviceSN: "shelly.test", CollectedAt: collectedAt,
		Metrics: []model.Metric{{Group: "grid_load", Key: "total_power", Name: "Grid Load Total Power", Unit: "W", Value: value}},
	}
}

func assertGridGauge(t *testing.T, families []*dto.MetricFamily, name string, want float64) {
	t.Helper()
	if got := metricFamily(t, families, name).GetMetric()[0].GetGauge().GetValue(); got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func assertGridMetric(t *testing.T, families []*dto.MetricFamily, group, key string, want float64) *dto.Metric {
	t.Helper()
	for _, metric := range metricFamily(t, families, "solar_metric").GetMetric() {
		labels := metricLabels(metric)
		if labels["group"] == group && labels["key"] == key {
			if got := metric.GetGauge().GetValue(); got != want {
				t.Errorf("%s/%s = %v, want %v", group, key, got, want)
			}
			return metric
		}
	}
	t.Fatalf("no value for %s/%s", group, key)
	return nil
}

func assertGridFamilyAbsent(t *testing.T, families []*dto.MetricFamily, name string) {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			t.Errorf("unexpected metric family %s", name)
		}
	}
}

func assertGridHealthLabels(t *testing.T, families []*dto.MetricFamily) {
	t.Helper()
	wantLabels := metricLabels(metricFamily(t, families, "solar_up").GetMetric()[0])
	for _, name := range []string{"solar_grid_load_up", "solar_grid_load_data_age_seconds", "solar_grid_load_last_update_timestamp_seconds", "solar_grid_load_last_poll_success_timestamp_seconds"} {
		gotLabels := metricLabels(metricFamily(t, families, name).GetMetric()[0])
		if !reflect.DeepEqual(gotLabels, wantLabels) {
			t.Errorf("%s labels = %#v, want %#v", name, gotLabels, wantLabels)
		}
	}
}
