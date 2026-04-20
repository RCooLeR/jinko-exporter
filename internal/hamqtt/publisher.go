package hamqtt

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

const (
	availabilityOnline  = "online"
	availabilityOffline = "offline"
)

type Publisher struct {
	cfg               config.MQTTConfig
	client            mqtt.Client
	topicPrefix       string
	discoveryPrefix   string
	availabilityTopic string

	mu                sync.Mutex
	discoveryPayloads map[string]string
	discoveredMetrics map[string]metricEntity
}

type metricEntity struct {
	StateKey string
	Metric   model.Metric
}

type statePayload struct {
	Source              string              `json:"source"`
	DeviceSN            string              `json:"device_sn,omitempty"`
	ParentSN            string              `json:"parent_sn,omitempty"`
	DeviceID            string              `json:"device_id,omitempty"`
	SiteID              string              `json:"site_id,omitempty"`
	CollectedAt         string              `json:"collected_at"`
	PublishedAt         string              `json:"published_at"`
	Up                  bool                `json:"up"`
	Metrics             map[string]*float64 `json:"metrics"`
	MetricCount         int                 `json:"metric_count"`
	AlertMetrics        map[string]float64  `json:"alert_metrics"`
	AlertCount          int                 `json:"alert_count"`
	AlertsActive        bool                `json:"alerts_active"`
	PollDurationSeconds float64             `json:"poll_duration_seconds"`
	Meta                map[string]string   `json:"meta,omitempty"`
}

func NewPublisher(cfg config.MQTTConfig) (*Publisher, error) {
	topicPrefix := cleanTopicPrefix(cfg.TopicPrefix)
	discoveryPrefix := cleanTopicPrefix(cfg.DiscoveryPrefix)
	if topicPrefix == "" {
		return nil, fmt.Errorf("mqtt topic prefix is required")
	}
	if discoveryPrefix == "" {
		return nil, fmt.Errorf("mqtt discovery prefix is required")
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
	opts.SetConnectTimeout(cfg.Timeout)
	opts.SetWriteTimeout(cfg.Timeout)
	opts.SetPingTimeout(cfg.Timeout)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetOrderMatters(false)
	opts.SetWill(p.availabilityTopic, availabilityOffline, cfg.QOS, cfg.Retain)
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
	if err := p.wait(p.client.Connect()); err != nil {
		return fmt.Errorf("connect MQTT broker: %w", err)
	}
	if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
		return fmt.Errorf("publish initial MQTT availability: %w", err)
	}
	log.Info().Str("broker", p.cfg.Broker).Str("topic_prefix", p.topicPrefix).Msg("connected MQTT publisher")
	return nil
}

func (p *Publisher) Close() {
	if p == nil || p.client == nil {
		return
	}
	if p.client.IsConnectionOpen() {
		if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err != nil {
			log.Warn().Err(err).Msg("failed to publish MQTT offline availability during shutdown")
		}
	}
	p.client.Disconnect(250)
}

func (p *Publisher) OnPollSuccess(snapshot *model.Snapshot, duration time.Duration) error {
	if snapshot == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	device := p.device(snapshot)
	stateTopic := p.stateTopic(device.ID)

	discoveryMessages, err := p.discoveryMessages(snapshot, device, stateTopic)
	if err != nil {
		return err
	}
	for _, msg := range discoveryMessages {
		if p.discoveryPayloads[msg.topic] == msg.payload {
			continue
		}
		if err := p.publishString(msg.topic, msg.payload, true); err != nil {
			return err
		}
		p.discoveryPayloads[msg.topic] = msg.payload
	}

	payload, err := json.Marshal(p.buildStatePayload(snapshot, duration))
	if err != nil {
		return fmt.Errorf("encode MQTT state payload: %w", err)
	}
	if err := p.publishBytes(stateTopic, payload, p.cfg.Retain); err != nil {
		return err
	}
	if err := p.publishString(p.availabilityTopic, availabilityOnline, p.cfg.Retain); err != nil {
		return err
	}

	log.Debug().
		Str("state_topic", stateTopic).
		Int("metric_count", len(snapshot.Metrics)).
		Int("discovered_metric_count", len(p.discoveredMetrics)).
		Msg("published MQTT state")
	return nil
}

