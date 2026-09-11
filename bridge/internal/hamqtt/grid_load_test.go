package hamqtt

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

const independentStateTopic = "jinko-exporter/stable_inverter/state"

func gridLoadSnapshotAt(at time.Time, value float64) *model.Snapshot {
	return &model.Snapshot{
		Source: "shelly_grid_load", CollectedAt: at,
		Metrics: []model.Metric{{Group: "grid_load", Key: "total_power", Name: "Grid Load Total Power", Unit: "W", Value: value}},
		Meta:    map[string]string{"shelly_grid_load_url": "http://shelly.test", "shelly_grid_load_em_id": "0"},
	}
}

func TestIndependentGridLoadColdStartHasNoInventedInverterSuccess(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		name := "memory"
		path := ""
		if persistent {
			name = "persistent"
			path = filepath.Join(t.TempDir(), "discovery.json")
		}
		t.Run(name, func(t *testing.T) {
			publisher, client := newPersistentPublisher(t, path)
			defer publisher.Close()
			observer := publisher.GridLoadObserver()
			at := time.Now().UTC().Truncate(time.Second)
			if err := observer.OnPollSuccess(gridLoadSnapshotAt(at, 900), time.Second); err != nil {
				t.Fatal(err)
			}
			state := publishedState(t, client, independentStateTopic)
			if state.Up || state.CollectedAt != "" || state.PublishedAt != "" || state.Source != "modbus" {
				t.Fatalf("cold Shelly poll invented inverter state: %+v", state)
			}
			if state.GridLoadUp == nil || !*state.GridLoadUp || state.GridLoadCollectedAt != at.Format(time.RFC3339) || state.GridLoadPublishedAt == "" {
				t.Fatalf("missing independent Shelly health/timestamps: %+v", state)
			}
			if got := derefFloat(state.Metrics["grid_load_total_power"]); got != 900 {
				t.Fatalf("Shelly power = %v, want 900", got)
			}
			if !client.hasTopic(publisher.availabilityTopic, true, availabilityOnline) {
				t.Fatal("fresh Shelly did not keep global transport availability online")
			}
			discovery := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_grid_load_total_power/config")
			if discovery["unique_id"] != "stable_inverter_grid_load_total_power" || discovery["state_topic"] != independentStateTopic {
				t.Fatalf("Shelly entity identity/topic changed: %+v", discovery)
			}
			assertStreamAvailability(t, discovery, "grid_load_up", "grid_load_total_power")
			meta := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_meta_shelly_grid_load_url/config")
			assertStreamAvailability(t, meta, "grid_load_up", "shelly_grid_load_url")
			if state.Meta["shelly_grid_load_url"] != "http://shelly.test" || state.Meta["shelly_grid_load_em_id"] != "0" {
				t.Fatalf("cold grid-only state lost Shelly metadata: %+v", state.Meta)
			}
			if persistent {
				stored := readDiscoveryStateForTest(t, path, publisher.discoveryState.Binding)
				if len(stored.GridLoadMetrics) != 1 || len(stored.OrdinaryMetrics) != 0 || len(stored.AlertMetrics) != 0 {
					t.Fatalf("cold Shelly created inverter schema: %+v", stored)
				}
			}
		})
	}
}

