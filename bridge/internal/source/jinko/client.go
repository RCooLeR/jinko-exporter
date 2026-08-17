package jinko

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/rs/zerolog/log"
)

var (
	_ source.Source               = (*Client)(nil)
	_ source.BackgroundMaintainer = (*Client)(nil)
)

const (
	maxHTTPResponseBodyBytes               = 2 * 1024 * 1024
	maxTokenStateBytes                     = 64 * 1024
	backgroundFailureRetryInterval         = time.Minute
	minimumBackgroundRefreshInterval       = time.Minute
	minimumTokenStateRetryDelay            = 25 * time.Millisecond
	tokenStatePersistenceAttempts          = 3
	defaultTokenTransactionTimeout         = 30 * time.Second
	maximumOAuthExpiresInSeconds     int64 = int64((1<<63 - 1) / time.Second)
)

type Client struct {
	cfg                         config.JinkoConfig
	hc                          *http.Client
	tokenHC                     *http.Client
	alerts                      *alert.Manager
	requestBody                 []byte
	deviceID                    string
	siteID                      string
	tokenMu                     sync.Mutex
	refreshGate                 chan struct{}
	maintenanceWake             chan struct{}
	persistState                func(string, tokenState) error
	bearerToken                 string
	refreshToken                string
	tokenVersion                uint64
	bearerTokenExp              time.Time
	hasBearerTokenExp           bool
	tokenUpdatedAt              time.Time
	refreshedUnknown            bool
	unknownExpirySent           bool
	unknownExpiryNotifyInFlight bool
	refreshInFlight             bool
	refreshUncertain            bool
	uncertainSent               bool
	uncertainNotifyInFlight     bool
	pendingTokenState           *pendingTokenState
}

type tokenState struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	// RefreshOutcomeUncertain is persisted before an OAuth refresh-token POST
	// and cleared only with a durable valid response pair. Older state files
	// remain compatible because the field is optional.
	RefreshOutcomeUncertain bool `json:"refresh_outcome_uncertain,omitempty"`
}

type refreshResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresIn    json.RawMessage `json:"expires_in"`
}

type refreshReason uint8

const (
	refreshProactive refreshReason = iota
	refreshUnauthorized
	refreshBackground
)

type refreshRequest struct {
	reason        refreshReason
	failedVersion uint64
}

type refreshMaterial struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	updatedAt    time.Time
}

type refreshOutcomeUncertainError struct {
	stage  string
	status int
}

func (e refreshOutcomeUncertainError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("Jinko token refresh outcome is uncertain: stage=%s status=%d; automatic refresh is paused for manual recovery", e.stage, e.status)
	}
	return fmt.Sprintf("Jinko token refresh outcome is uncertain: stage=%s; automatic refresh is paused for manual recovery", e.stage)
}

type tokenStateDurabilityError struct {
	err error
}

func (e tokenStateDurabilityError) Error() string {
	return e.err.Error()
}

func (e tokenStateDurabilityError) Unwrap() error {
	return e.err
}

type pendingTokenState struct {
	state       tokenState
	baseVersion uint64
}

func New(cfg config.JinkoConfig, alerts *alert.Manager) *Client {
	token := normalizeBearerToken(cfg.BearerToken)
	refreshToken := strings.TrimSpace(cfg.RefreshToken)
	bearerTokenExp, hasBearerTokenExp := bearerExpiry(token)
	var tokenUpdatedAt time.Time
	var refreshUncertain bool

	if state, err := loadTokenState(cfg.TokenStateFile); err != nil {
		log.Warn().Err(err).Str("source", "jinko").Msg("failed to load Jinko token state; using configured credentials")
	} else {
		// A persisted in-flight marker is authoritative across restarts even if
		// configuration precedence selects another access token. Operators must
		// install a complete recovered pair and replace/remove the marked state;
		// the marker must never be cleared while retaining the old refresh token.
		refreshUncertain = state.RefreshOutcomeUncertain
		stateAccess := normalizeBearerToken(state.AccessToken)
		stateRefresh := strings.TrimSpace(state.RefreshToken)
		if preferTokenState(token, bearerTokenExp, hasBearerTokenExp, state) {
			token = stateAccess
			bearerTokenExp, hasBearerTokenExp = tokenStateExpiry(state)
			tokenUpdatedAt = state.UpdatedAt
			if stateRefresh != "" {
				// Preserve the winning state's non-empty refresh member. For the
				// same access this also retains a rotated runtime refresh token.
				refreshToken = stateRefresh
			}
			// If state refresh is absent, keep the configured refresh token as
			// the only available completion of the winning partial state pair.
		} else if token == "" && stateAccess == "" && stateRefresh != "" {
			// A refresh-only state is a deliberate bootstrap pair with no access
			// token on either side.
			refreshToken = stateRefresh
		} else if refreshToken == "" && stateRefresh != "" {
			// Configured access won, but its pair is partial. Fill only the
			// missing refresh member; never replace a non-empty configured one.
			refreshToken = stateRefresh
		}
	}
	cfg.BearerToken = token
	requestBody, _ := json.Marshal(struct {
		DeviceID             int64  `json:"deviceId"`
		Language             string `json:"language"`
		NeedRealtimeDataFlag bool   `json:"needRealTimeDataFlag"`
		SiteID               int64  `json:"siteId"`
	}{
		DeviceID:             cfg.DeviceID,
		Language:             cfg.Language,
		NeedRealtimeDataFlag: cfg.NeedRealtimeData,
		SiteID:               cfg.SiteID,
	})
	detailTransport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureSkipVerify {
		detailTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	// Never carry the detail endpoint's legacy TLS bypass into OAuth. Refresh
	// requests contain a long-lived credential and must always verify the
	// token endpoint certificate.
	tokenTransport := http.DefaultTransport.(*http.Transport).Clone()

	hc := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: detailTransport,
		// The detail request carries both a bearer token and, optionally, a
		// browser-session cookie. Keep those credentials pinned to the validated
		// endpoint instead of letting net/http replay them across a redirect.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Client{
		cfg: cfg,
		hc:  hc,
		tokenHC: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: tokenTransport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		alerts:            alerts,
		requestBody:       requestBody,
		deviceID:          fmt.Sprintf("%d", cfg.DeviceID),
		siteID:            fmt.Sprintf("%d", cfg.SiteID),
		bearerToken:       token,
		refreshToken:      refreshToken,
		refreshGate:       make(chan struct{}, 1),
		maintenanceWake:   make(chan struct{}, 1),
		persistState:      persistTokenState,
		tokenVersion:      1,
		bearerTokenExp:    bearerTokenExp,
		hasBearerTokenExp: hasBearerTokenExp,
		tokenUpdatedAt:    tokenUpdatedAt,
		refreshUncertain:  refreshUncertain,
	}
}

