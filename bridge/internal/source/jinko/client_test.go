package jinko

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

func TestRandomRequestJitterHandlesBoundaryDurations(t *testing.T) {
	if got := randomRequestJitter(-time.Nanosecond); got != 0 {
		t.Fatalf("randomRequestJitter(-1ns) = %s, want 0", got)
	}
	if got := randomRequestJitter(0); got != 0 {
		t.Fatalf("randomRequestJitter(0) = %s, want 0", got)
	}
	for range 100 {
		if got := randomRequestJitter(time.Nanosecond); got < 0 || got > time.Nanosecond {
			t.Fatalf("randomRequestJitter(1ns) = %s, want [0,1ns]", got)
		}
	}
	maximum := time.Duration(1<<63 - 1)
	if got := randomRequestJitter(maximum); got < 0 || got > maximum {
		t.Fatalf("randomRequestJitter(max duration) = %s, want non-negative bounded duration", got)
	}
}

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

func TestBearerOnly401DoesNotLeakResponseBody(t *testing.T) {
	const responseSecret = "secret-returned-by-upstream"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error_description":"`+responseSecret+`"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	client := New(testJinkoConfig(server.URL), alert.NewManager(notifier, 0))
	_, err := client.Fetch(t.Context())
	if err == nil {
		t.Fatal("Fetch() error = nil, want auth failure")
	}
	if strings.Contains(err.Error(), responseSecret) || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("Fetch() error leaked authentication data: %q", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	for _, body := range notifier.Bodies() {
		if strings.Contains(body, responseSecret) || strings.Contains(body, "test-token") {
			t.Fatalf("alert leaked authentication data: %q", body)
		}
	}
}

func TestFetchProactivelyRefreshesTokenWithOfficialForm(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(30*time.Second), "old")
	newAccess := testJWT(t, time.Now().Add(time.Hour), "new")
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("refresh Authorization = %q, want empty", got)
			}
			if got := r.Header.Get("Cookie"); got != "" {
				t.Errorf("refresh Cookie = %q, want no detail-session cookie", got)
			}
			if got := r.Header.Get("User-Agent"); got != "jinko-test" {
				t.Errorf("refresh User-Agent = %q", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
				t.Errorf("refresh Content-Type = %q", got)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			want := map[string]string{
				"grant_type":    "refresh_token",
				"refresh_token": "refresh-old",
				"client_id":     "test",
				"system":        "JinKO",
				"area":          "FOREIGN_1",
				"origin_id":     "origin-test",
			}
			for key, value := range want {
				if got := r.Form.Get(key); got != value {
					t.Errorf("refresh form[%q] = %q, want %q", key, got, value)
				}
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "refresh-new"})
		case "/detail":
			detailCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+newAccess {
				t.Errorf("detail Authorization = %q, want refreshed bearer", got)
			}
			if got := r.Header.Get("Cookie"); got != "session=test-cookie" {
				t.Errorf("detail Cookie = %q, want configured detail-session cookie", got)
			}
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.Cookie = "session=test-cookie"
	cfg.OriginID = "origin-test"
	notifier := &recordingNotifier{}
	client := New(cfg, alert.NewManager(notifier, 0))
	if _, err := client.Fetch(t.Context()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls = %d, want 1", got)
	}
	if bodies := notifier.Bodies(); len(bodies) != 0 {
		t.Fatalf("alerts = %q, want none after successful automatic refresh", bodies)
	}
}

func TestRefreshTransportNeverInheritsDetailTLSBypass(t *testing.T) {
	cfg := testJinkoConfig("https://detail.example.test")
	cfg.InsecureSkipVerify = true
	client := New(cfg, nil)

	detailTransport, ok := client.hc.Transport.(*http.Transport)
	if !ok || detailTransport.TLSClientConfig == nil || !detailTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("detail transport did not preserve configured TLS bypass")
	}
	tokenTransport, ok := client.tokenHC.Transport.(*http.Transport)
	if !ok {
		t.Fatal("token transport is not an HTTP transport")
	}
	if tokenTransport.TLSClientConfig != nil && tokenTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("token transport inherited insecure TLS verification bypass")
	}
}

