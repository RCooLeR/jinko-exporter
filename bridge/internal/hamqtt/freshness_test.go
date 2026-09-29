package hamqtt

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
)

func TestPollerRejectedCloudDataPublishesOfflineWithoutNewState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		publisher, client := newPublisherWithRecordingClient(t)
		defer publisher.Close()
		snapshot := &model.Snapshot{
			Source: "solarman", DeviceSN: "ABC123", CollectedAt: time.Now().Add(-5 * time.Minute),
			Metrics: []model.Metric{{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 2230}},
		}
		state := poller.NewState("solarman")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		runner := poller.NewRunner(&offlineCloudSource{snapshot: snapshot}, time.Minute, state, nil, config.AlertConfig{}, publisher)
		go runner.Run(ctx)
		synctest.Wait()
		stateTopic := "jinko-exporter/abc123/state"
		original := client.payloadsForTopic(stateTopic)
		if len(original) != 1 || !client.hasTopic(publisher.availabilityTopic, true, availabilityOnline) {
			t.Fatal("initial accepted poll did not publish one state and online availability")
		}
		client.clear()

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if !client.hasTopic(publisher.availabilityTopic, true, availabilityOffline) {
			t.Fatal("rejected cloud data did not publish retained offline availability")
		}
		if got := client.payloadsForTopic(stateTopic); len(got) != 0 {
			t.Fatalf("failed polls republished state payloads: %v", got)
		}
		for _, payload := range client.payloadsForTopic(publisher.availabilityTopic) {
			if payload != availabilityOffline {
				t.Fatalf("availability after invalid data = %q, want offline", payload)
			}
		}
		if string(publisher.cachedState) != original[0] {
			t.Fatal("failed polls changed the cached payload or its collection/publication timestamps")
		}
	})
}

type offlineCloudSource struct {
	snapshot *model.Snapshot
	called   bool
}

func (*offlineCloudSource) Name() string { return "solarman" }

func (s *offlineCloudSource) Fetch(context.Context) (*model.Snapshot, error) {
	if s.called {
		return nil, errors.New("solarman currentData rejected: stale/offline")
	}
	s.called = true
	return s.snapshot, nil
}