func (c *Client) Name() string {
	return "jinko"
}

func (c *Client) Fetch(ctx context.Context) (*model.Snapshot, error) {
	c.notifyRefreshOutcomeUncertain(ctx)
	if err := c.persistPendingTokenState(ctx); err != nil {
		c.alertRefreshFailure(ctx, "durability")
		return nil, fmt.Errorf("jinko token state is not durable: %w", err)
	}
	if err := c.refreshIfNeeded(ctx); err != nil {
		if isRefreshOutcomeUncertainError(err) {
			c.notifyRefreshOutcomeUncertain(ctx)
		}
		c.alertRefreshFailure(ctx, "proactive")
		if isTokenStateDurabilityError(err) {
			return nil, fmt.Errorf("proactive Jinko token refresh was not durable: %w", err)
		}
		if !c.hasUsableBearerToken() {
			return nil, fmt.Errorf("proactive Jinko token refresh failed: %w", err)
		}
		// A proactive refresh failure must not discard a bearer token that may still work.
		log.Warn().Err(err).Str("source", c.Name()).Msg("proactive Jinko token refresh failed; trying current bearer token")
	}
	c.checkBearerToken(ctx)

	if c.cfg.RequestJitterMax > 0 {
		// The private Jinko endpoint is browser-oriented, so keep polls slightly de-synchronized.
		jitter := randomRequestJitter(c.cfg.RequestJitterMax)
		log.Info().Dur("jitter", jitter).Msg("sleeping before Jinko request")
		timer := time.NewTimer(jitter)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	reqBody := c.requestBody
	if len(reqBody) == 0 {
		return nil, fmt.Errorf("jinko request body is empty")
	}

	raw, status, tokenVersion, err := c.doDetailRequestWithRetry(ctx, reqBody)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		refreshed, refreshErr := c.refreshAfterUnauthorized(ctx, tokenVersion)
		if refreshErr != nil {
			if isRefreshOutcomeUncertainError(refreshErr) {
				c.notifyRefreshOutcomeUncertain(ctx)
			}
			c.alertRefreshFailure(ctx, "after_401")
			log.Warn().Err(refreshErr).Str("source", c.Name()).Msg("Jinko token refresh after 401 failed")
			if isTokenStateDurabilityError(refreshErr) {
				return nil, fmt.Errorf("jinko token refresh after 401 was not durable: %w", refreshErr)
			}
		}
		if refreshed {
			raw, status, _, err = c.doDetailRequestWithRetry(ctx, reqBody)
			if err != nil {
				return nil, err
			}
		}
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		c.alertAuthFailure(ctx, status)
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("jinko detail request failed: status=%d", status)
	}

	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", c.cfg.URL).Msg("failed to parse Jinko detail response")
		return nil, err
	}
	if len(snapshot.Metrics) == 0 {
		return nil, fmt.Errorf("jinko detail response contained no metrics")
	}
	if strings.TrimSpace(snapshot.DeviceSN) == "" {
		return nil, fmt.Errorf("jinko detail response contained no device serial")
	}
	if err := c.persistPendingTokenState(ctx); err != nil {
		c.alertRefreshFailure(ctx, "durability")
		return nil, fmt.Errorf("jinko token state became undurable during detail fetch: %w", err)
	}
	snapshot.Source = c.Name()
	snapshot.DeviceID = c.deviceID
	snapshot.SiteID = c.siteID
	return snapshot, nil
}

