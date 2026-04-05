package solarman

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/alert"
	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/RCooLeR/jinko-exporter/internal/source"
	"github.com/RCooLeR/jinko-exporter/internal/source/jinko"
	"github.com/rs/zerolog/log"
)

var _ source.Source = (*Client)(nil)

type Client struct {
	cfg    config.SolarmanConfig
	hc     *http.Client
	alerts *alert.Manager

	mu              sync.Mutex
	token           tokenResponse
	discoveredDevSN string
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

type station struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type device struct {
	DeviceSN string `json:"deviceSn"`
}

func New(cfg config.SolarmanConfig, alerts *alert.Manager) *Client {
	return &Client{
		cfg:    cfg,
		hc:     &http.Client{Timeout: cfg.Timeout},
		alerts: alerts,
	}
}

func (c *Client) Name() string {
	return "solarman"
}

func (c *Client) Fetch(ctx context.Context) (*model.Snapshot, error) {
	deviceSN, err := c.resolveDeviceSN(ctx)
	if err != nil {
		c.notifyFailure(ctx, "device-discovery", "", err, nil)
		return nil, err
	}

	body := map[string]any{"deviceSn": deviceSN}
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/device/%s/currentData", c.cfg.APIVersion), true, body)
	if err != nil {
		c.notifyFailure(ctx, "currentData", deviceSN, err, nil)
		return nil, err
	}
	if status != http.StatusOK {
		err := fmt.Errorf("solarman currentData failed: status=%d body=%s", status, strings.TrimSpace(string(raw)))
		c.notifyFailure(ctx, "currentData", deviceSN, err, raw)
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("device_sn", deviceSN).Msg("failed to decode Solarman currentData response")
		c.notifyFailure(ctx, "currentData-decode", deviceSN, err, raw)
		return nil, fmt.Errorf("decode solarman currentData: %w", err)
	}
	if success, ok := payload["success"].(bool); ok && !success {
		err := fmt.Errorf("solarman API error: %v", payload["msg"])
		c.notifyFailure(ctx, "currentData-api-error", deviceSN, err, raw)
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
		if key == "" {
			key = jinko.SanitizeKey(name)
		}
		metrics = append(metrics, model.Metric{
			Group: classifyGroup(key, name),
			Key:   key,
			Name:  name,
			Unit:  unit,
			Value: value,
		})
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

func (c *Client) resolveDeviceSN(ctx context.Context) (string, error) {
	if strings.TrimSpace(c.cfg.DeviceSN) != "" {
		return strings.TrimSpace(c.cfg.DeviceSN), nil
	}

	if err := c.ensureToken(ctx); err != nil {
		return "", err
	}

	c.mu.Lock()
	if c.discoveredDevSN != "" {
		defer c.mu.Unlock()
		return c.discoveredDevSN, nil
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
			return "", fmt.Errorf("solarman device discovery found no stations")
		}
		stationID = stations[0].ID
		log.Info().Str("source", c.Name()).Int64("station_id", stationID).Str("station_name", stations[0].Name).Msg("using first Solarman station for discovery")
	}

	devices, err := c.listStationDevices(ctx, stationID)
	if err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", fmt.Errorf("solarman station %d has no devices", stationID)
	}

	c.mu.Lock()
	c.discoveredDevSN = devices[0].DeviceSN
	c.mu.Unlock()

	log.Info().Str("source", c.Name()).Str("device_sn", devices[0].DeviceSN).Msg("discovered Solarman device serial number")
	return devices[0].DeviceSN, nil
}

func (c *Client) listStations(ctx context.Context) ([]station, error) {
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/station/%s/list", c.cfg.APIVersion), false, map[string]any{})
	if err != nil {
		c.notifyFailure(ctx, "station-list", "", err, nil)
		return nil, err
	}
	if status != http.StatusOK {
		err := fmt.Errorf("solarman station list failed: status=%d body=%s", status, strings.TrimSpace(string(raw)))
		c.notifyFailure(ctx, "station-list", "", err, raw)
		return nil, err
	}
	var payload struct {
		StationList []station `json:"stationList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		c.notifyFailure(ctx, "station-list-decode", "", err, raw)
		return nil, err
	}
	return payload.StationList, nil
}

func (c *Client) listStationDevices(ctx context.Context, stationID int64) ([]device, error) {
	raw, status, err := c.doJSONAuthRetry(ctx, http.MethodPost, fmt.Sprintf("/station/%s/device", c.cfg.APIVersion), false, map[string]any{"stationId": stationID})
	if err != nil {
		c.notifyFailure(ctx, "station-device-list", "", err, nil)
		return nil, err
	}
	if status != http.StatusOK {
		err := fmt.Errorf("solarman station device list failed: status=%d body=%s", status, strings.TrimSpace(string(raw)))
		c.notifyFailure(ctx, "station-device-list", "", err, raw)
		return nil, err
	}
	var payload struct {
		DeviceList []device `json:"deviceList"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		c.notifyFailure(ctx, "station-device-list-decode", "", err, raw)
		return nil, err
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
		return err
	}

	body := map[string]any{
		"appSecret": c.cfg.AppSecret,
		"email":     c.cfg.Email,
		"password":  passHex,
	}
	raw, status, err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/account/%s/token", c.cfg.APIVersion), true, false, body)
	if err != nil {
		c.notifyFailure(ctx, "token", "", err, nil)
		return err
	}
	if status != http.StatusOK {
		err := fmt.Errorf("solarman token request failed: status=%d body=%s", status, strings.TrimSpace(string(raw)))
		c.notifyFailure(ctx, "token", "", err, raw)
		return err
	}

	var token tokenResponse
	if err := json.Unmarshal(raw, &token); err != nil {
		c.notifyFailure(ctx, "token-decode", "", err, raw)
		return fmt.Errorf("decode solarman token response: %w", err)
	}
	if !token.Success || token.AccessToken == "" {
		err := fmt.Errorf("solarman token error: %s", token.Msg)
		c.notifyFailure(ctx, "token-api-error", "", err, raw)
		return err
	}
	token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn-5) * time.Second)

	c.mu.Lock()
	c.token = token
	c.mu.Unlock()

	log.Info().Str("source", c.Name()).Time("expires_at", token.ExpiresAt).Msg("obtained Solarman access token")
	return nil
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
	u, err := c.buildURL(path, withAppLang)
	if err != nil {
		return nil, 0, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
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

	log.Info().
		Str("source", c.Name()).
		Str("method", method).
		Str("url", u).
		Bool("with_auth", withAuth).
		Bytes("request_body", payload).
		Msg("sending API request")

	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		log.Error().Err(err).Str("source", c.Name()).Str("url", u).Msg("API request failed")
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	log.Info().
		Str("source", c.Name()).
		Str("method", method).
		Str("url", u).
		Int("status", resp.StatusCode).
		Dur("duration", time.Since(start)).
		Int("response_bytes", len(raw)).
		Msg("received API response")

	return raw, resp.StatusCode, nil
}

