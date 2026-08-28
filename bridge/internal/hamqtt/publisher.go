package hamqtt

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
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
	cfg               config.MQTTConfig
	client            mqtt.Client
	topicPrefix       string
	discoveryPrefix   string
	availabilityTopic string

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
		cfg:               cfg,
		topicPrefix:       topicPrefix,
		discoveryPrefix:   discoveryPrefix,
		availabilityTopic: topicPrefix + "/availability",
		discoveryPayloads: make(map[string]string),
		discoveredMetrics: make(map[string]metricEntity),
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

func (p *Publisher) OnPollSuccess(snapshot *model.Snapshot, duration time.Duration) error {
	if snapshot == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}

	device := p.device(snapshot)
	stateTopic := p.stateTopic(device.ID)

	discoveryShapeSig := p.discoverySignature(snapshot, device, stateTopic)
	var discoveryMessages []discoveryMessage
	if discoveryShapeSig != p.discoveryShapeSig {
		var err error
		discoveryMessages, err = p.discoveryMessages(snapshot, device, stateTopic)
		if err != nil {
			return err
		}
		p.cachedDiscovery = cloneDiscoveryMessages(discoveryMessages)
		p.cachedDiscoverySig = discoveryShapeSig
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
	if discoveryShapeSig != p.discoveryShapeSig {
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

	log.Warn().
		Err(err).
		Str("source", sourceName).
		Dur("duration", duration).
		Uint64("error_count", errorCount).
		Msg("marking MQTT entities unavailable after poll failure")
	if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
		p.logPublishSkipped(err, p.availabilityTopic)
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

func (p *Publisher) discoveryMessages(snapshot *model.Snapshot, device deviceInfo, stateTopic string) ([]discoveryMessage, error) {
	messages := make([]discoveryMessage, 0, len(snapshot.Metrics)+16)

	add := func(component, objectSuffix string, payload map[string]any) error {
		objectID := sanitizeID(device.ID + "_" + objectSuffix)
		topic := fmt.Sprintf("%s/%s/%s/config", p.discoveryPrefix, component, objectID)
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode MQTT discovery payload for %s: %w", objectID, err)
		}
		messages = append(messages, discoveryMessage{topic: topic, payload: string(raw)})
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

	alertDomain := sanitizeID(snapshot.Source)
	if alertDomain == "" {
		alertDomain = "unknown_source"
	}
	hasAlertMetrics := slices.ContainsFunc(snapshot.Metrics, isAlertMetric)
	if hasAlertMetrics {
		objectSuffix := alertDomain + "_warning_alarm_fault_active"
		alertPayload := p.baseDiscoveryPayload(device, "Warning/Alarm/Fault Active ("+snapshot.Source+")", device.ID+"_"+objectSuffix, stateTopic)
		alertPayload["value_template"] = "{{ 'ON' if value_json.alert_domain == '" + alertDomain + "' and value_json.alerts_known and value_json.alerts_active else 'OFF' if value_json.alert_domain == '" + alertDomain + "' and value_json.alerts_known else none }}"
		alertPayload["payload_on"] = "ON"
		alertPayload["payload_off"] = "OFF"
		alertPayload["device_class"] = "problem"
		if err := add("binary_sensor", objectSuffix, alertPayload); err != nil {
			return nil, err
		}
	}

	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
			continue
		}
		p.discoveredMetrics[stateKey] = metricEntity{StateKey: stateKey, Metric: metric}

		payload := p.baseDiscoveryPayload(device, metricName(metric), device.ID+"_"+stateKey, stateTopic)
		payload["value_template"] = "{{ value_json.metrics." + stateKey + " }}"

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
			binaryPayload["value_template"] = "{{ 'ON' if '" + stateKey + "' in value_json.alert_metrics and value_json.alert_metrics." + stateKey + "|float != 0 else 'OFF' if '" + stateKey + "' in value_json.alert_metrics else none }}"
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

func (p *Publisher) buildStatePayload(snapshot *model.Snapshot, duration time.Duration) statePayload {
	metrics := make(map[string]*float64, len(p.discoveredMetrics)+len(snapshot.Metrics))
	for stateKey := range p.discoveredMetrics {
		metrics[stateKey] = nil
	}

	alertMetrics := make(map[string]float64)
	alertCount := 0
	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
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
		AlertDomain:         firstNonEmpty(sanitizeID(snapshot.Source), "unknown_source"),
		AlertCount:          alertCount,
		AlertsKnown:         len(alertMetrics) > 0,
		AlertsActive:        alertCount > 0,
		PollDurationSeconds: duration.Seconds(),
		Meta:                normalizedMeta(snapshot.Meta),
	}
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
