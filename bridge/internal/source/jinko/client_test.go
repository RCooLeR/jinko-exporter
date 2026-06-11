package jinko

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestFetchSendsAuthHeaderAndDoesNotRetryAuthFailure(t *testing.T) {
	var calls atomic.Int32
	var authorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		authorization = r.Header.Get("Authorization")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(testJinkoConfig(server.URL), nil)
	if _, err := client.Fetch(t.Context()); err == nil {
		t.Fatal("Fetch() error = nil, want auth failure")
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if authorization != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", authorization)
	}
}

func TestFetchRetriesServerErrors(t *testing.T) {
	fixture, err := os.ReadFile("../../../testdata/jinko_detail_response.json")
	if err != nil {
		t.Fatalf("ReadFile fixture error = %v", err)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := New(testJinkoConfig(server.URL), nil)
	snapshot, err := client.Fetch(t.Context())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	if snapshot.Source != "jinko" || snapshot.DeviceID != "100000001" || snapshot.SiteID != "200000001" {
		t.Fatalf("snapshot source/ids = %q/%q/%q", snapshot.Source, snapshot.DeviceID, snapshot.SiteID)
	}
}

func TestReadResponseBodyRejectsOversizedBody(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxHTTPResponseBodyBytes+1))
	if _, err := readResponseBody(body); err == nil {
		t.Fatal("readResponseBody() error = nil, want oversized body error")
	}
}

func testJinkoConfig(url string) config.JinkoConfig {
	return config.JinkoConfig{
		URL:              url,
		Timeout:          time.Second,
		RetryAttempts:    3,
		RetryBackoff:     0,
		DeviceID:         100000001,
		SiteID:           200000001,
		Language:         "en",
		NeedRealtimeData: true,
		BearerToken:      "Bearer test-token",
		UserAgent:        "jinko-test",
	}
}
