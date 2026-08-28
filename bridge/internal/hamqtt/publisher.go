package hamqtt

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

const (
	availabilityOnline   = "online"
	availabilityOffline  = "offline"
	connectRetryInterval = 5 * time.Second
)

var (
	errClientNotConnected = errors.New("MQTT client is not connected")
	errPublisherClosed    = errors.New("MQTT publisher is closed")
)

type Publisher struct {
	cfg                     config.MQTTConfig
	client                  mqtt.Client
	topicPrefix             string
	discoveryPrefix         string
	availabilityTopic       string
	discoveryStateFile      string
	primarySource           string
	discoveryState          *discoveryState
	discoveryStatePersisted bool

	lifecycleMu      sync.Mutex
	connectAttemptMu sync.Mutex
	connectCancel    context.CancelFunc
	connectDone      chan struct{}
	// beforeConnectAttempt is an optional deterministic test barrier. It must
	// only be configured before Start.
	beforeConnectAttempt func()

	mu                 sync.Mutex
	started            bool
	closing            bool
	closed             bool
	discoveryPayloads  map[string]string
	discoveredMetrics  map[string]metricEntity
	discoveryShapeSig  string
	cachedDiscovery    []discoveryMessage
	cachedDiscoverySig string
	cachedStateTopic   string
	cachedState        []byte
	lastAvailability   string
	// offlinePublishPending records an offline transition that was not
	// acknowledged by the broker. It lets a later failed poll retry the exact
	// retained write even though lastAvailability is already offline.
	offlinePublishPending bool
}

type metricEntity struct {
	StateKey string
	Metric   model.Metric
}

type statePayload struct {
	Source              string              `json:"source"`
	DeviceSN            string              `json:"device_sn"`
	ParentSN            string              `json:"parent_sn"`
	DeviceID            string              `json:"device_id"`
	SiteID              string              `json:"site_id"`
	CollectedAt         string              `json:"collected_at"`
	PublishedAt         string              `json:"published_at"`
	Up                  bool                `json:"up"`
	Metrics             map[string]*float64 `json:"metrics"`
	MetricCount         int                 `json:"metric_count"`
	AlertMetrics        map[string]float64  `json:"alert_metrics"`
	AlertDomain         string              `json:"alert_domain"`
	AlertCount          int                 `json:"alert_count"`
	AlertsKnown         bool                `json:"alerts_known"`
	AlertsActive        bool                `json:"alerts_active"`
	PollDurationSeconds float64             `json:"poll_duration_seconds"`
	Meta                map[string]string   `json:"meta,omitempty"`
}

func NewPublisher(cfg config.MQTTConfig) (*Publisher, error) {
	topicPrefix, err := config.NormalizeMQTTTopicPrefix(cfg.TopicPrefix)
	if err != nil {
		return nil, fmt.Errorf("mqtt topic prefix: %w", err)
	}
	discoveryPrefix, err := config.NormalizeMQTTTopicPrefix(cfg.DiscoveryPrefix)
	if err != nil {
		return nil, fmt.Errorf("mqtt discovery prefix: %w", err)
	}

	p := &Publisher{
		cfg:                cfg,
		topicPrefix:        topicPrefix,
		discoveryPrefix:    discoveryPrefix,
		availabilityTopic:  topicPrefix + "/availability",
		discoveryStateFile: strings.TrimSpace(cfg.DiscoveryStateFile),
		primarySource:      normalizeSourceName(cfg.PrimarySource),
		discoveryPayloads:  make(map[string]string),
		discoveredMetrics:  make(map[string]metricEntity),
	}
	if p.discoveryStateFile != "" {
		if strings.TrimSpace(cfg.DeviceID) == "" {
			return nil, errors.New("mqtt-device-id is required when mqtt-discovery-state-file is configured")
		}
		if p.primarySource == "" {
			return nil, errors.New("MQTT primary source is required when mqtt-discovery-state-file is configured")
		}
		device := p.device(&model.Snapshot{})
		stateTopic := p.stateTopic(device.ID)
		binding := discoveryStateBinding{
			TopicPrefix:       p.topicPrefix,
			DiscoveryPrefix:   p.discoveryPrefix,
			DeviceID:          device.ID,
			DeviceIdentifier:  device.Identifier,
			StateTopic:        stateTopic,
			AvailabilityTopic: p.availabilityTopic,
			PrimarySource:     p.primarySource,
		}
		state, exists, err := loadDiscoveryState(p.discoveryStateFile, binding)
		if err != nil {
			return nil, err
		}
		p.discoveryState = &state
		p.discoveryStatePersisted = exists
		if err := p.syncPersistentMetricRegistry(); err != nil {
			return nil, err
		}
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(strings.TrimSpace(cfg.Broker))
	opts.SetClientID(strings.TrimSpace(cfg.ClientID))
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	// Track initial connection retries ourselves. Paho's ConnectRetry keeps its
	// Connect token pending in an internal retry goroutine, which cannot be
	// joined by Publisher.Close. AutoReconnect still handles connection loss
	// after the first successful connection.
	opts.SetConnectRetry(false)
	opts.SetConnectTimeout(cfg.Timeout)
	opts.SetWriteTimeout(cfg.Timeout)
	opts.SetPingTimeout(cfg.Timeout)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetOrderMatters(false)
	opts.SetWill(p.availabilityTopic, availabilityOffline, cfg.QOS, cfg.Retain)
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		p.onConnect(client)
	})
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Warn().Err(err).Str("broker", p.cfg.Broker).Msg("lost MQTT broker connection")
	})
	if strings.TrimSpace(cfg.Username) != "" {
		opts.SetUsername(strings.TrimSpace(cfg.Username))
		opts.SetPassword(cfg.Password)
	}
	if cfg.InsecureSkipVerify || strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Broker)), "tls://") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Broker)), "ssl://") {
		opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify})
	}

	p.client = mqtt.NewClient(opts)
	return p, nil
}

