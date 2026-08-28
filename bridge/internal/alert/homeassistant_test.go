package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestHomeAssistantNotifierSendsOneFixedSanitizedRequest(t *testing.T) {
	type receivedPayload struct {
		Title   string            `json:"title"`
		Message string            `json:"message"`
		Data    map[string]string `json:"data"`
	}
	received := make(chan receivedPayload, 1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/services/notify/mobile_app_operator_phone" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ha-test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var payload receivedPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("Decode(payload) error = %v", err)
		}
		received <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	notifier := mustHomeAssistantNotifier(t, config.HomeAssistantConfig{
		BaseURL:           server.URL,
		Token:             "ha-test-token",
		NotifyService:     "notify.mobile_app_operator_phone",
		Timeout:           time.Second,
		AllowInsecureHTTP: true,
	})
	err := notifier.Send(context.Background(), HomeAssistantNotification{
		Title:   " Modbus\r\nAlert ",
		Message: " Non-zero\x00 warning\nword ",
		Data: map[string]string{
			"tag":   " jinko\r\nalert ",
			"group": " inverter ",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	payload := <-received
	if payload.Title != "Modbus  Alert" {
		t.Fatalf("title = %q", payload.Title)
	}
	if payload.Message != "Non-zero  warning word" {
		t.Fatalf("message = %q", payload.Message)
	}
	if payload.Data["tag"] != "jinko  alert" || payload.Data["group"] != "inverter" {
		t.Fatalf("data = %#v", payload.Data)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestHomeAssistantNotifierDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	var initial atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		initial.Add(1)
		http.Redirect(w, r, target.URL+"/capture", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	notifier := mustHomeAssistantNotifier(t, testHomeAssistantConfig(redirect.URL))
	err := notifier.Notify(context.Background(), "Alert", "message")
	if err == nil || !strings.Contains(err.Error(), "status 307") {
		t.Fatalf("Notify() error = %v, want redirect rejection", err)
	}
	if initial.Load() != 1 || redirected.Load() != 0 {
		t.Fatalf("request counts initial=%d redirected=%d", initial.Load(), redirected.Load())
	}
}

func TestHomeAssistantNotifierErrorsDoNotExposeSecretsOrResponse(t *testing.T) {
	const responseSentinel = "PRIVATE_RESPONSE_SENTINEL"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(responseSentinel))
	}))
	defer server.Close()
	cfg := testHomeAssistantConfig(server.URL)
	cfg.Token = "PRIVATE_TOKEN_SENTINEL"
	notifier := mustHomeAssistantNotifier(t, cfg)

	err := notifier.Notify(context.Background(), "Alert", "message")
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("Notify() error = %v", err)
	}
	for _, secret := range []string{responseSentinel, cfg.Token, server.URL} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed %q: %v", secret, err)
		}
	}
}

func TestHomeAssistantNotifierNeverRetriesFailedPOST(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("ResponseWriter does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("Hijack() error = %v", err)
			return
		}
		_ = conn.Close()
	}))
	server.Start()
	defer server.Close()

	notifier := mustHomeAssistantNotifier(t, testHomeAssistantConfig(server.URL))
	if err := notifier.Notify(context.Background(), "Alert", "message"); err == nil {
		t.Fatal("Notify() error = nil, want closed-connection failure")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want exactly one attempt", requests.Load())
	}
}

