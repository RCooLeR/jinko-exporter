package source

import (
	"context"

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
		for key, value := range snapshot.Meta {
			enriched.Meta[key] = value
		}
	}

	for _, extra := range e.extras {
		if extra == nil {
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
			for key, value := range extraSnapshot.Meta {
				enriched.Meta[key] = value
			}
		}
	}

	return &enriched, nil
}
