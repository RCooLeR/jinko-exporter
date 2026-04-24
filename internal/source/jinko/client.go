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
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/alert"
	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/RCooLeR/jinko-exporter/internal/source"
	"github.com/rs/zerolog/log"
)

var _ source.Source = (*Client)(nil)

type Client struct {
	cfg               config.JinkoConfig
	hc                *http.Client
	alerts            *alert.Manager
	requestBody       []byte
	deviceID          string
	siteID            string
	bearerTokenExp    time.Time
	hasBearerTokenExp bool
}

func New(cfg config.JinkoConfig, alerts *alert.Manager) *Client {
	token := strings.TrimSpace(cfg.BearerToken)
	if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	}
	cfg.BearerToken = strings.TrimSpace(token)
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
	bearerTokenExp, hasBearerTokenExp := bearerExpiry(cfg.BearerToken)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		cfg: cfg,
		hc: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		alerts:            alerts,
		requestBody:       requestBody,
		deviceID:          fmt.Sprintf("%d", cfg.DeviceID),
		siteID:            fmt.Sprintf("%d", cfg.SiteID),
		bearerTokenExp:    bearerTokenExp,
		hasBearerTokenExp: hasBearerTokenExp,
	}
}

func (c *Client) Name() string {
	return "jinko"
}

func (c *Client) Fetch(ctx context.Context) (*model.Snapshot, error) {
	c.checkBearerToken(ctx)

	if c.cfg.RequestJitterMax > 0 {
		// The private Jinko endpoint is browser-oriented, so keep polls slightly de-synchronized.
		jitter := time.Duration(rand.Int64N(c.cfg.RequestJitterMax.Nanoseconds() + 1))
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

	raw, status, err := c.doDetailRequestWithRetry(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		c.alertAuthFailure(ctx, status, raw)
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("jinko detail request failed: status=%d body=%s", status, strings.TrimSpace(string(raw)))
	}

	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", c.cfg.URL).Msg("failed to parse Jinko detail response")
		return nil, err
	}
	snapshot.Source = c.Name()
	snapshot.DeviceID = c.deviceID
	snapshot.SiteID = c.siteID
	return snapshot, nil
}

func (c *Client) doDetailRequestWithRetry(ctx context.Context, reqBody []byte) ([]byte, int, error) {
	attempts := c.requestAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		raw, status, err := c.doDetailRequest(ctx, reqBody, attempt, attempts)
		if err == nil {
			return raw, status, nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		if attempt == attempts || !isRetryableRequestError(err) {
			log.Error().
				Err(err).
				Str("source", c.Name()).
				Str("url", c.cfg.URL).
				Int("attempt", attempt).
				Int("max_attempts", attempts).
				Msg("API request failed")
			return nil, 0, err
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

		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, 0, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, 0, fmt.Errorf("jinko detail request failed after %d attempts", attempts)
}

func (c *Client) doDetailRequest(ctx context.Context, reqBody []byte, attempt, attempts int) ([]byte, int, error) {
	req, err := c.newDetailRequest(ctx, reqBody)
	if err != nil {
		return nil, 0, err
	}

	fields := log.Info().
		Str("source", c.Name()).
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Int("attempt", attempt).
		Int("max_attempts", attempts).
		Int64("device_id", c.cfg.DeviceID).
		Int64("site_id", c.cfg.SiteID)
	if c.hasBearerTokenExp {
		fields = fields.Time("token_expires_at", c.bearerTokenExp)
	}
	fields.Bytes("request_body", reqBody).Msg("sending API request")

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
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

	return raw, resp.StatusCode, nil
}

func (c *Client) newDetailRequest(ctx context.Context, reqBody []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
	if c.cfg.Cookie != "" {
		req.Header.Set("Cookie", c.cfg.Cookie)
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	return req, nil
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
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
	return errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary())
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

func (c *Client) checkBearerToken(ctx context.Context) {
	if c.alerts == nil {
		return
	}

	if !c.hasBearerTokenExp {
		return
	}
	expiry := c.bearerTokenExp

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

func (c *Client) alertAuthFailure(ctx context.Context, status int, raw []byte) {
	if c.alerts == nil {
		return
	}

	body := strings.TrimSpace(string(raw))
	if len(body) > 2000 {
		body = body[:2000] + "...(truncated)"
	}

	c.alerts.Notify(ctx, alert.Event{
		Key:     fmt.Sprintf("jinko_auth_failure_%d", status),
		Subject: fmt.Sprintf("Jinko API authentication failed with HTTP %d", status),
		Body: fmt.Sprintf(
			"The Jinko detail request returned an authentication-related HTTP status.\n\nSource: %s\nDevice ID: %d\nSite ID: %d\nStatus: %d\nURL: %s\nResponse Body: %s\n\nRefresh JINKO_BEARER_TOKEN and, if needed, JINKO_COOKIE.",
			c.Name(),
			c.cfg.DeviceID,
			c.cfg.SiteID,
			status,
			c.cfg.URL,
			body,
		),
	})
}
