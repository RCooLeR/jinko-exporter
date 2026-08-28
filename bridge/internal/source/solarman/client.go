package solarman

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
	"github.com/rs/zerolog/log"
)

var _ source.Source = (*Client)(nil)

const yearlyRequestWindow = 365 * 24 * time.Hour
const maxHTTPResponseBodyBytes = 2 * 1024 * 1024
const transientRequestAttempts = 2
const transientRetryBackoff = 100 * time.Millisecond

// Solarman normally returns a short-lived access token. A deliberately broad
// one-year ceiling accepts realistic deployments while rejecting corrupt or
// hostile values long before duration/time arithmetic becomes ambiguous.
const maxTokenLifetimeSeconds = int64((365 * 24 * time.Hour) / time.Second)

type Client struct {
	cfg    config.SolarmanConfig
	hc     *http.Client
	alerts *alert.Manager

	mu              sync.Mutex
	token           tokenResponse
	discoveredDevSN string
	discoveredAt    time.Time
	lastRequestAt   time.Time
}

type tokenResponse struct {
	Success      bool   `json:"success"`
	Msg          string `json:"msg"`
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in,string"`
	ExpiresAt    time.Time
}

// tokenFailureError deliberately exposes only structured, non-secret context.
// In particular, its string representation must never include an upstream token
// response or the credentials sent to the token endpoint.
type tokenFailureError struct {
	status   int
	category string
	cause    error
}

func (e *tokenFailureError) Error() string {
	status := "unavailable"
	if e.status > 0 {
		status = strconv.Itoa(e.status)
	}
	return fmt.Sprintf("solarman token request failed: stage=token status=%s category=%s", status, e.category)
}

func (e *tokenFailureError) Unwrap() error {
	return e.cause
}

func newTokenFailure(status int, category string, cause error) error {
	return &tokenFailureError{status: status, category: category, cause: cause}
}

// requestFailureError keeps operationally useful, closed-vocabulary context
// while retaining the original cause for errors.Is/errors.As. Its Error method
// must never stringify the cause: net/http errors may contain the full URL and
// upstream responses may contain account or device data.
type requestFailureError struct {
	stage    string
	status   int
	category string
	cause    error
}

func (e *requestFailureError) Error() string {
	status := "unavailable"
	if e.status > 0 {
		status = strconv.Itoa(e.status)
	}
	return fmt.Sprintf("solarman request failed: stage=%s status=%s category=%s", e.stage, status, e.category)
}

func (e *requestFailureError) Unwrap() error {
	return e.cause
}

func newRequestFailure(stage string, status int, category string, cause error) error {
	return &requestFailureError{stage: stage, status: status, category: category, cause: cause}
}

func requestFailureCategory(fallback string, err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return fallback
	}
}

func requestStage(path string) string {
	switch {
	case strings.HasSuffix(path, "/currentData"):
		return "currentData"
	case strings.HasSuffix(path, "/station-list"):
		return "station-list"
	case strings.Contains(path, "/station/") && strings.HasSuffix(path, "/list"):
		return "station-list"
	case strings.Contains(path, "/station/") && strings.HasSuffix(path, "/device"):
		return "station-device-list"
	case strings.Contains(path, "/account/") && strings.HasSuffix(path, "/token"):
		return "token"
	default:
		return "unknown"
	}
}

type station struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type device struct {
	DeviceSN string `json:"deviceSn"`
}

func New(cfg config.SolarmanConfig, alerts *alert.Manager) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		cfg: cfg,
		hc: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
			// Every Solarman request carries either account credentials or an
			// access token. Treat redirects as responses so net/http never replays
			// those credentials to a different endpoint.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		alerts: alerts,
	}
}

func (c *Client) Name() string {
	return "solarman"
}

