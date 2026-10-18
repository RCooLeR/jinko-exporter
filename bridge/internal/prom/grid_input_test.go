package prom

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

func inputSnapshot(at time.Time) *model.Snapshot {
	return &model.Snapshot{Source: "shelly_grid_input", CollectedAt: at, Metrics: []model.Metric{{Group: "grid_input", Key: "total_power", Name: "Grid Input Total Power", Unit: "W", Value: 500}}}
}

func TestThreePrometheusStreamsKeepIndependentFreshness(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		for _, failed := range []string{"inverter", "grid_load", "grid_input"} {
			t.Run(failed, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					at := time.Now()
					inverter, load, input := poller.NewState("modbus"), poller.NewState("shelly_grid_load"), poller.NewState("shelly_grid_input")
					for _, stream := range []struct {
						name     string
						state    *poller.State
						snapshot func(time.Time) *model.Snapshot
					}{
						{"inverter", inverter, inverterSnapshot},
						{"grid_load", load, func(at time.Time) *model.Snapshot { return gridLoadSnapshot(at, 400) }},
						{"grid_input", input, inputSnapshot},
					} {
						var src source.Source = gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) { return stream.snapshot(time.Now()), nil }}
						if stream.name == failed {
							src = &freshnessSource{snapshot: stream.snapshot(at)}
						}
						go poller.NewRunner(src, time.Minute, stream.state, nil, config.AlertConfig{}).Run(ctx)
					}
					synctest.Wait()
					time.Sleep(2 * time.Minute)
					synctest.Wait()
					families := gatherCollector(t, NewCollector("solar", inverter, dropSource, WithGridLoad(load, "INV"), WithGridInput(input, "INV")))
					for _, group := range []string{"inverter", "grid_load", "grid_input"} {
						prefix := "solar_"
						if group != "inverter" {
							prefix += group + "_"
						}
						up, age, timestamp := 1.0, 0.0, float64(time.Now().Unix())
						if group == failed {
							up, age, timestamp = 0, 120, float64(at.Unix())
						}
						assertGridGauge(t, families, prefix+"up", up)
						assertGridGauge(t, families, prefix+"data_age_seconds", age)
						assertGridGauge(t, families, prefix+"last_update_timestamp_seconds", timestamp)
						assertGridGauge(t, families, prefix+"last_poll_success_timestamp_seconds", timestamp)
					}
					assertGridMetric(t, families, "grid_input", "total_power", 500)
					assertGridMetric(t, families, "grid_load", "total_power", 400)
				})
			})
		}
	}
}

func TestGridInputPrometheusColdStartAndOwnership(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		inverter, input := poller.NewState("modbus"), poller.NewState("shelly_grid_input")
		go poller.NewRunner(gridTestSource{fetch: func(context.Context) (*model.Snapshot, error) {
			s := inputSnapshot(time.Now())
			s.Metrics = append(s.Metrics, model.Metric{Group: "electric", Key: "DP1", Value: 999}, model.Metric{Group: "grid_load", Key: "total_power", Value: 999})
			return s, nil
		}}, time.Minute, input, nil, config.AlertConfig{}).Run(ctx)
		synctest.Wait()
		families := gatherCollector(t, NewCollector("solar", inverter, false, WithGridInput(input, "INV")))
		assertGridGauge(t, families, "solar_up", 0)
		assertGridGauge(t, families, "solar_grid_input_up", 1)
		assertGridFamilyAbsent(t, families, "solar_grid_load_up")
		assertGridFamilyAbsent(t, families, "solar_last_update_timestamp_seconds")
		metric := assertGridMetric(t, families, "grid_input", "total_power", 500)
		if metricLabels(metric)["device_sn"] != "INV" {
			t.Fatal("upstream meter replaced inverter identity")
		}
		if got := len(metricFamily(t, families, "solar_metric").GetMetric()); got != 1 {
			t.Fatalf("upstream accepted another stream's metrics: %d", got)
		}
	})
	families := gatherCollector(t, NewCollector("solar", poller.NewState("modbus"), true))
	assertGridFamilyAbsent(t, families, "solar_grid_input_up")
}
