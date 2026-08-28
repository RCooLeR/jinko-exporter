package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/rs/zerolog/log"
)

type Priority struct {
	name                   string
	sources                []Source
	projectFallbackMetrics bool
	primarySurface         *metricSurface
	sourceGates            []*sourceFetchGate
	correlationMu          sync.RWMutex
	alertCorrelation       *alertCorrelationWorker
}

type metricSurface struct {
	deviceSN string
	parentSN string
	deviceID string
	siteID   string
	metrics  map[string]model.Metric
}

func NewPriority(sources []Source, projectFallbackMetrics bool) *Priority {
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		if isNilInterface(src) {
			continue
		}
		names = append(names, src.Name())
	}
	gates := make([]*sourceFetchGate, len(sources))
	for index, src := range sources {
		for priorIndex := range index {
			if sameSourceInstance(src, sources[priorIndex]) {
				gates[index] = gates[priorIndex]
				break
			}
		}
		if gates[index] == nil {
			gates[index] = newSourceFetchGate()
		}
	}
	return &Priority{
		name:                   strings.Join(names, ","),
		sources:                sources,
		projectFallbackMetrics: projectFallbackMetrics,
		sourceGates:            gates,
	}
}

func (p *Priority) Name() string {
	return p.name
}

func (p *Priority) RunBackground(ctx context.Context) {
	runBackgroundMaintainers(ctx, p.backgroundMaintainers())
}

func (p *Priority) backgroundMaintainers() []BackgroundMaintainer {
	maintainers := collectBackgroundMaintainers(p.sources)
	if correlation := p.correlationWorker(); correlation != nil {
		maintainers = append(maintainers, correlation)
	}
	return maintainers
}

func (p *Priority) Fetch(ctx context.Context) (*model.Snapshot, error) {
	errs := make([]error, 0, len(p.sources))
	for idx, src := range p.sources {
		if isNilInterface(src) {
			errs = append(errs, errors.New("nil priority source"))
			continue
		}
		snapshot, err := p.sourceGates[idx].fetchNormal(ctx, src)
		if err == nil && snapshot != nil {
			if p.projectFallbackMetrics {
				if idx == 0 {
					p.rememberPrimarySurface(snapshot)
				} else if p.fallbackDeviceConflictsWithPrimary(snapshot) {
					err = errors.New("fallback snapshot device serial does not match the learned primary device serial")
				} else {
					snapshot = p.projectToPrimarySurface(snapshot)
					if p.primarySurface != nil && !hasOrdinaryTelemetry(snapshot) {
						err = errors.New("projected fallback snapshot has no telemetry metrics compatible with the primary surface")
					}
				}
			}
			if err != nil {
				// Continue to the next source. Source-local alert words alone are
				// preserved when telemetry overlaps, but cannot make an otherwise
				// incompatible fallback count as a successful telemetry poll.
			} else {
				// Correlate only a snapshot that passed every Priority acceptance
				// check. In particular, a mismatched Modbus fallback must not
				// trigger cloud diagnostics for a device learned from another
				// source.
				if correlation := p.correlationWorker(); correlation != nil {
					correlation.observe(snapshot)
				}
				if len(errs) > 0 {
					log.Warn().
						Str("source", src.Name()).
						Int("failed_sources", len(errs)).
						Msg("priority source fetch succeeded after earlier source failures")
				}
				return snapshot, nil
			}
		}
		if err == nil {
			err = errors.New("source returned a nil snapshot without an error")
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		errs = append(errs, fmt.Errorf("%s: %w", src.Name(), err))
		log.Warn().
			Err(err).
			Str("source", src.Name()).
			Msg("priority source fetch failed, trying next source")
	}

	joined := errors.Join(errs...)
	if joined == nil {
		joined = errors.New("no priority sources configured")
	}
	return nil, fmt.Errorf("all priority sources failed (%s): %w", p.name, joined)
}

func (p *Priority) fallbackDeviceConflictsWithPrimary(snapshot *model.Snapshot) bool {
	if snapshot == nil || p.primarySurface == nil {
		return false
	}
	primaryDeviceSN := strings.TrimSpace(p.primarySurface.deviceSN)
	fallbackDeviceSN := strings.TrimSpace(snapshot.DeviceSN)
	return primaryDeviceSN != "" && fallbackDeviceSN != "" && fallbackDeviceSN != primaryDeviceSN
}

func (p *Priority) rememberPrimarySurface(snapshot *model.Snapshot) {
	if snapshot == nil {
		return
	}

	surface := &metricSurface{
		deviceSN: strings.TrimSpace(snapshot.DeviceSN),
		parentSN: strings.TrimSpace(snapshot.ParentSN),
		deviceID: strings.TrimSpace(snapshot.DeviceID),
		siteID:   strings.TrimSpace(snapshot.SiteID),
		metrics:  make(map[string]model.Metric, len(snapshot.Metrics)),
	}
	for _, metric := range snapshot.Metrics {
		key := strings.TrimSpace(metric.Key)
		if key == "" {
			continue
		}
		if _, ok := surface.metrics[key]; !ok {
			surface.metrics[key] = metric
		}
	}
	p.primarySurface = surface
}

func (p *Priority) projectToPrimarySurface(snapshot *model.Snapshot) *model.Snapshot {
	if snapshot == nil || p.primarySurface == nil {
		return snapshot
	}

	projected := *snapshot
	fallbackDeviceSN := strings.TrimSpace(projected.DeviceSN)
	sameDevice := fallbackDeviceSN != "" &&
		p.primarySurface.deviceSN != "" &&
		fallbackDeviceSN == p.primarySurface.deviceSN
	if sameDevice {
		if strings.TrimSpace(projected.ParentSN) == "" && p.primarySurface.parentSN != "" {
			projected.ParentSN = p.primarySurface.parentSN
		}
		if strings.TrimSpace(projected.DeviceID) == "" && p.primarySurface.deviceID != "" {
			projected.DeviceID = p.primarySurface.deviceID
		}
		if strings.TrimSpace(projected.SiteID) == "" && p.primarySurface.siteID != "" {
			projected.SiteID = p.primarySurface.siteID
		}
	}

	metrics := make([]model.Metric, 0, len(snapshot.Metrics))
	seen := make(map[string]struct{}, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		key := strings.TrimSpace(metric.Key)
		if key == "" {
			continue
		}
		// Alert metrics are source-domain data, not interchangeable telemetry.
		// Preserve the fallback source's own warning/alarm/fault surface so an
		// active cloud fault cannot disappear merely because the primary Modbus
		// surface uses different Deye-specific raw warning words.
		if isSourceAlertMetric(metric) {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			metrics = append(metrics, metric)
			continue
		}
		primaryMetric, ok := p.primarySurface.metrics[key]
		if !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		metric.Group = primaryMetric.Group
		metric.Key = primaryMetric.Key
		metric.Name = primaryMetric.Name
		metric.Unit = primaryMetric.Unit
		metrics = append(metrics, metric)
	}
	projected.Metrics = metrics
	return &projected
}

func isSourceAlertMetric(metric model.Metric) bool {
	text := strings.ToLower(metric.Group + " " + metric.Key + " " + metric.Name)
	return strings.TrimSpace(strings.ToLower(metric.Group)) == "alert" ||
		strings.Contains(text, "alarm") || strings.Contains(text, "fault")
}

func hasOrdinaryTelemetry(snapshot *model.Snapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, metric := range snapshot.Metrics {
		if strings.TrimSpace(metric.Key) != "" && !isSourceAlertMetric(metric) {
			return true
		}
	}
	return false
}
