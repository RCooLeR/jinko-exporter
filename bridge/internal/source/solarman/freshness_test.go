package solarman

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestFetchValidatesDeviceStateAndMeasurementTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name         string
		change       map[string]any
		omit         string
		maxAge       time.Duration
		wantCategory string
	}{
		{name: "online"},
		{name: "alarm is live telemetry", change: map[string]any{"deviceState": 2}},
		{name: "offline even with current time", change: map[string]any{"deviceState": 3}, wantCategory: "device-offline"},
		{name: "two days old", change: map[string]any{"collectionTime": now.Add(-48 * time.Hour).Unix()}, wantCategory: "stale-data"},
		{name: "configured shorter age", change: map[string]any{"collectionTime": now.Add(-2 * time.Minute).Unix()}, maxAge: time.Minute, wantCategory: "stale-data"},
		{name: "configured longer age", change: map[string]any{"collectionTime": now.Add(-20 * time.Minute).Unix()}, maxAge: 30 * time.Minute},
		{name: "future time", change: map[string]any{"collectionTime": now.Add(2 * time.Minute).Unix()}, wantCategory: "future-collection-time"},
		{name: "missing time", omit: "collectionTime", wantCategory: "invalid-collection-time"},
		{name: "null time", change: map[string]any{"collectionTime": nil}, wantCategory: "invalid-collection-time"},
		{name: "zero time", change: map[string]any{"collectionTime": 0}, wantCategory: "invalid-collection-time"},
		{name: "negative time", change: map[string]any{"collectionTime": -1}, wantCategory: "invalid-collection-time"},
		{name: "overflow time", change: map[string]any{"collectionTime": json.Number("9223372036854775808")}, wantCategory: "decode"},
		{name: "out of range time", change: map[string]any{"collectionTime": int64(math.MaxInt64)}, wantCategory: "invalid-collection-time"},
		{name: "fractional time", change: map[string]any{"collectionTime": float64(now.Unix()) + 0.5}, wantCategory: "decode"},
		{name: "string time", change: map[string]any{"collectionTime": "2026-09-11"}, wantCategory: "decode"},
		{name: "missing state", omit: "deviceState", wantCategory: "invalid-device-state"},
		{name: "null state", change: map[string]any{"deviceState": nil}, wantCategory: "invalid-device-state"},
		{name: "unknown state", change: map[string]any{"deviceState": 4}, wantCategory: "invalid-device-state"},
		{name: "zero state", change: map[string]any{"deviceState": 0}, wantCategory: "invalid-device-state"},
		{name: "negative state", change: map[string]any{"deviceState": -1}, wantCategory: "invalid-device-state"},
		{name: "string state", change: map[string]any{"deviceState": "1"}, wantCategory: "decode"},
		{name: "fractional state", change: map[string]any{"deviceState": 1.5}, wantCategory: "decode"},
		{name: "missing success", omit: "success", wantCategory: "api-rejected"},
		{name: "null success", change: map[string]any{"success": nil}, wantCategory: "api-rejected"},
		{name: "false success", change: map[string]any{"success": false}, wantCategory: "api-rejected"},
		{name: "string success", change: map[string]any{"success": "true"}, wantCategory: "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"success":        true,
				"deviceState":    1,
				"collectionTime": now.Unix(),
				"dataList": []any{
					map[string]any{"key": "DP1", "name": "PV1", "unit": "W", "value": 0},
					map[string]any{"dataKey": "BMS_SOC", "dataName": "SOC", "dataUnit": "%", "val": "88.5"},
				},
			}
			for key, value := range tt.change {
				payload[key] = value
			}
			delete(payload, tt.omit)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/device/v1.0/currentData" {
					t.Errorf("unexpected API request %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				calls.Add(1)
				if err := json.NewEncoder(w).Encode(payload); err != nil {
					t.Errorf("encode fixture: %v", err)
				}
			}))
			defer server.Close()
			cfg := testSolarmanConfig(server.URL)
			cfg.MaxDataAge = tt.maxAge
			client := New(cfg, nil)
			client.token = tokenResponse{AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour)}
			snapshot, err := client.Fetch(t.Context())
			if got := calls.Load(); got != 1 {
				t.Errorf("currentData requests = %d, want exactly 1", got)
			}
			if tt.wantCategory != "" {
				if snapshot != nil || err == nil || !strings.Contains(err.Error(), "category="+tt.wantCategory) {
					t.Fatalf("Fetch() = %#v, %v, want nil snapshot and category=%s", snapshot, err, tt.wantCategory)
				}
				return
			}
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
			wantTime := time.Unix(payload["collectionTime"].(int64), 0).UTC()
			if !snapshot.CollectedAt.Equal(wantTime) {
				t.Fatalf("CollectedAt = %s, want real collection time %s", snapshot.CollectedAt, wantTime)
			}
			if len(snapshot.Metrics) != 2 || snapshot.Metrics[0].Value != 0 || snapshot.Metrics[1].Value != 88.5 {
				t.Fatalf("metrics = %#v, want zero power and compatible alternate point fields", snapshot.Metrics)
			}
		})
	}
}

func TestFetchRepeatedCloudTimestampExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collectedAt := time.Now().UTC().Truncate(time.Second)
		payload := fmt.Sprintf(`{"success":true,"deviceState":1,"collectionTime":%d,"dataList":[{"key":"DP1","value":2230}]}`, collectedAt.Unix())
		client := New(testSolarmanConfig("https://solarman.example.test"), nil)
		client.token = tokenResponse{AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour)}
		calls := 0
		client.hc.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, nil
		})
		for i := range 2 {
			snapshot, err := client.Fetch(t.Context())
			if err != nil || !snapshot.CollectedAt.Equal(collectedAt) {
				t.Fatalf("fresh cached poll %d = %#v, %v", i, snapshot, err)
			}
			time.Sleep(model.DefaultMaxDataAge / 2)
		}
		time.Sleep(time.Second)
		snapshot, err := client.Fetch(t.Context())
		if snapshot != nil || err == nil || !strings.Contains(err.Error(), "category=stale-data") {
			t.Fatalf("expired cached poll = %#v, %v, want stale data failure", snapshot, err)
		}
		if calls != 3 {
			t.Fatalf("request count = %d, want 3 without additional freshness requests", calls)
		}
	})
}
