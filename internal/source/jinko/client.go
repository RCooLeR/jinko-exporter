package jinko

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
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
	cfg    config.JinkoConfig
	hc     *http.Client
	alerts *alert.Manager
}

func New(cfg config.JinkoConfig, alerts *alert.Manager) *Client {
	token := strings.TrimSpace(cfg.BearerToken)
	if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	}
	cfg.BearerToken = strings.TrimSpace(token)
	return &Client{
		cfg: cfg,
		hc: &http.Client{
			Timeout: cfg.Timeout,
		},
		alerts: alerts,
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

	body := map[string]any{
		"deviceId":             c.cfg.DeviceID,
		"language":             c.cfg.Language,
		"needRealTimeDataFlag": c.cfg.NeedRealtimeData,
		"siteId":               c.cfg.SiteID,
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

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

	fields := log.Info().
		Str("source", c.Name()).
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Int64("device_id", c.cfg.DeviceID).
		Int64("site_id", c.cfg.SiteID)
	if exp, ok := bearerExpiry(c.cfg.BearerToken); ok {
		fields = fields.Time("token_expires_at", exp)
	}
	fields.Bytes("request_body", reqBody).Msg("sending API request")

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", req.URL.String()).Msg("API request failed")
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", req.URL.String()).Msg("failed to read API response body")
		return nil, err
	}

	log.Info().
		Str("source", c.Name()).
		Str("method", req.Method).
		Str("url", req.URL.String()).
		Int("status", resp.StatusCode).
		Dur("duration", time.Since(start)).
		Int("response_bytes", len(raw)).
		Msg("received API response")

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		c.alertAuthFailure(ctx, resp.StatusCode, raw)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jinko detail request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	snapshot, err := ParseDetailResponse(raw)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", req.URL.String()).Msg("failed to parse Jinko detail response")
		return nil, err
	}
	snapshot.Source = c.Name()
	snapshot.DeviceID = fmt.Sprintf("%d", c.cfg.DeviceID)
	snapshot.SiteID = fmt.Sprintf("%d", c.cfg.SiteID)
	return snapshot, nil
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

	expiry, ok := bearerExpiry(c.cfg.BearerToken)
	if !ok {
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
