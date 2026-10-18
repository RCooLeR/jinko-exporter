package hamqtt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
)

func gridInputSnapshotAt(at time.Time, value float64) *model.Snapshot {
	return &model.Snapshot{Source: "shelly_grid_input", CollectedAt: at,
		Metrics: []model.Metric{{Group: "grid_input", Key: "total_power", Name: "Grid Input Total Power", Unit: "W", Value: value},
			{Group: "grid_input", Key: "internal_temperature", Name: "Shelly Grid Input Internal Temperature", Unit: "°C", Value: 0}},
		Meta: map[string]string{"shelly_grid_input_url": "http://upstream.test", "shelly_grid_input_em_id": "0"}}
}

func TestThreeIndependentMQTTStreamsFailureMatrix(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		for mask := range 8 {
			t.Run(fmt.Sprintf("persistent=%t/mask=%d", persistent, mask), func(t *testing.T) {
				path := ""
				if persistent {
					path = filepath.Join(t.TempDir(), "discovery.json")
				}
				p, client := newPersistentPublisher(t, path)
				defer p.Close()
				observers := []poller.Observer{p, p.GridLoadObserver(), p.GridInputObserver()}
				at := time.Now().UTC().Truncate(time.Second)
				inverter := discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Name: "PV1", Unit: "W", Value: 800})
				inverter.CollectedAt = at
				snapshots := []*model.Snapshot{inverter, gridLoadSnapshotAt(at.Add(time.Minute), 400), gridInputSnapshotAt(at.Add(2*time.Minute), 500)}
				for i, observer := range observers {
					if err := observer.OnPollSuccess(snapshots[i], time.Second); err != nil {
						t.Fatal(err)
					}
				}
				before := publishedState(t, client, independentStateTopic)
				discovery := cloneDiscoveryMessages(p.cachedDiscovery)
				for i, observer := range observers {
					if mask&(1<<i) == 0 {
						if err := observer.OnPollFailure(snapshots[i].Source, errors.New("offline"), time.Second, 1); err != nil {
							t.Fatal(err)
						}
					}
				}
				state := publishedState(t, client, independentStateTopic)
				if state.GridLoadUp == nil || state.GridInputUp == nil {
					t.Fatal("missing independent health")
				}
				for i, up := range []bool{state.Up, *state.GridLoadUp, *state.GridInputUp} {
					want := mask&(1<<i) != 0
					if up != want {
						t.Fatalf("stream %d up=%t want=%t", i, up, want)
					}
					key := []string{"electric_dp1", "grid_load_total_power", "grid_input_total_power"}[i]
					if (state.Metrics[key] != nil) != want {
						t.Fatalf("stream %d leaked stale value or lost healthy value", i)
					}
				}
				if state.CollectedAt != before.CollectedAt || state.PublishedAt != before.PublishedAt || state.GridLoadCollectedAt != before.GridLoadCollectedAt || state.GridLoadPublishedAt != before.GridLoadPublishedAt || state.GridInputCollectedAt != before.GridInputCollectedAt || state.GridInputPublishedAt != before.GridInputPublishedAt {
					t.Fatal("failure altered independent success timestamps")
				}
				wantAvailability := availabilityOnline
				if mask == 0 {
					wantAvailability = availabilityOffline
				}
				if p.lastAvailability != wantAvailability {
					t.Fatal("shared transport availability ignored a stream")
				}
				for _, group := range []string{"grid_load", "grid_input"} {
					metric := decodeDiscovery(t, discovery, "homeassistant/sensor/stable_inverter_"+group+"_total_power/config")
					assertStreamAvailability(t, metric, group+"_up", group+"_total_power")
					meta := decodeDiscovery(t, discovery, "homeassistant/sensor/stable_inverter_meta_shelly_"+group+"_url/config")
					assertStreamAvailability(t, meta, group+"_up", "shelly_"+group+"_url")
				}
				cached := string(p.cachedState)
				client.clear()
				p.onConnect(client)
				if values := client.payloadsForTopic(independentStateTopic); len(values) != 1 || values[0] != cached {
					t.Fatal("reconnect changed stream values/timestamps")
				}
				if err := observers[2].OnPollSuccess(gridInputSnapshotAt(at.Add(3*time.Minute), 0), time.Second); err != nil {
					t.Fatal(err)
				}
				after := publishedState(t, client, independentStateTopic)
				if after.GridInputUp == nil || !*after.GridInputUp || after.Metrics["grid_input_total_power"] == nil || *after.Metrics["grid_input_total_power"] != 0 || after.Up != state.Up || *after.GridLoadUp != *state.GridLoadUp || after.GridLoadCollectedAt != state.GridLoadCollectedAt || after.CollectedAt != state.CollectedAt {
					t.Fatal("input recovery disturbed another stream or discarded zero")
				}
			})
		}
	}
}

func TestGridInputColdStartAndLegacyManifestMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovery.json")
	old, _ := newPersistentPublisher(t, path)
	if err := old.GridLoadObserver().OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
		t.Fatal(err)
	}
	old.Close()
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte("grid_input_metrics")) {
		t.Fatal("disabled upstream changed legacy manifest shape", err)
	}
	p, client := newPersistentPublisher(t, path)
	defer p.Close()
	observer := p.GridInputObserver()
	at := time.Now().UTC().Truncate(time.Second)
	if err := observer.OnPollSuccess(gridInputSnapshotAt(at, 500), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if state.Up || state.GridLoadUp != nil || state.GridInputUp == nil || !*state.GridInputUp || state.CollectedAt != "" || state.PublishedAt != "" || state.GridInputCollectedAt != at.Format(time.RFC3339) || state.GridInputPublishedAt == "" {
		t.Fatal("upstream cold start invented another stream's success")
	}
	if state.Metrics["grid_load_total_power"] != nil || state.Metrics["grid_input_internal_temperature"] == nil || *state.Metrics["grid_input_internal_temperature"] != 0 {
		t.Fatal("restart leaked stale downstream or lost zero temperature")
	}
	temperature := decodeDiscovery(t, p.cachedDiscovery, "homeassistant/sensor/stable_inverter_grid_input_internal_temperature/config")
	assertStreamAvailability(t, temperature, "grid_input_up", "grid_input_internal_temperature")
	if temperature["device_class"] != "temperature" || temperature["entity_category"] != "diagnostic" {
		t.Fatal("upstream temperature metadata lost")
	}
	stored := readDiscoveryStateForTest(t, path, p.discoveryState.Binding)
	if len(stored.GridLoadMetrics) != 1 || len(stored.GridInputMetrics) != 2 || len(stored.OrdinaryMetrics) != 0 {
		t.Fatal("independent durable ownership mixed")
	}
	p.Close()
	restarted, next := newPersistentPublisher(t, path)
	defer restarted.Close()
	input := restarted.GridInputObserver()
	if err := input.OnPollFailure("shelly_grid_input", errors.New("offline"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, next, independentStateTopic)
	if state.GridInputUp == nil || *state.GridInputUp || state.Metrics["grid_input_total_power"] != nil {
		t.Fatal("restart invented retained input success")
	}
}

func TestGridInputRejectsCrossStreamSnapshotsAndPublicationFailure(t *testing.T) {
	p, client := newPersistentPublisher(t, "")
	defer p.Close()
	load, input := p.GridLoadObserver(), p.GridInputObserver()
	if err := load.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := input.OnPollSuccess(gridInputSnapshotAt(time.Now(), 500), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := input.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 900), time.Second); err == nil {
		t.Fatal("input accepted downstream metrics")
	}
	state := publishedState(t, client, independentStateTopic)
	if *state.GridInputUp || !*state.GridLoadUp || state.Metrics["grid_input_total_power"] != nil || derefFloat(state.Metrics["grid_load_total_power"]) != 400 {
		t.Fatal("cross-stream rejection was not isolated")
	}
	client.failNextPublishes(errors.New("state rejected"))
	if err := input.OnPollSuccess(gridInputSnapshotAt(time.Now(), 600), time.Second); err == nil {
		t.Fatal("publish failure silently accepted")
	}
	if p.lastAvailability != availabilityOffline {
		t.Fatal("publish failure retained online availability")
	}
	p.onConnect(client)
	if p.lastAvailability != availabilityOffline {
		t.Fatal("reconnect revived failed publication")
	}
}