func (c *Client) Fetch(ctx context.Context) (*model.Snapshot, error) {
	deviceSN, err := c.resolveDeviceSN(ctx)
	if err != nil {
		c.notifyFailure(ctx, "device-discovery", err)
		return nil, err
	}
	if strings.TrimSpace(deviceSN) == "" {
		return nil, newRequestFailure("device-discovery", 0, "empty-device-serial", nil)
	}

	body := map[string]any{"deviceSn": deviceSN}
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/device/%s/currentData", c.cfg.APIVersion), true, body)
	if err != nil {
		c.notifyFailure(ctx, "currentData", err)
		return nil, err
	}
	if status != http.StatusOK {
		err := newRequestFailure("currentData", status, "http-status", nil)
		c.notifyFailure(ctx, "currentData", err)
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		failure := newRequestFailure("currentData", status, "decode", err)
		log.Error().Err(failure).Str("source", c.Name()).Msg("failed to decode Solarman currentData response")
		c.notifyFailure(ctx, "currentData-decode", failure)
		return nil, failure
	}
	if success, ok := payload["success"].(bool); ok && !success {
		err := newRequestFailure("currentData", status, "api-rejected", nil)
		c.notifyFailure(ctx, "currentData-api-error", err)
		return nil, err
	}

	pointsAny, _ := payload["dataList"].([]any)
	metrics := make([]model.Metric, 0, len(pointsAny))
	for _, item := range pointsAny {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := firstString(entry, "key", "dataKey", "id", "sn")
		name := firstString(entry, "name", "dataName", "title", "paramName")
		unit := firstString(entry, "unit", "dataUnit")
		value, ok := toFloat(entry["value"])
		if !ok {
			value, ok = toFloat(entry["val"])
			if !ok {
				continue
			}
		}
		metric, ok := c.metricFromPoint(key, name, unit, value)
		if !ok {
			continue
		}
		metrics = append(metrics, metric)
	}
	if len(metrics) == 0 {
		err := newRequestFailure("currentData", status, "no-numeric-metrics", nil)
		c.notifyFailure(ctx, "currentData-empty", err)
		return nil, err
	}

	return &model.Snapshot{
		Source:      c.Name(),
		DeviceSN:    deviceSN,
		CollectedAt: time.Now().UTC(),
		Metrics:     metrics,
		Meta: map[string]string{
			"base_url": c.cfg.BaseURL,
		},
	}, nil
}

func (c *Client) metricFromPoint(key, name, unit string, value float64) (model.Metric, bool) {
	if key == "" {
		key = jinko.SanitizeKey(name)
	}
	metric := model.Metric{
		Group: classifyGroup(key, name),
		Key:   key,
		Name:  name,
		Unit:  unit,
		Value: value,
	}
	// A known logical metric must have one stable label set regardless of
	// whether it came from Jinko, Solarman, or the local Modbus reader. This is
	// especially important when Prometheus's source label is disabled: a
	// source-specific group, name, or unit would otherwise create a second time
	// series for the same key after failover.
	if canonical, ok := jinko.CanonicalizeMetric(metric); ok {
		return canonical, true
	}

	// Keep Solarman-only points in compatibility mode. The legacy strict flag
	// still limits the surface to metrics present in the shared Jinko dictionary.
	if c.cfg.CanonicalJinkoMetrics {
		return model.Metric{}, false
	}
	return metric, true
}