func TestFetchRefreshesAndRetriesDetailOnceAfter401(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(time.Hour), "old")
	newAccess := testJWT(t, time.Now().Add(2*time.Hour), "new")
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess})
		case "/detail":
			call := detailCalls.Add(1)
			if call == 1 {
				http.Error(w, "sensitive-detail-body", http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+newAccess {
				t.Errorf("retry Authorization = %q, want refreshed bearer", got)
			}
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.RefreshBefore = 0
	client := New(cfg, nil)
	if _, err := client.Fetch(t.Context()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := detailCalls.Load(); got != 2 {
		t.Fatalf("detail calls = %d, want 2", got)
	}
}

func Test401RefreshSurfacesFinalPersistenceFailureAndBackgroundRepairsWithoutOAuth(t *testing.T) {
	oldAccess := testJWT(t, time.Now().Add(time.Hour), "old")
	newAccess := testJWT(t, time.Now().Add(2*time.Hour), "new")
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	statePath := filepath.Join(stateDir, "jinko-token-state.json")
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			// The preflight write has succeeded. Simulate the writable mount
			// disappearing before the rotated response can be persisted.
			if err := os.Remove(statePath); err != nil {
				t.Errorf("Remove(state) error = %v", err)
			}
			if err := os.Remove(stateDir); err != nil {
				t.Errorf("Remove(state dir) error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			detailCalls.Add(1)
			http.Error(w, "expired", http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.RefreshBefore = 0
	cfg.TokenStateFile = statePath
	notifier := &recordingNotifier{}
	client := New(cfg, alert.NewManager(notifier, 0))
	if _, err := client.Fetch(t.Context()); err == nil || !isTokenStateDurabilityError(err) {
		t.Fatalf("Fetch() error = %v, want typed durability failure", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls = %d, want no success after undurable rotation", got)
	}
	if bodies := notifier.Bodies(); len(bodies) != 1 {
		t.Fatalf("refresh persistence alerts = %d, want 1", len(bodies))
	}
	access, _, _, _ := client.tokenSnapshot()
	if access != oldAccess {
		t.Fatalf("active access token changed before rotated state became durable")
	}

	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("restore state directory error = %v", err)
	}
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		client.RunBackground(backgroundCtx)
		close(backgroundDone)
	}()
	waitForCondition(t, func() bool {
		state, err := loadTokenState(statePath)
		access, _, _, _ := client.tokenSnapshot()
		return err == nil && state.AccessToken == newAccess && state.RefreshToken == "rotated-refresh" &&
			!state.RefreshOutcomeUncertain && access == newAccess
	})
	access, _, _, _ = client.tokenSnapshot()
	if access != newAccess {
		t.Fatalf("durably repaired token state was not promoted to the active bearer")
	}
	cancelBackground()
	waitForSignal(t, backgroundDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls after durability repair = %d, want no second OAuth call", got)
	}
}

func TestRefreshRequiresDurableStateBeforeConsumingToken(t *testing.T) {
	expiredAccess := testJWT(t, time.Now().Add(-time.Minute), "expired")
	var upstreamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Run("missing state path", func(t *testing.T) {
		cfg := refreshJinkoConfig(t, server.URL, expiredAccess)
		cfg.TokenStateFile = ""
		_, err := New(cfg, nil).Fetch(t.Context())
		if err == nil || !strings.Contains(err.Error(), "requires a token state file") {
			t.Fatalf("Fetch() error = %v, want durable-state requirement", err)
		}
	})

	t.Run("unwritable state path", func(t *testing.T) {
		cfg := refreshJinkoConfig(t, server.URL, expiredAccess)
		cfg.TokenStateFile = filepath.Join(t.TempDir(), "missing", "token-state.json")
		_, err := New(cfg, nil).Fetch(t.Context())
		if err == nil || !strings.Contains(err.Error(), "prepare Jinko token state") {
			t.Fatalf("Fetch() error = %v, want preflight persistence failure", err)
		}
	})

	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("upstream calls = %d, want 0 before durable-state preflight", got)
	}
}

func TestRefreshBootstrapsFromRefreshOnlyState(t *testing.T) {
	fixture := readDetailFixture(t)
	newAccess := testJWT(t, time.Now().Add(time.Hour), "new")
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "state-refresh" {
				t.Errorf("refresh token = %q, want state token", got)
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "token-state.json")
	if err := persistTokenState(statePath, tokenState{RefreshToken: "state-refresh", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("persistTokenState() error = %v", err)
	}
	cfg := refreshJinkoConfig(t, server.URL, "")
	cfg.RefreshToken = ""
	cfg.TokenStateFile = statePath
	if _, err := New(cfg, nil).Fetch(t.Context()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestRefreshRotatesPersistsAndReloadsNewerState(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(30*time.Second), "old")
	newAccess := testJWT(t, time.Now().Add(2*time.Hour), "new")
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	var refreshCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			if got := r.Header.Get("Authorization"); got != "Bearer "+newAccess {
				t.Errorf("detail Authorization = %q, want persisted bearer", got)
			}
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.TokenStateFile = statePath
	if _, err := New(cfg, nil).Fetch(t.Context()); err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	state, err := loadTokenState(statePath)
	if err != nil {
		t.Fatalf("loadTokenState() error = %v", err)
	}
	if state.AccessToken != newAccess || state.RefreshToken != "rotated-refresh" {
		t.Fatalf("persisted token state did not contain rotated credentials")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(statePath)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("state mode = %o, want 600", got)
		}
	}

	// The persisted access token has a later signed expiry and must win over stale config.
	client := New(cfg, nil)
	if _, err := client.Fetch(t.Context()); err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want state reload to avoid a second refresh", got)
	}
}

func TestTokenStateAndManualBootstrapPrecedence(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name              string
		configuredAccess  string
		configuredRefresh string
		stateAccess       string
		stateRefresh      string
		wantAccess        string
		wantRefresh       string
	}{
		{
			name:              "newer rotated state wins stale bootstrap",
			configuredAccess:  testJWT(t, now.Add(time.Hour), "configured-access"),
			configuredRefresh: testJWT(t, now.Add(30*24*time.Hour), "configured-refresh"),
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "state-access"),
			stateRefresh:      testJWT(t, now.Add(60*24*time.Hour), "state-refresh"),
		},
		{
			name:              "newer manual pair wins stale state",
			configuredAccess:  testJWT(t, now.Add(3*time.Hour), "configured-access"),
			configuredRefresh: testJWT(t, now.Add(90*24*time.Hour), "configured-refresh"),
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "state-access"),
			stateRefresh:      testJWT(t, now.Add(60*24*time.Hour), "state-refresh"),
		},
		{
			name:              "same access keeps rotated opaque refresh",
			configuredAccess:  testJWT(t, now.Add(2*time.Hour), "same-access"),
			configuredRefresh: "bootstrap-opaque-refresh",
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "same-access"),
			stateRefresh:      "rotated-opaque-refresh",
		},
		{
			name:              "newer manual access keeps its opaque refresh pair",
			configuredAccess:  testJWT(t, now.Add(3*time.Hour), "configured-newer-access"),
			configuredRefresh: "Rnew-opaque",
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "state-older-access"),
			stateRefresh:      "Rold-opaque",
		},
		{
			name:              "state access winner fills missing refresh from configuration",
			configuredAccess:  testJWT(t, now.Add(time.Hour), "configured-older-access"),
			configuredRefresh: "configured-refresh-only",
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "state-newer-access"),
			stateRefresh:      "",
		},
		{
			name:              "configured access fills missing refresh from refresh-only state",
			configuredAccess:  testJWT(t, now.Add(3*time.Hour), "configured-access-only"),
			configuredRefresh: "",
			stateAccess:       "",
			stateRefresh:      "state-refresh-only",
		},
		{
			name:              "configured complete pair is not overwritten by refresh-only state",
			configuredAccess:  testJWT(t, now.Add(3*time.Hour), "configured-complete-access"),
			configuredRefresh: "configured-complete-refresh",
			stateAccess:       "",
			stateRefresh:      "conflicting-state-refresh",
		},
		{
			name:              "state complete pair fills empty configuration",
			configuredAccess:  "",
			configuredRefresh: "",
			stateAccess:       testJWT(t, now.Add(2*time.Hour), "state-complete-access"),
			stateRefresh:      "state-complete-refresh",
		},
	}
	tests[0].wantAccess, tests[0].wantRefresh = tests[0].stateAccess, tests[0].stateRefresh
	tests[1].wantAccess, tests[1].wantRefresh = tests[1].configuredAccess, tests[1].configuredRefresh
	tests[2].wantAccess, tests[2].wantRefresh = tests[2].stateAccess, tests[2].stateRefresh
	tests[3].wantAccess, tests[3].wantRefresh = tests[3].configuredAccess, tests[3].configuredRefresh
	tests[4].wantAccess, tests[4].wantRefresh = tests[4].stateAccess, tests[4].configuredRefresh
	tests[5].wantAccess, tests[5].wantRefresh = tests[5].configuredAccess, tests[5].stateRefresh
	tests[6].wantAccess, tests[6].wantRefresh = tests[6].configuredAccess, tests[6].configuredRefresh
	tests[7].wantAccess, tests[7].wantRefresh = tests[7].stateAccess, tests[7].stateRefresh

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "token-state.json")
			if err := persistTokenState(statePath, tokenState{
				AccessToken:  tc.stateAccess,
				RefreshToken: tc.stateRefresh,
				UpdatedAt:    now.UTC(),
			}); err != nil {
				t.Fatalf("persistTokenState() error = %v", err)
			}
			cfg := testJinkoConfig("https://detail.example.test")
			cfg.BearerToken = tc.configuredAccess
			cfg.RefreshToken = tc.configuredRefresh
			cfg.TokenStateFile = statePath
			client := New(cfg, nil)
			gotAccess, _, _, _ := client.tokenSnapshot()
			if gotAccess != tc.wantAccess || client.refreshToken != tc.wantRefresh {
				t.Fatalf("selected access/refresh pair did not match precedence policy")
			}
		})
	}
}

