package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/internal/config"
	"github.com/RCooLeR/jinko-exporter/internal/model"
)

func EvaluateSnapshot(ctx context.Context, manager *Manager, cfg config.AlertConfig, snapshot *model.Snapshot) {
	if manager == nil || snapshot == nil {
		return
	}

	index := indexMetrics(snapshot.Metrics)
	evaluateAlarmMetrics(ctx, manager, snapshot)
	evaluateGridDown(ctx, manager, cfg, snapshot, index)
	evaluateBatterySOC(ctx, manager, cfg, snapshot, index)
	evaluateHighTemperature(ctx, manager, cfg, snapshot, index)
}

func EvaluateNoSuccessfulPoll(ctx context.Context, manager *Manager, cfg config.AlertConfig, sourceName string, startedAt time.Time, lastSuccessAt time.Time, lastError string) {
	if manager == nil || cfg.NoSuccessfulPollWindow <= 0 {
		return
	}

	reference := startedAt
	label := "Exporter Start"
	if !lastSuccessAt.IsZero() {
		reference = lastSuccessAt
		label = "Last Successful Poll"
	}

	elapsed := time.Since(reference)
	if elapsed < cfg.NoSuccessfulPollWindow {
		return
	}

	manager.Notify(ctx, Event{
		Key:     "no_successful_poll_" + strings.ToLower(strings.TrimSpace(sourceName)),
		Subject: fmt.Sprintf("No successful poll for %s", strings.TrimSpace(sourceName)),
		Body: fmt.Sprintf(
			"No successful poll has completed within the configured window.\n\nSource: %s\n%s: %s\nElapsed: %s\nThreshold: %s\nLast Error: %s",
			strings.TrimSpace(sourceName),
			label,
			reference.UTC().Format(time.RFC3339),
			elapsed.Round(time.Second),
			cfg.NoSuccessfulPollWindow,
			fallbackText(strings.TrimSpace(lastError), "<none>"),
		),
	})
}

func evaluateAlarmMetrics(ctx context.Context, manager *Manager, snapshot *model.Snapshot) {
	var triggered []model.Metric
	for _, metric := range snapshot.Metrics {
		text := strings.ToLower(metric.Group + " " + metric.Key + " " + metric.Name)
		if strings.Contains(text, "alarm") || strings.Contains(text, "fault") || metric.Group == "alert" {
			if metric.Value != 0 {
				triggered = append(triggered, metric)
			}
		}
	}
	if len(triggered) == 0 {
		return
	}

	sort.Slice(triggered, func(i, j int) bool { return triggered[i].Key < triggered[j].Key })
	lines := make([]string, 0, len(triggered))
	for _, metric := range triggered {
		lines = append(lines, formatMetricLine(metric))
	}

	manager.Notify(ctx, Event{
		Key:     "metric_alarm_" + alertIdentity(snapshot),
		Subject: fmt.Sprintf("Inverter alarm metrics active for %s", alertIdentity(snapshot)),
		Body: fmt.Sprintf(
			"One or more inverter alarm or fault metrics are non-zero.\n\nSource: %s\nDevice: %s\nCollected At: %s\n\nTriggered Metrics:\n%s",
			snapshot.Source,
			alertIdentity(snapshot),
			snapshot.CollectedAt.Format("2006-01-02T15:04:05Z07:00"),
			strings.Join(lines, "\n"),
		),
	})
}