func randomRequestJitter(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	if maximum == time.Duration(1<<63-1) {
		// Adding one would overflow Int64N's bound. Int64 still spans every
		// practical non-negative duration and keeps direct Client construction
		// safe even if startup validation was bypassed.
		return time.Duration(rand.Int64())
	}
	return time.Duration(rand.Int64N(maximum.Nanoseconds() + 1))
}

func (c *Client) doDetailRequestWithRetry(ctx context.Context, reqBody []byte) ([]byte, int, uint64, error) {
	attempts := c.requestAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, status, tokenVersion, err := c.doDetailRequest(ctx, reqBody, attempt, attempts)
		if err == nil {
			if shouldRetryHTTPStatus(status) && attempt < attempts {
				delay := c.retryDelay(attempt)
				log.Warn().
					Str("source", c.Name()).
					Str("url", c.cfg.URL).
					Int("status", status).
					Int("attempt", attempt).
					Int("max_attempts", attempts).
					Dur("retry_in", delay).
					Msg("API request returned retryable HTTP status, retrying")
				if err := sleepBeforeRetry(ctx, delay); err != nil {
					return nil, 0, tokenVersion, err
				}
				continue
			}
			return raw, status, tokenVersion, nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, tokenVersion, ctxErr
		}
		if attempt == attempts || !isRetryableRequestError(err) {
			log.Error().
				Err(err).
				Str("source", c.Name()).
				Str("url", c.cfg.URL).
				Int("attempt", attempt).
				Int("max_attempts", attempts).
				Msg("API request failed")
			return nil, 0, tokenVersion, err
		}

		delay := c.retryDelay(attempt)
		log.Warn().
			Err(err).
			Str("source", c.Name()).
			Str("url", c.cfg.URL).
			Int("attempt", attempt).
			Int("max_attempts", attempts).
			Dur("retry_in", delay).
			Msg("API request failed, retrying")

		if err := sleepBeforeRetry(ctx, delay); err != nil {
			return nil, 0, tokenVersion, err
		}
	}

	return nil, 0, 0, fmt.Errorf("jinko detail request failed after %d attempts", attempts)
}

func (c *Client) doDetailRequest(ctx context.Context, reqBody []byte, attempt, attempts int) ([]byte, int, uint64, error) {
	bearerToken, bearerTokenExp, hasBearerTokenExp, tokenVersion, err := c.detailTokenSnapshot()
	if err != nil {
		return nil, 0, tokenVersion, err
	}
	req, err := c.newDetailRequestWithBearer(ctx, reqBody, bearerToken)
	if err != nil {
		return nil, 0, tokenVersion, err
	}

	fields := log.Info().
		Str("source", c.Name()).
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Int("attempt", attempt).
		Int("max_attempts", attempts)
	if hasBearerTokenExp {
		fields = fields.Time("token_expires_at", bearerTokenExp)
	}
	fields.Msg("sending API request")

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, tokenVersion, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, 0, tokenVersion, err
	}

	log.Info().
		Str("source", c.Name()).
		Str("method", http.MethodPost).
		Str("url", c.cfg.URL).
		Int("attempt", attempt).
		Int("max_attempts", attempts).
		Int("status", resp.StatusCode).
		Dur("duration", time.Since(start)).
		Int("response_bytes", len(raw)).
		Msg("received API response")

	return raw, resp.StatusCode, tokenVersion, nil
}

func (c *Client) newDetailRequestWithBearer(ctx context.Context, reqBody []byte, bearerToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	c.applyBrowserHeaders(req)
	return req, nil
}

func (c *Client) applyBrowserHeaders(req *http.Request) {
	if c.cfg.Cookie != "" {
		req.Header.Set("Cookie", c.cfg.Cookie)
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
}

func (c *Client) refreshIfNeeded(ctx context.Context) error {
	if !c.proactiveRefreshDue(time.Now()) {
		return nil
	}
	_, err := c.refresh(ctx, refreshRequest{reason: refreshProactive})
	return err
}

func (c *Client) refreshAfterUnauthorized(ctx context.Context, failedVersion uint64) (bool, error) {
	return c.refresh(ctx, refreshRequest{reason: refreshUnauthorized, failedVersion: failedVersion})
}

func (c *Client) canRefreshLocked() bool {
	return c.hasRefreshMaterialLocked() && !c.refreshUncertain
}

func (c *Client) hasRefreshMaterialLocked() bool {
	return strings.TrimSpace(c.cfg.TokenURL) != "" && strings.TrimSpace(c.refreshToken) != ""
}

func (c *Client) hasRefreshCredentials() bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.canRefreshLocked()
}

func (c *Client) hasRefreshMaterial() bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.hasRefreshMaterialLocked()
}