func TestStateAccessOnlyKeepsConfiguredRefreshForBackgroundRotation(t *testing.T) {
	now := time.Now()
	configuredAccess := testJWT(t, now.Add(-time.Hour), "configured-expired")
	stateAccess := testJWT(t, now.Add(10*time.Second), "state-due")
	rotatedAccess := testJWT(t, now.Add(time.Hour), "rotated")
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	if err := persistTokenState(statePath, tokenState{
		AccessToken: stateAccess,
		UpdatedAt:   now.Add(-2 * time.Minute).UTC(),
	}); err != nil {
		t.Fatalf("persistTokenState() error = %v", err)
	}

	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm() error = %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "configured-refresh" {
				t.Errorf("refresh token = %q, want configured partial-pair member", got)
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: rotatedAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			detailCalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, configuredAccess)
	cfg.RefreshToken = "configured-refresh"
	cfg.TokenStateFile = statePath
	client := New(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()
	waitForCondition(t, func() bool {
		access, _, _, _ := client.tokenSnapshot()
		return refreshCalls.Load() == 1 && access == rotatedAccess
	})
	cancel()
	waitForSignal(t, done)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := detailCalls.Load(); got != 0 {
		t.Fatalf("detail calls = %d, want 0", got)
	}
}

func TestStateUpdatedAtRestoresBackgroundAntiSpinFloor(t *testing.T) {
	now := time.Now()
	updatedAt := now.UTC()
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	if err := persistTokenState(statePath, tokenState{
		AccessToken:  testJWT(t, now.Add(10*time.Second), "short-lived"),
		RefreshToken: "state-refresh",
		UpdatedAt:    updatedAt,
	}); err != nil {
		t.Fatalf("persistTokenState() error = %v", err)
	}
	cfg := testJinkoConfig("https://detail.example.test")
	cfg.BearerToken = ""
	cfg.RefreshToken = ""
	cfg.TokenURL = "https://token.example.test"
	cfg.TokenStateFile = statePath
	cfg.RefreshBefore = time.Minute
	client := New(cfg, nil)

	due, wait, enabled := client.backgroundRefreshSchedule(now)
	if !enabled || due {
		t.Fatalf("schedule enabled/due = %t/%t, want true/false", enabled, due)
	}
	if wait < 59*time.Second || wait > 61*time.Second {
		t.Fatalf("schedule wait = %s, want approximately one-minute anti-spin floor", wait)
	}
	if !client.tokenUpdatedAt.Equal(updatedAt) {
		t.Fatalf("tokenUpdatedAt = %s, want restored %s", client.tokenUpdatedAt, updatedAt)
	}
}

func TestFutureStateUpdatedAtDoesNotDelayRefreshPastTokenSchedule(t *testing.T) {
	now := time.Now()
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	if err := persistTokenState(statePath, tokenState{
		AccessToken:  testJWT(t, now.Add(10*time.Minute), "normal-expiry"),
		RefreshToken: "state-refresh",
		UpdatedAt:    now.AddDate(1, 0, 0).UTC(),
	}); err != nil {
		t.Fatalf("persistTokenState() error = %v", err)
	}
	cfg := testJinkoConfig("https://detail.example.test")
	cfg.BearerToken = ""
	cfg.RefreshToken = ""
	cfg.TokenURL = "https://token.example.test"
	cfg.TokenStateFile = statePath
	cfg.RefreshBefore = 5 * time.Minute
	client := New(cfg, nil)

	due, wait, enabled := client.backgroundRefreshSchedule(now)
	if !enabled || due {
		t.Fatalf("schedule enabled/due = %t/%t, want true/false", enabled, due)
	}
	if wait < 4*time.Minute+55*time.Second || wait > 5*time.Minute+time.Second {
		t.Fatalf("schedule wait = %s, want approximately five minutes despite future UpdatedAt", wait)
	}
}

func TestConcurrentProactiveRefreshIsSingleFlight(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(10*time.Second), "old")
	newAccess := testJWT(t, time.Now().Add(time.Hour), "new")
	var refreshCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			time.Sleep(25 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess})
		case "/detail":
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(refreshJinkoConfig(t, server.URL, oldAccess), nil)
	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client.Fetch(t.Context())
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Fetch() error = %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestBackgroundStartedDuringFetchRotationDoesNotParkOnInFlightMarker(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(10*time.Second), "due")
	newAccess := testJWT(t, time.Now().Add(time.Hour), "rotated")
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var startedOnce sync.Once
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			startedOnce.Do(func() { close(requestStarted) })
			<-releaseResponse
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			detailCalls.Add(1)
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(refreshJinkoConfig(t, server.URL, oldAccess), nil)
	fetchDone := make(chan error, 1)
	go func() {
		_, err := client.Fetch(context.Background())
		fetchDone <- err
	}()
	waitForSignal(t, requestStarted)
	client.tokenMu.Lock()
	if !client.refreshInFlight || client.refreshUncertain {
		t.Fatalf("refresh state inFlight/uncertain = %t/%t, want true/false", client.refreshInFlight, client.refreshUncertain)
	}
	client.tokenMu.Unlock()

	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		client.RunBackground(backgroundCtx)
		close(backgroundDone)
	}()
	time.Sleep(25 * time.Millisecond)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("OAuth calls while transaction in flight = %d, want 1", got)
	}
	close(releaseResponse)
	select {
	case err := <-fetchDone:
		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Fetch")
	}
	select {
	case <-backgroundDone:
		t.Fatal("background maintainer parked/exited after successful in-flight rotation")
	default:
	}
	cancelBackground()
	waitForSignal(t, backgroundDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("OAuth calls = %d, want one single-flight rotation", got)
	}
	if got := detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls = %d, want 1", got)
	}
}