func (p *Publisher) Start() error {
	if p == nil || p.client == nil {
		return errPublisherClosed
	}

	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.mu.Lock()
	if p.closing || p.closed {
		p.mu.Unlock()
		return errPublisherClosed
	}
	if p.started {
		p.mu.Unlock()
		return nil
	}
	if p.discoveryState != nil && !p.discoveryStatePersisted {
		if err := persistDiscoveryState(p.discoveryStateFile, *p.discoveryState); err != nil {
			p.mu.Unlock()
			return err
		}
		p.discoveryStatePersisted = true
	}
	p.started = true
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.connectCancel = cancel
	p.connectDone = done
	p.mu.Unlock()

	go p.connectUntilReady(ctx, done)
	log.Info().
		Str("broker", p.cfg.Broker).
		Str("topic_prefix", p.topicPrefix).
		Dur("retry_interval", connectRetryInterval).
		Msg("starting MQTT publisher with background reconnect")
	return nil
}

func (p *Publisher) Close() {
	if p == nil || p.client == nil {
		return
	}

	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.mu.Lock()
	if p.closing || p.closed {
		p.mu.Unlock()
		return
	}
	p.closing = true
	p.lastAvailability = availabilityOffline
	if p.client.IsConnectionOpen() {
		if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
			log.Warn().Err(err).Msg("failed to publish MQTT offline availability during shutdown")
		}
	}
	cancel := p.connectCancel
	done := p.connectDone
	p.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Disconnect even while the initial connection is in progress so a late
	// successful connection cannot outlive the publisher.
	p.connectAttemptMu.Lock()
	p.client.Disconnect(250)
	p.connectAttemptMu.Unlock()
	if done != nil {
		<-done
	}

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *Publisher) connectUntilReady(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if p.beforeConnectAttempt != nil {
			p.beforeConnectAttempt()
		}
		p.connectAttemptMu.Lock()
		if ctx.Err() != nil {
			p.connectAttemptMu.Unlock()
			return
		}
		token := p.client.Connect()
		p.connectAttemptMu.Unlock()
		token.Wait()
		if ctx.Err() != nil {
			return
		}
		if err := token.Error(); err == nil {
			return
		} else {
			log.Warn().
				Err(err).
				Str("broker", p.cfg.Broker).
				Dur("retry_interval", connectRetryInterval).
				Msg("MQTT broker unavailable; publisher will retry in background")
		}

		retry := time.NewTimer(connectRetryInterval)
		select {
		case <-ctx.Done():
			if !retry.Stop() {
				<-retry.C
			}
			return
		case <-retry.C:
		}
	}
}