func (c *Client) resolveDeviceSN(ctx context.Context) (string, error) {
	if strings.TrimSpace(c.cfg.DeviceSN) != "" {
		return strings.TrimSpace(c.cfg.DeviceSN), nil
	}

	if err := c.ensureToken(ctx); err != nil {
		return "", err
	}

	now := time.Now()
	c.mu.Lock()
	if c.discoveredDevSN != "" {
		if c.cfg.DiscoveryRefreshInterval == 0 || now.Sub(c.discoveredAt) < c.cfg.DiscoveryRefreshInterval {
			defer c.mu.Unlock()
			return c.discoveredDevSN, nil
		}
		log.Info().
			Str("source", c.Name()).
			Time("discovered_at", c.discoveredAt).
			Dur("refresh_interval", c.cfg.DiscoveryRefreshInterval).
			Msg("refreshing cached Solarman device discovery")
	}
	c.mu.Unlock()

	// Solarman can discover device SNs through stations, but we cache the first successful result
	// so normal polling does not keep calling discovery endpoints.
	stationID := c.cfg.StationID
	if stationID == 0 {
		stations, err := c.listStations(ctx)
		if err != nil {
			return "", err
		}
		if len(stations) == 0 {
			return "", newRequestFailure("device-discovery", http.StatusOK, "no-stations", nil)
		}
		stationID = stations[0].ID
		log.Info().Str("source", c.Name()).Msg("using first Solarman station for discovery")
	}

	devices, err := c.listStationDevices(ctx, stationID)
	if err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", newRequestFailure("device-discovery", http.StatusOK, "no-devices", nil)
	}
	deviceSN := strings.TrimSpace(devices[0].DeviceSN)
	if deviceSN == "" {
		return "", newRequestFailure("device-discovery", http.StatusOK, "empty-device-serial", nil)
	}

	c.mu.Lock()
	c.discoveredDevSN = deviceSN
	c.discoveredAt = time.Now()
	c.mu.Unlock()

	log.Info().Str("source", c.Name()).Msg("discovered Solarman device serial number")
	return deviceSN, nil
}

func (c *Client) listStations(ctx context.Context) ([]station, error) {
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/station/%s/list", c.cfg.APIVersion), false, map[string]any{})
	if err != nil {
		c.notifyFailure(ctx, "station-list", err)
		return nil, err
	}
	if status != http.StatusOK {
		err := newRequestFailure("station-list", status, "http-status", nil)
		c.notifyFailure(ctx, "station-list", err)
		return nil, err
	}
	var payload struct {
		StationList []station `json:"stationList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		failure := newRequestFailure("station-list", status, "decode", err)
		c.notifyFailure(ctx, "station-list-decode", failure)
		return nil, failure
	}
	return payload.StationList, nil
}

func (c *Client) listStationDevices(ctx context.Context, stationID int64) ([]device, error) {
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/station/%s/device", c.cfg.APIVersion), false, map[string]any{"stationId": stationID})
	if err != nil {
		c.notifyFailure(ctx, "station-device-list", err)
		return nil, err
	}
	if status != http.StatusOK {
		err := newRequestFailure("station-device-list", status, "http-status", nil)
		c.notifyFailure(ctx, "station-device-list", err)
		return nil, err
	}
	var payload struct {
		DeviceList      []device `json:"deviceList"`
		DeviceListItems []device `json:"deviceListItems"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		failure := newRequestFailure("station-device-list", status, "decode", err)
		c.notifyFailure(ctx, "station-device-list-decode", failure)
		return nil, failure
	}
	if len(payload.DeviceListItems) > 0 {
		return payload.DeviceListItems, nil
	}
	return payload.DeviceList, nil
}

func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token.AccessToken != "" && time.Now().Before(token.ExpiresAt) {
		return nil
	}
	return c.obtainToken(ctx)
}