func evaluateGridDown(ctx context.Context, manager *Manager, cfg config.AlertConfig, snapshot *model.Snapshot, index map[string]model.Metric) {
	threshold := cfg.GridDownVoltageThreshold
	if threshold <= 0 {
		return
	}

	keys := []string{"G_V_L1", "G_V_L2", "G_V_L3"}
	var present []model.Metric
	for _, key := range keys {
		if metric, ok := index[key]; ok {
			present = append(present, metric)
		}
	}
	if len(present) == 0 {
		return
	}

	for _, metric := range present {
		if metric.Value > threshold {
			return
		}
	}

	lines := make([]string, 0, len(present))
	for _, metric := range present {
		lines = append(lines, formatMetricLine(metric))
	}

	manager.Notify(ctx, Event{
		Key:     "grid_down_" + alertIdentity(snapshot),
		Subject: fmt.Sprintf("Grid down detected for %s", alertIdentity(snapshot)),
		Body: fmt.Sprintf(
			"All available grid voltage metrics are at or below the configured threshold.\n\nSource: %s\nDevice: %s\nCollected At: %s\nThreshold: %.2f V\n\nGrid Voltages:\n%s",
			snapshot.Source,
			alertIdentity(snapshot),
			snapshot.CollectedAt.Format("2006-01-02T15:04:05Z07:00"),
			threshold,
			strings.Join(lines, "\n"),
		),
	})
}

func evaluateBatterySOC(ctx context.Context, manager *Manager, cfg config.AlertConfig, snapshot *model.Snapshot, index map[string]model.Metric) {
	threshold := cfg.BatterySOCLowThreshold
	if threshold <= 0 {
		return
	}

	for _, key := range []string{"BMS_SOC", "B_left_cap1"} {
		metric, ok := index[key]
		if !ok || metric.Value > threshold {
			continue
		}

		manager.Notify(ctx, Event{
			Key:     "battery_soc_low_" + alertIdentity(snapshot),
			Subject: fmt.Sprintf("Battery SOC low for %s", alertIdentity(snapshot)),
			Body: fmt.Sprintf(
				"Battery state of charge is at or below the configured threshold.\n\nSource: %s\nDevice: %s\nCollected At: %s\nThreshold: %.2f %%\nMetric: %s",
				snapshot.Source,
				alertIdentity(snapshot),
				snapshot.CollectedAt.Format("2006-01-02T15:04:05Z07:00"),
				threshold,
				formatMetricLine(metric),
			),
		})
		return
	}
}

func evaluateHighTemperature(ctx context.Context, manager *Manager, cfg config.AlertConfig, snapshot *model.Snapshot, index map[string]model.Metric) {
	threshold := cfg.HighTemperatureThreshold
	if threshold <= 0 {
		return
	}

	keys := []string{"AC_T", "T_DC", "B_T1", "BMST"}
	var triggered []model.Metric
	for _, key := range keys {
		metric, ok := index[key]
		if ok && metric.Value >= threshold {
			triggered = append(triggered, metric)
		}
	}
	if len(triggered) == 0 {
		return
	}

	lines := make([]string, 0, len(triggered))
	for _, metric := range triggered {
		lines = append(lines, formatMetricLine(metric))
	}

	manager.Notify(ctx, Event{
		Key:     "high_temperature_" + alertIdentity(snapshot),
		Subject: fmt.Sprintf("High temperature detected for %s", alertIdentity(snapshot)),
		Body: fmt.Sprintf(
			"One or more temperature metrics are at or above the configured threshold.\n\nSource: %s\nDevice: %s\nCollected At: %s\nThreshold: %.2f C\n\nTriggered Metrics:\n%s",
			snapshot.Source,
			alertIdentity(snapshot),
			snapshot.CollectedAt.Format("2006-01-02T15:04:05Z07:00"),
			threshold,
			strings.Join(lines, "\n"),
		),
	})
}

func indexMetrics(metrics []model.Metric) map[string]model.Metric {
	index := make(map[string]model.Metric, len(metrics))
	for _, metric := range metrics {
		index[strings.TrimSpace(metric.Key)] = metric
	}
	return index
}

func alertIdentity(snapshot *model.Snapshot) string {
	for _, value := range []string{snapshot.DeviceSN, snapshot.DeviceID, snapshot.SiteID, snapshot.Source} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "unknown-device"
}

func formatMetricLine(metric model.Metric) string {
	if strings.TrimSpace(metric.Unit) == "" {
		return fmt.Sprintf("- %s (%s): %.2f", metric.Name, metric.Key, metric.Value)
	}
	return fmt.Sprintf("- %s (%s): %.2f %s", metric.Name, metric.Key, metric.Value, metric.Unit)
}

func fallbackText(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
