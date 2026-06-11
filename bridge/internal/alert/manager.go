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
}

func NewManager(notifier Notifier, cooldown time.Duration) *Manager {
	return &Manager{
		notifier: notifier,
		cooldown: cooldown,
		lastSent: make(map[string]time.Time),
		active:   make(map[string]struct{}),
	}
}

func (m *Manager) Notify(ctx context.Context, event Event) {
	if m == nil || m.notifier == nil {
		return
	}
	if event.Key == "" {
		event.Key = event.Subject
	}
	cooldown := event.Cooldown
	if cooldown <= 0 {
		cooldown = m.cooldown
	}

	if !m.shouldSend(event.Key, cooldown) {
		log.Info().Str("alert_key", event.Key).Dur("cooldown", cooldown).Msg("skipping alert due to cooldown")
		return
	}

	if err := m.notifier.Notify(ctx, event.Subject, event.Body); err != nil {
		log.Error().Err(err).Str("alert_key", event.Key).Msg("failed to deliver alert")
		return
	}

	m.markSent(event.Key)
	log.Info().Str("alert_key", event.Key).Msg("alert delivered")
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
	m.Notify(ctx, recovery)
}

func (m *Manager) shouldSend(key string, cooldown time.Duration) bool {
	if cooldown <= 0 {
		return true
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	lastSent, ok := m.lastSent[key]
	return !ok || time.Since(lastSent) >= cooldown
}

func (m *Manager) markSent(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSent[key] = time.Now()
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