func TestHomeAssistantNotifierPinsSingleLabelDNSLookupToPrivateAddresses(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(server.URL) error = %v", err)
	}
	cfg := testHomeAssistantConfig("http://homeassistant:" + parsed.Port())
	dialer := &net.Dialer{Timeout: time.Second}
	var lookups atomic.Int32
	privateLookup := func(_ context.Context, network string, host string) ([]netip.Addr, error) {
		lookups.Add(1)
		if network != "ip" || host != "homeassistant" {
			t.Fatalf("lookup = %q/%q", network, host)
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	notifier, err := newHomeAssistantNotifierWithNetwork(cfg, privateLookup, dialer.DialContext)
	if err != nil {
		t.Fatalf("newHomeAssistantNotifierWithNetwork() error = %v", err)
	}
	if err := notifier.Notify(context.Background(), "Alert", "message"); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if lookups.Load() != 1 || requests.Load() != 1 {
		t.Fatalf("lookups=%d requests=%d, want 1/1", lookups.Load(), requests.Load())
	}

	mixedLookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("127.0.0.1"),
			netip.MustParseAddr("203.0.113.20"),
		}, nil
	}
	unsafeNotifier, err := newHomeAssistantNotifierWithNetwork(cfg, mixedLookup, dialer.DialContext)
	if err != nil {
		t.Fatalf("construct unsafe-resolution notifier error = %v", err)
	}
	err = unsafeNotifier.Notify(context.Background(), "Alert", "message")
	if err == nil {
		t.Fatal("Notify() error = nil, want mixed public DNS rejection")
	}
	if requests.Load() != 1 {
		t.Fatalf("public DNS result reached server; requests=%d", requests.Load())
	}
	if strings.Contains(err.Error(), "203.0.113.20") || strings.Contains(err.Error(), "homeassistant") {
		t.Fatalf("network error exposed destination: %v", err)
	}
}

func TestSanitizeHomeAssistantNotificationBoundsAndValidatesData(t *testing.T) {
	payload, err := sanitizeHomeAssistantNotification(HomeAssistantNotification{
		Title:   strings.Repeat("і", maxNotificationTitleRunes+10),
		Message: strings.Repeat("м", maxNotificationMessageRunes+10),
		Data:    map[string]string{"tag": strings.Repeat("д", maxNotificationDataValueRunes+10)},
	})
	if err != nil {
		t.Fatalf("sanitizeHomeAssistantNotification() error = %v", err)
	}
	if utf8.RuneCountInString(payload.Title) != maxNotificationTitleRunes || utf8.RuneCountInString(payload.Message) != maxNotificationMessageRunes {
		t.Fatalf("title/message rune counts = %d/%d", utf8.RuneCountInString(payload.Title), utf8.RuneCountInString(payload.Message))
	}
	if utf8.RuneCountInString(payload.Data["tag"]) != maxNotificationDataValueRunes {
		t.Fatalf("data rune count = %d", utf8.RuneCountInString(payload.Data["tag"]))
	}

	for name, notification := range map[string]HomeAssistantNotification{
		"empty message":    {Message: "\x00\n"},
		"unsafe data key":  {Message: "message", Data: map[string]string{"../service": "value"}},
		"too many entries": {Message: "message", Data: makeDataEntries(maxNotificationDataEntries + 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := sanitizeHomeAssistantNotification(notification); err == nil {
				t.Fatal("sanitizeHomeAssistantNotification() error = nil")
			}
		})
	}
}

func TestNewHomeAssistantNotifierRejectsUnsafeDirectConfig(t *testing.T) {
	tests := []config.HomeAssistantConfig{
		{BaseURL: "http://203.0.113.20:8123", Token: "token", NotifyService: "mobile_app_phone", Timeout: time.Second, AllowInsecureHTTP: true},
		{BaseURL: "https://ha.example.test", Token: "token", NotifyService: "persistent_notification", Timeout: time.Second},
		{BaseURL: "https://ha.example.test/path", Token: "token", NotifyService: "mobile_app_phone", Timeout: time.Second},
	}
	for _, cfg := range tests {
		if _, err := NewHomeAssistantNotifier(cfg); err == nil {
			t.Fatalf("NewHomeAssistantNotifier(%+v) error = nil", cfg)
		}
	}
}

func mustHomeAssistantNotifier(t *testing.T, cfg config.HomeAssistantConfig) *HomeAssistantNotifier {
	t.Helper()
	notifier, err := NewHomeAssistantNotifier(cfg)
	if err != nil {
		t.Fatalf("NewHomeAssistantNotifier() error = %v", err)
	}
	return notifier
}

func testHomeAssistantConfig(baseURL string) config.HomeAssistantConfig {
	return config.HomeAssistantConfig{
		BaseURL:           baseURL,
		Token:             "ha-test-token",
		NotifyService:     "mobile_app_test_phone",
		Timeout:           time.Second,
		AllowInsecureHTTP: true,
	}
}

func makeDataEntries(count int) map[string]string {
	values := make(map[string]string, count)
	for i := range count {
		values[fmt.Sprintf("key%d", i)] = "value"
	}
	return values
}
