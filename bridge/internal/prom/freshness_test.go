package prom

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
)

func TestCollectorDataAgeTracksUpstreamTimeAcrossPollFailure(t *testing.T) {
	for _, dropSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "source_label", true: "source_label_dropped"}[dropSource], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collectedAt := time.Now().Add(-10 * time.Minute)
				snapshot := &model.Snapshot{
					Source: "solarman", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: collectedAt,
					Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 2230}},
				}
				state := poller.NewState("solarman")
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				runner := poller.NewRunner(&freshnessSource{snapshot: snapshot}, time.Minute, state, nil, config.AlertConfig{})
				go runner.Run(ctx)
				synctest.Wait()

				collector := NewCollector("custom", state, dropSource)
				families := gatherCollector(t, collector)
				ageMetric := metricFamily(t, families, "custom_data_age_seconds").GetMetric()[0]
				if got := ageMetric.GetGauge().GetValue(); got != 600 {
					t.Fatalf("initial data age = %v, want 600 seconds from upstream collection", got)
				}
				updateMetric := metricFamily(t, families, "custom_last_update_timestamp_seconds").GetMetric()[0]
				if !reflect.DeepEqual(metricLabels(ageMetric), metricLabels(updateMetric)) {
					t.Fatalf("data-age labels %v differ from last-update labels %v", metricLabels(ageMetric), metricLabels(updateMetric))
				}
				lastPollSuccess := metricFamily(t, families, "custom_last_poll_success_timestamp_seconds").GetMetric()[0].GetGauge().GetValue()

				time.Sleep(2 * time.Minute)
				synctest.Wait()
				families = gatherCollector(t, collector)
				for name, want := range map[string]float64{
					"custom_data_age_seconds":                    720,
					"custom_up":                                  0,
					"custom_poll_success":                        0,
					"custom_last_update_timestamp_seconds":       float64(collectedAt.Unix()),
					"custom_last_poll_success_timestamp_seconds": lastPollSuccess,
					"custom_metric":                              2230,
				} {
					if got := metricFamily(t, families, name).GetMetric()[0].GetGauge().GetValue(); got != want {
						t.Errorf("%s = %v, want %v", name, got, want)
					}
				}
				if got := counterValueForLabels(t, metricFamily(t, families, "custom_polls_total"), map[string]string{"result": "success"}); got != 1 {
					t.Fatalf("successful polls after failures = %v, want 1", got)
				}
			})
		})
	}
}

func TestCollectorDataAgeOmitsUnknownAndClampsFutureSkew(t *testing.T) {
	for _, name := range []string{"no_snapshot", "unknown_collection_time", "small_future_skew"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := poller.NewState("solarman")
				if name != "no_snapshot" {
					collectedAt := time.Time{}
					if name == "small_future_skew" {
						collectedAt = time.Now().Add(30 * time.Second)
					}
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					runner := poller.NewRunner(staticSource{snapshot: &model.Snapshot{
						Source: "solarman", DeviceSN: "SYNTHETIC_INV_001", CollectedAt: collectedAt,
					}}, time.Hour, state, nil, config.AlertConfig{})
					go runner.Run(ctx)
					synctest.Wait()
				}
				families := gatherCollector(t, NewCollector("solar", state, false))
				if name == "small_future_skew" {
					if got := metricFamily(t, families, "solar_data_age_seconds").GetMetric()[0].GetGauge().GetValue(); got != 0 {
						t.Fatalf("data age with future skew = %v, want zero", got)
					}
					return
				}
				for _, family := range families {
					if family.GetName() == "solar_data_age_seconds" {
						t.Fatal("unknown data age was exported")
					}
				}
			})
		})
	}
}

type freshnessSource struct {
	snapshot *model.Snapshot
	called   bool
}

func (*freshnessSource) Name() string { return "solarman" }

func (s *freshnessSource) Fetch(context.Context) (*model.Snapshot, error) {
	if s.called {
		return nil, errors.New("upstream data is stale")
	}
	s.called = true
	return s.snapshot, nil
}