func TestIndependentGridLoadHealthAndTimestampsSurviveFailuresRecoveryAndReconnect(t *testing.T) {
	publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
	defer publisher.Close()
	observer := publisher.GridLoadObserver()
	inverter := discoverySnapshot("modbus",
		model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800},
		model.Metric{Group: "alert", Key: "fault", Name: "Fault", Value: 1},
	)
	if err := publisher.OnPollSuccess(inverter, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	initial := publishedState(t, client, independentStateTopic)
	shellyTime := inverter.CollectedAt.Add(time.Hour)
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(shellyTime, 600), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if !state.Up || state.GridLoadUp == nil || !*state.GridLoadUp || state.CollectedAt != initial.CollectedAt || state.PublishedAt != initial.PublishedAt || state.PollDurationSeconds != 2 {
		t.Fatalf("Shelly update changed inverter health/time/duration: %+v", state)
	}
	ordinary := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_electric_dp1/config")
	assertStreamAvailability(t, ordinary, "up", "electric_dp1")
	alert := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/binary_sensor/stable_inverter_alert_fault_active/config")
	assertStreamAvailability(t, alert, "up", "alert_fault")
	if err := publisher.OnPollFailure("modbus", errors.New("inverter off"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(shellyTime.Add(time.Minute), 700), time.Second); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, independentStateTopic)
	if state.Up || state.Metrics["electric_dp1"] != nil || state.Metrics["alert_fault"] != nil || state.AlertsKnown || state.CollectedAt != initial.CollectedAt || state.PublishedAt != initial.PublishedAt {
		t.Fatalf("inverter failure retained live inverter values or advanced time: %+v", state)
	}
	if state.GridLoadUp == nil || !*state.GridLoadUp || derefFloat(state.Metrics["grid_load_total_power"]) != 700 || publisher.lastAvailability != availabilityOnline {
		t.Fatalf("inverter failure hid fresh Shelly: %+v", state)
	}
	primaryRecovery := cloneStreamSnapshot(inverter)
	primaryRecovery.CollectedAt = shellyTime.Add(2 * time.Minute)
	primaryRecovery.Metrics[0].Value = 1000
	if err := publisher.OnPollSuccess(primaryRecovery, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := observer.OnPollFailure("shelly_grid_load", errors.New("meter off"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, independentStateTopic)
	if !state.Up || state.GridLoadUp == nil || *state.GridLoadUp || state.Metrics["grid_load_total_power"] != nil || derefFloat(state.Metrics["electric_dp1"]) != 1000 || publisher.lastAvailability != availabilityOnline {
		t.Fatalf("Shelly failure hid inverter or kept stale meter data live: %+v", state)
	}
	if err := publisher.OnPollFailure("modbus", errors.New("inverter off"), time.Second, 2); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, independentStateTopic)
	if state.Up || state.GridLoadUp == nil || *state.GridLoadUp || publisher.lastAvailability != availabilityOffline {
		t.Fatalf("both failures did not become offline: %+v", state)
	}
	cached := string(publisher.cachedState)
	client.clear()
	publisher.onConnect(client)
	states := client.payloadsForTopic(independentStateTopic)
	if len(states) != 1 || states[0] != cached {
		t.Fatalf("reconnect changed timestamps or stream statuses: %v", states)
	}
	if values := client.payloadsForTopic(publisher.availabilityTopic); len(values) != 1 || values[0] != availabilityOffline {
		t.Fatalf("reconnect revived unavailable streams: %v", values)
	}
}

func TestIndependentGridLoadRestartUpdatesExistingInverterDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovery.json")
	old, _ := newPersistentPublisher(t, path)
	inverter := discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800})
	if err := old.OnPollSuccess(inverter, time.Second); err != nil {
		t.Fatal(err)
	}
	old.Close()
	restarted, client := newPersistentPublisher(t, path)
	defer restarted.Close()
	observer := restarted.GridLoadObserver()
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if state.Up || state.CollectedAt != "" || state.Metrics["electric_dp1"] != nil {
		t.Fatalf("restart revived pre-restart inverter data: %+v", state)
	}
	ordinary := decodeDiscovery(t, restarted.cachedDiscovery, "homeassistant/sensor/stable_inverter_electric_dp1/config")
	assertStreamAvailability(t, ordinary, "up", "electric_dp1")
	if !client.hasTopic("homeassistant/sensor/stable_inverter_electric_dp1/config", true, "value_json.get('up', false)") {
		t.Fatal("existing retained inverter discovery was not migrated in-place")
	}
}