func (c *Client) doJSONAuthRetry(ctx context.Context, method, path string, withAppLang bool, body any) ([]byte, int, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, 0, err
	}

	// Solarman regularly returns 401 when the short-lived token expires. Refresh once and retry
	// so both discovery and metric reads behave the same way.
	raw, status, err := c.doJSON(ctx, method, path, withAppLang, true, body)
	if err != nil {
		return nil, 0, err
	}
	if status != http.StatusUnauthorized {
		return raw, status, nil
	}

	log.Warn().Str("source", c.Name()).Str("path", path).Msg("received 401 from Solarman API, refreshing token and retrying once")
	if err := c.obtainToken(ctx); err != nil {
		c.notifyFailure(ctx, "token-refresh-after-401", "", err, raw)
		return raw, status, fmt.Errorf("solarman token refresh after 401 failed: %w", err)
	}
	return c.doJSON(ctx, method, path, withAppLang, true, body)
}

func (c *Client) notifyFailure(ctx context.Context, step string, deviceSN string, err error, raw []byte) {
	if c.alerts == nil || err == nil {
		return
	}

	body := strings.TrimSpace(string(raw))
	if len(body) > 2000 {
		body = body[:2000] + "...(truncated)"
	}

	subject := fmt.Sprintf("Solarman request failure: %s", step)
	message := fmt.Sprintf(
		"A Solarman request failed.\n\nSource: %s\nStep: %s\nBase URL: %s\nDevice SN: %s\nConfigured Station ID: %d\nError: %s",
		c.Name(),
		step,
		c.cfg.BaseURL,
		valueOrFallback(deviceSN, strings.TrimSpace(c.cfg.DeviceSN), "<discovery>"),
		c.cfg.StationID,
		err.Error(),
	)
	if body != "" {
		message += "\nResponse Body: " + body
	}

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

func valueOrFallback(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func classifyGroup(key string, name string) string {
	text := strings.ToLower(key + " " + name)
	switch {
	case strings.Contains(text, "pv"), strings.Contains(text, "dc "):
		return "pv"
	case strings.Contains(text, "grid"):
		return "grid"
	case strings.Contains(text, "bms"):
		return "bms"
	case strings.Contains(text, "battery"), strings.Contains(text, "soc"):
		return "battery"
	case strings.Contains(text, "load"), strings.Contains(text, "house"), strings.Contains(text, "consumption"):
		return "load"
	case strings.Contains(text, "temp"):
		return "temperature"
	case strings.Contains(text, "alarm"), strings.Contains(text, "fault"):
		return "alarms"
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
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(typed, 64)
		return v, err == nil
	default:
		return 0, false
	}
}