func TestBackgroundKeeperImmediatelyRefreshesDueUnknownAndBootstrapTokens(t *testing.T) {
	tests := []struct {
		name        string
		accessToken func(*testing.T) string
	}{
		{
			name: "due JWT",
			accessToken: func(t *testing.T) string {
				return testJWT(t, time.Now().Add(10*time.Second), "due")
			},
		},
		{name: "unknown expiry", accessToken: func(*testing.T) string { return "opaque-access" }},
		{name: "refresh-only bootstrap", accessToken: func(*testing.T) string { return "" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var refreshCalls atomic.Int32
			var detailCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth2-s/oauth/token":
					refreshCalls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"access_token":  "opaque-rotated-access",
						"refresh_token": "rotated-refresh",
						"expires_in":    3600,
					})
				case "/detail":
					detailCalls.Add(1)
					http.Error(w, "background keeper must not fetch telemetry", http.StatusInternalServerError)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			cfg := refreshJinkoConfig(t, server.URL, tc.accessToken(t))
			client := New(cfg, nil)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				client.RunBackground(ctx)
				close(done)
			}()

			waitForCondition(t, func() bool {
				access, expiresAt, hasExpiry, _ := client.tokenSnapshot()
				return access == "opaque-rotated-access" && hasExpiry && expiresAt.After(time.Now().Add(50*time.Minute))
			})
			cancel()
			waitForSignal(t, done)

			if got := refreshCalls.Load(); got != 1 {
				t.Fatalf("refresh calls = %d, want 1", got)
			}
			if got := detailCalls.Load(); got != 0 {
				t.Fatalf("detail calls = %d, want 0", got)
			}
			state, err := loadTokenState(cfg.TokenStateFile)
			if err != nil {
				t.Fatalf("loadTokenState() error = %v", err)
			}
			if state.AccessToken != "opaque-rotated-access" || state.RefreshToken != "rotated-refresh" || state.ExpiresAt.IsZero() {
				t.Fatalf("persisted state = %#v, want rotated credentials with expires_in-derived expiry", state)
			}
		})
	}
}

func TestBackgroundKeeperImmediatelyRefreshesStateLoadedOpaqueToken(t *testing.T) {
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "opaque-rotated-access",
			"refresh_token": "rotated-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "token-state.json")
	if err := persistTokenState(statePath, tokenState{
		AccessToken:  "opaque-state-access",
		RefreshToken: "opaque-state-refresh",
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("persistTokenState() error = %v", err)
	}
	cfg := testJinkoConfig(server.URL + "/detail")
	cfg.BearerToken = ""
	cfg.RefreshToken = ""
	cfg.TokenURL = server.URL + "/oauth2-s/oauth/token"
	cfg.TokenStateFile = statePath
	cfg.RefreshBefore = time.Minute
	client := New(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()

	waitForCondition(t, func() bool {
		access, _, hasExpiry, _ := client.tokenSnapshot()
		return access == "opaque-rotated-access" && hasExpiry
	})
	cancel()
	waitForSignal(t, done)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want immediate one-time rotation", got)
	}
}

func TestBackgroundKeeperFreshTokenWaitsAndCancelsWithoutHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, testJWT(t, time.Now().Add(time.Hour), "fresh"))
	client := New(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()

	time.Sleep(25 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0 for a fresh token", got)
	}
	cancel()
	waitForSignal(t, done)
}

func TestBackgroundKeeperObservesLegacyBearerExpiryWithoutHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testJinkoConfig(server.URL + "/detail")
	cfg.BearerToken = testJWT(t, time.Now().Add(-time.Minute), "expired")
	cfg.TokenURL = server.URL + "/oauth2-s/oauth/token"
	cfg.RefreshToken = ""
	notifier := &recordingNotifier{}
	client := New(cfg, alert.NewManager(notifier, 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()

	waitForCondition(t, func() bool { return len(notifier.Bodies()) == 1 })
	if got := calls.Load(); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0 from expiry observer", got)
	}
	cancel()
	waitForSignal(t, done)
}

func TestUncertainRefreshNotificationCoalescesConcurrentAttemptsAndRetriesAfterFailure(t *testing.T) {
	notifier := newBlockingFailOnceNotifier()
	client := New(testJinkoConfig("https://example.invalid/detail"), alert.NewManager(notifier, 0))
	client.tokenMu.Lock()
	client.refreshUncertain = true
	client.tokenMu.Unlock()

	firstDone := make(chan struct{})
	go func() {
		client.notifyRefreshOutcomeUncertain(t.Context())
		close(firstDone)
	}()
	waitForSignal(t, notifier.started)

	// A second lifecycle path can observe the same state while delivery is in
	// progress. It must not start a duplicate notifier call.
	client.notifyRefreshOutcomeUncertain(t.Context())
	if got := notifier.Calls(); got != 1 {
		t.Fatalf("concurrent notifier calls = %d, want 1", got)
	}
	close(notifier.release)
	waitForSignal(t, firstDone)

	client.tokenMu.Lock()
	sentAfterFailure := client.uncertainSent
	inFlightAfterFailure := client.uncertainNotifyInFlight
	client.tokenMu.Unlock()
	if sentAfterFailure || inFlightAfterFailure {
		t.Fatalf("state after failed delivery sent/in-flight = %t/%t, want false/false", sentAfterFailure, inFlightAfterFailure)
	}

	client.notifyRefreshOutcomeUncertain(t.Context())
	client.tokenMu.Lock()
	sentAfterRetry := client.uncertainSent
	inFlightAfterRetry := client.uncertainNotifyInFlight
	client.tokenMu.Unlock()
	if !sentAfterRetry || inFlightAfterRetry {
		t.Fatalf("state after successful retry sent/in-flight = %t/%t, want true/false", sentAfterRetry, inFlightAfterRetry)
	}
	client.notifyRefreshOutcomeUncertain(t.Context())
	if got := notifier.Calls(); got != 2 {
		t.Fatalf("notifier calls = %d, want failed attempt plus one successful retry", got)
	}
}

