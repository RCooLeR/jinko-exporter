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
	name    string
	sources []Source
}

func NewPriority(sources []Source) *Priority {
	names := make([]string, 0, len(sources))
	for _, src := range sources {
		names = append(names, src.Name())
	}
	return &Priority{
		name:    strings.Join(names, ","),
		sources: sources,
	}
}

func (p *Priority) Name() string {
	return p.name
}

func (p *Priority) Fetch(ctx context.Context) (*model.Snapshot, error) {
	errs := make([]error, 0, len(p.sources))
	for _, src := range p.sources {
		snapshot, err := src.Fetch(ctx)
		if err == nil {
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