func (p *Publisher) OnPollSuccess(snapshot *model.Snapshot, duration time.Duration) (resultErr error) {
	if snapshot == nil {
		return nil
	}

	p.mu.Lock()
	defer func() {
		if resultErr != nil {
			wasOnline := p.lastAvailability == availabilityOnline
			p.lastAvailability = availabilityOffline
			if wasOnline {
				p.offlinePublishPending = true
			}
			if p.offlinePublishPending {
				// A schema/manifest failure invalidates this poll even though the
				// poller itself reached its success observer. Replace retained
				// online availability before returning the original error. Keep a
				// failed write pending for the next invalid poll or reconnect, and
				// never mask the original processing error.
				if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
					log.Warn().
						Err(err).
						Str("broker", p.cfg.Broker).
						Msg("failed to publish MQTT offline availability after poll processing error")
				} else {
					p.offlinePublishPending = false
				}
			}
		}
		p.mu.Unlock()
	}()
	if p.closing || p.closed {
		return nil
	}

	device := p.device(snapshot)
	stateTopic := p.stateTopic(device.ID)

	var discoveryMessages []discoveryMessage
	discoveryShapeSig := ""
	if p.discoveryState != nil {
		candidate := p.discoveryState.clone()
		changed, err := candidate.mergeSnapshot(snapshot, p.primarySource)
		if err != nil {
			return err
		}
		if changed || !p.discoveryStatePersisted {
			if err := validateDiscoveryState(candidate, p.discoveryState.Binding); err != nil {
				return err
			}
			// The durable schema must commit before the in-memory cache or any
			// MQTT topic can expose the newly learned entity ownership.
			if err := persistDiscoveryState(p.discoveryStateFile, candidate); err != nil {
				return err
			}
			p.discoveryState = &candidate
			p.discoveryStatePersisted = true
		}
		if err := p.refreshPersistentDiscovery(device, stateTopic); err != nil {
			return err
		}
		discoveryMessages = cloneDiscoveryMessages(p.cachedDiscovery)
		discoveryShapeSig = p.cachedDiscoverySig
	} else {
		discoveryShapeSig = p.discoverySignature(snapshot, device, stateTopic)
		if discoveryShapeSig != p.discoveryShapeSig {
			var err error
			discoveryMessages, err = p.discoveryMessages(snapshot, device, stateTopic)
			if err != nil {
				return err
			}
			p.cachedDiscovery = cloneDiscoveryMessages(discoveryMessages)
			p.cachedDiscoverySig = discoveryShapeSig
		}
	}

	// Cache state before publishing so a reconnect can replay the latest poll even
	// when the broker was unavailable during the original publish attempt.
	payload, err := json.Marshal(p.buildStatePayload(snapshot, duration))
	if err != nil {
		return fmt.Errorf("encode MQTT state payload: %w", err)
	}
	p.cachedStateTopic = stateTopic
	p.cachedState = append(p.cachedState[:0], payload...)
	p.lastAvailability = availabilityOnline
	p.offlinePublishPending = false

	for _, msg := range discoveryMessages {
		if previous, published := p.discoveryPayloads[msg.topic]; published && previous == msg.payload {
			continue
		}
		if err := p.publishString(msg.topic, msg.payload, true); err != nil {
			p.logPublishSkipped(err, msg.topic)
			return nil
		}
		p.discoveryPayloads[msg.topic] = msg.payload
	}
	if discoveryShapeSig != "" && discoveryShapeSig != p.discoveryShapeSig {
		p.discoveryShapeSig = discoveryShapeSig
	}

	if err := p.publishBytes(stateTopic, payload, p.cfg.Retain); err != nil {
		p.logPublishSkipped(err, stateTopic)
		return nil
	}
	if err := p.publishString(p.availabilityTopic, availabilityOnline, p.cfg.Retain); err != nil {
		p.logPublishSkipped(err, p.availabilityTopic)
		return nil
	}

	log.Debug().
		Str("state_topic", stateTopic).
		Int("metric_count", len(snapshot.Metrics)).
		Int("discovered_metric_count", len(p.discoveredMetrics)).
		Msg("published MQTT state")
	return nil
}

func (p *Publisher) OnPollFailure(sourceName string, err error, duration time.Duration, errorCount uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}
	p.lastAvailability = availabilityOffline
	p.offlinePublishPending = true

	log.Warn().
		Err(err).
		Str("source", sourceName).
		Dur("duration", duration).
		Uint64("error_count", errorCount).
		Msg("marking MQTT entities unavailable after poll failure")
	if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
		p.logPublishSkipped(err, p.availabilityTopic)
	} else {
		p.offlinePublishPending = false
	}
	return nil
}

func (p *Publisher) onConnect(_ mqtt.Client) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		// A connection can complete concurrently with Close. Never replay cached
		// state or publish online once shutdown has started. If the socket is
		// momentarily open, replace retained availability with offline before the
		// disconnect finishes.
		if p.client.IsConnectionOpen() {
			if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
				log.Warn().Err(err).Str("broker", p.cfg.Broker).Msg("failed to preserve MQTT offline availability during shutdown")
			}
		}
		return
	}

	// Home Assistant may miss retained discovery/state writes during broker
	// restarts, so replay the cached shape and state on every reconnect.
	for _, msg := range p.cachedDiscovery {
		if err := p.publishString(msg.topic, msg.payload, true); err != nil {
			log.Warn().Err(err).Str("broker", p.cfg.Broker).Str("topic", msg.topic).Msg("failed to republish MQTT discovery after connect")
			return
		}
		p.discoveryPayloads[msg.topic] = msg.payload
	}
	if len(p.cachedDiscovery) > 0 {
		p.discoveryShapeSig = p.cachedDiscoverySig
	}

	if p.cachedStateTopic != "" && len(p.cachedState) > 0 {
		if err := p.publishBytes(p.cachedStateTopic, p.cachedState, p.cfg.Retain); err != nil {
			log.Warn().Err(err).Str("broker", p.cfg.Broker).Str("topic", p.cachedStateTopic).Msg("failed to republish MQTT state after connect")
			return
		}
	}

	availability := p.lastAvailability
	if availability == "" {
		availability = availabilityOffline
	}
	// Preserve offline after a failed poll; reconnect alone must not make stale
	// values look healthy.
	if err := p.publishString(p.availabilityTopic, availability, p.cfg.Retain); err != nil {
		log.Warn().Err(err).Str("broker", p.cfg.Broker).Msg("failed to publish MQTT availability after connect")
		return
	}
	p.offlinePublishPending = false
	log.Info().Str("broker", p.cfg.Broker).Str("topic_prefix", p.topicPrefix).Msg("connected MQTT publisher")
}

