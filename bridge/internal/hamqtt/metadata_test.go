package hamqtt

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDeviceMetadataDiscoveryPreservesRawStringsAndStableIDs(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "persistent"}[persistent], func(t *testing.T) {
			path := ""
			if persistent {
				path = filepath.Join(t.TempDir(), "discovery.json")
			}
			publisher, client := newPersistentPublisher(t, path)
			defer publisher.Close()
			primary := discoverySnapshot("modbus")
			primary.Meta = deviceMetadataValues()
			if err := publisher.OnPollSuccess(primary, time.Second); err != nil {
				t.Fatal(err)
			}
			state := publishedState(t, client, independentStateTopic)
			if !reflect.DeepEqual(state.Meta, primary.Meta) {
				t.Fatalf("metadata state = %#v, want unmodified raw strings %#v", state.Meta, primary.Meta)
			}
			for key := range primary.Meta {
				assertDeviceMetadataDiscovery(t, publisher.cachedDiscovery, key)
			}

			// Metadata values do not form any part of the entity identity or schema.
			signature := publisher.cachedDiscoverySig
			recovery := discoverySnapshot("modbus")
			recovery.Meta = deviceMetadataValues()
			recovery.Meta["protocol_version_hex"] = "BEEF"
			recovery.Meta["battery_input_code"] = "4"
			if err := publisher.OnPollSuccess(recovery, time.Second); err != nil {
				t.Fatal(err)
			}
			if publisher.cachedDiscoverySig != signature {
				t.Fatal("metadata value update changed discovery signature")
			}
			if got := publishedState(t, client, independentStateTopic).Meta; !reflect.DeepEqual(got, recovery.Meta) {
				t.Fatalf("updated metadata state = %#v, want %#v", got, recovery.Meta)
			}

			// Already discovered entities must resolve missing fallback data through
			// the missing-safe template, without replaying the previous raw value.
			fallback := discoverySnapshot("jinko")
			if err := publisher.OnPollSuccess(fallback, time.Second); err != nil {
				t.Fatal(err)
			}
			state = publishedState(t, client, independentStateTopic)
			if !state.Up || len(state.Meta) != 0 {
				t.Fatalf("fallback state = %+v, want up with metadata missing/unknown", state)
			}
			if persistent {
				for key := range primary.Meta {
					assertDeviceMetadataDiscovery(t, publisher.cachedDiscovery, key)
				}
			}

			partial := discoverySnapshot("modbus")
			partial.Meta = map[string]string{"protocol_version_hex": "00BC"}
			if err := publisher.OnPollSuccess(partial, time.Second); err != nil {
				t.Fatal(err)
			}
			if got := publishedState(t, client, independentStateTopic).Meta; !reflect.DeepEqual(got, partial.Meta) {
				t.Fatalf("partial recovery metadata = %#v, want only current values %#v", got, partial.Meta)
			}
			assertDeviceMetadataDiscovery(t, publisher.cachedDiscovery, "protocol_version_hex")
		})
	}
}

func TestDeviceMetadataUnavailableOnInverterFailureWithHealthyShelly(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "persistent"}[persistent], func(t *testing.T) {
			path := ""
			if persistent {
				path = filepath.Join(t.TempDir(), "discovery.json")
			}
			publisher, client := newPersistentPublisher(t, path)
			defer publisher.Close()
			observer := publisher.GridLoadObserver()
			primary := discoverySnapshot("modbus")
			primary.Meta = deviceMetadataValues()
			if err := publisher.OnPollSuccess(primary, time.Second); err != nil {
				t.Fatal(err)
			}
			if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 600), time.Second); err != nil {
				t.Fatal(err)
			}
			for key := range primary.Meta {
				discovery := assertDeviceMetadataDiscovery(t, publisher.cachedDiscovery, key)
				if _, exists := discovery["availability_topic"]; exists {
					t.Fatalf("metadata %s uses global availability without inverter health", key)
				}
				if discovery["availability_mode"] != "all" {
					t.Fatalf("metadata %s availability mode = %v, want all", key, discovery["availability_mode"])
				}
				want := []any{
					map[string]any{"topic": publisher.availabilityTopic, "payload_available": "online", "payload_not_available": "offline"},
					map[string]any{"topic": independentStateTopic, "value_template": "{{ 'online' if value_json.get('up', false) else 'offline' }}", "payload_available": "online", "payload_not_available": "offline"},
				}
				if !reflect.DeepEqual(discovery["availability"], want) {
					t.Fatalf("metadata %s availability = %#v, want %#v", key, discovery["availability"], want)
				}
			}
			if err := publisher.OnPollFailure("modbus", errors.New("inverter unavailable"), time.Second, 1); err != nil {
				t.Fatal(err)
			}
			if err := observer.OnPollSuccess(gridLoadSnapshotAt(time.Now(), 700), time.Second); err != nil {
				t.Fatal(err)
			}
			state := publishedState(t, client, independentStateTopic)
			if state.Up || state.GridLoadUp == nil || !*state.GridLoadUp || publisher.lastAvailability != availabilityOnline {
				t.Fatalf("failure with healthy Shelly state = %+v, availability = %q", state, publisher.lastAvailability)
			}
			if got := derefFloat(state.Metrics["grid_load_total_power"]); got != 700 {
				t.Fatalf("healthy Shelly power = %v, want 700", got)
			}

			// A successful fallback makes the inverter available again, with no
			// stale metadata inherited from the previous Modbus snapshot.
			if err := publisher.OnPollSuccess(discoverySnapshot("jinko"), time.Second); err != nil {
				t.Fatal(err)
			}
			state = publishedState(t, client, independentStateTopic)
			if !state.Up {
				t.Fatal("fallback did not restore inverter availability")
			}
			for key := range primary.Meta {
				if value, exists := state.Meta[key]; exists {
					t.Fatalf("fallback retained stale metadata %s = %q", key, value)
				}
			}
		})
	}
}

func deviceMetadataValues() map[string]string {
	return map[string]string{
		"protocol_version_hex": "00AF", "control_firmware_words": "000A-BCDE",
		"communication_firmware_words": "000A-BCDE-00FF", "bootloader_version_hex": "0B",
		"assistant_version_hex": "EF", "battery_input_code": "0",
	}
}

func assertDeviceMetadataDiscovery(t *testing.T, messages []discoveryMessage, key string) map[string]any {
	t.Helper()
	id := "stable_inverter_meta_" + key
	discovery := decodeDiscovery(t, messages, "homeassistant/sensor/"+id+"/config")
	for field, want := range map[string]any{
		"unique_id": id, "state_topic": independentStateTopic, "entity_category": "diagnostic",
		"value_template": "{{ value_json.get('meta', {}).get('" + key + "') }}",
	} {
		if got := discovery[field]; got != want {
			t.Errorf("metadata %s discovery %s = %#v, want %#v", key, field, got, want)
		}
	}
	for _, field := range []string{"unit_of_measurement", "state_class", "device_class"} {
		if value, exists := discovery[field]; exists {
			t.Errorf("raw metadata %s unexpectedly has %s = %#v", key, field, value)
		}
	}
	return discovery
}