func TestUnknownExpiryNotificationRetriesAfterFailure(t *testing.T) {
	notifier := &failOnceNotifier{}
	client := New(testJinkoConfig("https://example.invalid/detail"), alert.NewManager(notifier, 0))
	client.tokenMu.Lock()
	client.refreshedUnknown = true
	client.hasBearerTokenExp = false
	client.tokenMu.Unlock()

	client.notifyUnknownExpiryPause(t.Context())
	client.tokenMu.Lock()
	sentAfterFailure := client.unknownExpirySent
	inFlightAfterFailure := client.unknownExpiryNotifyInFlight
	client.tokenMu.Unlock()
	if sentAfterFailure || inFlightAfterFailure {
		t.Fatalf("state after failed delivery sent/in-flight = %t/%t, want false/false", sentAfterFailure, inFlightAfterFailure)
	}

	client.notifyUnknownExpiryPause(t.Context())
	client.tokenMu.Lock()
	sentAfterRetry := client.unknownExpirySent
	inFlightAfterRetry := client.unknownExpiryNotifyInFlight
	client.tokenMu.Unlock()
	if !sentAfterRetry || inFlightAfterRetry {
		t.Fatalf("state after successful retry sent/in-flight = %t/%t, want true/false", sentAfterRetry, inFlightAfterRetry)
	}
	client.notifyUnknownExpiryPause(t.Context())
	if got := notifier.Calls(); got != 2 {
		t.Fatalf("notifier calls = %d, want failed attempt plus one successful retry", got)
	}
}

func TestBackgroundKeeperDoesNotGuessOpaqueOrTooShortNextExpiry(t *testing.T) {
	tests := []struct {
		name     string
		response func(*testing.T) map[string]any
	}{
		{
			name: "opaque response without expires_in",
			response: func(*testing.T) map[string]any {
				return map[string]any{"access_token": "opaque-new", "refresh_token": "rotated-refresh"}
			},
		},
		{
			name: "already expired response",
			response: func(t *testing.T) map[string]any {
				return map[string]any{
					"access_token":  testJWT(t, time.Now().Add(-time.Minute), "bad-upstream-expiry"),
					"refresh_token": "rotated-refresh",
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := tc.response(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			cfg := refreshJinkoConfig(t, server.URL, testJWT(t, time.Now().Add(-time.Minute), "expired"))
			client := New(cfg, nil)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				client.RunBackground(ctx)
				close(done)
			}()

			waitForCondition(t, func() bool { return calls.Load() == 1 })
			time.Sleep(25 * time.Millisecond)
			if got := calls.Load(); got != 1 {
				t.Fatalf("refresh calls = %d, want no tight-loop second rotation", got)
			}
			select {
			case <-done:
				t.Fatal("RunBackground returned before cancellation")
			default:
			}
			cancel()
			waitForSignal(t, done)
		})
	}
}

func TestBackgroundKeeperAlertsWhenRefreshedExpiryIsUnknown(t *testing.T) {
	const (
		accessSecret  = "opaque-secret-access"
		refreshSecret = "opaque-secret-refresh"
	)
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: accessSecret, RefreshToken: refreshSecret})
		case "/detail":
			detailCalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	cfg := refreshJinkoConfig(t, server.URL, "opaque-old-access")
	client := New(cfg, alert.NewManager(notifier, 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()
	waitForCondition(t, func() bool { return len(notifier.Bodies()) == 1 })
	bodies := notifier.Bodies()
	if !strings.Contains(strings.ToLower(bodies[0]), "unknown") ||
		strings.Contains(bodies[0], accessSecret) || strings.Contains(bodies[0], refreshSecret) {
		t.Fatalf("unknown-expiry alert was missing or leaked credentials: %q", bodies[0])
	}
	if refreshCalls.Load() != 1 || detailCalls.Load() != 0 {
		t.Fatalf("HTTP calls refresh/detail=%d/%d, want 1/0", refreshCalls.Load(), detailCalls.Load())
	}
	cancel()
	waitForSignal(t, done)
}

func TestBackgroundKeeperNeverReplaysAmbiguousRefreshAndRestartKeepsPause(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(10*time.Second), "still-usable-old")
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			state, err := loadTokenState(statePath)
			if err != nil {
				t.Errorf("load in-flight token state error = %v", err)
			} else if state.AccessToken != oldAccess || state.RefreshToken != "refresh-old" || !state.RefreshOutcomeUncertain {
				t.Errorf("pre-POST token state = %#v, want old pair with uncertainty marker", state)
			}
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("httptest response writer does not support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("Hijack() error = %v", err)
				return
			}
			_ = conn.Close()
		case "/detail":
			detailCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer "+oldAccess {
				t.Errorf("detail Authorization = %q, want still-usable old bearer", got)
			}
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.TokenStateFile = statePath
	cfg.RetryAttempts = 5
	client := New(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()

	waitForCondition(t, func() bool {
		return refreshCalls.Load() == 1 && client.hasUncertainRefreshOutcome()
	})
	for i := 0; i < 3; i++ {
		client.notifyMaintenance()
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("RunBackground returned after ambiguous refresh; want paused nonfatal lifecycle")
	default:
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want exactly one non-replayed OAuth POST", got)
	}
	cancel()
	waitForSignal(t, done)
	state, err := loadTokenState(statePath)
	if err != nil {
		t.Fatalf("loadTokenState() error = %v", err)
	}
	if !state.RefreshOutcomeUncertain || state.AccessToken != oldAccess || state.RefreshToken != "refresh-old" {
		t.Fatalf("persisted state = %#v, want old pair with durable uncertainty marker", state)
	}

	// A new process must honor the marker: no OAuth replay, while a still-usable
	// access token may continue serving read-only telemetry.
	notifier := &recordingNotifier{}
	restarted := New(cfg, alert.NewManager(notifier, 0))
	restartCtx, cancelRestart := context.WithCancel(context.Background())
	restartDone := make(chan struct{})
	go func() {
		restarted.RunBackground(restartCtx)
		close(restartDone)
	}()
	if _, err := restarted.Fetch(t.Context()); err != nil {
		t.Fatalf("Fetch() with paused refresh and usable bearer error = %v", err)
	}
	waitForCondition(t, func() bool { return len(notifier.Bodies()) == 1 })
	for _, body := range notifier.Bodies() {
		if strings.Contains(body, oldAccess) || strings.Contains(body, "refresh-old") {
			t.Fatalf("uncertain-outcome alert leaked credentials: %q", body)
		}
	}
	time.Sleep(50 * time.Millisecond)
	cancelRestart()
	waitForSignal(t, restartDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls after restart = %d, want zero replay", got)
	}
	if got := detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls = %d, want telemetry to remain available with usable bearer", got)
	}
}

