package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
)

const (
	maxHomeAssistantResponseBytes = 64 * 1024
	maxNotificationTitleRunes     = 200
	maxNotificationMessageRunes   = 4000
	maxNotificationDataEntries    = 16
	maxNotificationDataValueRunes = 512
)

var (
	errHomeAssistantRequest = errors.New("home assistant notification request failed")
	notificationDataKey     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
)

// HomeAssistantNotification is the deliberately small payload supported by
// the dedicated mobile-app notifier. String-only data prevents an alert path
// from becoming a generic Home Assistant service-call proxy.
type HomeAssistantNotification struct {
	Title   string
	Message string
	Data    map[string]string
}

// HomeAssistantNotifier sends one bounded, non-retried POST to one configured
// notify.mobile_app_* service. It is separate from Manager so Modbus alert
// correlation cannot accidentally inherit the generic SMTP alert lifecycle.
type HomeAssistantNotifier struct {
	endpoint *url.URL
	token    string
	client   *http.Client
}

type lookupNetIPFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func NewHomeAssistantNotifier(cfg config.HomeAssistantConfig) (*HomeAssistantNotifier, error) {
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	return newHomeAssistantNotifierWithNetwork(cfg, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

func newHomeAssistantNotifierWithNetwork(cfg config.HomeAssistantConfig, lookup lookupNetIPFunc, dial dialContextFunc) (*HomeAssistantNotifier, error) {
	if err := config.ValidateHomeAssistantConfig(cfg); err != nil {
		return nil, err
	}
	service, err := config.NormalizeHomeAssistantNotifyService(cfg.NotifyService)
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil {
		// ValidateHomeAssistantConfig already parsed the same value. Keep this
		// fallback static so a rejected URL is never reflected in an error.
		return nil, fmt.Errorf("home assistant notifier endpoint is invalid")
	}
	baseURL.Path = "/api/services/notify/" + service
	baseURL.RawPath = ""
	baseURL.RawQuery = ""
	baseURL.ForceQuery = false
	baseURL.Fragment = ""
	baseURL.RawFragment = ""

	dialContext := dial
	if strings.EqualFold(baseURL.Scheme, "http") {
		if _, err := netip.ParseAddr(baseURL.Hostname()); err != nil {
			dialContext = privateResolvedDialContext(baseURL, lookup, dial)
		}
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialContext,
		DisableKeepAlives:   true,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: cfg.Timeout,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &HomeAssistantNotifier{
		endpoint: baseURL,
		token:    cfg.Token,
		client:   client,
	}, nil
}

// privateResolvedDialContext closes the DNS-rebinding gap introduced by the
// explicitly allowed single-label HTTP hostname. Every lookup result must be a
// private/loopback address, and the connection is made to that approved IP
// rather than asking the dialer to resolve the hostname a second time.
func privateResolvedDialContext(endpoint *url.URL, lookup lookupNetIPFunc, dial dialContextFunc) dialContextFunc {
	expectedHost := endpoint.Hostname()
	expectedPort := endpoint.Port()
	if expectedPort == "" {
		expectedPort = "80"
	}
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, expectedHost) || port != expectedPort {
			return nil, fmt.Errorf("home assistant notification destination was rejected")
		}
		addresses, err := lookup(ctx, "ip", expectedHost)
		if err != nil || len(addresses) == 0 {
			return nil, fmt.Errorf("home assistant internal hostname could not be resolved safely")
		}
		approved := make([]netip.Addr, 0, len(addresses))
		for _, candidate := range addresses {
			candidate = candidate.Unmap()
			if !candidate.IsValid() || (!candidate.IsPrivate() && !candidate.IsLoopback()) {
				return nil, fmt.Errorf("home assistant internal hostname resolved outside the private network")
			}
			approved = append(approved, candidate)
		}

		for _, candidate := range approved {
			conn, err := dial(ctx, network, net.JoinHostPort(candidate.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("home assistant private addresses could not be reached")
	}
}

// Notify implements Notifier without exposing arbitrary Home Assistant data.
func (n *HomeAssistantNotifier) Notify(ctx context.Context, title string, message string) error {
	return n.Send(ctx, HomeAssistantNotification{Title: title, Message: message})
}

// Send adds optional, bounded string data for mobile-app features such as a
// stable tag. Control characters are collapsed and all strings are capped
// before JSON encoding.
func (n *HomeAssistantNotifier) Send(ctx context.Context, notification HomeAssistantNotification) error {
	if n == nil || n.client == nil || n.endpoint == nil {
		return fmt.Errorf("home assistant notifier is not initialized")
	}
	if ctx == nil {
		return fmt.Errorf("home assistant notification context is required")
	}
	payload, err := sanitizeHomeAssistantNotification(notification)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("home assistant notification payload is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("home assistant notification request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+n.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "jinko-exporter/home-assistant-notifier")
	// A fresh connection plus a non-replayable body makes the single-attempt
	// contract explicit even if net/http retry behavior changes later.
	request.Close = true
	request.GetBody = nil

	response, err := n.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%w: %v", errHomeAssistantRequest, ctxErr)
		}
		return errHomeAssistantRequest
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, maxHomeAssistantResponseBytes))
	closeErr := response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("home assistant notification returned non-success status %d", response.StatusCode)
	}
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("home assistant notification response could not be closed safely")
	}
	return nil
}

type homeAssistantPayload struct {
	Title   string            `json:"title,omitempty"`
	Message string            `json:"message"`
	Data    map[string]string `json:"data,omitempty"`
}

func sanitizeHomeAssistantNotification(notification HomeAssistantNotification) (homeAssistantPayload, error) {
	payload := homeAssistantPayload{
		Title:   sanitizeNotificationText(notification.Title, maxNotificationTitleRunes),
		Message: sanitizeNotificationText(notification.Message, maxNotificationMessageRunes),
	}
	if payload.Message == "" {
		return homeAssistantPayload{}, fmt.Errorf("home assistant notification message is required")
	}
	if len(notification.Data) > maxNotificationDataEntries {
		return homeAssistantPayload{}, fmt.Errorf("home assistant notification data contains too many entries")
	}
	if len(notification.Data) > 0 {
		payload.Data = make(map[string]string, len(notification.Data))
		for key, value := range notification.Data {
			if !notificationDataKey.MatchString(key) {
				return homeAssistantPayload{}, fmt.Errorf("home assistant notification data contains an invalid key")
			}
			payload.Data[key] = sanitizeNotificationText(value, maxNotificationDataValueRunes)
		}
	}
	return payload, nil
}

func sanitizeNotificationText(value string, maxRunes int) string {
	// Cap input before any normalization allocation. UTF-8 uses at most four
	// bytes per rune, so this still leaves enough input for maxRunes output.
	if maxBytes := maxRunes * utf8.UTFMax; len(value) > maxBytes {
		value = value[:maxBytes]
	}
	value = strings.ToValidUTF8(value, string(utf8.RuneError))
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value
}

// Compile-time assertion that the dedicated notifier can be used wherever the
// narrow alert.Notifier interface is appropriate.
var _ Notifier = (*HomeAssistantNotifier)(nil)
