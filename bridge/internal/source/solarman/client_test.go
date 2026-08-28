package solarman

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	testAccessSecret   = "ACCESS_TOKEN_SENTINEL_71f6"
	testRefreshSecret  = "REFRESH_TOKEN_SENTINEL_89ce"
	testAppSecret      = "APP_SECRET_SENTINEL_4d20"
	testPasswordSecret = "PASSWORD_SECRET_SENTINEL_53ab"
	testEmailSecret    = "EMAIL_IDENTIFIER_SENTINEL_6c84"
	testDeviceSecret   = "DEVICE_IDENTIFIER_SENTINEL_8e19"
	testSiteSecret     = "SITE_IDENTIFIER_SENTINEL_9a37"
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
	if snapshot.ParentSN != "" || snapshot.DeviceID != "" || snapshot.SiteID != "" {
		t.Fatalf("optional identity = parent %q device %q site %q, want empty", snapshot.ParentSN, snapshot.DeviceID, snapshot.SiteID)
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

func TestFetchRejectsMetriclessCurrentData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			_, _ = w.Write([]byte(`{"success":true,"dataList":[{"key":"DP1","value":"NaN"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := New(testSolarmanConfig(server.URL), nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "category=no-numeric-metrics") {
		t.Fatalf("Fetch() error = %v, want empty metric error", err)
	}
}

func TestFetchRejectsDiscoveredEmptyDeviceSerial(t *testing.T) {
	var currentDataCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/station/v1.0/device":
			_, _ = w.Write([]byte(`{"success":true,"deviceListItems":[{"deviceSn":" "}]}`))
		case "/device/v1.0/currentData":
			currentDataCalls.Add(1)
			http.Error(w, "must not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testSolarmanConfig(server.URL)
	cfg.DeviceSN = ""
	cfg.StationID = 42
	_, err := New(cfg, nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "category=empty-device-serial") {
		t.Fatalf("Fetch() error = %v, want empty device serial error", err)
	}
	if got := currentDataCalls.Load(); got != 0 {
		t.Fatalf("currentData calls = %d, want 0", got)
	}
}

func TestCurrentDataDoesNotFollowRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		default:
			http.NotFound(w, r)
		}
	}))
	defer redirect.Close()

	_, err := New(testSolarmanConfig(redirect.URL), nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "status=307") {
		t.Fatalf("Fetch() error = %v, want redirect status", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}

func TestTokenRequestDoesNotFollowRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	_, err := New(testSolarmanConfig(redirect.URL), nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "status=307") || !strings.Contains(err.Error(), "category=http-status") {
		t.Fatalf("Fetch() error = %v, want sanitized token redirect status", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}

func TestTokenExpiryValidatesBoundsWithoutOverflow(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	for name, seconds := range map[string]int64{
		"zero":                          0,
		"negative":                      -1,
		"above one-year safety ceiling": maxTokenLifetimeSeconds + 1,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tokenExpiry(now, seconds); err == nil {
				t.Fatalf("tokenExpiry(%d) error = nil, want range rejection", seconds)
			}
		})
	}

	shortExpiry, err := tokenExpiry(now, 1)
	if err != nil {
		t.Fatalf("tokenExpiry(short) error = %v", err)
	}
	if !shortExpiry.After(now) || !shortExpiry.Before(now.Add(time.Second)) {
		t.Fatalf("short expiry = %s, want strictly inside advertised one-second lifetime", shortExpiry)
	}
	if _, err := tokenExpiry(now, maxTokenLifetimeSeconds); err != nil {
		t.Fatalf("tokenExpiry(one-year ceiling) error = %v", err)
	}
}

func TestFetchRejectsInvalidTokenExpiresIn(t *testing.T) {
	for name, expiresIn := range map[string]string{
		"zero":     "0",
		"negative": "-1",
		"overflow": strconv.FormatInt(maxTokenLifetimeSeconds+1, 10),
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/account/v1.0/token" {
					t.Fatalf("unexpected request path %q after invalid token response", r.URL.Path)
				}
				_, _ = fmt.Fprintf(w, `{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":%q}`, expiresIn)
			}))
			defer server.Close()

			_, err := New(testSolarmanConfig(server.URL), nil).Fetch(t.Context())
			if err == nil || !strings.Contains(err.Error(), "category=invalid-expires-in") {
				t.Fatalf("Fetch() error = %v, want sanitized expires_in rejection", err)
			}
		})
	}
}

