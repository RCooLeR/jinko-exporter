package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/alert"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/rs/zerolog/log"
)

const modbusAlertNotificationTagPrefix = "jinko_bridge_modbus_raw_alert_"

type correlationLogMetric struct {
	Group string  `json:"group"`
	Key   string  `json:"key"`
	Unit  string  `json:"unit,omitempty"`
	Value float64 `json:"value"`
}

type correlationLogWords struct {
	R553 uint16 `json:"r553"`
	R554 uint16 `json:"r554"`
	R555 uint16 `json:"r555"`
	R556 uint16 `json:"r556"`
	R557 uint16 `json:"r557"`
	R558 uint16 `json:"r558"`
}

type homeAssistantNotificationSender interface {
	Send(context.Context, alert.HomeAssistantNotification) error
}

func configureModbusAlertCorrelation(priority *source.Priority, cfg config.Config) error {
	if priority == nil {
		return errors.New("configure Modbus alert correlation: priority source is nil")
	}
	notifier, err := alert.NewHomeAssistantNotifier(cfg.HomeAssistant)
	if err != nil {
		return fmt.Errorf("build Home Assistant Modbus alert notifier: %w", err)
	}

	if err := priority.ConfigureAlertCorrelation(source.AlertCorrelationConfig{
		Cooldown:       cfg.ModbusAlertCorrelation.Cooldown,
		NotifyTimeout:  cfg.HomeAssistant.Timeout,
		JobTimeout:     cfg.ModbusAlertCorrelation.JobTimeout,
		Notify:         homeAssistantCorrelationCallback(notifier, modbusAlertNotificationTag(cfg.Modbus.DeviceSN, cfg.HomeAssistant.Token)),
		RecordEvidence: recordAlertCorrelationEvidence,
	}); err != nil {
		return fmt.Errorf("configure Modbus alert correlation: %w", err)
	}
	return nil
}

func homeAssistantCorrelationCallback(notifier homeAssistantNotificationSender, notificationTag string) source.AlertCorrelationNotifyFunc {
	return func(ctx context.Context, event source.AlertCorrelationEvent) error {
		correlationID := alertCorrelationID(event.ObservedAt, event.Signature)
		if event.Kind == source.AlertCorrelationComplete {
			log.Info().
				Str("correlation_id", correlationID).
				Str("event", string(event.Kind)).
				Str("cloud_statuses", formatCorrelationStatuses(event.Sources)).
				Time("observed_at", event.ObservedAt.UTC()).
				Interface("modbus_words", correlationWords(event.Signature)).
				Msg("Modbus warning/fault cloud correlation completed")
			return nil
		}

		notification, err := alertCorrelationNotification(event, notificationTag)
		if err != nil {
			log.Error().
				Err(err).
				Str("correlation_id", correlationID).
				Msg("rejected invalid Modbus alert correlation event")
			return err
		}

		logEvent := log.Info().
			Str("correlation_id", correlationID).
			Str("event", string(event.Kind)).
			Time("observed_at", event.ObservedAt.UTC()).
			Interface("modbus_words", correlationWords(event.Signature))
		if event.Kind == source.AlertCorrelationDetected {
			logEvent = log.Warn().
				Str("correlation_id", correlationID).
				Str("event", string(event.Kind)).
				Time("observed_at", event.ObservedAt.UTC()).
				Interface("modbus_words", correlationWords(event.Signature))
		}
		logEvent.Msg("processing Modbus warning/fault correlation event")

		if err := notifier.Send(ctx, notification); err != nil {
			log.Error().
				Err(err).
				Str("correlation_id", correlationID).
				Str("event", string(event.Kind)).
				Msg("Home Assistant Modbus alert notification failed")
			return err
		}
		log.Info().
			Str("correlation_id", correlationID).
			Str("event", string(event.Kind)).
			Msg("Home Assistant Modbus alert notification delivered")
		return nil
	}
}

func alertCorrelationNotification(event source.AlertCorrelationEvent, notificationTag string) (alert.HomeAssistantNotification, error) {
	words := formatCorrelationWords(event.Signature)
	notification := alert.HomeAssistantNotification{
		Data: map[string]string{"tag": notificationTag},
	}
	switch event.Kind {
	case source.AlertCorrelationDetected:
		notification.Title = "Inverter warning/fault detected"
		notification.Message = "A complete Modbus snapshot reported non-zero raw warning/fault words: " + words + ". Jinko and Solarman correlation is running."
	case source.AlertCorrelationRecovered:
		notification.Title = "Inverter warning/fault cleared"
		notification.Message = "A complete Modbus snapshot reported R553-R558 as zero."
	default:
		return alert.HomeAssistantNotification{}, fmt.Errorf("unsupported Modbus alert correlation event kind")
	}
	return notification, nil
}

