package shelly

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestGridLoadClientFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "0" {
			t.Fatalf("id query = %q, want 0", r.URL.Query().Get("id"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/private-proxy/rpc/EM.GetStatus":
			_, _ = fmt.Fprint(w, `{
				"id":0,
				"a_current":1.1,"a_voltage":231.2,"a_act_power":253.0,"a_aprt_power":254.0,"a_pf":0.99,"a_freq":50.0,
				"b_current":2.2,"b_voltage":232.3,"b_act_power":510.0,"b_aprt_power":511.0,"b_pf":0.98,"b_freq":50.1,
				"c_current":3.3,"c_voltage":233.4,"c_act_power":770.0,"c_aprt_power":771.0,"c_pf":0.97,"c_freq":50.2,
				"n_current":0.4,
				"total_current":6.6,
				"total_act_power":1533.0,
				"total_aprt_power":1536.0
			}`)
		case "/private-proxy/rpc/EMData.GetStatus":
			_, _ = fmt.Fprint(w, `{
				"id":0,
				"a_total_act_energy":1000,
				"a_total_act_ret_energy":10,
				"b_total_act_energy":2000,
				"b_total_act_ret_energy":20,
				"c_total_act_energy":3000,
				"c_total_act_ret_energy":30,
				"total_act":6000,
				"total_act_ret":60
			}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewGridLoadClient(config.ShellyGridLoadConfig{
		BaseURL: server.URL + "/private-proxy",
		EMID:    0,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewGridLoadClient() error = %v", err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "shelly_grid_load" {
		t.Fatalf("Source = %q, want shelly_grid_load", snapshot.Source)
	}
	if got := snapshot.Meta["shelly_grid_load_url"]; got != server.URL {
		t.Fatalf("safe Shelly metadata URL = %q, want origin %q", got, server.URL)
	}
	assertMetric(t, snapshot.Metrics, "grid_load", "total_power", 1533, "W")
	assertMetric(t, snapshot.Metrics, "grid_load", "l1_voltage", 231.2, "V")
	assertMetric(t, snapshot.Metrics, "grid_load", "l2_current", 2.2, "A")
	assertMetric(t, snapshot.Metrics, "grid_load", "l3_power", 770, "W")
	assertMetric(t, snapshot.Metrics, "grid_load", "energy_total", 6, "kWh")
	assertMetric(t, snapshot.Metrics, "grid_load", "returned_energy_total", 0.06, "kWh")
}

func TestNewGridLoadClientRejectsURLWithoutHost(t *testing.T) {
	_, err := NewGridLoadClient(config.ShellyGridLoadConfig{
		BaseURL: "192.0.2.50",
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("NewGridLoadClient() error = nil, want URL error")
	}
}

func TestNewGridLoadClientValidatesAndRedactsBaseURL(t *testing.T) {
	const (
		sentinelUser     = "SHELLY_SENTINEL_USER"
		sentinelPassword = "SHELLY_SENTINEL_PASSWORD"
		sentinelToken    = "SHELLY_SENTINEL_TOKEN"
	)
	tests := []string{
		"ftp://shelly.example.test",
		"http://" + sentinelUser + ":" + sentinelPassword + "@shelly.example.test",
		"http://shelly.example.test?token=" + sentinelToken,
		"http://shelly.example.test?",
		"http://shelly.example.test#" + sentinelToken,
		"http://shelly.example.test#",
		"http://[::1",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			_, err := NewGridLoadClient(config.ShellyGridLoadConfig{BaseURL: rawURL, Timeout: time.Second})
			if err == nil {
				t.Fatalf("NewGridLoadClient(%q) error = nil, want rejection", rawURL)
			}
			text := err.Error()
			for _, secret := range []string{sentinelUser, sentinelPassword, sentinelToken} {
				if strings.Contains(text, secret) {
					t.Fatalf("URL validation error exposed %q: %q", secret, text)
				}
			}
		})
	}
}

func TestGridLoadClientRejectsOversizedRPCResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, strings.Repeat(" ", maxShellyRPCResponseBytes+1))
	}))
	defer server.Close()

	client, err := NewGridLoadClient(config.ShellyGridLoadConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewGridLoadClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "body exceeds") {
		t.Fatalf("Fetch() error = %v, want bounded-response rejection", err)
	}
}

func TestGridLoadClientRejectsRPCErrorWithoutEchoingDeviceMessage(t *testing.T) {
	const sentinelMessage = "SHELLY_RPC_SENTINEL_MESSAGE"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":-103,"message":%q}`, sentinelMessage)
	}))
	defer server.Close()

	client, err := NewGridLoadClient(config.ShellyGridLoadConfig{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewGridLoadClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "RPC error code -103") {
		t.Fatalf("Fetch() error = %v, want Shelly RPC error", err)
	}
	if strings.Contains(err.Error(), sentinelMessage) {
		t.Fatalf("Fetch() error exposed device-provided message: %q", err)
	}
}

func TestGridLoadClientRefusesRedirects(t *testing.T) {
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit = true
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/rpc/EM.GetStatus", http.StatusFound)
	}))
	defer origin.Close()

	client, err := NewGridLoadClient(config.ShellyGridLoadConfig{BaseURL: origin.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewGridLoadClient() error = %v", err)
	}
	_, err = client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Fatalf("Fetch() error = %v, want redirect rejection", err)
	}
	if targetHit {
		t.Fatal("Shelly client followed a cross-origin redirect")
	}
}

func TestGridLoadClientRejectsEmptyTelemetry(t *testing.T) {
	validEM := `{"id":0,"total_act_power":0}`
	validEMData := `{"id":0,"total_act":0}`
	tests := []struct {
		name      string
		em        string
		emData    string
		wantError string
	}{
		{name: "empty EM response", em: `{"id":0}`, emData: validEMData, wantError: "EM.GetStatus"},
		{name: "empty EMData response", em: validEM, emData: `{"id":0}`, wantError: "EMData.GetStatus"},
		{name: "missing component id", em: `{"total_act_power":0}`, emData: validEMData, wantError: "missing component id"},
		{name: "wrong component id", em: `{"id":1,"total_act_power":0}`, emData: validEMData, wantError: "component id is 1, want 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/rpc/EM.GetStatus":
					_, _ = fmt.Fprint(w, tt.em)
				case "/rpc/EMData.GetStatus":
					_, _ = fmt.Fprint(w, tt.emData)
				default:
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			client, err := NewGridLoadClient(config.ShellyGridLoadConfig{BaseURL: server.URL, Timeout: time.Second})
			if err != nil {
				t.Fatalf("NewGridLoadClient() error = %v", err)
			}
			_, err = client.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Fetch() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func assertMetric(t *testing.T, metrics []model.Metric, group, key string, want float64, unit string) {
	t.Helper()
	for _, metric := range metrics {
		if metric.Group == group && metric.Key == key {
			if metric.Value != want || metric.Unit != unit {
				t.Fatalf("%s/%s = value %v unit %q, want value %v unit %q", group, key, metric.Value, metric.Unit, want, unit)
			}
			return
		}
	}
	t.Fatalf("metric %s/%s not found in %#v", group, key, metrics)
}