func TestIndependentGridLoadConcurrentUpdatesAndClose(t *testing.T) {
	publisher, client := newPersistentPublisher(t, "")
	observer := publisher.GridLoadObserver()
	inverter := discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800})
	var updates sync.WaitGroup
	updates.Go(func() {
		for range 20 {
			if err := publisher.OnPollSuccess(inverter, time.Second); err != nil {
				t.Error(err)
			}
		}
	})
	updates.Go(func() {
		for range 20 {
			if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
				t.Error(err)
			}
		}
	})
	updates.Wait()
	state := publishedState(t, client, independentStateTopic)
	if !state.Up || state.GridLoadUp == nil || !*state.GridLoadUp || derefFloat(state.Metrics["electric_dp1"]) != 800 || derefFloat(state.Metrics["grid_load_total_power"]) != 400 {
		t.Fatalf("concurrent streams lost data: %+v", state)
	}
	publisher.Close()
	client.clear()
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 500), time.Second); err != nil {
		t.Fatal(err)
	}
	publisher.onConnect(client)
	if len(client.payloadsForTopic(independentStateTopic)) != 0 || publisher.lastAvailability != availabilityOffline {
		t.Fatal("late grid poll/reconnect published after Close")
	}
}

func TestIndependentGridLoadMetadataDoesNotDependOnPrimarySource(t *testing.T) {
	publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
	defer publisher.Close()
	observer := publisher.GridLoadObserver()
	fallback := discoverySnapshot("solarman", model.Metric{Group: "alert", Key: "warning", Name: "Warning", Value: 0})
	fallback.Meta = map[string]string{"unowned_fallback_meta": "not a primary diagnostic", "shelly_grid_load_url": "http://stale.test"}
	if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
		t.Fatal(err)
	}
	state := publishedState(t, client, independentStateTopic)
	if state.Meta["shelly_grid_load_url"] != "http://shelly.test" || state.Meta["unowned_fallback_meta"] != "" {
		t.Fatalf("fallback replaced/lost independently owned metadata: %+v", state.Meta)
	}
	if err := publisher.OnPollFailure("solarman", errors.New("inverter off"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, independentStateTopic)
	if state.Meta["shelly_grid_load_url"] != "http://shelly.test" {
		t.Fatalf("inverter failure hid Shelly diagnostics: %+v", state.Meta)
	}
	meta := decodeDiscovery(t, publisher.cachedDiscovery, "homeassistant/sensor/stable_inverter_meta_shelly_grid_load_url/config")
	assertStreamAvailability(t, meta, "grid_load_up", "shelly_grid_load_url")
	if err := observer.OnPollFailure("shelly_grid_load", errors.New("meter off"), time.Second, 1); err != nil {
		t.Fatal(err)
	}
	state = publishedState(t, client, independentStateTopic)
	if _, exists := state.Meta["shelly_grid_load_url"]; exists {
		t.Fatalf("failed grid poll exposed old inverter-cached Shelly metadata: %+v", state.Meta)
	}
}

func TestIndependentGridLoadStatePublishFailureInvalidatesRetainedOnline(t *testing.T) {
	for _, failingStream := range []string{"inverter", "grid_load"} {
		for _, offlineRejected := range []bool{false, true} {
			name := failingStream
			if offlineRejected {
				name += "_offline_write_rejected"
			}
			t.Run(name, func(t *testing.T) {
				publisher, client := newPersistentPublisher(t, filepath.Join(t.TempDir(), "discovery.json"))
				defer publisher.Close()
				observer := publisher.GridLoadObserver()
				inverter := discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800})
				if err := publisher.OnPollSuccess(inverter, time.Second); err != nil {
					t.Fatal(err)
				}
				if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
					t.Fatal(err)
				}
				client.clear()
				client.failNextPublishes(errors.New("state rejected"))
				if offlineRejected {
					client.failNextPublishes(errors.New("offline rejected"))
				}
				var err error
				if failingStream == "inverter" {
					err = publisher.OnPollFailure("modbus", errors.New("off"), time.Second, 1)
				} else {
					err = observer.OnPollFailure("shelly_grid_load", errors.New("off"), time.Second, 1)
				}
				if err == nil {
					t.Fatal("failed independent state write was silently accepted")
				}
				if values := client.payloadsForTopic(publisher.availabilityTopic); len(values) != 1 || values[0] != availabilityOffline {
					t.Fatalf("failed state write did not separately invalidate retained online: %v", values)
				}
				if publisher.lastAvailability != availabilityOffline || publisher.offlinePublishPending != offlineRejected {
					t.Fatalf("offline status/pending = %s/%v", publisher.lastAvailability, publisher.offlinePublishPending)
				}
				client.clear()
				publisher.onConnect(client)
				state := publishedState(t, client, independentStateTopic)
				if failingStream == "inverter" && state.Up || failingStream == "grid_load" && (state.GridLoadUp == nil || *state.GridLoadUp) {
					t.Fatalf("reconnect restored failed stream as up: %+v", state)
				}
				if values := client.payloadsForTopic(publisher.availabilityTopic); len(values) != 1 || values[0] != availabilityOffline || publisher.offlinePublishPending {
					t.Fatalf("reconnect revived availability or missed pending retry: %v", values)
				}
			})
		}
	}
}

