package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