type deviceInfo struct {
	ID           string
	Identifier   string
	Name         string
	SerialNumber string
}

type discoveryMessage struct {
	topic   string
	payload string
}

func cloneDiscoveryMessages(messages []discoveryMessage) []discoveryMessage {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]discoveryMessage, len(messages))
	copy(cloned, messages)
	return cloned
}

func (p *Publisher) refreshPersistentDiscovery(device deviceInfo, stateTopic string) error {
	if p.discoveryState == nil {
		return nil
	}
	metrics, err := p.discoveryState.allMetrics()
	if err != nil {
		return err
	}
	discovered := make(map[string]metricEntity, len(metrics))
	for _, metric := range metrics {
		stateKey := metricStateKey(metric)
		discovered[stateKey] = metricEntity{StateKey: stateKey, Metric: metric}
	}
	alertMetricDomains, err := p.discoveryState.alertMetricDomains()
	if err != nil {
		return err
	}
	messages, err := p.discoveryMessagesForSchema(
		metrics,
		p.discoveryState.PrimaryMetaKeys,
		p.discoveryState.alertSources(),
		alertMetricDomains,
		device,
		stateTopic,
	)
	if err != nil {
		return err
	}

	// Swap the complete runtime view only after generation succeeds. On schema
	// updates the state file has already been durably replaced by the caller.
	p.discoveredMetrics = discovered
	p.cachedDiscovery = cloneDiscoveryMessages(messages)
	p.cachedDiscoverySig = discoveryMessagesSignature(messages)
	return nil
}

func (p *Publisher) syncPersistentMetricRegistry() error {
	if p.discoveryState == nil {
		return nil
	}
	metrics, err := p.discoveryState.allMetrics()
	if err != nil {
		return err
	}
	discovered := make(map[string]metricEntity, len(metrics))
	for _, metric := range metrics {
		stateKey := metricStateKey(metric)
		discovered[stateKey] = metricEntity{StateKey: stateKey, Metric: metric}
	}
	p.discoveredMetrics = discovered
	return nil
}

func discoveryMessagesSignature(messages []discoveryMessage) string {
	var b strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&b, "%d:%s=%d:%s|", len(message.topic), message.topic, len(message.payload), message.payload)
	}
	return b.String()
}

func (p *Publisher) discoveryMessages(snapshot *model.Snapshot, device deviceInfo, stateTopic string) ([]discoveryMessage, error) {
	metaKeys := make([]string, 0, len(snapshot.Meta))
	for key := range snapshot.Meta {
		stateKey := sanitizeID(key)
		if stateKey != "" {
			metaKeys = append(metaKeys, stateKey)
		}
	}
	sort.Strings(metaKeys)

	alertSources := make([]string, 0, 1)
	alertMetricDomains := make(map[string][]string)
	newlyDiscovered := make(map[string]metricEntity, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
			continue
		}
		newlyDiscovered[stateKey] = metricEntity{StateKey: stateKey, Metric: metric}
		if isAlertMetric(metric) && len(alertSources) == 0 {
			alertSources = append(alertSources, firstNonEmpty(normalizeSourceName(snapshot.Source), "unknown_source"))
		}
		if isAlertMetric(metric) {
			alertMetricDomains[stateKey] = []string{alertDomain(snapshot.Source)}
		}
	}
	messages, err := p.discoveryMessagesForSchema(snapshot.Metrics, metaKeys, alertSources, alertMetricDomains, device, stateTopic)
	if err != nil {
		return nil, err
	}
	maps.Copy(p.discoveredMetrics, newlyDiscovered)
	return messages, nil
}