func TestIndependentGridLoadUsesOneSharedOfflineLWT(t *testing.T) {
	publisher, err := NewPublisher(persistentMQTTConfig(""))
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	publisher.GridLoadObserver()
	options := publisher.client.OptionsReader()
	if !options.WillEnabled() || options.WillTopic() != publisher.availabilityTopic || string(options.WillPayload()) != availabilityOffline || !options.WillRetained() {
		t.Fatal("independent streams lost their shared retained offline LWT")
	}
}

func TestIndependentGridLoadReconnectDeliveryFailureFailsClosed(t *testing.T) {
	for _, stage := range []string{"discovery", "state", "availability"} {
		t.Run(stage, func(t *testing.T) {
			publisher, client := newPersistentPublisher(t, "")
			defer publisher.Close()
			observer := publisher.GridLoadObserver()
			if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
				t.Fatal(err)
			}
			before := 0
			if stage != "discovery" {
				before = len(publisher.cachedDiscovery)
			}
			if stage == "availability" {
				before++
			}
			failures := make([]error, before+1)
			failures[before] = errors.New("replay rejected")
			client.failNextPublishes(failures...)
			client.clear()
			publisher.onConnect(client)
			values := client.payloadsForTopic(publisher.availabilityTopic)
			if len(values) == 0 || values[len(values)-1] != availabilityOffline || publisher.lastAvailability != availabilityOffline {
				t.Fatalf("reconnect %s failure did not fail closed: %v", stage, values)
			}
		})
	}
}

func TestIndependentGridLoadRejectsNonGridMetricsWithoutKeepingMeterUp(t *testing.T) {
	publisher, client := newPersistentPublisher(t, "")
	defer publisher.Close()
	observer := publisher.GridLoadObserver()
	inverter := discoverySnapshot("modbus", model.Metric{Group: "electric", Key: "DP1", Name: "PV1 Power", Unit: "W", Value: 800})
	if err := publisher.OnPollSuccess(inverter, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 400), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := observer.OnPollSuccess(inverter, time.Second); err == nil {
		t.Fatal("grid-load observer accepted inverter metrics")
	}
	state := publishedState(t, client, independentStateTopic)
	if !state.Up || state.GridLoadUp == nil || *state.GridLoadUp || state.Metrics["grid_load_total_power"] != nil || derefFloat(state.Metrics["electric_dp1"]) != 800 {
		t.Fatalf("invalid grid observation changed inverter or retained meter up: %+v", state)
	}
}

func assertStreamAvailability(t *testing.T, discovery map[string]any, flag, metricKey string) {
	t.Helper()
	if discovery["availability_mode"] != "all" {
		t.Fatalf("missing combined LWT/stream availability: %+v", discovery)
	}
	availability, ok := discovery["availability"].([]any)
	if !ok || len(availability) != 2 {
		t.Fatalf("availability conditions = %+v", discovery["availability"])
	}
	transport := availability[0].(map[string]any)
	if transport["topic"] != "jinko-exporter/availability" {
		t.Fatalf("missing shared LWT topic: %+v", transport)
	}
	stream := availability[1].(map[string]any)
	template, _ := stream["value_template"].(string)
	if stream["topic"] != independentStateTopic || !strings.Contains(template, "value_json.get('"+flag+"', false)") || !strings.Contains(template, metricKey) {
		t.Fatalf("incorrect stream/point availability: %+v", stream)
	}
}
