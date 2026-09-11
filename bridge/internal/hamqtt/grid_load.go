package hamqtt

import (
	"errors"
	"maps"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/poller"
)

// GridLoadObserver enables independent grid-load availability and returns the
// observer for its poll runner. Call it before Start and before polling begins.
// The inverter continues to use Publisher itself as its observer. Both streams
// share one connection, one LWT, and the existing stable Home Assistant identity.
func (p *Publisher) GridLoadObserver() poller.Observer {
	p.mu.Lock()
	p.independentGridLoad = true
	p.mu.Unlock()
	return gridLoadObserver{publisher: p}
}

type gridLoadObserver struct {
	publisher *Publisher
}

func (observer gridLoadObserver) OnPollSuccess(snapshot *model.Snapshot, _ time.Duration) (resultErr error) {
	if snapshot == nil {
		return nil
	}
	p := observer.publisher
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}
	for _, metric := range snapshot.Metrics {
		if !isGridLoadMetric(metric) {
			p.gridLoadUp = false
			if err := p.publishIndependentLocked(); err != nil {
				p.markPublicationFailureLocked()
			}
			return errors.New("grid-load MQTT observer received a non-grid-load metric")
		}
	}
	previous, previousPublishedAt := p.gridLoadSnapshot, p.gridLoadPublishedAt
	p.gridLoadSnapshot = cloneStreamSnapshot(snapshot)
	p.gridLoadUp = true
	p.gridLoadPublishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := p.publishIndependentLocked(); err != nil {
		p.gridLoadSnapshot, p.gridLoadPublishedAt = previous, previousPublishedAt
		p.gridLoadUp = false
		p.markPublicationFailureLocked()
		return err
	}
	return nil
}

func (observer gridLoadObserver) OnPollFailure(_ string, _ error, _ time.Duration, _ uint64) error {
	p := observer.publisher
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}
	p.gridLoadUp = false
	if err := p.publishIndependentLocked(); err != nil {
		p.markPublicationFailureLocked()
		return err
	}
	return nil
}

func (p *Publisher) publishIndependentLocked() error {
	combined := &model.Snapshot{Source: p.primarySource}
	if p.inverterSnapshot != nil {
		combined = cloneStreamSnapshot(p.inverterSnapshot)
		combined.Metrics = nil
		// Legacy enriched snapshots may have included Shelly metadata. The
		// independent stream now owns those keys, including their availability.
		for key := range combined.Meta {
			if isGridLoadMetaKey(key) {
				delete(combined.Meta, key)
			}
		}
		if p.inverterUp {
			for _, metric := range p.inverterSnapshot.Metrics {
				if !isGridLoadMetric(metric) {
					combined.Metrics = append(combined.Metrics, metric)
				}
			}
		}
	}
	if p.gridLoadUp && p.gridLoadSnapshot != nil {
		combined.Metrics = append(combined.Metrics, p.gridLoadSnapshot.Metrics...)
		if combined.Meta == nil {
			combined.Meta = make(map[string]string)
		}
		maps.Copy(combined.Meta, gridLoadMetadata(p.gridLoadSnapshot.Meta))
	}
	return p.publishSnapshotLocked(combined, p.inverterDuration)
}

// Fail closed after a schema/encoding error. A later successful transaction can
// restore availability; a reconnect alone may only replay this offline status.
func (p *Publisher) markPublicationFailureLocked() {
	p.lastAvailability = availabilityOffline
	p.offlinePublishPending = true
	if err := p.publishString(p.availabilityTopic, availabilityOffline, p.cfg.Retain); err == nil {
		p.offlinePublishPending = false
	}
}

func cloneStreamSnapshot(snapshot *model.Snapshot) *model.Snapshot {
	cloned := *snapshot
	cloned.Metrics = append([]model.Metric(nil), snapshot.Metrics...)
	cloned.Meta = maps.Clone(snapshot.Meta)
	return &cloned
}

func isGridLoadMetric(metric model.Metric) bool {
	return strings.EqualFold(strings.TrimSpace(metric.Group), "grid_load")
}

func isGridLoadMetaKey(key string) bool {
	return strings.HasPrefix(sanitizeID(key), "shelly_grid_load_")
}

func gridLoadMetadata(meta map[string]string) map[string]string {
	owned := make(map[string]string)
	for key, value := range meta {
		if isGridLoadMetaKey(key) {
			owned[key] = value
		}
	}
	return owned
}