func (p *Publisher) discoveryMessagesForSchema(metrics []model.Metric, metaKeys, alertSources []string, alertMetricDomains map[string][]string, device deviceInfo, stateTopic string) ([]discoveryMessage, error) {
	messages := make([]discoveryMessage, 0, len(metrics)+len(alertSources)+16)
	seenTopics := make(map[string]struct{}, cap(messages))

	add := func(component, objectSuffix string, payload map[string]any) error {
		objectID := sanitizeID(device.ID + "_" + objectSuffix)
		topic := fmt.Sprintf("%s/%s/%s/config", p.discoveryPrefix, component, objectID)
		if _, duplicate := seenTopics[topic]; duplicate {
			return fmt.Errorf("duplicate MQTT discovery topic %s", topic)
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode MQTT discovery payload for %s: %w", objectID, err)
		}
		messages = append(messages, discoveryMessage{topic: topic, payload: string(raw)})
		seenTopics[topic] = struct{}{}
		return nil
	}

	for _, entity := range diagnosticEntities {
		payload := p.baseDiscoveryPayload(device, entity.Name, device.ID+"_"+entity.StateKey, stateTopic)
		payload["value_template"] = entity.ValueTemplate
		payload["entity_category"] = "diagnostic"
		if entity.DeviceClass != "" {
			payload["device_class"] = entity.DeviceClass
		}
		if entity.StateClass != "" {
			payload["state_class"] = entity.StateClass
		}
		if entity.Unit != "" {
			payload["unit_of_measurement"] = entity.Unit
		}
		if entity.Icon != "" {
			payload["icon"] = entity.Icon
		}
		if err := add("sensor", entity.StateKey, payload); err != nil {
			return nil, err
		}
	}

	for _, key := range metaKeys {
		stateKey := sanitizeID(key)
		if stateKey == "" {
			continue
		}
		payload := p.baseDiscoveryPayload(device, "Meta "+key, device.ID+"_meta_"+stateKey, stateTopic)
		payload["value_template"] = "{{ value_json.get('meta', {}).get('" + stateKey + "') }}"
		payload["entity_category"] = "diagnostic"
		payload["icon"] = "mdi:information"
		if err := add("sensor", "meta_"+stateKey, payload); err != nil {
			return nil, err
		}
	}

	upPayload := p.baseDiscoveryPayload(device, "Poll Up", device.ID+"_poll_up", stateTopic)
	upPayload["value_template"] = "{{ 'ON' if value_json.up else 'OFF' }}"
	upPayload["payload_on"] = "ON"
	upPayload["payload_off"] = "OFF"
	upPayload["device_class"] = "connectivity"
	upPayload["entity_category"] = "diagnostic"
	if err := add("binary_sensor", "poll_up", upPayload); err != nil {
		return nil, err
	}

	// Remove the legacy cross-source aggregate. It could turn OFF when a
	// different source reported a clear but semantically distinct alert domain.
	legacyObjectID := sanitizeID(device.ID + "_alarm_or_fault_active")
	messages = append(messages, discoveryMessage{
		topic:   fmt.Sprintf("%s/binary_sensor/%s/config", p.discoveryPrefix, legacyObjectID),
		payload: "",
	})

	for _, source := range alertSources {
		domain := alertDomain(source)
		objectSuffix := domain + "_warning_alarm_fault_active"
		alertPayload := p.baseDiscoveryPayload(device, "Warning/Alarm/Fault Active ("+source+")", device.ID+"_"+objectSuffix, stateTopic)
		alertPayload["value_template"] = "{{ 'ON' if value_json.get('alert_domain') == '" + domain + "' and value_json.get('alerts_known', false) and value_json.get('alerts_active', false) else 'OFF' }}"
		p.setEntityAvailability(alertPayload, stateTopic, "{{ 'online' if value_json.get('alert_domain') == '"+domain+"' and value_json.get('alerts_known', false) else 'offline' }}")
		alertPayload["payload_on"] = "ON"
		alertPayload["payload_off"] = "OFF"
		alertPayload["device_class"] = "problem"
		if err := add("binary_sensor", objectSuffix, alertPayload); err != nil {
			return nil, err
		}
	}

	for _, metric := range metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
			continue
		}

		payload := p.baseDiscoveryPayload(device, metricName(metric), device.ID+"_"+stateKey, stateTopic)
		payload["value_template"] = "{{ value_json.get('metrics', {}).get('" + stateKey + "') }}"
		if isAlertMetric(metric) {
			domains, ok := alertMetricDomains[stateKey]
			if !ok || len(domains) == 0 {
				return nil, fmt.Errorf("MQTT discovery alert metric %q has no source ownership", stateKey)
			}
			availabilityTemplate, err := alertMetricAvailabilityTemplate(stateKey, domains)
			if err != nil {
				return nil, err
			}
			payload["value_template"] = "{{ value_json.get('alert_metrics', {}).get('" + stateKey + "') }}"
			p.setEntityAvailability(payload, stateTopic, availabilityTemplate)
		}

		meta := metricSensorMeta(metric)
		if meta.DeviceClass != "" {
			payload["device_class"] = meta.DeviceClass
		}
		if meta.StateClass != "" {
			payload["state_class"] = meta.StateClass
		}
		if meta.Unit != "" {
			payload["unit_of_measurement"] = meta.Unit
		}
		if meta.EntityCategory != "" {
			payload["entity_category"] = meta.EntityCategory
		}
		if meta.Icon != "" {
			payload["icon"] = meta.Icon
		}
		if meta.SuggestedDisplayPrecision != nil {
			payload["suggested_display_precision"] = *meta.SuggestedDisplayPrecision
		}
		if err := add("sensor", stateKey, payload); err != nil {
			return nil, err
		}

		if isAlertMetric(metric) {
			binaryPayload := p.baseDiscoveryPayload(device, metricName(metric)+" Active", device.ID+"_"+stateKey+"_active", stateTopic)
			availabilityTemplate, err := alertMetricAvailabilityTemplate(stateKey, alertMetricDomains[stateKey])
			if err != nil {
				return nil, err
			}
			binaryPayload["value_template"] = "{{ 'ON' if '" + stateKey + "' in value_json.get('alert_metrics', {}) and value_json.get('alert_metrics', {}).get('" + stateKey + "')|float(0) != 0 else 'OFF' if '" + stateKey + "' in value_json.get('alert_metrics', {}) else none }}"
			p.setEntityAvailability(binaryPayload, stateTopic, availabilityTemplate)
			binaryPayload["payload_on"] = "ON"
			binaryPayload["payload_off"] = "OFF"
			binaryPayload["device_class"] = "problem"
			binaryPayload["entity_category"] = "diagnostic"
			if err := add("binary_sensor", stateKey+"_active", binaryPayload); err != nil {
				return nil, err
			}
		}
	}

	return messages, nil
}

