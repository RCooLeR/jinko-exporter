package shelly

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		case "/rpc/EM.GetStatus":
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
		case "/rpc/EMData.GetStatus":
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
		BaseURL: server.URL,
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
	assertMetric(t, snapshot.Metrics, "grid_load", "total_power", 1533, "W")
	assertMetric(t, snapshot.Metrics, "grid_load", "l1_voltage", 231.2, "V")
	assertMetric(t, snapshot.Metrics, "grid_load", "l2_current", 2.2, "A")
	assertMetric(t, snapshot.Metrics, "grid_load", "l3_power", 770, "W")
	assertMetric(t, snapshot.Metrics, "grid_load", "energy_total", 6, "kWh")
	assertMetric(t, snapshot.Metrics, "grid_load", "returned_energy_total", 0.06, "kWh")
}

func TestNewGridLoadClientRejectsURLWithoutHost(t *testing.T) {
	_, err := NewGridLoadClient(config.ShellyGridLoadConfig{
		BaseURL: "192.168.120.50",
		Timeout: time.Second,
	})
	if err == nil {
		t.Fatal("NewGridLoadClient() error = nil, want URL error")
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
