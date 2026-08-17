package alert

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Notifier interface {
	Notify(ctx context.Context, subject string, body string) error
}

type Event struct {
	Key      string
	Subject  string
	Body     string
	Cooldown time.Duration
}

type Manager struct {
	notifier Notifier
	cooldown time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time
	active   map[string]struct{}
	inFlight map[string]struct{}
}

func NewManager(notifier Notifier, cooldown time.Duration) *Manager {
	return &Manager{
		notifier: notifier,
		cooldown: cooldown,
		lastSent: make(map[string]time.Time),
		active:   make(map[string]struct{}),
		inFlight: make(map[string]struct{}),
	}
}

func (m *Manager) Notify(ctx context.Context, event Event) {
	_ = m.NotifyDelivered(ctx, event)
}

// NotifyDelivered reports whether this call delivered the event. Delivery is
// reserved atomically per key so concurrent poll/background paths cannot both
// pass the cooldown check and send duplicates. A failed delivery releases the
// reservation without advancing the cooldown, allowing a later retry.
func (m *Manager) NotifyDelivered(ctx context.Context, event Event) bool {
	if m == nil || m.notifier == nil {
		return false
	}
	if event.Key == "" {
		event.Key = event.Subject
	}
	cooldown := event.Cooldown
	if cooldown <= 0 {
		cooldown = m.cooldown
	}

	if !m.reserveSend(event.Key, cooldown) {
		log.Info().Str("alert_key", event.Key).Dur("cooldown", cooldown).Msg("skipping alert due to cooldown")
		return false
	}

	if err := m.notifier.Notify(ctx, event.Subject, event.Body); err != nil {
		m.finishSend(event.Key, false)
		log.Error().Err(err).Str("alert_key", event.Key).Msg("failed to deliver alert")
		return false
	}

	m.finishSend(event.Key, true)
	log.Info().Str("alert_key", event.Key).Msg("alert delivered")
	return true
}

func (m *Manager) NotifyActive(ctx context.Context, event Event) {
	if m == nil {
		return
	}
	if event.Key == "" {
		event.Key = event.Subject
	}
	m.markActive(event.Key)
	m.Notify(ctx, event)
}

func (m *Manager) Resolve(ctx context.Context, activeKey string, recovery Event, notify bool) {
	if m == nil || activeKey == "" {
		return
	}
	if !m.markResolved(activeKey) {
		return
	}
	if !notify {
		return
	}
	if recovery.Key == "" {
		recovery.Key = activeKey + "_recovered"
	}
	if !m.NotifyDelivered(ctx, recovery) {
		// Keep the transition retryable. A later clear snapshot can attempt the
		// recovery notification again, while a concurrently reactivated key is
		// preserved by the idempotent active set.
		m.markActive(activeKey)
	}
}

func (m *Manager) reserveSend(key string, cooldown time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.inFlight[key]; ok {
		return false
	}
	if cooldown > 0 {
		if lastSent, ok := m.lastSent[key]; ok && time.Since(lastSent) < cooldown {
			return false
		}
	}
	m.inFlight[key] = struct{}{}
	return true
}

func (m *Manager) finishSend(key string, delivered bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.inFlight, key)
	if delivered {
		m.lastSent[key] = time.Now()
	}
}

func (m *Manager) markActive(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active[key] = struct{}{}
}

func (m *Manager) markResolved(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.active[key]; !ok {
		return false
	}
	delete(m.active, key)
	return true
}
