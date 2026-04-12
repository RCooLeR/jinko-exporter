package source

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RCooLeR/jinko-exporter/internal/model"
	"github.com/rs/zerolog/log"
)

type Priority struct {
	name                   string
	sources                []Source
	projectFallbackMetrics bool
	primarySurface         *metricSurface
}

type metricSurface struct {
	deviceSN string
	metrics  map[string]model.Metric
}

func NewPriority(sources []Source, projectFallbackMetrics bool) *Priority {
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name())
	}
	return &Priority{
		name:                   strings.Join(names, ","),
		sources:                sources,
		projectFallbackMetrics: projectFallbackMetrics,
	}
}

func (p *Priority) Name() string {
	return p.name
}

func (p *Priority) Fetch(ctx context.Context) (*model.Snapshot, error) {
	errs := make([]error, 0, len(p.sources))
	for idx, src := range p.sources {
		snapshot, err := src.Fetch(ctx)
		if err == nil {
			if p.projectFallbackMetrics {
				if idx == 0 {
					p.rememberPrimarySurface(snapshot)
				} else {
					snapshot = p.projectToPrimarySurface(snapshot)
				}
			}
			if len(errs) > 0 {
				log.Warn().
					Str("source", src.Name()).
					Int("failed_sources", len(errs)).
					Msg("priority source fetch succeeded after earlier source failures")
			}
			return snapshot, nil
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

	return nil, fmt.Errorf("all priority sources failed (%s): %w", p.name, errors.Join(errs...))
}

func (p *Priority) rememberPrimarySurface(snapshot *model.Snapshot) {
	if snapshot == nil {
		return
	}

	surface := &metricSurface{
		deviceSN: strings.TrimSpace(snapshot.DeviceSN),
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
	if p.primarySurface.deviceSN != "" {
		projected.DeviceSN = p.primarySurface.deviceSN
	}

	metrics := make([]model.Metric, 0, len(snapshot.Metrics))
	seen := make(map[string]struct{}, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		key := strings.TrimSpace(metric.Key)
		if key == "" {
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
