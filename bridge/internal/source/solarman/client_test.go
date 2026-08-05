package solarman

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestPasswordSHA256HexPrefersProvidedHash(t *testing.T) {
	client := New(config.SolarmanConfig{
		Password:       "plain-password",
		PasswordSHA256: "ABCDEF012345",
	}, nil)

	got, err := client.passwordSHA256Hex()
	if err != nil {
		t.Fatalf("passwordSHA256Hex() error = %v", err)
	}
	if got != "abcdef012345" {
		t.Fatalf("passwordSHA256Hex() = %q, want lowercase provided hash", got)
	}
}

func TestPasswordSHA256HexHashesPassword(t *testing.T) {
	client := New(config.SolarmanConfig{Password: "plain-password"}, nil)

	got, err := client.passwordSHA256Hex()
	if err != nil {
		t.Fatalf("passwordSHA256Hex() error = %v", err)
	}
	sum := sha256.Sum256([]byte("plain-password"))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("passwordSHA256Hex() = %q, want %q", got, want)
	}
}

func TestReadResponseBodyRejectsOversizedBody(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxHTTPResponseBodyBytes+1))
	if _, err := readResponseBody(body); err == nil {
		t.Fatal("readResponseBody() error = nil, want oversized body error")
	}
}

func TestFetchWithDeviceSNObtainsTokenAndParsesCurrentData(t *testing.T) {
	var tokenRequest map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			if r.URL.Query().Get("appId") != "app-id" || r.URL.Query().Get("language") != "en" {
				t.Fatalf("token query = %s, want appId/language", r.URL.RawQuery)
			}
			if err := json.NewDecoder(r.Body).Decode(&tokenRequest); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("Authorization = %q, want Bearer access-token", r.Header.Get("Authorization"))
			}
			if r.URL.Query().Get("appId") != "app-id" || r.URL.Query().Get("language") != "en" {
				t.Fatalf("currentData query = %s, want appId/language", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{
				"success": true,
				"dataList": [
					{"key": "DP1", "name": "DC Power PV1", "unit": "W", "value": 321},
					{"key": "BMS_SOC", "name": "BMS_SOC", "unit": "%", "value": "88.5"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(testSolarmanConfig(server.URL), nil)
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if tokenRequest["appSecret"] != "app-secret" || tokenRequest["password"] != "password-sha" {
		t.Fatalf("token request = %#v, want app secret and provided password hash", tokenRequest)
	}
	if snapshot.Source != "solarman" || snapshot.DeviceSN != "DEVICE_SN" {
		t.Fatalf("snapshot source/device = %q/%q", snapshot.Source, snapshot.DeviceSN)
	}
	metrics := make(map[string]float64, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		metrics[metric.Key] = metric.Value
	}
	if metrics["DP1"] != 321 || metrics["BMS_SOC"] != 88.5 {
		t.Fatalf("metrics = %#v, want DP1/BMS_SOC", metrics)
	}
}

func TestFetchDiscoversDeviceSNFromDeviceListItems(t *testing.T) {
	var currentDataRequest map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/station/v1.0/device":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("discovery Authorization = %q, want Bearer access-token", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"success":true,"deviceListItems":[{"deviceSn":"DISCOVERED_SN"}]}`))
		case "/device/v1.0/currentData":
			if err := json.NewDecoder(r.Body).Decode(&currentDataRequest); err != nil {
				t.Fatalf("decode currentData body: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"dataList":[{"key":"DP1","name":"DC Power PV1","unit":"W","value":123}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testSolarmanConfig(server.URL)
	cfg.DeviceSN = ""
	cfg.StationID = 42
	client := New(cfg, nil)
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if snapshot.DeviceSN != "DISCOVERED_SN" {
		t.Fatalf("snapshot.DeviceSN = %q, want DISCOVERED_SN", snapshot.DeviceSN)
	}
	if currentDataRequest["deviceSn"] != "DISCOVERED_SN" {
		t.Fatalf("currentData deviceSn = %#v, want DISCOVERED_SN", currentDataRequest["deviceSn"])
	}
}

func TestFetchRetriesTransientSolarmanError(t *testing.T) {
	var currentDataCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			if currentDataCalls.Add(1) == 1 {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"dataList":[{"key":"DP1","name":"DC Power PV1","unit":"W","value":456}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(testSolarmanConfig(server.URL), nil)
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := currentDataCalls.Load(); got != 2 {
		t.Fatalf("currentData calls = %d, want 2", got)
	}
	if len(snapshot.Metrics) != 1 || snapshot.Metrics[0].Value != 456 {
		t.Fatalf("snapshot metrics = %#v, want retried value 456", snapshot.Metrics)
	}
}

func TestFetchReturnsSolarmanBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			_, _ = w.Write([]byte(`{"success":false,"msg":"quota exhausted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(testSolarmanConfig(server.URL), nil)
	_, err := client.Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "quota exhausted") {
		t.Fatalf("Fetch() error = %v, want quota exhausted business error", err)
	}
}

func testSolarmanConfig(baseURL string) config.SolarmanConfig {
	return config.SolarmanConfig{
		BaseURL:        baseURL,
		APIVersion:     "v1.0",
		Language:       "en",
		Timeout:        time.Second,
		AppID:          "app-id",
		AppSecret:      "app-secret",
		Email:          "solar@example.test",
		PasswordSHA256: "password-sha",
		DeviceSN:       "DEVICE_SN",
	}
}