func TestPersistedGridInputStaysUnavailableWhenDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovery.json")
	old, _ := newPersistentPublisher(t, path)
	if err := old.GridInputObserver().OnPollSuccess(gridInputSnapshotAt(time.Now(), 500), time.Second); err != nil {
		t.Fatal(err)
	}
	old.Close()
	// Neither meter observer is installed in this restart, matching disabled
	// configuration. The remembered upstream schema must not use inverter up.
	p, client := newPersistentPublisher(t, path)
	defer p.Close()
	if err := p.OnPollSuccess(discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Value: 800}), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if !state.Up || state.GridInputUp != nil || state.GridInputCollectedAt != "" || state.GridInputPublishedAt != "" || state.Metrics["grid_input_total_power"] != nil {
		t.Fatal("disabled upstream acquired live data or health")
	}
	for _, suffix := range []string{"grid_input_total_power", "meta_shelly_grid_input_url"} {
		payload := decodeDiscovery(t, p.cachedDiscovery, "homeassistant/sensor/stable_inverter_"+suffix+"/config")
		assertStreamAvailability(t, payload, "grid_input_up", strings.TrimPrefix(suffix, "meta_"))
	}
}

func TestGridInputManifestRejectsNullAndCrossOwnedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovery.json")
	p, _ := newPersistentPublisher(t, path)
	defer p.Close()
	if err := p.GridInputObserver().OnPollSuccess(gridInputSnapshotAt(time.Now(), 500), time.Second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document["grid_input_metrics"] = json.RawMessage(`null`)
	raw, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(t.TempDir(), "null-input.json")
	if err := os.WriteFile(invalidPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadDiscoveryState(invalidPath, p.discoveryState.Binding); err == nil {
		t.Fatal("explicit null was treated as an omitted legacy extension")
	}
	for _, category := range []string{"ordinary", "grid_load"} {
		state := p.discoveryState.clone()
		key := "grid_input_total_power"
		if category == "ordinary" {
			state.OrdinaryMetrics[key] = state.GridInputMetrics[key]
		} else {
			state.GridLoadMetrics[key] = state.GridInputMetrics[key]
		}
		delete(state.GridInputMetrics, key)
		if err := validateDiscoveryState(state, state.Binding); err == nil {
			t.Fatalf("upstream schema accepted in %s ownership", category)
		}
	}
}

func TestThreeIndependentMQTTStreamsConcurrentUpdates(t *testing.T) {
	p, client := newPersistentPublisher(t, "")
	defer p.Close()
	observers := []poller.Observer{p, p.GridLoadObserver(), p.GridInputObserver()}
	snapshots := []*model.Snapshot{discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Value: 800}), gridLoadSnapshotAt(time.Now(), 400), gridInputSnapshotAt(time.Now(), 500)}
	var workers sync.WaitGroup
	errors := make(chan error, len(observers))
	for i, observer := range observers {
		workers.Go(func() {
			for range 10 {
				if err := observer.OnPollSuccess(snapshots[i], time.Second); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if !state.Up || !*state.GridLoadUp || !*state.GridInputUp || derefFloat(state.Metrics["grid_input_total_power"]) != 500 || derefFloat(state.Metrics["grid_load_total_power"]) != 400 {
		t.Fatal("concurrent streams overwrote each other")
	}
}