func TestToFloatRejectsNonFiniteValues(t *testing.T) {
	tests := map[string]any{
		"float NaN":       math.NaN(),
		"float +Inf":      math.Inf(1),
		"float -Inf":      math.Inf(-1),
		"float32 +Inf":    float32(math.Inf(1)),
		"json number Inf": json.Number("Inf"),
		"string NaN":      "NaN",
		"string +Inf":     "+Inf",
		"string -Inf":     "-Inf",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			if got, ok := toFloat(value); ok {
				t.Fatalf("toFloat(%v) = %v, true; want rejected non-finite value", value, got)
			}
		})
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
	if err == nil || !strings.Contains(err.Error(), "category=api-rejected") || strings.Contains(err.Error(), "quota exhausted") {
		t.Fatalf("Fetch() error = %v, want sanitized API-rejected category", err)
	}
}

func TestMetricFromPointCanonicalizesKnownLabelsInEveryMode(t *testing.T) {
	tests := []struct {
		key   string
		group string
		name  string
		unit  string
	}{
		{key: "DP1", group: "electric", name: "DC Power PV1", unit: "W"},
		{key: "DP2", group: "electric", name: "DC Power PV2", unit: "W"},
		{key: "DV1", group: "electric", name: "DC Voltage PV1", unit: "V"},
		{key: "DC1", group: "electric", name: "DC Current PV1", unit: "A"},
		{key: "DV2", group: "electric", name: "DC Voltage PV2", unit: "V"},
		{key: "DC2", group: "electric", name: "DC Current PV2", unit: "A"},
		{key: "S_P_T", group: "electric", name: "Total Solar Power", unit: "W"},
		// This key deliberately starts in a different inferred group and proves
		// that canonicalization fixes more than the six new PV labels.
		{key: "B_T1", group: "temperature", name: "Temperature- Battery", unit: "C"},
	}

	for _, strict := range []bool{false, true} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("strict=%t/%s", strict, tt.key), func(t *testing.T) {
				client := New(config.SolarmanConfig{CanonicalJinkoMetrics: strict}, nil)
				got, ok := client.metricFromPoint(tt.key, "Solarman-specific name", "source-unit", 28)
				if !ok {
					t.Fatal("metricFromPoint() rejected a known canonical metric")
				}
				if got.Group != tt.group || got.Key != tt.key || got.Name != tt.name || got.Unit != tt.unit || got.Value != 28 {
					t.Fatalf("metricFromPoint() = %#v, want canonical %s/%s/%s/%s value 28", got, tt.group, tt.key, tt.name, tt.unit)
				}
			})
		}
	}
}

func TestMetricFromPointLegacyStrictModeOnlyFiltersUnknownMetrics(t *testing.T) {
	compatible := New(config.SolarmanConfig{CanonicalJinkoMetrics: false}, nil)
	got, ok := compatible.metricFromPoint("SOLARMAN_ONLY", "Solarman-only diagnostic", "raw", 7)
	if !ok {
		t.Fatal("compatibility mode rejected an unknown Solarman metric")
	}
	if got.Key != "SOLARMAN_ONLY" || got.Name != "Solarman-only diagnostic" || got.Unit != "raw" || got.Value != 7 {
		t.Fatalf("compatibility metric = %#v, want original unknown point", got)
	}

	strict := New(config.SolarmanConfig{CanonicalJinkoMetrics: true}, nil)
	if got, ok := strict.metricFromPoint("SOLARMAN_ONLY", "Solarman-only diagnostic", "raw", 7); ok {
		t.Fatalf("strict metricFromPoint() = %#v, want unknown point rejected", got)
	}
}