func (c *Client) obtainToken(ctx context.Context) error {
	passHex, err := c.passwordSHA256Hex()
	if err != nil {
		err = newTokenFailure(0, "credentials", err)
		c.notifyFailure(ctx, "token", err)
		return err
	}

	body := map[string]any{
		"appSecret": c.cfg.AppSecret,
		"email":     c.cfg.Email,
		"password":  passHex,
	}
	raw, status, err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/account/%s/token", c.cfg.APIVersion), true, false, body)
	if err != nil {
		category := "transport"
		switch {
		case errors.Is(err, context.Canceled):
			category = "canceled"
		case errors.Is(err, context.DeadlineExceeded):
			category = "timeout"
		}
		err = newTokenFailure(0, category, err)
		c.notifyFailure(ctx, "token", err)
		return err
	}
	if status != http.StatusOK {
		err := newTokenFailure(status, "http-status", nil)
		c.notifyFailure(ctx, "token", err)
		return err
	}

	var token tokenResponse
	if err := json.Unmarshal(raw, &token); err != nil {
		err = newTokenFailure(status, "decode", err)
		c.notifyFailure(ctx, "token-decode", err)
		return err
	}
	if !token.Success {
		err := newTokenFailure(status, "api-rejected", nil)
		c.notifyFailure(ctx, "token-api-error", err)
		return err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		err := newTokenFailure(status, "missing-access-token", nil)
		c.notifyFailure(ctx, "token-api-error", err)
		return err
	}
	expiresAt, err := tokenExpiry(time.Now(), token.ExpiresIn)
	if err != nil {
		err = newTokenFailure(status, "invalid-expires-in", err)
		c.notifyFailure(ctx, "token-api-error", err)
		return err
	}
	token.ExpiresAt = expiresAt

	c.mu.Lock()
	c.token = token
	c.mu.Unlock()

	log.Info().Str("source", c.Name()).Time("expires_at", token.ExpiresAt).Msg("obtained Solarman access token")
	return nil
}

func tokenExpiry(now time.Time, expiresInSeconds int64) (time.Time, error) {
	if expiresInSeconds <= 0 || expiresInSeconds > maxTokenLifetimeSeconds {
		return time.Time{}, fmt.Errorf("expires_in must be between 1 and %d seconds", maxTokenLifetimeSeconds)
	}

	lifetime := time.Duration(expiresInSeconds) * time.Second
	refreshSkew := 5 * time.Second
	if lifetime <= refreshSkew {
		// Even unusually short valid lifetimes remain usable for part of their
		// advertised window instead of being stored as already expired.
		refreshSkew = lifetime / 2
	}
	return now.Add(lifetime - refreshSkew), nil
}