func TestNonSuccessTokenResponseLeavesDurablePauseWithoutReplay(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			oldAccess := testJWT(t, time.Now().Add(-time.Minute), "expired")
			var refreshCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				refreshCalls.Add(1)
				http.Error(w, "sensitive upstream response", status)
			}))
			defer server.Close()

			cfg := refreshJinkoConfig(t, server.URL, oldAccess)
			cfg.RetryAttempts = 5
			client := New(cfg, nil)
			if _, err := client.refresh(t.Context(), refreshRequest{reason: refreshProactive}); err == nil || !isRefreshOutcomeUncertainError(err) {
				t.Fatalf("refresh error = %v, want typed uncertain outcome", err)
			}
			for i := 0; i < 3; i++ {
				if _, err := client.refresh(t.Context(), refreshRequest{reason: refreshProactive}); err != nil {
					t.Fatalf("paused refresh recheck error = %v", err)
				}
			}
			if got := refreshCalls.Load(); got != 1 {
				t.Fatalf("OAuth calls = %d, want exactly one POST for status %d", got, status)
			}
			state, err := loadTokenState(cfg.TokenStateFile)
			if err != nil {
				t.Fatalf("loadTokenState() error = %v", err)
			}
			if !state.RefreshOutcomeUncertain || state.AccessToken != oldAccess || state.RefreshToken != "refresh-old" {
				t.Fatalf("persisted state = %#v, want marked old pair", state)
			}
		})
	}
}

