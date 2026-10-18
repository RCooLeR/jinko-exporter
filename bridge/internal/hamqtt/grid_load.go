package hamqtt

import (
	"fmt"
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
	return gridMeterObserver{publisher: p, group: "grid_load"}
}

// GridInputObserver owns the optional upstream-of-stabilizers meter. Its
// availability and timestamps are independent of both other poll runners.
func (p *Publisher) GridInputObserver() poller.Observer {
	p.mu.Lock()
	p.independentGridInput = true
	p.mu.Unlock()
	return gridMeterObserver{publisher: p, group: "grid_input"}
}

type gridMeterObserver struct {
	publisher *Publisher
	group     string
}

type gridMeterStream struct {
	group       string
	enabled     bool
	snapshot    **model.Snapshot
	up          *bool
	publishedAt *string
}

// Call only while holding the publisher lock, or during construction.
func (p *Publisher) gridMeterStreams() []gridMeterStream {
	return []gridMeterStream{
		{"grid_load", p.independentGridLoad, &p.gridLoadSnapshot, &p.gridLoadUp, &p.gridLoadPublishedAt},
		{"grid_input", p.independentGridInput, &p.gridInputSnapshot, &p.gridInputUp, &p.gridInputPublishedAt},
	}
}

func (p *Publisher) independentMeters() bool { return p.independentGridLoad || p.independentGridInput }

func (p *Publisher) ownsIndependentMetric(metric model.Metric) bool {
	return p.independentGridLoad && isGridLoadMetric(metric) || p.independentGridInput && isGridInputMetric(metric)
}

func (p *Publisher) ownsIndependentMeta(key string) bool {
	return p.independentGridLoad && isGridLoadMetaKey(key) || p.independentGridInput && isGridInputMetaKey(key)
}

func (observer gridMeterObserver) stream() gridMeterStream {
	for _, stream := range observer.publisher.gridMeterStreams() {
		if stream.group == observer.group {
			return stream
		}
	}
	panic("unknown independent meter observer")
}

func (observer gridMeterObserver) OnPollSuccess(snapshot *model.Snapshot, _ time.Duration) (resultErr error) {
	if snapshot == nil {
		return nil
	}
	p := observer.publisher
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}
	stream := observer.stream()
	for _, metric := range snapshot.Metrics {
		if !strings.EqualFold(strings.TrimSpace(metric.Group), stream.group) {
			*stream.up = false
			if err := p.publishIndependentLocked(); err != nil {
				p.markPublicationFailureLocked()
			}
			return fmt.Errorf("%s MQTT observer received a metric from another stream", stream.group)
		}
	}
	previous, previousPublishedAt := *stream.snapshot, *stream.publishedAt
	*stream.snapshot = cloneStreamSnapshot(snapshot)
	*stream.up = true
	*stream.publishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := p.publishIndependentLocked(); err != nil {
		*stream.snapshot, *stream.publishedAt = previous, previousPublishedAt
		*stream.up = false
		p.markPublicationFailureLocked()
		return err
	}
	return nil
}

func (observer gridMeterObserver) OnPollFailure(_ string, _ error, _ time.Duration, _ uint64) error {
	p := observer.publisher
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing || p.closed {
		return nil
	}
	*observer.stream().up = false
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
			if p.ownsIndependentMeta(key) {
				delete(combined.Meta, key)
			}
		}
		if p.inverterUp {
			for _, metric := range p.inverterSnapshot.Metrics {
				if !p.ownsIndependentMetric(metric) {
					combined.Metrics = append(combined.Metrics, metric)
				}
			}
		}
	}
	for _, stream := range p.gridMeterStreams() {
		if !stream.enabled || !*stream.up || *stream.snapshot == nil {
			continue
		}
		snapshot := *stream.snapshot
		combined.Metrics = append(combined.Metrics, snapshot.Metrics...)
		if combined.Meta == nil {
			combined.Meta = make(map[string]string)
		}
		maps.Copy(combined.Meta, gridMeterMetadata(snapshot.Meta, stream.group))
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

func isGridInputMetric(metric model.Metric) bool {
	return strings.EqualFold(strings.TrimSpace(metric.Group), "grid_input")
}

func isGridInputMetaKey(key string) bool {
	return strings.HasPrefix(sanitizeID(key), "shelly_grid_input_")
}

func gridMeterMetadata(meta map[string]string, group string) map[string]string {
	owned := make(map[string]string)
	for key, value := range meta {
		if strings.HasPrefix(sanitizeID(key), "shelly_"+group+"_") {
			owned[key] = value
		}
	}
	return owned
}
