package source

import (
	"context"

	"github.com/RCooLeR/jinko-exporter/internal/model"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context) (*model.Snapshot, error)
}