func TestBackgroundKeeperFinishesStartedRotationDuringShutdown(t *testing.T) {
	newAccess := testJWT(t, time.Now().Add(time.Hour), "rotated-during-shutdown")
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	var requestStartedOnce sync.Once
	var refreshCalls atomic.Int32
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			requestStartedOnce.Do(func() { close(requestStarted) })
			select {
			case <-r.Context().Done():
				t.Errorf("token transaction was cancelled by serve shutdown: %v", r.Context().Err())
				return
			case <-releaseResponse:
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		case "/detail":
			detailCalls.Add(1)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, testJWT(t, time.Now().Add(-time.Minute), "expired"))
	client := New(cfg, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()
	waitForSignal(t, requestStarted)
	cancel()
	select {
	case <-done:
		t.Fatal("RunBackground exited before the started token transaction completed")
	default:
	}
	close(releaseResponse)
	waitForSignal(t, done)

	state, err := loadTokenState(cfg.TokenStateFile)
	if err != nil {
		t.Fatalf("loadTokenState() error = %v", err)
	}
	if state.AccessToken != newAccess || state.RefreshToken != "rotated-refresh" {
		t.Fatalf("state = %#v, want durable rotated pair after shutdown", state)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	if got := detailCalls.Load(); got != 0 {
		t.Fatalf("detail calls = %d, want 0", got)
	}
}

func TestBackgroundKeeperRetriesFinalPersistenceDuringShutdownWithoutOAuthReplay(t *testing.T) {
	newAccess := testJWT(t, time.Now().Add(time.Hour), "rotated-after-transient-storage-failure")
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	firstFinalPersistFailed := make(chan struct{})
	var requestStartedOnce sync.Once
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2-s/oauth/token" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		requestStartedOnce.Do(func() { close(requestStarted) })
		<-releaseResponse
		_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, testJWT(t, time.Now().Add(-time.Minute), "expired"))
	cfg.RetryAttempts = 1
	cfg.RetryBackoff = 24 * time.Hour
	client := New(cfg, nil)
	realPersist := client.persistState
	var persistCalls atomic.Int32
	client.persistState = func(path string, state tokenState) error {
		call := persistCalls.Add(1)
		if call == 2 {
			close(firstFinalPersistFailed)
			return &os.PathError{Op: "rename", Path: path, Err: os.ErrNotExist}
		}
		return realPersist(path, state)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client.RunBackground(ctx)
		close(done)
	}()
	waitForSignal(t, requestStarted)
	cancel()
	close(releaseResponse)
	waitForSignal(t, firstFinalPersistFailed)
	waitForSignal(t, done)

	state, err := loadTokenState(cfg.TokenStateFile)
	if err != nil {
		t.Fatalf("loadTokenState() error = %v", err)
	}
	if state.AccessToken != newAccess || state.RefreshToken != "rotated-refresh" || state.RefreshOutcomeUncertain {
		t.Fatalf("state = %#v, want durable rotated pair with cleared marker", state)
	}
	access, _, _, _ := client.tokenSnapshot()
	if access != newAccess {
		t.Fatalf("active access = %q, want durable rotated bearer", access)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("OAuth calls = %d, want 1", got)
	}
	if got := persistCalls.Load(); got != 3 {
		t.Fatalf("persistence calls = %d, want marker + failed final + fixed-delay retry", got)
	}
}

func TestBackgroundRefreshRacingFetch401ConsumesRefreshTokenOnce(t *testing.T) {
	fixture := readDetailFixture(t)
	oldAccess := testJWT(t, time.Now().Add(time.Hour), "old")
	newAccess := testJWT(t, time.Now().Add(2*time.Hour), "new")
	detailStarted := make(chan struct{})
	releaseUnauthorized := make(chan struct{})
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var detailStartedOnce sync.Once
	var refreshStartedOnce sync.Once
	var detailCalls atomic.Int32
	var refreshCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/detail":
			if detailCalls.Add(1) == 1 {
				detailStartedOnce.Do(func() { close(detailStarted) })
				<-releaseUnauthorized
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+newAccess {
				t.Errorf("retried detail Authorization = %q, want refreshed bearer", got)
			}
			_, _ = w.Write(fixture)
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			refreshStartedOnce.Do(func() { close(refreshStarted) })
			<-releaseRefresh
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.RefreshBefore = 0
	client := New(cfg, nil)
	fetchDone := make(chan error, 1)
	go func() {
		_, err := client.Fetch(context.Background())
		fetchDone <- err
	}()
	waitForSignal(t, detailStarted)

	client.tokenMu.Lock()
	client.bearerTokenExp = time.Now().Add(-time.Second)
	client.hasBearerTokenExp = true
	client.tokenMu.Unlock()
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		client.RunBackground(backgroundCtx)
		close(backgroundDone)
	}()
	waitForSignal(t, refreshStarted)
	close(releaseUnauthorized)
	close(releaseRefresh)

	select {
	case err := <-fetchDone:
		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Fetch")
	}
	cancelBackground()
	waitForSignal(t, backgroundDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want one rotated-token consumption", got)
	}
	if got := detailCalls.Load(); got != 2 {
		t.Fatalf("detail calls = %d, want initial 401 plus one retry", got)
	}
}

func TestUndurableBackgroundRotationCannotPowerFetch401Retry(t *testing.T) {
	oldAccess := testJWT(t, time.Now().Add(time.Hour), "old")
	newAccess := testJWT(t, time.Now().Add(2*time.Hour), "new")
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	statePath := filepath.Join(stateDir, "jinko-token-state.json")
	detailStarted := make(chan struct{})
	releaseUnauthorized := make(chan struct{})
	var detailStartedOnce sync.Once
	var detailCalls atomic.Int32
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/detail":
			detailCalls.Add(1)
			detailStartedOnce.Do(func() { close(detailStarted) })
			<-releaseUnauthorized
			http.Error(w, "expired", http.StatusUnauthorized)
		case "/oauth2-s/oauth/token":
			refreshCalls.Add(1)
			if err := os.Remove(statePath); err != nil {
				t.Errorf("Remove(state) error = %v", err)
			}
			if err := os.Remove(stateDir); err != nil {
				t.Errorf("Remove(state dir) error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(refreshResponse{AccessToken: newAccess, RefreshToken: "rotated-refresh"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, oldAccess)
	cfg.RefreshBefore = 0
	cfg.TokenStateFile = statePath
	client := New(cfg, nil)
	fetchDone := make(chan error, 1)
	go func() {
		_, err := client.Fetch(context.Background())
		fetchDone <- err
	}()
	waitForSignal(t, detailStarted)

	client.tokenMu.Lock()
	client.bearerTokenExp = time.Now().Add(-time.Second)
	client.hasBearerTokenExp = true
	client.tokenMu.Unlock()
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		client.RunBackground(backgroundCtx)
		close(backgroundDone)
	}()
	waitForCondition(t, func() bool {
		client.tokenMu.Lock()
		defer client.tokenMu.Unlock()
		return client.pendingTokenState != nil
	})
	close(releaseUnauthorized)
	select {
	case err := <-fetchDone:
		if err == nil || !isTokenStateDurabilityError(err) {
			t.Fatalf("Fetch() error = %v, want durability failure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Fetch")
	}
	cancelBackground()
	waitForSignal(t, backgroundDone)
	if got := detailCalls.Load(); got != 1 {
		t.Fatalf("detail calls = %d, want no retry with an undurable bearer", got)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want one OAuth consumption", got)
	}
	activeAccess, _, _, _ := client.tokenSnapshot()
	if activeAccess != oldAccess {
		t.Fatalf("undurable rotated bearer was promoted to active state")
	}

	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatalf("restore state directory error = %v", err)
	}
	repairCtx, cancelRepair := context.WithCancel(context.Background())
	repairDone := make(chan struct{})
	go func() {
		client.RunBackground(repairCtx)
		close(repairDone)
	}()
	waitForCondition(t, func() bool {
		state, err := loadTokenState(statePath)
		return err == nil && state.AccessToken == newAccess
	})
	cancelRepair()
	waitForSignal(t, repairDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls after repair = %d, want persistence-only recovery", got)
	}
}

func TestTokenTransactionTimeoutHasHardCeiling(t *testing.T) {
	cfg := testJinkoConfig("https://detail.example.test")
	cfg.Timeout = 24 * time.Hour
	if got := New(cfg, nil).tokenTransactionTimeout(); got != defaultTokenTransactionTimeout {
		t.Fatalf("huge configured timeout produced transaction timeout %s, want hard ceiling %s", got, defaultTokenTransactionTimeout)
	}
	cfg.Timeout = 2 * time.Second
	if got := New(cfg, nil).tokenTransactionTimeout(); got != 2*time.Second {
		t.Fatalf("short configured timeout produced transaction timeout %s, want 2s", got)
	}
	cfg.Timeout = 0
	if got := New(cfg, nil).tokenTransactionTimeout(); got != defaultTokenTransactionTimeout {
		t.Fatalf("zero configured timeout produced transaction timeout %s, want safe default", got)
	}
}

func TestRefreshGateWaitHonorsContext(t *testing.T) {
	refreshStarted := make(chan struct{})
	releaseServer := make(chan struct{})
	var refreshStartedOnce sync.Once
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		refreshStartedOnce.Do(func() { close(refreshStarted) })
		select {
		case <-r.Context().Done():
		case <-releaseServer:
		}
	}))
	defer server.Close()

	cfg := refreshJinkoConfig(t, server.URL, testJWT(t, time.Now().Add(-time.Minute), "expired"))
	client := New(cfg, nil)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- client.refreshIfNeeded(firstCtx)
	}()
	waitForSignal(t, refreshStarted)

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelWait()
	err := client.refreshIfNeeded(waitCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting refresh error = %v, want context deadline", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want waiting caller not to consume token", got)
	}

	cancelFirst()
	close(releaseServer)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("timed out cancelling gate holder")
	}
}

