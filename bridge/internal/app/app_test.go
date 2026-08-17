package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
)

func TestRunHealthcheckCommandDerivesURLFromListenAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path = %q, want /healthz", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exitCode := Run([]string{"jinko-exporter", "--listen", server.Listener.Addr().String(), "healthcheck", "--timeout", "1s"})
	if exitCode != 0 {
		t.Fatalf("Run(healthcheck) exit code = %d, want 0", exitCode)
	}
}

func TestRunHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := runHealthcheck(context.Background(), server.URL, time.Second); err != nil {
		t.Fatalf("runHealthcheck() error = %v", err)
	}
}

func TestRunHealthcheckFailsOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := runHealthcheck(context.Background(), server.URL, time.Second); err == nil {
		t.Fatal("runHealthcheck() error = nil, want bad-status error")
	}
}

func TestDefaultHealthcheckURL(t *testing.T) {
	tests := []struct {
		name    string
		listen  string
		want    string
		wantErr bool
	}{
		{
			name:   "port only",
			listen: ":9876",
			want:   "http://127.0.0.1:9876/healthz",
		},
		{
			name:   "wildcard ipv4",
			listen: "0.0.0.0:9877",
			want:   "http://127.0.0.1:9877/healthz",
		},
		{
			name:   "wildcard ipv6",
			listen: "[::]:9878",
			want:   "http://127.0.0.1:9878/healthz",
		},
		{
			name:   "loopback ipv6",
			listen: "[::1]:9879",
			want:   "http://[::1]:9879/healthz",
		},
		{
			name:    "invalid",
			listen:  "9876",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defaultHealthcheckURL(tt.listen)
			if tt.wantErr {
				if err == nil {
					t.Fatal("defaultHealthcheckURL() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultHealthcheckURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("defaultHealthcheckURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewHTTPServerSetsBoundedConnectionLimits(t *testing.T) {
	handler := http.NewServeMux()
	server := newHTTPServer("127.0.0.1:9876", handler)

	if server.Addr != "127.0.0.1:9876" || server.Handler != handler {
		t.Fatalf("server address/handler = %q/%T, want configured values", server.Addr, server.Handler)
	}
	if server.ReadHeaderTimeout != httpReadHeaderTimeout || server.ReadTimeout != httpReadTimeout ||
		server.WriteTimeout != httpWriteTimeout || server.IdleTimeout != httpIdleTimeout {
		t.Fatalf("server timeouts = header %s read %s write %s idle %s", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatal("all HTTP server timeouts must be bounded and positive")
	}
	if server.MaxHeaderBytes != httpMaxHeaderBytes || server.MaxHeaderBytes <= 0 {
		t.Fatalf("server MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, httpMaxHeaderBytes)
	}
}

func TestBuildSourcePreservesModbusJinkoSolarmanPriorityOrder(t *testing.T) {
	cfg := config.Config{
		SourcePriority: []string{"modbus", "jinko", "solarman"},
		Jinko: config.JinkoConfig{
			URL:           "https://jinko.example.test/detail",
			Timeout:       time.Second,
			RetryAttempts: 1,
			DeviceID:      100,
			SiteID:        200,
			BearerToken:   "opaque-test-token",
		},
		Solarman: config.SolarmanConfig{
			BaseURL:    "https://solarman.example.test",
			APIVersion: "v1.0",
			Timeout:    time.Second,
			AppID:      "app-id",
			AppSecret:  "app-secret",
			Email:      "solar@example.test",
			Password:   "password",
			DeviceSN:   "INVERTER_SN_EXAMPLE",
		},
		Modbus: config.ModbusConfig{
			Host:         "192.168.50.25",
			Port:         8899,
			LoggerSerial: "305419896",
			DeviceSN:     "INVERTER_SN_EXAMPLE",
			UnitID:       1,
			Timeout:      5 * time.Second,
		},
	}

	src, err := buildSource(cfg, nil)
	if err != nil {
		t.Fatalf("buildSource() error = %v", err)
	}
	if src.Name() != "modbus,jinko,solarman" {
		t.Fatalf("source name/order = %q, want modbus,jinko,solarman", src.Name())
	}
	if _, ok := src.(source.BackgroundMaintainer); !ok {
		t.Fatalf("mixed priority source type %T does not propagate background maintenance", src)
	}
}

func TestStartBackgroundMaintenanceRunsAndWaitsForCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stub := &maintenanceSource{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	done := startBackgroundMaintenance(ctx, stub)
	select {
	case <-stub.started:
	case <-time.After(time.Second):
		t.Fatal("background maintenance did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background maintenance did not stop after cancellation")
	}
	select {
	case <-stub.stopped:
	default:
		t.Fatal("background maintainer did not observe context cancellation")
	}
}

func TestStartBackgroundMaintenanceIsNoopForOrdinarySource(t *testing.T) {
	done := startBackgroundMaintenance(context.Background(), ordinarySource{})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ordinary source returned a blocking maintenance handle")
	}
}

func TestRunServeBindsListenerBeforeStartingSourceWork(t *testing.T) {
	var detailCalls atomic.Int32
	detailServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		detailCalls.Add(1)
	}))
	defer detailServer.Close()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve listener: %v", err)
	}
	defer occupied.Close()
	mqttBroker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start fake MQTT broker: %v", err)
	}
	defer mqttBroker.Close()

	cfg := config.Config{
		SourcePriority: []string{"jinko"},
		ListenAddress:  occupied.Addr().String(),
		MetricsPath:    "/metrics",
		MetricPrefix:   "solar",
		PollInterval:   time.Minute,
		MQTT: config.MQTTConfig{
			Enabled:         true,
			Broker:          "tcp://" + mqttBroker.Addr().String(),
			ClientID:        "bind-first-test",
			TopicPrefix:     "test/jinko",
			DiscoveryPrefix: "homeassistant",
			Timeout:         100 * time.Millisecond,
		},
		Jinko: config.JinkoConfig{
			URL:           detailServer.URL,
			Timeout:       time.Second,
			RetryAttempts: 1,
			DeviceID:      100,
			SiteID:        200,
			BearerToken:   "opaque-test-token",
		},
	}
	if err := runServe(context.Background(), cfg); err == nil {
		t.Fatal("runServe() error = nil, want occupied-listener failure before polling")
	}
	time.Sleep(20 * time.Millisecond)
	if got := detailCalls.Load(); got != 0 {
		t.Fatalf("Jinko detail calls = %d, want zero when listener bind fails", got)
	}
	tcpBroker, ok := mqttBroker.(*net.TCPListener)
	if !ok {
		t.Fatalf("fake MQTT listener type = %T, want *net.TCPListener", mqttBroker)
	}
	if err := tcpBroker.SetDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatalf("set fake MQTT broker deadline: %v", err)
	}
	conn, acceptErr := tcpBroker.Accept()
	if acceptErr == nil {
		_ = conn.Close()
		t.Fatal("MQTT connection was attempted before the HTTP listener bind succeeded")
	}
	if netErr, ok := acceptErr.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("fake MQTT broker Accept() error = %v, want timeout with zero connections", acceptErr)
	}
}