func (p *Publisher) OnPollFailure(sourceName string, err error, duration time.Duration, errorCount uint64) error {
	log.Warn().
		Err(err).
		Str("source", sourceName).
		Dur("duration", duration).
		Uint64("error_count", errorCount).
		Msg("marking MQTT entities unavailable after poll failure")
	return p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain)
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

	for _, entity := range diagnosticSensorEntities() {
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

	for key := range snapshot.Meta {
		stateKey := sanitizeID(key)
		if stateKey == "" {
			continue
		}
		payload := p.baseDiscoveryPayload(device, "Meta "+key, device.ID+"_meta_"+stateKey, stateTopic)
		payload["value_template"] = "{{ value_json.meta." + stateKey + " }}"
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

	alertPayload := p.baseDiscoveryPayload(device, "Alarm Or Fault Active", device.ID+"_alarm_or_fault_active", stateTopic)
	alertPayload["value_template"] = "{{ 'ON' if value_json.alerts_active else 'OFF' }}"
	alertPayload["payload_on"] = "ON"
	alertPayload["payload_off"] = "OFF"
	alertPayload["device_class"] = "problem"
	if err := add("binary_sensor", "alarm_or_fault_active", alertPayload); err != nil {
		return nil, err
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
			binaryPayload["value_template"] = "{{ 'ON' if value_json.alert_metrics." + stateKey + "|default(0)|float != 0 else 'OFF' }}"
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
	metricsByStateKey := make(map[string]float64, len(snapshot.Metrics))
	alertMetrics := make(map[string]float64)
	alertCount := 0
	for _, metric := range snapshot.Metrics {
		stateKey := metricStateKey(metric)
		if stateKey == "" {
			continue
		}
		metricsByStateKey[stateKey] = metric.Value
		if isAlertMetric(metric) {
			alertMetrics[stateKey] = metric.Value
			if metric.Value != 0 {
				alertCount++
			}
		}
	}

	metrics := make(map[string]*float64, len(p.discoveredMetrics))
	for stateKey := range p.discoveredMetrics {
		if value, ok := metricsByStateKey[stateKey]; ok && !math.IsNaN(value) && !math.IsInf(value, 0) {
			v := value
			metrics[stateKey] = &v
			continue
		}
		metrics[stateKey] = nil
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
		AlertCount:          alertCount,
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
		return fmt.Errorf("MQTT client is not connected")
	}
	if err := p.wait(p.client.Publish(topic, p.cfg.QOS, retain, payload)); err != nil {
		return fmt.Errorf("publish MQTT topic %s: %w", topic, err)
	}
	return nil
}

func (p *Publisher) wait(token mqtt.Token) error {
	if !token.WaitTimeout(p.cfg.Timeout) {
		return fmt.Errorf("timeout after %s", p.cfg.Timeout)
	}
	return token.Error()
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

func diagnosticSensorEntities() []diagnosticEntity {
	return []diagnosticEntity{
		{StateKey: "source", Name: "Data Source", ValueTemplate: "{{ value_json.source }}", Icon: "mdi:database-import"},
		{StateKey: "device_sn", Name: "Device Serial", ValueTemplate: "{{ value_json.device_sn }}", Icon: "mdi:identifier"},
		{StateKey: "parent_sn", Name: "Parent Serial", ValueTemplate: "{{ value_json.parent_sn }}", Icon: "mdi:identifier"},
		{StateKey: "device_id", Name: "Device ID", ValueTemplate: "{{ value_json.device_id }}", Icon: "mdi:identifier"},
		{StateKey: "site_id", Name: "Site ID", ValueTemplate: "{{ value_json.site_id }}", Icon: "mdi:home-lightning-bolt"},
		{StateKey: "collected_at", Name: "Collected At", ValueTemplate: "{{ value_json.collected_at }}", DeviceClass: "timestamp"},
		{StateKey: "published_at", Name: "Published At", ValueTemplate: "{{ value_json.published_at }}", DeviceClass: "timestamp"},
		{StateKey: "poll_duration", Name: "Poll Duration", ValueTemplate: "{{ value_json.poll_duration_seconds }}", DeviceClass: "duration", StateClass: "measurement", Unit: "s"},
		{StateKey: "metric_count", Name: "Metric Count", ValueTemplate: "{{ value_json.metric_count }}", StateClass: "measurement", Icon: "mdi:counter"},
		{StateKey: "alert_count", Name: "Active Alarm Or Fault Count", ValueTemplate: "{{ value_json.alert_count }}", StateClass: "measurement", Icon: "mdi:alert-circle"},
	}
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

func cleanTopicPrefix(value string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(value), "/"), "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
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