func TestRefreshAndDetailErrorsAreRedacted(t *testing.T) {
	const secret = "response-secret-token"
	expiredAccess := testJWT(t, time.Now().Add(-time.Minute), "expired")
	var detailCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2-s/oauth/token":
			http.Error(w, `{"error_description":"`+secret+`"}`, http.StatusUnauthorized)
		case "/detail":
			detailCalls.Add(1)
			http.Error(w, secret, http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	notifier := &recordingNotifier{}
	client := New(refreshJinkoConfig(t, server.URL, expiredAccess), alert.NewManager(notifier, 0))
	_, err := client.Fetch(t.Context())
	if err == nil {
		t.Fatal("Fetch() error = nil, want refresh failure")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "refresh-old") || strings.Contains(err.Error(), expiredAccess) {
		t.Fatalf("Fetch() error leaked secret: %q", err)
	}
	if got := detailCalls.Load(); got != 0 {
		t.Fatalf("detail calls = %d, want 0 after expired-token proactive refresh failure", got)
	}
	for _, body := range notifier.Bodies() {
		if strings.Contains(body, secret) || strings.Contains(body, "refresh-old") || strings.Contains(body, expiredAccess) {
			t.Fatalf("alert leaked secret: %q", body)
		}
	}
}

func TestRefreshDoesNotFollowRedirect(t *testing.T) {
	expiredAccess := testJWT(t, time.Now().Add(-time.Minute), "expired")
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

	cfg := refreshJinkoConfig(t, redirect.URL, expiredAccess)
	cfg.TokenURL = redirect.URL
	_, err := New(cfg, nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "status=307") {
		t.Fatalf("Fetch() error = %v, want sanitized redirect status", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}

func TestDetailDoesNotFollowRedirect(t *testing.T) {
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

	cfg := testJinkoConfig(redirect.URL)
	cfg.Cookie = "session=must-not-be-forwarded"
	_, err := New(cfg, nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "status=307") {
		t.Fatalf("Fetch() error = %v, want redirect status", err)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
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

func TestFetchRejectsMetriclessSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"msg":"token expired"}`))
	}))
	defer server.Close()

	client := New(testJinkoConfig(server.URL), nil)
	_, err := client.Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "contained no metrics") {
		t.Fatalf("Fetch() error = %v, want metricless response error", err)
	}
}

func TestFetchRejectsResponseWithoutDeviceSerial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deviceSn":" ",
			"paramCategoryList":[{
				"tag":"electric",
				"fieldList":[{"key":"DC Power PV1","storageName":"DP1","orgValue":"123","unit":"W"}]
			}]
		}`))
	}))
	defer server.Close()

	_, err := New(testJinkoConfig(server.URL), nil).Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no device serial") {
		t.Fatalf("Fetch() error = %v, want missing device serial error", err)
	}
}

func TestReadResponseBodyRejectsOversizedBody(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxHTTPResponseBodyBytes+1))
	if _, err := readResponseBody(body); err == nil {
		t.Fatal("readResponseBody() error = nil, want oversized body error")
	}
}

func TestPersistTokenStateDurablyReplacesMarkedPair(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "token-state.json")
	oldState := tokenState{
		AccessToken:             "old-access",
		RefreshToken:            "old-refresh",
		UpdatedAt:               time.Now().Add(-time.Minute).UTC(),
		RefreshOutcomeUncertain: true,
	}
	if err := persistTokenState(statePath, oldState); err != nil {
		t.Fatalf("persist old token state error = %v", err)
	}
	newState := tokenState{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		UpdatedAt:    time.Now().UTC(),
	}
	if err := persistTokenState(statePath, newState); err != nil {
		t.Fatalf("replace token state error = %v", err)
	}
	got, err := loadTokenState(statePath)
	if err != nil {
		t.Fatalf("loadTokenState() error = %v", err)
	}
	if got.AccessToken != newState.AccessToken || got.RefreshToken != newState.RefreshToken || got.RefreshOutcomeUncertain {
		t.Fatalf("reloaded state = %#v, want replacement pair with cleared marker", got)
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

func refreshJinkoConfig(t *testing.T, serverURL, accessToken string) config.JinkoConfig {
	t.Helper()
	cfg := testJinkoConfig(serverURL + "/detail")
	cfg.BearerToken = accessToken
	cfg.TokenURL = serverURL + "/oauth2-s/oauth/token"
	cfg.RefreshToken = "refresh-old"
	cfg.RefreshBefore = time.Minute
	cfg.System = "JinKO"
	cfg.Area = "FOREIGN_1"
	cfg.TokenStateFile = filepath.Join(t.TempDir(), "jinko-token-state.json")
	return cfg
}

func readDetailFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile("../../../testdata/jinko_detail_response.json")
	if err != nil {
		t.Fatalf("ReadFile fixture error = %v", err)
	}
	return fixture
}

func testJWT(t *testing.T, expiry time.Time, marker string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{"exp": expiry.Unix(), "marker": marker})
	if err != nil {
		t.Fatalf("Marshal JWT payload error = %v", err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

type recordingNotifier struct {
	mu     sync.Mutex
	bodies []string
}

type failOnceNotifier struct {
	mu    sync.Mutex
	calls int
}

func (n *failOnceNotifier) Notify(_ context.Context, _, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	if n.calls == 1 {
		return errors.New("temporary notifier failure")
	}
	return nil
}

func (n *failOnceNotifier) Calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

type blockingFailOnceNotifier struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func newBlockingFailOnceNotifier() *blockingFailOnceNotifier {
	return &blockingFailOnceNotifier{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (n *blockingFailOnceNotifier) Notify(ctx context.Context, _, _ string) error {
	if n.calls.Add(1) != 1 {
		return nil
	}
	close(n.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-n.release:
		return errors.New("temporary notifier failure")
	}
}

func (n *blockingFailOnceNotifier) Calls() int {
	return int(n.calls.Load())
}

func (n *recordingNotifier) Notify(_ context.Context, _ string, body string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.bodies = append(n.bodies, body)
	return nil
}

func (n *recordingNotifier) Bodies() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.bodies...)
}

func waitForCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}