func TestRunServeWaitsForFetchInitiatedTokenRotationOnShutdown(t *testing.T) {
	tokenRequestStarted := make(chan struct{})
	releaseTokenResponse := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/detail":
			w.WriteHeader(http.StatusUnauthorized)
		case "/oauth/token":
			refreshCalls.Add(1)
			startedOnce.Do(func() { close(tokenRequestStarted) })
			<-releaseTokenResponse
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "rotated-access",
				"refresh_token": "rotated-refresh",
				"expires_in":    3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseTokenResponse) })
		server.Close()
	})

	statePath := filepath.Join(t.TempDir(), "jinko-token-state.json")
	cfg := config.Config{
		SourcePriority: []string{"jinko"},
		ListenAddress:  "127.0.0.1:0",
		MetricsPath:    "/metrics",
		MetricPrefix:   "solar",
		PollInterval:   time.Minute,
		Jinko: config.JinkoConfig{
			URL:            server.URL + "/detail",
			TokenURL:       server.URL + "/oauth/token",
			Timeout:        2 * time.Second,
			RetryAttempts:  1,
			DeviceID:       100,
			SiteID:         200,
			BearerToken:    appTestJWT(t, time.Now().Add(time.Hour)),
			RefreshToken:   "bootstrap-refresh",
			TokenStateFile: statePath,
			RefreshBefore:  5 * time.Minute,
			System:         "JinKO",
			Area:           "FOREIGN_1",
		},
	}
	parent, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runServe(parent, cfg) }()

	select {
	case <-tokenRequestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Fetch did not reach the token endpoint")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("runServe returned before the consumed-token response was released: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseTokenResponse) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runServe() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runServe did not join the Fetch-initiated token transaction")
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read rotated token state: %v", err)
	}
	var state struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode rotated token state: %v", err)
	}
	if state.AccessToken != "rotated-access" || state.RefreshToken != "rotated-refresh" || refreshCalls.Load() != 1 {
		t.Fatalf("rotated state/calls = %#v/%d, want durable new pair and one refresh", state, refreshCalls.Load())
	}
}

func TestStopServeWorkersClosesPublisherAfterWorkersExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	maintenanceDone := make(chan struct{})
	runnerDone := make(chan struct{})
	publisherClosed := make(chan struct{})
	var closeCalls atomic.Int32
	cleanupDone := make(chan struct{})
	go func() {
		stopServeWorkers(cancel, maintenanceDone, runnerDone, func() {
			closeCalls.Add(1)
			close(publisherClosed)
		})
		close(cleanupDone)
	}()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel workers")
	}
	select {
	case <-publisherClosed:
		t.Fatal("publisher closed before maintenance and runner exited")
	default:
	}

	close(maintenanceDone)
	select {
	case <-publisherClosed:
		t.Fatal("publisher closed before runner exited")
	default:
	}

	close(runnerDone)
	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after workers exited")
	}
	if got := closeCalls.Load(); got != 1 {
		t.Fatalf("publisher close calls = %d, want 1", got)
	}
}

func appTestJWT(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]int64{"exp": expiresAt.Unix()})
	if err != nil {
		t.Fatalf("encode JWT payload: %v", err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

type maintenanceSource struct {
	started chan struct{}
	stopped chan struct{}
}

func (*maintenanceSource) Name() string { return "maintenance" }

func (*maintenanceSource) Fetch(context.Context) (*model.Snapshot, error) {
	return &model.Snapshot{Source: "maintenance"}, nil
}

func (s *maintenanceSource) RunBackground(ctx context.Context) {
	close(s.started)
	<-ctx.Done()
	close(s.stopped)
}

type ordinarySource struct{}

func (ordinarySource) Name() string { return "ordinary" }

func (ordinarySource) Fetch(context.Context) (*model.Snapshot, error) {
	return &model.Snapshot{Source: "ordinary"}, nil
}