func modbusAlertNotificationTag(deviceSN, secret string) string {
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte(strings.TrimSpace(deviceSN)))
	return modbusAlertNotificationTagPrefix + hex.EncodeToString(digest.Sum(nil)[:8])
}

func recordAlertCorrelationEvidence(_ context.Context, evidence source.AlertCorrelationEvidence) error {
	correlationID := alertCorrelationID(evidence.ObservedAt, evidence.Signature)
	for _, sourceEvidence := range evidence.Sources {
		metrics := make([]correlationLogMetric, len(sourceEvidence.Metrics))
		for index, metric := range sourceEvidence.Metrics {
			metrics[index] = correlationLogMetric{
				Group: metric.Group,
				Key:   metric.Key,
				Unit:  metric.Unit,
				Value: metric.Value,
			}
		}

		status := safeCorrelationStatus(sourceEvidence.Status)
		sourceName := safeCorrelationSource(sourceEvidence.Source)
		logEvent := log.Info()
		if sourceEvidence.Status != source.AlertCorrelationSourceOK {
			logEvent = log.Warn()
		}
		logEvent = logEvent.
			Str("correlation_id", correlationID).
			Str("source", sourceName).
			Str("status", status).
			Dur("duration", sourceEvidence.Duration).
			Int("metric_count", sourceEvidence.TotalMetricCount).
			Int("alert_metric_count", sourceEvidence.AlertMetricCount).
			Bool("metrics_truncated", sourceEvidence.Truncated).
			Str("metrics_sha256", sourceEvidence.MetricsSHA256).
			Interface("modbus_words", correlationWords(evidence.Signature)).
			Interface("metrics", metrics)
		if !sourceEvidence.CollectedAt.IsZero() {
			logEvent = logEvent.Time("collected_at", sourceEvidence.CollectedAt.UTC())
		}
		logEvent.Msg("recorded sanitized cloud evidence for Modbus warning/fault correlation")
	}
	return nil
}

func correlationWords(signature source.ModbusAlertSignature) correlationLogWords {
	return correlationLogWords{
		R553: signature.R553,
		R554: signature.R554,
		R555: signature.R555,
		R556: signature.R556,
		R557: signature.R557,
		R558: signature.R558,
	}
}

func formatCorrelationWords(signature source.ModbusAlertSignature) string {
	words := correlationWords(signature)
	return fmt.Sprintf(
		"R553=%d (0x%04X), R554=%d (0x%04X), R555=%d (0x%04X), R556=%d (0x%04X), R557=%d (0x%04X), R558=%d (0x%04X)",
		words.R553, words.R553,
		words.R554, words.R554,
		words.R555, words.R555,
		words.R556, words.R556,
		words.R557, words.R557,
		words.R558, words.R558,
	)
}

func formatCorrelationStatuses(summaries []source.AlertCorrelationSourceSummary) string {
	statuses := map[string]string{
		"jinko":    "not_reported",
		"solarman": "not_reported",
	}
	for _, summary := range summaries {
		name := safeCorrelationSource(summary.Source)
		if name == "jinko" || name == "solarman" {
			statuses[name] = safeCorrelationStatus(summary.Status)
		}
	}
	return "Jinko=" + statuses["jinko"] + ", Solarman=" + statuses["solarman"]
}

func safeCorrelationSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "jinko":
		return "jinko"
	case "solarman":
		return "solarman"
	default:
		return "unknown"
	}
}

func safeCorrelationStatus(status source.AlertCorrelationSourceStatus) string {
	switch status {
	case source.AlertCorrelationSourceOK,
		source.AlertCorrelationSourceFailed,
		source.AlertCorrelationSourceTimedOut,
		source.AlertCorrelationSourceCanceled,
		source.AlertCorrelationSourcePreempted,
		source.AlertCorrelationSourceEmpty,
		source.AlertCorrelationSourceUnverified,
		source.AlertCorrelationSourceMismatch,
		source.AlertCorrelationSourceWrongType,
		source.AlertCorrelationSourceStale,
		source.AlertCorrelationSourceBadTime,
		source.AlertCorrelationSourceNoMetrics:
		return string(status)
	default:
		return "unknown"
	}
}

func alertCorrelationID(observedAt time.Time, signature source.ModbusAlertSignature) string {
	var canonical [20]byte
	binary.BigEndian.PutUint64(canonical[0:8], uint64(observedAt.UTC().UnixNano()))
	words := correlationWords(signature)
	binary.BigEndian.PutUint16(canonical[8:10], words.R553)
	binary.BigEndian.PutUint16(canonical[10:12], words.R554)
	binary.BigEndian.PutUint16(canonical[12:14], words.R555)
	binary.BigEndian.PutUint16(canonical[14:16], words.R556)
	binary.BigEndian.PutUint16(canonical[16:18], words.R557)
	binary.BigEndian.PutUint16(canonical[18:20], words.R558)
	digest := sha256.Sum256(canonical[:])
	return hex.EncodeToString(digest[:12])
}
