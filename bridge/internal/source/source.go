package source

import (
	"context"
	"reflect"
	"sync"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

type Source interface {
	Name() string
	Fetch(ctx context.Context) (*model.Snapshot, error)
}

// BackgroundMaintainer is implemented by sources that need a long-running,
// context-bound maintenance task independently of telemetry selection.
// RunBackground blocks until ctx is cancelled.
type BackgroundMaintainer interface {
	RunBackground(ctx context.Context)
}

// backgroundMaintainerProvider lets source combinators expose their leaf
// maintainers without starting nested runners. Flattening the tree first lets
// an Enriched(Priority(...)) graph run a shared client exactly once.
type backgroundMaintainerProvider interface {
	backgroundMaintainers() []BackgroundMaintainer
}

func collectBackgroundMaintainers(sources []Source) []BackgroundMaintainer {
	maintainers := make([]BackgroundMaintainer, 0, len(sources))
	for _, src := range sources {
		if isNilInterface(src) {
			continue
		}
		if provider, ok := src.(backgroundMaintainerProvider); ok {
			maintainers = append(maintainers, provider.backgroundMaintainers()...)
			continue
		}
		if maintainer, ok := src.(BackgroundMaintainer); ok && !isNilInterface(maintainer) {
			maintainers = append(maintainers, maintainer)
		}
	}
	return uniqueBackgroundMaintainers(maintainers)
}

func uniqueBackgroundMaintainers(maintainers []BackgroundMaintainer) []BackgroundMaintainer {
	unique := make([]BackgroundMaintainer, 0, len(maintainers))
	seenComparable := make(map[BackgroundMaintainer]struct{}, len(maintainers))
	seenPointers := make(map[struct {
		typeName string
		pointer  uintptr
	}]struct{}, len(maintainers))

	for _, maintainer := range maintainers {
		if isNilInterface(maintainer) {
			continue
		}

		value := reflect.ValueOf(maintainer)
		typeOf := value.Type()
		if typeOf.Comparable() {
			if _, exists := seenComparable[maintainer]; exists {
				continue
			}
			seenComparable[maintainer] = struct{}{}
		} else if value.Kind() == reflect.Pointer {
			key := struct {
				typeName string
				pointer  uintptr
			}{typeName: typeOf.String(), pointer: value.Pointer()}
			if _, exists := seenPointers[key]; exists {
				continue
			}
			seenPointers[key] = struct{}{}
		}
		unique = append(unique, maintainer)
	}
	return unique
}

func runBackgroundMaintainers(ctx context.Context, maintainers []BackgroundMaintainer) {
	maintainers = uniqueBackgroundMaintainers(maintainers)
	if len(maintainers) == 0 {
		<-ctx.Done()
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(maintainers))
	for _, maintainer := range maintainers {
		go func() {
			defer wg.Done()
			maintainer.RunBackground(ctx)
		}()
	}
	wg.Wait()
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