func (c *Client) RunBackground(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.persistPendingTokenStateInBackground(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			c.alertRefreshFailure(ctx, "background_durability")
			log.Warn().Err(err).Str("source", c.Name()).Msg("pending Jinko token state is not durable yet; telemetry source selection continues")
			if !c.waitForMaintenance(ctx, backgroundFailureRetryInterval) {
				return
			}
			continue
		}
		if c.hasUncertainRefreshOutcome() {
			c.notifyRefreshOutcomeUncertain(ctx)
			c.observeBearerExpiry(ctx)
			return
		}
		if !c.hasRefreshMaterial() {
			c.observeBearerExpiry(ctx)
			return
		}

		due, wait, enabled := c.backgroundRefreshSchedule(time.Now())
		if !enabled {
			c.notifyUnknownExpiryPause(ctx)
			if !c.waitForMaintenance(ctx, 0) {
				return
			}
			continue
		}
		if !due {
			if !c.waitForMaintenance(ctx, wait) {
				return
			}
			continue
		}

		if err := c.refreshInBackground(ctx); err != nil {
			if isRefreshOutcomeUncertainError(err) {
				c.notifyRefreshOutcomeUncertain(ctx)
				c.observeBearerExpiry(ctx)
				return
			}
			if ctx.Err() != nil {
				return
			}
			c.alertRefreshFailure(ctx, "background")
			log.Warn().Err(err).Str("source", c.Name()).Msg("background Jinko token refresh failed; telemetry source selection continues")
			if !c.waitForMaintenance(ctx, backgroundFailureRetryInterval) {
				return
			}
		}
	}
}

func (c *Client) hasUncertainRefreshOutcome() bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.refreshUncertain
}

func (c *Client) notifyRefreshOutcomeUncertain(ctx context.Context) {
	c.tokenMu.Lock()
	shouldNotify := c.refreshUncertain && !c.uncertainSent && !c.uncertainNotifyInFlight
	if shouldNotify {
		c.uncertainNotifyInFlight = true
	}
	c.tokenMu.Unlock()
	if !shouldNotify {
		return
	}

	log.Error().Str("source", c.Name()).Msg("Jinko refresh-token outcome is uncertain; automatic refresh is paused and operator recovery is required")
	delivered := false
	if c.alerts != nil {
		delivered = c.alerts.NotifyDelivered(ctx, alert.Event{
			Key:     "jinko_token_refresh_outcome_uncertain",
			Subject: "Jinko token refresh needs manual recovery",
			Body:    "A Jinko refresh-token request had an uncertain outcome. Automatic token refresh is paused to prevent replaying a possibly consumed credential; telemetry may continue with the current access token while it remains usable. Stop the bridge and obtain a complete new credential pair. Install it by replacing the persisted state, or configure the complete new pair before removing the old state, then restart. Never merely clear the marker while reusing the possibly consumed refresh token.",
		})
	}

	c.tokenMu.Lock()
	c.uncertainNotifyInFlight = false
	if delivered && c.refreshUncertain {
		c.uncertainSent = true
	}
	c.tokenMu.Unlock()
}

func (c *Client) notifyUnknownExpiryPause(ctx context.Context) {
	c.tokenMu.Lock()
	shouldNotify := c.refreshedUnknown && !c.hasBearerTokenExp && !c.unknownExpirySent && !c.unknownExpiryNotifyInFlight
	if shouldNotify {
		c.unknownExpiryNotifyInFlight = true
	}
	c.tokenMu.Unlock()
	if !shouldNotify {
		return
	}

	log.Warn().Str("source", c.Name()).Msg("refreshed Jinko access-token expiry is unknown; background rotation is paused until a fallback request receives 401")
	delivered := false
	if c.alerts != nil {
		delivered = c.alerts.NotifyDelivered(ctx, alert.Event{
			Key:     "jinko_token_expiry_unknown",
			Subject: "Jinko token expiry is unknown",
			Body:    "The refreshed Jinko access-token expiry is unknown because the response contained neither JWT exp nor OAuth expires_in. Automatic background rotation is paused; a 401 during a Jinko fallback request will trigger the next rotation.",
		})
	}

	c.tokenMu.Lock()
	c.unknownExpiryNotifyInFlight = false
	if delivered && c.refreshedUnknown && !c.hasBearerTokenExp {
		c.unknownExpirySent = true
	}
	c.tokenMu.Unlock()
}

func (c *Client) waitForMaintenance(ctx context.Context, wait time.Duration) bool {
	if wait <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-c.maintenanceWake:
			return true
		}
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-c.maintenanceWake:
		return true
	case <-timer.C:
		return true
	}
}

func (c *Client) notifyMaintenance() {
	select {
	case c.maintenanceWake <- struct{}{}:
	default:
	}
}