func (c *Client) passwordSHA256Hex() (string, error) {
	if strings.TrimSpace(c.cfg.PasswordSHA256) != "" {
		return strings.ToLower(strings.TrimSpace(c.cfg.PasswordSHA256)), nil
	}
	if strings.TrimSpace(c.cfg.Password) == "" {
		return "", fmt.Errorf("missing Solarman password")
	}
	sum := sha256.Sum256([]byte(c.cfg.Password))
	return hex.EncodeToString(sum[:]), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, withAppLang bool, withAuth bool, body any) ([]byte, int, error) {
	stage := requestStage(path)
	u, err := c.buildURL(path, withAppLang)
	if err != nil {
		return nil, 0, newRequestFailure(stage, 0, "build-url", err)
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, newRequestFailure(stage, 0, "encode-request", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, newRequestFailure(stage, 0, requestFailureCategory("build-request", err), err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if withAuth {
		c.mu.Lock()
		tokenType := strings.TrimSpace(c.token.TokenType)
		if tokenType == "" {
			tokenType = "Bearer"
		}
		req.Header.Set("Authorization", tokenType+" "+strings.TrimSpace(c.token.AccessToken))
		c.mu.Unlock()
	}

	if err := c.waitForRequestBudget(ctx); err != nil {
		return nil, 0, err
	}

	log.Info().
		Str("source", c.Name()).
		Str("stage", stage).
		Str("method", method).
		Bool("with_auth", withAuth).
		Int("request_bytes", len(payload)).
		Msg("sending API request")

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		failure := newRequestFailure(stage, 0, requestFailureCategory("transport", err), err)
		log.Error().Err(failure).Str("source", c.Name()).Str("stage", stage).Msg("API request failed")
		return nil, 0, failure
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := readResponseBody(resp.Body)
	if err != nil {
		return nil, 0, newRequestFailure(stage, resp.StatusCode, requestFailureCategory("read-response", err), err)
	}

	log.Info().
		Str("source", c.Name()).
		Str("stage", stage).
		Str("method", method).
		Int("status", resp.StatusCode).
		Dur("duration", time.Since(start)).
		Int("response_bytes", len(raw)).
		Msg("received API response")

	return raw, resp.StatusCode, nil
}

func (c *Client) waitForRequestBudget(ctx context.Context) error {
	if c.cfg.YearlyRequestLimit <= 0 {
		return nil
	}

	minInterval := yearlyRequestWindow / time.Duration(c.cfg.YearlyRequestLimit)
	if minInterval <= 0 {
		return nil
	}

	for {
		c.mu.Lock()
		wait := time.Until(c.lastRequestAt.Add(minInterval))
		if c.lastRequestAt.IsZero() || wait <= 0 {
			c.lastRequestAt = time.Now()
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()

		log.Info().
			Str("source", c.Name()).
			Dur("wait", wait).
			Int("yearly_request_limit", c.cfg.YearlyRequestLimit).
			Msg("waiting to stay within Solarman API request budget")

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) doJSONAuthRetry(ctx context.Context, method, path string, withAppLang bool, body any) ([]byte, int, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, 0, err
	}

	// Solarman regularly returns 401 when the short-lived token expires. Refresh once and retry
	// so both discovery and metric reads behave the same way.
	raw, status, err := c.doJSONTransientRetry(ctx, method, path, withAppLang, true, body)
	if err != nil {
		return nil, 0, err
	}
	if status != http.StatusUnauthorized {
		return raw, status, nil
	}

	log.Warn().Str("source", c.Name()).Str("stage", requestStage(path)).Msg("received 401 from Solarman API, refreshing token and retrying once")
	if err := c.obtainToken(ctx); err != nil {
		c.notifyFailure(ctx, "token-refresh-after-401", err)
		return raw, status, fmt.Errorf("solarman token refresh after 401 failed: %w", err)
	}
	return c.doJSONTransientRetry(ctx, method, path, withAppLang, true, body)
}

func (c *Client) doJSONTransientRetry(ctx context.Context, method, path string, withAppLang bool, withAuth bool, body any) ([]byte, int, error) {
	var lastRaw []byte
	var lastStatus int
	var lastErr error

	for attempt := 1; attempt <= transientRequestAttempts; attempt++ {
		raw, status, err := c.doJSON(ctx, method, path, withAppLang, withAuth, body)
		lastRaw, lastStatus, lastErr = raw, status, err
		if err == nil && !shouldRetrySolarmanStatus(status) {
			return raw, status, nil
		}
		if status == http.StatusUnauthorized || attempt == transientRequestAttempts {
			return raw, status, err
		}

		log.Warn().
			Err(err).
			Str("source", c.Name()).
			Str("stage", requestStage(path)).
			Int("status", status).
			Int("attempt", attempt).
			Int("max_attempts", transientRequestAttempts).
			Dur("retry_in", transientRetryBackoff).
			Msg("Solarman API request failed transiently, retrying")
		if err := sleepBeforeSolarmanRetry(ctx, transientRetryBackoff); err != nil {
			return nil, 0, err
		}
	}

	return lastRaw, lastStatus, lastErr
}

func shouldRetrySolarmanStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func sleepBeforeSolarmanRetry(ctx context.Context, delay time.Duration) error {
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

func (c *Client) notifyFailure(ctx context.Context, step string, err error) {
	if c.alerts == nil || err == nil || source.IsDiagnosticFetch(ctx) {
		return
	}

	if tokenErr, ok := errors.AsType[*tokenFailureError](err); ok {
		c.notifyTokenFailure(ctx, step, tokenErr)
		return
	}

	status := "unavailable"
	category := "internal"
	if requestErr, ok := errors.AsType[*requestFailureError](err); ok {
		category = requestErr.category
		if requestErr.status > 0 {
			status = strconv.Itoa(requestErr.status)
		}
	} else {
		category = requestFailureCategory(category, err)
	}

	subject := fmt.Sprintf("Solarman request failure: %s", step)
	message := fmt.Sprintf(
		"A Solarman request failed.\n\nSource: %s\nStep: %s\nStatus: %s\nCategory: %s\n\nNo URL, account/device identity, credential, or upstream response body is included.",
		c.Name(),
		step,
		status,
		category,
	)

	c.alerts.Notify(ctx, alert.Event{
		Key:     "solarman_" + sanitizeAlertKey(step),
		Subject: subject,
		Body:    message,
	})
}

func (c *Client) notifyTokenFailure(ctx context.Context, step string, err *tokenFailureError) {
	subject := fmt.Sprintf("Solarman authentication failure: %s", step)
	message := fmt.Sprintf(
		"A Solarman authentication request failed.\n\nSource: %s\nStep: %s\nError: %s",
		c.Name(),
		step,
		err.Error(),
	)

	c.alerts.Notify(ctx, alert.Event{
		Key:     "solarman_" + sanitizeAlertKey(step),
		Subject: subject,
		Body:    message,
	})
}

func sanitizeAlertKey(value string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", "-", "_", ":", "_")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
}

func (c *Client) buildURL(path string, withAppLang bool) (string, error) {
	base := strings.TrimRight(c.cfg.BaseURL, "/")
	u, err := url.Parse(base + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	if withAppLang {
		query := u.Query()
		query.Set("appId", c.cfg.AppID)
		query.Set("language", c.cfg.Language)
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
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

func classifyGroup(key string, name string) string {
	text := strings.ToLower(key + " " + name)
	normalizedKey := strings.ToUpper(strings.TrimSpace(key))
	switch {
	case strings.HasPrefix(normalizedKey, "DV"),
		strings.HasPrefix(normalizedKey, "DC"),
		strings.HasPrefix(normalizedKey, "DP"),
		strings.HasPrefix(normalizedKey, "AV"),
		strings.HasPrefix(normalizedKey, "A_V_"),
		strings.HasPrefix(normalizedKey, "INV_O_P_"),
		strings.HasPrefix(normalizedKey, "AC_S_"),
		normalizedKey == "AC1",
		normalizedKey == "AC2",
		normalizedKey == "AC3",
		normalizedKey == "S_P_T",
		normalizedKey == "PV_D_P_G",
		normalizedKey == "O_P",
		normalizedKey == "P_F1",
		normalizedKey == "A_FO1",
		strings.HasPrefix(normalizedKey, "ET_GE"),
		strings.HasPrefix(normalizedKey, "ETDY_GE"),
		strings.Contains(text, "solar"),
		strings.Contains(text, " pv"),
		strings.Contains(text, "dc "),
		strings.Contains(text, "inverter output"):
		return "electric"
	case strings.Contains(text, "grid"):
		return "grid"
	case strings.Contains(text, "alarm"), strings.Contains(text, "fault"):
		return "alert"
	case strings.Contains(text, "bms"):
		return "bms"
	case strings.Contains(text, "battery"), strings.Contains(text, "soc"):
		return "battery"
	case strings.Contains(text, "load"), strings.Contains(text, "house"), strings.Contains(text, "consumption"):
		return "consumption"
	case strings.Contains(text, "temp"):
		return "temperature"
	case strings.Contains(text, "gen"):
		return "generator"
	default:
		return "inverter"
	}
}

func firstString(entry map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := entry[key]
		if !ok {
			continue
		}
		if asString, ok := value.(string); ok && strings.TrimSpace(asString) != "" {
			return strings.TrimSpace(asString)
		}
	}
	return ""
}

func toFloat(value any) (float64, bool) {
	finite := func(candidate float64) (float64, bool) {
		return candidate, !math.IsNaN(candidate) && !math.IsInf(candidate, 0)
	}
	switch typed := value.(type) {
	case float64:
		return finite(typed)
	case float32:
		return finite(float64(typed))
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return finite(v)
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, false
		}
		return finite(v)
	default:
		return 0, false
	}
}