func TestTokenResponseFailuresNeverExposeSecrets(t *testing.T) {
	stationID := int64(91929394)
	secretList := []string{
		testAccessSecret,
		testRefreshSecret,
		testAppSecret,
		testPasswordSecret,
		testEmailSecret,
		testDeviceSecret,
		testSiteSecret,
		strconv.FormatInt(stationID, 10),
	}
	allSentinels := strings.Join(secretList, " ")

	tests := []struct {
		name         string
		status       int
		body         string
		wantCategory string
	}{
		{
			name:         "non-200 response",
			status:       http.StatusUnauthorized,
			body:         fmt.Sprintf(`{"success":false,"msg":%q,"access_token":%q,"refresh_token":%q}`, allSentinels, testAccessSecret, testRefreshSecret),
			wantCategory: "http-status",
		},
		{
			name:         "API rejection",
			status:       http.StatusOK,
			body:         fmt.Sprintf(`{"success":false,"msg":%q,"access_token":%q,"refresh_token":%q}`, allSentinels, testAccessSecret, testRefreshSecret),
			wantCategory: "api-rejected",
		},
		{
			name:         "malformed success response",
			status:       http.StatusOK,
			body:         fmt.Sprintf(`{"success":true,"access_token":%q,"refresh_token":%q,"msg":%q`, testAccessSecret, testRefreshSecret, allSentinels),
			wantCategory: "decode",
		},
		{
			name:         "missing access token",
			status:       http.StatusOK,
			body:         fmt.Sprintf(`{"success":true,"access_token":"","refresh_token":%q,"msg":%q}`, testRefreshSecret, allSentinels),
			wantCategory: "missing-access-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/account/v1.0/token" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			notifier := &recordingAlertNotifier{}
			cfg := testSolarmanConfig(server.URL)
			cfg.AppSecret = testAppSecret
			cfg.PasswordSHA256 = testPasswordSecret
			cfg.Email = testEmailSecret
			cfg.DeviceSN = testDeviceSecret
			cfg.StationID = stationID
			client := New(cfg, alert.NewManager(notifier, 0))

			_, err := client.Fetch(t.Context())
			if err == nil {
				t.Fatal("Fetch() error = nil, want sanitized token failure")
			}

			status := strconv.Itoa(tt.status)
			for _, actionable := range []string{"stage=token", "status=" + status, "category=" + tt.wantCategory} {
				if !strings.Contains(err.Error(), actionable) {
					t.Errorf("Fetch() error = %q, want actionable context %q", err, actionable)
				}
				if !strings.Contains(notifier.joined(), actionable) {
					t.Errorf("alert payloads = %q, want actionable context %q", notifier.joined(), actionable)
				}
			}

			combined := err.Error() + "\n" + notifier.joined()
			for _, secret := range secretList {
				if strings.Contains(combined, secret) {
					t.Errorf("token failure output exposed sensitive sentinel %q: %q", secret, combined)
				}
			}
		})
	}
}

func TestCurrentDataFailuresNeverExposeSensitiveText(t *testing.T) {
	const upstreamMessage = "UPSTREAM_MESSAGE_SENTINEL_320b"
	tests := []struct {
		name         string
		status       int
		body         string
		wantCategory string
	}{
		{
			name:         "HTTP status body",
			status:       http.StatusTeapot,
			body:         fmt.Sprintf(`{"success":false,"msg":%q,"access_token":%q}`, upstreamMessage+" "+testEmailSecret, testAccessSecret),
			wantCategory: "http-status",
		},
		{
			name:         "API rejection message",
			status:       http.StatusOK,
			body:         fmt.Sprintf(`{"success":false,"msg":%q}`, upstreamMessage+" "+testDeviceSecret),
			wantCategory: "api-rejected",
		},
		{
			name:         "malformed response",
			status:       http.StatusOK,
			body:         fmt.Sprintf(`{"success":true,"dataList":[],"msg":%q`, upstreamMessage+" "+testRefreshSecret),
			wantCategory: "decode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureSolarmanLogs(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/account/v1.0/token":
					_, _ = w.Write([]byte(`{"success":true,"access_token":"short-lived-access","token_type":"Bearer","expires_in":"3600"}`))
				case "/device/v1.0/currentData":
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			notifier := &recordingAlertNotifier{}
			cfg := testSolarmanConfig(server.URL)
			cfg.AppID = "APP_ID_SENTINEL_72dd"
			cfg.AppSecret = testAppSecret
			cfg.Email = testEmailSecret
			cfg.PasswordSHA256 = testPasswordSecret
			cfg.DeviceSN = testDeviceSecret
			cfg.StationID = 91929394
			_, err := New(cfg, alert.NewManager(notifier, 0)).Fetch(t.Context())
			if err == nil || !strings.Contains(err.Error(), "stage=currentData") || !strings.Contains(err.Error(), "category="+tt.wantCategory) {
				t.Fatalf("Fetch() error = %v, want sanitized currentData/%s failure", err, tt.wantCategory)
			}
			if notifier.joined() == "" {
				t.Fatal("ordinary Fetch() did not emit its configured generic alert")
			}

			combined := err.Error() + "\n" + logs.String() + "\n" + notifier.joined()
			for _, sensitive := range []string{
				server.URL,
				"APP_ID_SENTINEL_72dd",
				testAppSecret,
				testPasswordSecret,
				testEmailSecret,
				testDeviceSecret,
				"91929394",
				testAccessSecret,
				testRefreshSecret,
				upstreamMessage,
			} {
				if strings.Contains(combined, sensitive) {
					t.Errorf("failure output exposed sensitive sentinel %q: %q", sensitive, combined)
				}
			}
		})
	}
}