func (p *Publisher) baseDiscoveryPayload(device deviceInfo, name string, uniqueID string, stateTopic string) map[string]any {
	return map[string]any{
		"name":                  name,
		"unique_id":             sanitizeID(uniqueID),
		"state_topic":           stateTopic,
		"availability_topic":    p.availabilityTopic,
		"payload_available":     availabilityOnline,
		"payload_not_available": availabilityOffline,
		"qos":                   int(p.cfg.QOS),
		"device": map[string]any{
			"identifiers":   []string{device.Identifier},
			"name":          device.Name,
			"manufacturer":  "Jinko",
			"model":         "Solar inverter via jinko-exporter",
			"serial_number": device.SerialNumber,
		},
	}
}

func (p *Publisher) setEntityAvailability(payload map[string]any, stateTopic, valueTemplate string) {
	delete(payload, "availability_topic")
	delete(payload, "payload_available")
	delete(payload, "payload_not_available")
	payload["availability"] = []map[string]any{
		{
			"topic":                 p.availabilityTopic,
			"payload_available":     availabilityOnline,
			"payload_not_available": availabilityOffline,
		},
		{
			"topic":                 stateTopic,
			"value_template":        valueTemplate,
			"payload_available":     availabilityOnline,
			"payload_not_available": availabilityOffline,
		},
	}
	payload["availability_mode"] = "all"
}

func alertMetricAvailabilityTemplate(stateKey string, domains []string) (string, error) {
	if stateKey == "" || len(domains) == 0 {
		return "", fmt.Errorf("MQTT discovery alert metric %q has no source ownership", stateKey)
	}
	unique := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = alertDomain(domain)
		if _, duplicate := seen[domain]; duplicate {
			continue
		}
		seen[domain] = struct{}{}
		unique = append(unique, domain)
	}
	sort.Strings(unique)
	domainCondition := "value_json.get('alert_domain') == '" + unique[0] + "'"
	if len(unique) > 1 {
		quoted := make([]string, len(unique))
		for i, domain := range unique {
			quoted[i] = "'" + domain + "'"
		}
		domainCondition = "value_json.get('alert_domain') in [" + strings.Join(quoted, ", ") + "]"
	}
	return "{{ 'online' if " + domainCondition + " and '" + stateKey + "' in value_json.get('alert_metrics', {}) else 'offline' }}", nil
}

func (p *Publisher) buildStatePayload(snapshot *model.Snapshot, duration time.Duration) statePayload {
	metrics := make(map[string]*float64, len(p.discoveredMetrics)+len(snapshot.Metrics))
	for stateKey := range p.discoveredMetrics {
		metrics[stateKey] = nil
	}

	alertMetrics := make(map[string]float64)
	alertCount := 0
	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" || !p.metricOwnedByDiscoveryState(snapshot.Source, stateKey, metric) {
			continue
		}
		if !math.IsNaN(metric.Value) && !math.IsInf(metric.Value, 0) {
			value := metric.Value
			metrics[stateKey] = &value
			if isAlertMetric(metric) {
				alertMetrics[stateKey] = value
				if value != 0 {
					alertCount++
				}
			}
		} else if _, ok := metrics[stateKey]; !ok {
			metrics[stateKey] = nil
		}
	}

	return statePayload{
		Source:              snapshot.Source,
		DeviceSN:            snapshot.DeviceSN,
		ParentSN:            snapshot.ParentSN,
		DeviceID:            snapshot.DeviceID,
		SiteID:              snapshot.SiteID,
		CollectedAt:         snapshot.CollectedAt.Format(time.RFC3339),
		PublishedAt:         time.Now().UTC().Format(time.RFC3339),
		Up:                  true,
		Metrics:             metrics,
		MetricCount:         len(snapshot.Metrics),
		AlertMetrics:        alertMetrics,
		AlertDomain:         alertDomain(snapshot.Source),
		AlertCount:          alertCount,
		AlertsKnown:         len(alertMetrics) > 0,
		AlertsActive:        alertCount > 0,
		PollDurationSeconds: duration.Seconds(),
		Meta:                p.discoveryStateMeta(snapshot.Meta),
	}
}

