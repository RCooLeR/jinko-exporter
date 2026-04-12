package jinko

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type timeoutError string

func (e timeoutError) Error() string {
	return string(e)
}

func (e timeoutError) Timeout() bool {
	return true
}

func (e timeoutError) Temporary() bool {
	return true
}

func TestDoDetailRequestRetriesTLSHandshakeTimeout(t *testing.T) {
	const responseBody = `{"success":true}`

	attempts := 0
	client := &Client{
		cfg: config.JinkoConfig{
			URL:           "https://example.test/device-s/device/v3/detail",
			RetryAttempts: 2,
			RetryBackoff:  0,
			BearerToken:   "token",
		},
		hc: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return nil, timeoutError("net/http: TLS handshake timeout")
				}
				if auth := req.Header.Get("Authorization"); auth != "Bearer token" {
					t.Fatalf("Authorization header = %q, want %q", auth, "Bearer token")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(responseBody)),
					Request:    req,
				}, nil
			}),
		},
	}

	raw, status, err := client.doDetailRequestWithRetry(context.Background(), []byte(`{"deviceId":1}`))
	if err != nil {
		t.Fatalf("doDetailRequestWithRetry() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if string(raw) != responseBody {
		t.Fatalf("raw = %q, want %q", string(raw), responseBody)
	}
}

func TestDoDetailRequestDoesNotRetryNonTransientError(t *testing.T) {
	attempts := 0
	client := &Client{
		cfg: config.JinkoConfig{
			URL:           "https://example.test/device-s/device/v3/detail",
			RetryAttempts: 3,
			RetryBackoff:  0,
			BearerToken:   "token",
		},
		hc: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				return nil, errors.New("x509: certificate has expired or is not yet valid")
			}),
		},
	}

	_, _, err := client.doDetailRequestWithRetry(context.Background(), []byte(`{"deviceId":1}`))
	if err == nil {
		t.Fatal("doDetailRequestWithRetry() error = nil, want error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestDoDetailRequestStopsRetryingWhenContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	client := &Client{
		cfg: config.JinkoConfig{
			URL:           "https://example.test/device-s/device/v3/detail",
			RetryAttempts: 3,
			RetryBackoff:  10 * time.Second,
			BearerToken:   "token",
		},
		hc: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				cancel()
				return nil, timeoutError("net/http: TLS handshake timeout")
			}),
		},
	}

	_, _, err := client.doDetailRequestWithRetry(ctx, []byte(`{"deviceId":1}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("doDetailRequestWithRetry() error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