func TestDiscoveryLogsNeverExposeStationOrDeviceIdentity(t *testing.T) {
	logs := captureSolarmanLogs(t)
	const (
		stationID   = int64(71727374)
		stationName = "STATION_NAME_SENTINEL_006e"
		deviceSN    = "DISCOVERED_DEVICE_SENTINEL_f4aa"
		appID       = "APP_ID_SENTINEL_a85c"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access-token","token_type":"Bearer","expires_in":"3600"}`))
		case "/station/v1.0/list":
			_, _ = fmt.Fprintf(w, `{"stationList":[{"id":%d,"name":%q}]}`, stationID, stationName)
		case "/station/v1.0/device":
			_, _ = fmt.Fprintf(w, `{"deviceListItems":[{"deviceSn":%q}]}`, deviceSN)
		case "/device/v1.0/currentData":
			_, _ = w.Write([]byte(`{"success":true,"dataList":[{"key":"DP1","name":"PV1","unit":"W","value":12}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := testSolarmanConfig(server.URL)
	cfg.DeviceSN = ""
	cfg.StationID = 0
	cfg.AppID = appID
	snapshot, err := New(cfg, nil).Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.DeviceSN != deviceSN {
		t.Fatalf("snapshot device serial = %q, want discovered identity retained in data model", snapshot.DeviceSN)
	}
	for _, sensitive := range []string{server.URL, appID, strconv.FormatInt(stationID, 10), stationName, deviceSN} {
		if strings.Contains(logs.String(), sensitive) {
			t.Errorf("discovery log exposed sensitive sentinel %q: %q", sensitive, logs.String())
		}
	}
}

func TestTransportFailurePreservesCauseWithoutStringifyingIt(t *testing.T) {
	logs := captureSolarmanLogs(t)
	transportCause := errors.New("PRIVATE_TRANSPORT_CAUSE_SENTINEL_17c2")
	notifier := &recordingAlertNotifier{}
	cfg := testSolarmanConfig("https://PRIVATE_ENDPOINT_SENTINEL.invalid")
	cfg.DeviceSN = testDeviceSecret
	client := New(cfg, alert.NewManager(notifier, 0))
	client.token = tokenResponse{AccessToken: "access-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}
	client.hc = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportCause
	})}

	_, err := client.Fetch(t.Context())
	if err == nil || !errors.Is(err, transportCause) || !strings.Contains(err.Error(), "category=transport") {
		t.Fatalf("Fetch() error = %v, want safe typed transport error preserving cause", err)
	}
	combined := err.Error() + "\n" + logs.String() + "\n" + notifier.joined()
	for _, sensitive := range []string{"PRIVATE_ENDPOINT_SENTINEL", transportCause.Error(), testDeviceSecret} {
		if strings.Contains(combined, sensitive) {
			t.Errorf("transport failure output exposed sensitive sentinel %q: %q", sensitive, combined)
		}
	}
}

func TestTokenRefreshFailureNeverIncludesTokenOrUnauthorizedResponse(t *testing.T) {
	unauthorizedSecret := "UNAUTHORIZED_RESPONSE_SENTINEL_b218"
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/account/v1.0/token":
			if tokenCalls.Add(1) == 1 {
				_, _ = w.Write([]byte(fmt.Sprintf(`{"success":true,"access_token":%q,"refresh_token":%q,"token_type":"Bearer","expires_in":"3600"}`, testAccessSecret, testRefreshSecret)))
				return
			}
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"success":false,"msg":%q,"access_token":%q,"refresh_token":%q}`, testAppSecret+" "+testPasswordSecret, testAccessSecret, testRefreshSecret)))
		case "/device/v1.0/currentData":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(unauthorizedSecret))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	notifier := &recordingAlertNotifier{}
	client := New(testSolarmanConfig(server.URL), alert.NewManager(notifier, 0))
	_, err := client.Fetch(t.Context())
	if err == nil {
		t.Fatal("Fetch() error = nil, want token refresh failure")
	}
	combined := err.Error() + "\n" + notifier.joined()
	for _, secret := range []string{testAccessSecret, testRefreshSecret, testAppSecret, testPasswordSecret, unauthorizedSecret, "DEVICE_SN"} {
		if strings.Contains(combined, secret) {
			t.Errorf("token refresh failure output exposed sensitive sentinel %q: %q", secret, combined)
		}
	}
	for _, actionable := range []string{"stage=token", "status=403", "category=http-status"} {
		if !strings.Contains(combined, actionable) {
			t.Errorf("token refresh failure output = %q, want actionable context %q", combined, actionable)
		}
	}
}

type recordingAlertNotifier struct {
	mu       sync.Mutex
	messages []string
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func captureSolarmanLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := log.Logger
	buffer := &bytes.Buffer{}
	log.Logger = zerolog.New(buffer)
	t.Cleanup(func() {
		log.Logger = previous
	})
	return buffer
}

func (n *recordingAlertNotifier) Notify(_ context.Context, subject string, body string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, subject+"\n"+body)
	return nil
}

func (n *recordingAlertNotifier) joined() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.messages, "\n---\n")
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
