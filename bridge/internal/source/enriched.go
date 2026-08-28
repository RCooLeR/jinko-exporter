package source

import (
	"context"
	"maps"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/rs/zerolog/log"
)

type Enriched struct {
	primary Source
	extras  []Source
}

func NewEnriched(primary Source, extras ...Source) *Enriched {
	return &Enriched{primary: primary, extras: extras}
}

func (e *Enriched) Name() string {
	return e.primary.Name()
}

func (e *Enriched) RunBackground(ctx context.Context) {
	runBackgroundMaintainers(ctx, e.backgroundMaintainers())
}

func (e *Enriched) backgroundMaintainers() []BackgroundMaintainer {
	sources := make([]Source, 0, 1+len(e.extras))
	sources = append(sources, e.primary)
	sources = append(sources, e.extras...)
	return collectBackgroundMaintainers(sources)
}

func (e *Enriched) Fetch(ctx context.Context) (*model.Snapshot, error) {
	snapshot, err := e.primary.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || len(e.extras) == 0 {
		return snapshot, nil
	}

	enriched := *snapshot
	enriched.Metrics = append([]model.Metric{}, snapshot.Metrics...)
	if len(snapshot.Meta) > 0 {
		enriched.Meta = make(map[string]string, len(snapshot.Meta)+len(e.extras))
		maps.Copy(enriched.Meta, snapshot.Meta)
	}

	for _, extra := range e.extras {
		if isNilInterface(extra) {
			continue
		}
		extraSnapshot, err := extra.Fetch(ctx)
		if err != nil {
			log.Warn().Err(err).Str("source", extra.Name()).Msg("snapshot enrichment source failed")
			continue
		}
		if extraSnapshot == nil {
			continue
		}
		enriched.Metrics = append(enriched.Metrics, extraSnapshot.Metrics...)
		if len(extraSnapshot.Meta) > 0 {
			if enriched.Meta == nil {
				enriched.Meta = make(map[string]string, len(extraSnapshot.Meta))
			}
			maps.Copy(enriched.Meta, extraSnapshot.Meta)
		}
	}

	return &enriched, nil
}