func (c *Client) persistPendingTokenStateInBackground(ctx context.Context) error {
	var lastErr error
	for attempt := 1; attempt <= tokenStatePersistenceAttempts; attempt++ {
		err := c.persistPendingTokenState(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == tokenStatePersistenceAttempts {
			break
		}
		if err := sleepBeforeRetry(ctx, minimumTokenStateRetryDelay); err != nil {
			return err
		}
	}
	return lastErr
}

func (c *Client) persistPendingTokenState(ctx context.Context) error {
	c.tokenMu.Lock()
	hasPending := c.pendingTokenState != nil
	c.tokenMu.Unlock()
	if !hasPending {
		return nil
	}

	if err := c.acquireRefreshGate(ctx); err != nil {
		return err
	}
	defer c.releaseRefreshGate()

	c.tokenMu.Lock()
	if c.pendingTokenState == nil {
		c.tokenMu.Unlock()
		return nil
	}
	pending := *c.pendingTokenState
	c.tokenMu.Unlock()

	if err := c.persistState(c.cfg.TokenStateFile, pending.state); err != nil {
		return tokenStateDurabilityError{err: fmt.Errorf("persist pending Jinko token state: %w", err)}
	}

	promoted := false
	c.tokenMu.Lock()
	if c.pendingTokenState != nil &&
		c.pendingTokenState.baseVersion == pending.baseVersion &&
		c.tokenVersion == pending.baseVersion {
		c.activateTokenStateLocked(pending.state)
		c.pendingTokenState = nil
		promoted = true
	}
	c.tokenMu.Unlock()
	if !promoted {
		return tokenStateDurabilityError{err: errors.New("pending Jinko token state changed before activation")}
	}
	c.notifyMaintenance()
	log.Info().Str("source", c.Name()).Msg("persisted pending Jinko token state")
	return nil
}

func (c *Client) observeBearerExpiry(ctx context.Context) {
	for {
		c.checkBearerToken(ctx)
		_, expiresAt, hasExpiry, _ := c.tokenSnapshot()
		if !hasExpiry {
			<-ctx.Done()
			return
		}

		now := time.Now()
		if !expiresAt.After(now) {
			<-ctx.Done()
			return
		}
		nextCheck := expiresAt
		if c.cfg.TokenAlertWindow > 0 {
			alertAt := expiresAt.Add(-c.cfg.TokenAlertWindow)
			if alertAt.After(now) {
				nextCheck = alertAt
			}
		}
		if err := sleepBeforeRetry(ctx, nextCheck.Sub(now)); err != nil {
			return
		}
	}
}

func (c *Client) refreshInBackground(ctx context.Context) error {
	// A refresh token may rotate when the server receives the POST. Never
	// replay at this layer: any post-send failure is ambiguous and permanently
	// pauses automatic reuse through the durable in-flight marker.
	_, err := c.refresh(ctx, refreshRequest{reason: refreshBackground})
	return err
}

func (c *Client) proactiveRefreshDue(now time.Time) bool {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.canRefreshLocked() && c.proactiveRefreshDueLocked(now)
}

func (c *Client) proactiveRefreshDueLocked(now time.Time) bool {
	if c.bearerToken == "" {
		return true
	}
	if !c.hasBearerTokenExp {
		// Opaque bearer tokens keep the historical Fetch behavior: try them
		// until a 401. The background keeper performs the one-time bootstrap.
		return false
	}
	return !c.bearerTokenExp.After(now.Add(c.refreshBefore()))
}

func (c *Client) backgroundRefreshSchedule(now time.Time) (due bool, wait time.Duration, enabled bool) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.backgroundRefreshScheduleLocked(now)
}

func (c *Client) refreshBefore() time.Duration {
	if c.cfg.RefreshBefore < 0 {
		return 0
	}
	return c.cfg.RefreshBefore
}

func (c *Client) refresh(ctx context.Context, request refreshRequest) (bool, error) {
	if err := c.acquireRefreshGate(ctx); err != nil {
		return false, err
	}
	defer c.releaseRefreshGate()

	material, result, refresh, err := c.refreshMaterialAfterGate(request, time.Now())
	if err != nil {
		return false, err
	}
	if !refresh {
		return result, nil
	}
	return c.performRefresh(ctx, material)
}

func (c *Client) acquireRefreshGate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.refreshGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) releaseRefreshGate() {
	<-c.refreshGate
}

func (c *Client) refreshMaterialAfterGate(request refreshRequest, now time.Time) (refreshMaterial, bool, bool, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.pendingTokenState != nil {
		return refreshMaterial{}, false, false, pendingTokenDurabilityError()
	}
	if request.reason == refreshUnauthorized && c.tokenVersion != request.failedVersion {
		// The exact bearer that received 401 was already replaced while this
		// caller waited for the context-aware gate.
		return refreshMaterial{}, true, false, nil
	}
	if !c.canRefreshLocked() {
		return refreshMaterial{}, false, false, nil
	}
	switch request.reason {
	case refreshProactive:
		if !c.proactiveRefreshDueLocked(now) {
			return refreshMaterial{}, false, false, nil
		}
	case refreshBackground:
		due, _, _ := c.backgroundRefreshScheduleLocked(now)
		if !due {
			return refreshMaterial{}, false, false, nil
		}
	}

	return refreshMaterial{
		accessToken:  c.bearerToken,
		refreshToken: c.refreshToken,
		expiresAt:    c.bearerTokenExp,
		updatedAt:    c.tokenUpdatedAt,
	}, false, true, nil
}

func (c *Client) backgroundRefreshScheduleLocked(now time.Time) (due bool, wait time.Duration, enabled bool) {
	if !c.canRefreshLocked() {
		return false, 0, false
	}
	if c.bearerToken == "" {
		return true, 0, true
	}
	if !c.hasBearerTokenExp {
		if !c.refreshedUnknown || c.tokenUpdatedAt.IsZero() {
			return true, 0, true
		}
		// Without JWT exp or OAuth expires_in there is no defensible next
		// deadline. Refresh once per process, then let a real 401 on a fallback
		// Fetch trigger the next rotation rather than consume tokens on a guess.
		return false, 0, false
	}
	dueAt := c.bearerTokenExp.Add(-c.refreshBefore())
	if !c.tokenUpdatedAt.IsZero() && !c.tokenUpdatedAt.After(now) {
		minimumDueAt := c.tokenUpdatedAt.Add(minimumBackgroundRefreshInterval)
		if dueAt.Before(minimumDueAt) {
			dueAt = minimumDueAt
		}
	}
	if !dueAt.After(now) {
		return true, 0, true
	}
	return false, dueAt.Sub(now), true
}