func (p *Publisher) metricOwnedByDiscoveryState(source, stateKey string, metric model.Metric) bool {
	if p.discoveryState == nil {
		return true
	}
	if isAlertMetric(metric) {
		source = firstNonEmpty(normalizeSourceName(source), "unknown_source")
		_, owned := p.discoveryState.AlertMetrics[source][stateKey]
		return owned
	}
	if strings.EqualFold(strings.TrimSpace(metric.Group), "grid_load") {
		_, owned := p.discoveryState.GridLoadMetrics[stateKey]
		return owned
	}
	_, owned := p.discoveryState.OrdinaryMetrics[stateKey]
	return owned
}

func (p *Publisher) discoveryStateMeta(meta map[string]string) map[string]string {
	normalized := normalizedMeta(meta)
	if p.discoveryState == nil || len(normalized) == 0 {
		return normalized
	}
	allowed := make(map[string]struct{}, len(p.discoveryState.PrimaryMetaKeys))
	for _, key := range p.discoveryState.PrimaryMetaKeys {
		allowed[key] = struct{}{}
	}
	for key := range normalized {
		if _, ok := allowed[key]; !ok {
			delete(normalized, key)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func (p *Publisher) device(snapshot *model.Snapshot) deviceInfo {
	rawID := firstNonEmpty(p.cfg.DeviceID, snapshot.DeviceSN, snapshot.DeviceID, snapshot.ParentSN, snapshot.SiteID, snapshot.Source, "jinko_exporter")
	id := sanitizeID(rawID)
	name := strings.TrimSpace(p.cfg.DeviceName)
	if name == "" {
		name = "Jinko Inverter"
		if strings.TrimSpace(snapshot.DeviceSN) != "" {
			name += " " + strings.TrimSpace(snapshot.DeviceSN)
		}
	}
	return deviceInfo{
		ID:           id,
		Identifier:   "jinko_exporter_" + id,
		Name:         name,
		SerialNumber: strings.TrimSpace(snapshot.DeviceSN),
	}
}

func (p *Publisher) stateTopic(deviceID string) string {
	return p.topicPrefix + "/" + sanitizeID(deviceID) + "/state"
}

func (p *Publisher) publishString(topic string, payload string, retain bool) error {
	return p.publishBytes(topic, []byte(payload), retain)
}

func (p *Publisher) publishBytes(topic string, payload []byte, retain bool) error {
	if !p.client.IsConnectionOpen() {
		return errClientNotConnected
	}
	if err := p.wait(p.client.Publish(topic, p.cfg.QOS, retain, payload)); err != nil {
		return fmt.Errorf("publish MQTT topic %s: %w", topic, err)
	}
	return nil
}

func (p *Publisher) logPublishSkipped(err error, topic string) {
	event := log.Warn()
	if errors.Is(err, errClientNotConnected) {
		event = log.Debug()
	}
	event.
		Err(err).
		Str("broker", p.cfg.Broker).
		Str("topic", topic).
		Msg("skipping MQTT publish")
}

func (p *Publisher) wait(token mqtt.Token) error {
	if !token.WaitTimeout(p.cfg.Timeout) {
		return fmt.Errorf("timeout after %s", p.cfg.Timeout)
	}
	return token.Error()
}

func (p *Publisher) discoverySignature(snapshot *model.Snapshot, device deviceInfo, stateTopic string) string {
	var b strings.Builder
	b.Grow(len(snapshot.Metrics)*32 + len(snapshot.Meta)*16 + len(device.ID) + len(stateTopic) + 64)
	b.WriteString(device.ID)
	b.WriteByte('|')
	b.WriteString(device.Identifier)
	b.WriteByte('|')
	b.WriteString(stateTopic)
	b.WriteByte('|')
	b.WriteString("source:")
	b.WriteString(strings.TrimSpace(snapshot.Source))
	b.WriteByte('|')
	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
			continue
		}
		b.WriteString(stateKey)
		b.WriteByte('=')
		b.WriteString(metric.Name)
		b.WriteByte('=')
		b.WriteString(metric.Unit)
		b.WriteByte('=')
		b.WriteString(metric.Group)
		b.WriteByte('|')
	}
	metaKeys := make([]string, 0, len(snapshot.Meta))
	for key := range snapshot.Meta {
		metaKeys = append(metaKeys, key)
	}
	sort.Strings(metaKeys)
	for _, key := range metaKeys {
		stateKey := sanitizeID(key)
		if stateKey == "" {
			continue
		}
		b.WriteString("meta:")
		b.WriteString(stateKey)
		b.WriteByte('|')
	}
	return b.String()
}

type diagnosticEntity struct {
	StateKey      string
	Name          string
	ValueTemplate string
	DeviceClass   string
	StateClass    string
	Unit          string
	Icon          string
}

var diagnosticEntities = []diagnosticEntity{
	{StateKey: "source", Name: "Data Source", ValueTemplate: "{{ value_json.source }}", Icon: "mdi:database-import"},
	{StateKey: "device_sn", Name: "Device Serial", ValueTemplate: "{{ value_json.device_sn }}", Icon: "mdi:identifier"},
	{StateKey: "parent_sn", Name: "Parent Serial", ValueTemplate: "{{ value_json.get('parent_sn', '') }}", Icon: "mdi:identifier"},
	{StateKey: "device_id", Name: "Device ID", ValueTemplate: "{{ value_json.get('device_id', '') }}", Icon: "mdi:identifier"},
	{StateKey: "site_id", Name: "Site ID", ValueTemplate: "{{ value_json.get('site_id', '') }}", Icon: "mdi:home-lightning-bolt"},
	{StateKey: "collected_at", Name: "Collected At", ValueTemplate: "{{ value_json.collected_at }}", DeviceClass: "timestamp"},
	{StateKey: "published_at", Name: "Published At", ValueTemplate: "{{ value_json.published_at }}", DeviceClass: "timestamp"},
	{StateKey: "poll_duration", Name: "Poll Duration", ValueTemplate: "{{ value_json.poll_duration_seconds }}", DeviceClass: "duration", StateClass: "measurement", Unit: "s"},
	{StateKey: "metric_count", Name: "Metric Count", ValueTemplate: "{{ value_json.metric_count }}", StateClass: "measurement", Icon: "mdi:counter"},
	{StateKey: "alert_count", Name: "Current Source Active Warning/Alarm/Fault Count", ValueTemplate: "{{ value_json.alert_count if value_json.alerts_known else none }}", StateClass: "measurement", Icon: "mdi:alert-circle"},
}

type metricMeta struct {
	DeviceClass               string
	StateClass                string
	Unit                      string
	EntityCategory            string
	Icon                      string
	SuggestedDisplayPrecision *int
}

func metricSensorMeta(metric model.Metric) metricMeta {
	unit := normalizeHAUnit(metric.Unit)
	text := strings.ToLower(metric.Group + " " + metric.Key + " " + metric.Name)
	meta := metricMeta{Unit: unit}

	switch strings.ToLower(unit) {
	case "w", "kw":
		meta.DeviceClass = "power"
		meta.StateClass = "measurement"
	case "kwh", "wh":
		meta.DeviceClass = "energy"
		meta.StateClass = "total_increasing"
	case "v":
		meta.DeviceClass = "voltage"
		meta.StateClass = "measurement"
	case "a":
		meta.DeviceClass = "current"
		meta.StateClass = "measurement"
	case "hz":
		meta.DeviceClass = "frequency"
		meta.StateClass = "measurement"
	case "\u00b0c":
		meta.DeviceClass = "temperature"
		meta.StateClass = "measurement"
	case "va":
		meta.DeviceClass = "apparent_power"
		meta.StateClass = "measurement"
	case "var":
		meta.DeviceClass = "reactive_power"
		meta.StateClass = "measurement"
	case "%":
		if strings.Contains(text, "soc") || strings.Contains(text, "soh") || strings.Contains(text, "battery") || strings.Contains(text, "cap") {
			meta.DeviceClass = "battery"
		}
		meta.StateClass = "measurement"
	case "h":
		meta.DeviceClass = "duration"
		meta.StateClass = "measurement"
	}

	if strings.Contains(text, "power factor") {
		meta.DeviceClass = "power_factor"
		meta.StateClass = "measurement"
	}

	switch strings.ToLower(strings.TrimSpace(metric.Group)) {
	case "basic", "version", "status", "state", "alert":
		meta.EntityCategory = "diagnostic"
	}
	if isAlertMetric(metric) {
		meta.Icon = "mdi:alert-circle"
	}
	return meta
}

func metricStateKey(metric model.Metric) string {
	key := firstNonEmpty(metric.Key, metric.Name)
	if key == "" {
		return ""
	}
	group := strings.TrimSpace(metric.Group)
	if group == "" {
		return sanitizeID(key)
	}
	return sanitizeID(group + "_" + key)
}

func metricName(metric model.Metric) string {
	name := strings.TrimSpace(metric.Name)
	if name == "" {
		name = strings.TrimSpace(metric.Key)
	}
	if name == "" {
		name = "Metric"
	}
	return name
}

func isAlertMetric(metric model.Metric) bool {
	text := strings.ToLower(metric.Group + " " + metric.Key + " " + metric.Name)
	return strings.Contains(text, "alarm") || strings.Contains(text, "fault") || strings.TrimSpace(strings.ToLower(metric.Group)) == "alert"
}

func normalizeHAUnit(unit string) string {
	unit = strings.TrimSpace(strings.ReplaceAll(unit, "\u00a0", " "))
	switch strings.ToLower(unit) {
	case "c", "\u2103", "\u00b0c":
		return "\u00b0C"
	case "var", "vars":
		return "var"
	case "ah":
		return "Ah"
	default:
		return unit
	}
}

func normalizedMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(meta))
	for key, value := range meta {
		stateKey := sanitizeID(key)
		if stateKey == "" {
			continue
		}
		normalized[stateKey] = value
	}
	return normalized
}

func sanitizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "\u00a0", " ")))
	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		keep := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if keep {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	value = strings.Trim(b.String(), "_")
	if value == "" {
		return "unknown"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
