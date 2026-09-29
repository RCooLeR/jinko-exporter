package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/solarman"
)

func TestDiagnosticSolarmanFailureSuppressesGenericNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/account/v1.0/token":
			_, _ = w.Write([]byte(`{"success":true,"access_token":"access","token_type":"Bearer","expires_in":"3600"}`))
		case "/device/v1.0/currentData":
			http.Error(w, "private upstream body", http.StatusBadRequest)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	recorder := &notificationRecorder{}
	client := solarman.New(config.SolarmanConfig{
		BaseURL:        server.URL,
		APIVersion:     "v1.0",
		Language:       "en",
		Timeout:        time.Second,
		AppID:          "app-id",
		AppSecret:      "app-secret",
		Email:          "operator@example.test",
		PasswordSHA256: "password-hash",
		DeviceSN:       "DEVICE",
	}, alert.NewManager(recorder, 0))

	runDiagnosticJob(t, staticSource{name: "jinko", snapshot: cloudSnapshot("jinko")}, client)
	if recorder.Count() != 0 {
		t.Fatalf("diagnostic notifications = %d, want 0", recorder.Count())
	}
	if _, err := client.Fetch(t.Context()); err == nil {
		t.Fatal("ordinary Solarman Fetch() error = nil, want request failure")
	}
	if recorder.Count() == 0 {
		t.Fatal("ordinary Solarman Fetch() did not retain generic notification behavior")
	}
}

func TestDiagnosticJinkoAuthFailureSuppressesGenericNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private upstream body", http.StatusUnauthorized)
	}))
	defer server.Close()

	recorder := &notificationRecorder{}
	client := jinko.New(config.JinkoConfig{
		URL:              server.URL + "/detail",
		Timeout:          time.Second,
		RetryAttempts:    1,
		DeviceID:         111,
		SiteID:           222,
		Language:         "en",
		NeedRealtimeData: true,
		BearerToken:      "opaque-access",
	}, alert.NewManager(recorder, 0))

	runDiagnosticJob(t, client, staticSource{name: "solarman", snapshot: cloudSnapshot("solarman")})
	if recorder.Count() != 0 {
		t.Fatalf("diagnostic notifications = %d, want 0", recorder.Count())
	}
	if _, err := client.Fetch(t.Context()); err == nil {
		t.Fatal("ordinary Jinko Fetch() error = nil, want authentication failure")
	}
	if recorder.Count() == 0 {
		t.Fatal("ordinary Jinko Fetch() did not retain generic authentication notification behavior")
	}
}

func runDiagnosticJob(t *testing.T, jinkoSource source.Source, solarmanSource source.Source) {
	t.Helper()
	priority := source.NewPriority([]source.Source{
		staticSource{name: "modbus", snapshot: activeModbusSnapshot()},
		jinkoSource,
		solarmanSource,
	}, false)
	evidence := make(chan struct{}, 1)
	if err := priority.ConfigureAlertCorrelation(source.AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    2 * time.Second,
		RecordEvidence: func(context.Context, source.AlertCorrelationEvidence) error {
			evidence <- struct{}{}
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		priority.RunBackground(ctx)
		close(done)
	}()
	if _, err := priority.Fetch(t.Context()); err != nil {
		cancel()
		t.Fatalf("Priority Fetch() error = %v", err)
	}
	select {
	case <-evidence:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for diagnostic evidence")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out stopping diagnostic worker")
	}
}

type staticSource struct {
	name     string
	snapshot *model.Snapshot
}

func (s staticSource) Name() string { return s.name }

func (s staticSource) Fetch(context.Context) (*model.Snapshot, error) {
	copy := *s.snapshot
	copy.Metrics = append([]model.Metric(nil), s.snapshot.Metrics...)
	return &copy, nil
}

func activeModbusSnapshot() *model.Snapshot {
	keys := []string{
		"DEYE_MODBUS_R553_WARNING_WORD_1_RAW",
		"DEYE_MODBUS_R554_WARNING_WORD_2_RAW",
		"DEYE_MODBUS_R555_FAULT_WORD_1_RAW",
		"DEYE_MODBUS_R556_FAULT_WORD_2_RAW",
		"DEYE_MODBUS_R557_FAULT_WORD_3_RAW",
		"DEYE_MODBUS_R558_FAULT_WORD_4_RAW",
	}
	metrics := make([]model.Metric, len(keys))
	for index, key := range keys {
		metrics[index] = model.Metric{Group: "alert", Key: key}
	}
	metrics[0].Value = 1
	return &model.Snapshot{Source: "modbus", DeviceSN: "DEVICE", CollectedAt: time.Now().UTC(), Metrics: metrics}
}

func cloudSnapshot(name string) *model.Snapshot {
	return &model.Snapshot{
		Source:      name,
		DeviceSN:    "DEVICE",
		CollectedAt: time.Now().UTC(),
		Metrics:     []model.Metric{{Group: "alert", Key: "ALERT", Value: 0}},
	}
}

type notificationRecorder struct {
	mu    sync.Mutex
	calls int
}

func (r *notificationRecorder) Notify(context.Context, string, string) error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return nil
}

func (r *notificationRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
