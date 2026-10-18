package shelly

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestGridInputClientPreservesAllMeasurementsWithDistinctIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantID := "5"
		if strings.HasSuffix(r.URL.Path, "Temperature.GetStatus") {
			wantID = "0"
		}
		if r.URL.Query().Get("id") != wantID {
			t.Errorf("component id=%q want%s", r.URL.Query().Get("id"), wantID)
		}
		switch r.URL.Path {
		case "/private-proxy/rpc/EM.GetStatus":
			fmt.Fprint(w, `{"id":5,"a_current":1.1,"a_voltage":231.2,"a_act_power":-253,"a_aprt_power":254,"a_pf":-0.99,"a_freq":50,"b_current":2.2,"b_voltage":232.3,"b_act_power":0,"b_aprt_power":511,"b_pf":0,"b_freq":50.1,"c_current":3.3,"c_voltage":233.4,"c_act_power":770,"c_aprt_power":771,"c_pf":0.97,"c_freq":50.2,"n_current":0.4,"total_current":6.6,"total_act_power":500,"total_aprt_power":1536}`)
		case "/private-proxy/rpc/EMData.GetStatus":
			fmt.Fprint(w, `{"id":5,"a_total_act_energy":1000,"a_total_act_ret_energy":10,"b_total_act_energy":2000,"b_total_act_ret_energy":20,"c_total_act_energy":3000,"c_total_act_ret_energy":30,"total_act":6000,"total_act_ret":60}`)
		case "/private-proxy/rpc/Temperature.GetStatus":
			fmt.Fprint(w, `{"id":0,"tC":51.1}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGridInputClient(config.ShellyGridInputConfig{BaseURL: server.URL + "/private-proxy", EMID: 5, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "shelly_grid_input" || snapshot.Source != client.Name() {
		t.Fatalf("wrong source %q", snapshot.Source)
	}
	if len(snapshot.Metrics) != 31 {
		t.Fatalf("metric count=%d want31", len(snapshot.Metrics))
	}
	if len(snapshot.Meta) != 2 || snapshot.Meta["shelly_grid_input_url"] != server.URL || snapshot.Meta["shelly_grid_input_em_id"] != "5" {
		t.Fatalf("wrong metadata: %+v", snapshot.Meta)
	}
	for _, metric := range snapshot.Metrics {
		wantPrefix := "Grid Input "
		if metric.Key == "internal_temperature" {
			wantPrefix = "Shelly Grid Input Internal Temperature"
		}
		if metric.Group != "grid_input" || !strings.HasPrefix(metric.Name, wantPrefix) || strings.Contains(metric.Name, "Grid Load") {
			t.Fatalf("cross-meter identity: %+v", metric)
		}
	}
	assertMetric(t, snapshot.Metrics, "grid_input", "l1_power", -253, "W")
	assertMetric(t, snapshot.Metrics, "grid_input", "l2_power", 0, "W")
	assertMetric(t, snapshot.Metrics, "grid_input", "total_power", 500, "W") // Independent total, not a phase sum.
	assertMetric(t, snapshot.Metrics, "grid_input", "l3_voltage", 233.4, "V")
	assertMetric(t, snapshot.Metrics, "grid_input", "energy_total", 6, "kWh")
	assertMetric(t, snapshot.Metrics, "grid_input", "returned_energy_total", 0.06, "kWh")
	assertMetric(t, snapshot.Metrics, "grid_input", "internal_temperature", 51.1, "°C")
}

func TestGridInputClientMissingValuesAndOptionalTemperature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rpc/EM.GetStatus":
			fmt.Fprint(w, `{"id":0,"a_voltage":0,"a_current":null}`)
		case "/rpc/EMData.GetStatus":
			fmt.Fprint(w, `{"id":0,"total_act":0}`)
		case "/rpc/Temperature.GetStatus":
			http.Error(w, "unsupported", http.StatusNotFound)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewGridInputClient(config.ShellyGridInputConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Metrics) != 2 {
		t.Fatalf("missing readings invented: %+v", snapshot.Metrics)
	}
	assertMetric(t, snapshot.Metrics, "grid_input", "l1_voltage", 0, "V")
	assertMetric(t, snapshot.Metrics, "grid_input", "energy_total", 0, "kWh")
}

func TestGridInputClientFailuresDoNotReturnPartialOrDownstreamData(t *testing.T) {
	for _, bad := range []string{`{"id":1,"a_voltage":230}`, `{"id":0}`, `{"code":500}`, `not JSON`} {
		t.Run(bad, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, bad) }))
			defer server.Close()
			client, err := NewGridInputClient(config.ShellyGridInputConfig{BaseURL: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err == nil || snapshot != nil || calls.Load() != 1 {
				t.Fatalf("failure returned partial data or retried: snapshot=%v err=%v calls=%d", snapshot, err, calls.Load())
			}
		})
	}
}

func TestGridInputClientRejectsPartialSnapshotWhenEnergyFails(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/rpc/EM.GetStatus":
			fmt.Fprint(w, `{"id":0,"a_voltage":230,"total_act_power":-100}`)
		case "/rpc/EMData.GetStatus":
			fmt.Fprint(w, `{"id":1,"total_act":5000}`)
		default:
			t.Errorf("unexpected request after required energy failure: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewGridInputClient(config.ShellyGridInputConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(t.Context())
	if err == nil || snapshot != nil || calls.Load() != 2 {
		t.Fatalf("partial snapshot or extra request: snapshot=%v err=%v calls=%d", snapshot, err, calls.Load())
	}
}

func TestGridInputClientConstructorValidation(t *testing.T) {
	const secret = "INPUT_SECRET"
	for _, cfg := range []config.ShellyGridInputConfig{
		{BaseURL: "http://" + secret + "@input.test", Timeout: time.Second},
		{BaseURL: "http://input.test?token=" + secret, Timeout: time.Second},
		{BaseURL: "http://input.test#" + secret, Timeout: time.Second},
		{BaseURL: "ftp://input.test", Timeout: time.Second},
		{BaseURL: "http://input.test", EMID: -1, Timeout: time.Second},
		{BaseURL: "http://input.test", Timeout: 0},
	} {
		client, err := NewGridInputClient(cfg)
		if err == nil || client != nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("invalid config accepted or secret echoed: %v", err)
		}
	}
}