func (c *Client) performRefresh(ctx context.Context, material refreshMaterial) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	statePath := strings.TrimSpace(c.cfg.TokenStateFile)
	if statePath == "" {
		return false, fmt.Errorf("jinko token refresh requires a token state file")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", material.refreshToken)
	form.Set("client_id", "test")
	form.Set("system", c.cfg.System)
	form.Set("area", c.cfg.Area)
	form.Set("origin_id", c.cfg.OriginID)

	req, err := http.NewRequest(http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("build Jinko token refresh request: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}

	markerUpdatedAt := material.updatedAt
	if markerUpdatedAt.IsZero() {
		markerUpdatedAt = time.Now().UTC()
	}
	// Persist an explicit in-flight marker with the old usable pair before the
	// only OAuth POST. A process crash or ambiguous response can therefore
	// never cause a restart to replay a refresh token that may have rotated.
	if err := c.persistState(statePath, tokenState{
		AccessToken:             material.accessToken,
		RefreshToken:            material.refreshToken,
		ExpiresAt:               material.expiresAt,
		UpdatedAt:               markerUpdatedAt,
		RefreshOutcomeUncertain: true,
	}); err != nil {
		return false, fmt.Errorf("prepare Jinko token state: %w", err)
	}
	c.tokenMu.Lock()
	c.refreshInFlight = true
	c.tokenMu.Unlock()

	// A refresh token may rotate as soon as the OAuth server accepts it. Once
	// the durable marker exists, finish the token HTTP/read/persist transaction
	// even if serve is shutting down; a separate hard timeout keeps it bounded.
	transactionCtx, cancelTransaction := context.WithTimeout(context.WithoutCancel(ctx), c.tokenTransactionTimeout())
	defer cancelTransaction()
	req = req.WithContext(transactionCtx)

	resp, err := c.tokenHC.Do(req)
	if err != nil {
		return false, c.markRefreshOutcomeUncertain("send", 0)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := readResponseBody(resp.Body)
	if err != nil {
		return false, c.markRefreshOutcomeUncertain("read_response", 0)
	}
	if resp.StatusCode != http.StatusOK {
		return false, c.markRefreshOutcomeUncertain("http_status", resp.StatusCode)
	}

	var payload refreshResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, c.markRefreshOutcomeUncertain("decode_response", 0)
	}
	accessToken := normalizeBearerToken(payload.AccessToken)
	if accessToken == "" {
		return false, c.markRefreshOutcomeUncertain("validate_response", 0)
	}
	refreshToken := strings.TrimSpace(payload.RefreshToken)
	if refreshToken == "" {
		refreshToken = material.refreshToken
	}

	updatedAt := time.Now().UTC()
	expiresAt, hasExpiry := refreshedTokenExpiry(accessToken, payload.ExpiresIn, updatedAt)

	state := tokenState{
		AccessToken:             accessToken,
		RefreshToken:            refreshToken,
		ExpiresAt:               expiresAt,
		UpdatedAt:               updatedAt,
		RefreshOutcomeUncertain: false,
	}
	persistErr := c.persistRefreshedTokenState(transactionCtx, statePath, state)

	c.tokenMu.Lock()
	c.refreshInFlight = false
	if persistErr != nil {
		// Do not expose an undurable rotated pair to detail requests. The OAuth
		// server may already have invalidated the old refresh token, so retain the
		// response separately for persistence-only recovery.
		c.pendingTokenState = &pendingTokenState{state: state, baseVersion: c.tokenVersion}
	} else {
		c.activateTokenStateLocked(state)
		c.pendingTokenState = nil
	}
	c.tokenMu.Unlock()
	c.notifyMaintenance()

	if persistErr != nil {
		log.Error().Err(persistErr).Str("source", c.Name()).Msg("rotated Jinko credential pair is not durable; shutdown or process loss could lose refresh capability")
		// Surface the durability failure so priority can fall through while the
		// background keeper retries only the atomic state write.
		return true, tokenStateDurabilityError{err: fmt.Errorf("persist refreshed Jinko token state: %w", persistErr)}
	}

	fields := log.Info().Str("source", c.Name())
	if hasExpiry {
		fields = fields.Time("token_expires_at", expiresAt)
	}
	fields.Msg("refreshed Jinko access token")
	return true, nil
}

func (c *Client) markRefreshOutcomeUncertain(stage string, status int) error {
	c.tokenMu.Lock()
	c.refreshInFlight = false
	if !c.refreshUncertain {
		c.uncertainSent = false
	}
	c.refreshUncertain = true
	c.tokenMu.Unlock()
	c.notifyMaintenance()
	return refreshOutcomeUncertainError{stage: stage, status: status}
}

func (c *Client) persistRefreshedTokenState(ctx context.Context, statePath string, state tokenState) error {
	var lastErr error
	for attempt := 1; attempt <= tokenStatePersistenceAttempts; attempt++ {
		if err := c.persistState(statePath, state); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == tokenStatePersistenceAttempts {
			break
		}
		if err := sleepBeforeRetry(ctx, minimumTokenStateRetryDelay); err != nil {
			return fmt.Errorf("retry refreshed Jinko token-state persistence: %w (last persistence error: %v)", err, lastErr)
		}
	}
	return lastErr
}

func (c *Client) activateTokenStateLocked(state tokenState) {
	c.bearerToken = normalizeBearerToken(state.AccessToken)
	c.refreshToken = strings.TrimSpace(state.RefreshToken)
	c.bearerTokenExp, c.hasBearerTokenExp = tokenStateExpiry(state)
	c.tokenUpdatedAt = state.UpdatedAt
	c.refreshedUnknown = !c.hasBearerTokenExp
	c.unknownExpirySent = false
	c.refreshInFlight = false
	c.refreshUncertain = state.RefreshOutcomeUncertain
	c.uncertainSent = false
	c.tokenVersion++
}

func (c *Client) tokenTransactionTimeout() time.Duration {
	if c.cfg.Timeout > 0 && c.cfg.Timeout < defaultTokenTransactionTimeout {
		return c.cfg.Timeout
	}
	return defaultTokenTransactionTimeout
}

func refreshedTokenExpiry(accessToken string, expiresIn json.RawMessage, issuedAt time.Time) (time.Time, bool) {
	if expiresAt, ok := bearerExpiry(accessToken); ok {
		return expiresAt, true
	}
	seconds, ok := oauthExpiresInSeconds(expiresIn)
	if !ok {
		return time.Time{}, false
	}
	return issuedAt.Add(time.Duration(seconds) * time.Second), true
}

func oauthExpiresInSeconds(raw json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return 0, false
	}
	if strings.HasPrefix(text, `"`) {
		var quoted string
		if err := json.Unmarshal(raw, &quoted); err != nil {
			return 0, false
		}
		text = strings.TrimSpace(quoted)
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil || seconds <= 0 || seconds > maximumOAuthExpiresInSeconds {
		return 0, false
	}
	return seconds, true
}

func isRefreshOutcomeUncertainError(err error) bool {
	var uncertainErr refreshOutcomeUncertainError
	return errors.As(err, &uncertainErr)
}

func isTokenStateDurabilityError(err error) bool {
	var durabilityErr tokenStateDurabilityError
	return errors.As(err, &durabilityErr)
}

func pendingTokenDurabilityError() error {
	return tokenStateDurabilityError{err: errors.New("rotated Jinko token state is pending durable persistence")}
}

func (c *Client) detailTokenSnapshot() (string, time.Time, bool, uint64, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.pendingTokenState != nil {
		return "", time.Time{}, false, c.tokenVersion, pendingTokenDurabilityError()
	}
	return c.bearerToken, c.bearerTokenExp, c.hasBearerTokenExp, c.tokenVersion, nil
}

func (c *Client) tokenSnapshot() (string, time.Time, bool, uint64) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	return c.bearerToken, c.bearerTokenExp, c.hasBearerTokenExp, c.tokenVersion
}

func (c *Client) hasUsableBearerToken() bool {
	token, expiresAt, hasExpiry, _ := c.tokenSnapshot()
	return token != "" && (!hasExpiry || expiresAt.After(time.Now()))
}

func (c *Client) requestAttempts() int {
	if c.cfg.RetryAttempts < 1 {
		return 1
	}
	return c.cfg.RetryAttempts
}

func (c *Client) retryDelay(failedAttempt int) time.Duration {
	if c.cfg.RetryBackoff <= 0 {
		return 0
	}
	if failedAttempt <= 1 {
		return c.cfg.RetryBackoff
	}

	delay := c.cfg.RetryBackoff
	const maxRetryDelay = time.Duration(1<<63 - 1)
	for i := 1; i < failedAttempt; i++ {
		if delay > maxRetryDelay/2 {
			return maxRetryDelay
		}
		delay *= 2
	}
	return delay
}

func isRetryableRequestError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	text := strings.ToLower(err.Error())
	if strings.Contains(text, "tls handshake timeout") ||
		strings.Contains(text, "connection reset") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "connection aborted") ||
		strings.Contains(text, "server closed idle connection") ||
		strings.Contains(text, "temporary failure") {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func shouldRetryHTTPStatus(status int) bool {
	return status >= http.StatusInternalServerError && status <= 599
}

func sleepBeforeRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readResponseBody(body io.Reader) ([]byte, error) {
	// Read one byte past the cap so callers can report oversize responses
	// without buffering an unbounded upstream body.
	raw, err := io.ReadAll(io.LimitReader(body, maxHTTPResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxHTTPResponseBodyBytes {
		return raw[:maxHTTPResponseBodyBytes], fmt.Errorf("response body exceeds %d bytes", maxHTTPResponseBodyBytes)
	}
	return raw, nil
}

func normalizeBearerToken(token string) string {
	token = strings.TrimSpace(token)
	if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	}
	return strings.TrimSpace(token)
}

func bearerExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

func tokenStateExpiry(state tokenState) (time.Time, bool) {
	if expiry, ok := bearerExpiry(normalizeBearerToken(state.AccessToken)); ok {
		return expiry, true
	}
	if !state.ExpiresAt.IsZero() {
		return state.ExpiresAt, true
	}
	return time.Time{}, false
}

func preferTokenState(configuredToken string, configuredExpiry time.Time, hasConfiguredExpiry bool, state tokenState) bool {
	stateToken := normalizeBearerToken(state.AccessToken)
	if stateToken == "" {
		return false
	}
	if configuredToken == "" || stateToken == normalizeBearerToken(configuredToken) {
		return true
	}
	stateExpiry, hasStateExpiry := tokenStateExpiry(state)
	switch {
	case hasConfiguredExpiry && hasStateExpiry:
		return stateExpiry.After(configuredExpiry)
	case hasStateExpiry:
		return true
	case hasConfiguredExpiry:
		return false
	default:
		return !state.UpdatedAt.IsZero()
	}
}

func loadTokenState(path string) (tokenState, error) {
	var state tokenState
	path = strings.TrimSpace(path)
	if path == "" {
		return state, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("open token state: %w", err)
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(io.LimitReader(file, maxTokenStateBytes+1))
	if err != nil {
		return state, fmt.Errorf("read token state: %w", err)
	}
	if len(raw) > maxTokenStateBytes {
		return state, fmt.Errorf("token state exceeds %d bytes", maxTokenStateBytes)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return tokenState{}, fmt.Errorf("decode token state: %w", err)
	}
	return state, nil
}

func persistTokenState(path string, state tokenState) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".jinko-token-state-*")
	if err != nil {
		return fmt.Errorf("create temporary token state: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		_ = tmp.Close()
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary token state: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(state); err != nil {
		return fmt.Errorf("encode token state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary token state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary token state: %w", err)
	}
	if err := replaceTokenStateFile(tmpName, path); err != nil {
		return fmt.Errorf("replace token state: %w", err)
	}
	removeTemp = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure token state: %w", err)
	}
	if err := syncTokenStateLocation(path); err != nil {
		return fmt.Errorf("sync token state location: %w", err)
	}
	return nil
}

func (c *Client) checkBearerToken(ctx context.Context) {
	if c.alerts == nil || c.hasRefreshCredentials() {
		return
	}

	_, expiry, hasExpiry, _ := c.tokenSnapshot()
	if !hasExpiry {
		return
	}

	now := time.Now()
	if !expiry.After(now) {
		c.alerts.Notify(ctx, alert.Event{
			Key:     "jinko_bearer_token_expired",
			Subject: "Jinko bearer token expired",
			Body: fmt.Sprintf(
				"The configured Jinko bearer token is already expired.\n\nSource: %s\nDevice ID: %d\nSite ID: %d\nExpired At: %s\nCurrent Time: %s\n\nReplace JINKO_BEARER_TOKEN before the next successful poll.",
				c.Name(),
				c.cfg.DeviceID,
				c.cfg.SiteID,
				expiry.UTC().Format(time.RFC3339),
				now.UTC().Format(time.RFC3339),
			),
		})
		return
	}

	if c.cfg.TokenAlertWindow > 0 && expiry.Sub(now) <= c.cfg.TokenAlertWindow {
		c.alerts.Notify(ctx, alert.Event{
			Key:     "jinko_bearer_token_expiring_soon",
			Subject: "Jinko bearer token expiring soon",
			Body: fmt.Sprintf(
				"The configured Jinko bearer token is close to expiry.\n\nSource: %s\nDevice ID: %d\nSite ID: %d\nExpires At: %s\nTime Remaining: %s\nAlert Window: %s\n\nRefresh JINKO_BEARER_TOKEN before it expires.",
				c.Name(),
				c.cfg.DeviceID,
				c.cfg.SiteID,
				expiry.UTC().Format(time.RFC3339),
				expiry.Sub(now).Round(time.Second),
				c.cfg.TokenAlertWindow,
			),
		})
	}
}

func (c *Client) alertRefreshFailure(ctx context.Context, phase string) {
	if c.alerts == nil {
		return
	}
	c.alerts.Notify(ctx, alert.Event{
		Key:     "jinko_token_refresh_failure_" + phase,
		Subject: "Jinko token refresh failed",
		Body: fmt.Sprintf(
			"Jinko could not refresh its access token.\n\nSource: %s\nPhase: %s\n\nNo token or upstream response body is included in this alert.",
			c.Name(),
			phase,
		),
	})
}

func (c *Client) alertAuthFailure(ctx context.Context, status int) {
	if c.alerts == nil {
		return
	}

	c.alerts.Notify(ctx, alert.Event{
		Key:     fmt.Sprintf("jinko_auth_failure_%d", status),
		Subject: fmt.Sprintf("Jinko API authentication failed with HTTP %d", status),
		Body: fmt.Sprintf(
			"The Jinko detail request returned an authentication-related HTTP status.\n\nSource: %s\nDevice ID: %d\nSite ID: %d\nStatus: %d\nURL: %s\n\nCheck the configured Jinko refresh token, bearer token, and cookie.",
			c.Name(),
			c.cfg.DeviceID,
			c.cfg.SiteID,
			status,
			c.cfg.URL,
		),
	})
}
