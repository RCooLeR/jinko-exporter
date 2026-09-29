package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

const (
	maxCorrelationEvidenceMetrics = 256
	maxCorrelationSnapshotAge     = 15 * time.Minute
	maxCorrelationFutureSkew      = 2 * time.Minute
)

var (
	// ErrInvalidModbusAlertSignature identifies a snapshot whose R553-R558
	// surface is incomplete or is not an exact set of unsigned 16-bit values.
	ErrInvalidModbusAlertSignature = errors.New("invalid Modbus alert signature")
	errDiagnosticPreempted         = errors.New("diagnostic source fetch preempted by normal polling")
)

var modbusAlertMetricKeys = [...]string{
	"DEYE_MODBUS_R553_WARNING_WORD_1_RAW",
	"DEYE_MODBUS_R554_WARNING_WORD_2_RAW",
	"DEYE_MODBUS_R555_FAULT_WORD_1_RAW",
	"DEYE_MODBUS_R556_FAULT_WORD_2_RAW",
	"DEYE_MODBUS_R557_FAULT_WORD_3_RAW",
	"DEYE_MODBUS_R558_FAULT_WORD_4_RAW",
}

// ModbusAlertSignature is the lossless, source-local R553-R558 evidence that
// caused cloud correlation. It deliberately does not assign meanings to bits
// that have not yet been verified against independent sources.
type ModbusAlertSignature struct {
	R553 uint16
	R554 uint16
	R555 uint16
	R556 uint16
	R557 uint16
	R558 uint16
}

func (s ModbusAlertSignature) values() [6]uint16 {
	return [6]uint16{s.R553, s.R554, s.R555, s.R556, s.R557, s.R558}
}

// Active reports whether at least one raw Modbus warning/fault word is set.
func (s ModbusAlertSignature) Active() bool {
	for _, value := range s.values() {
		if value != 0 {
			return true
		}
	}
	return false
}

// ModbusAlertSignatureFromSnapshot accepts only a successful Modbus snapshot
// containing every R553-R558 metric exactly once, in the alert group, as an
// exactly representable U16 integer. Invalid observations never affect worker
// cooldown/rearm state.
func ModbusAlertSignatureFromSnapshot(snapshot *model.Snapshot) (ModbusAlertSignature, error) {
	if snapshot == nil || strings.TrimSpace(snapshot.Source) != "modbus" {
		return ModbusAlertSignature{}, fmt.Errorf("%w: snapshot source is not modbus", ErrInvalidModbusAlertSignature)
	}

	var values [6]uint16
	var found [6]bool
	for _, metric := range snapshot.Metrics {
		key := strings.TrimSpace(metric.Key)
		index := -1
		for candidateIndex, candidateKey := range modbusAlertMetricKeys {
			if key == candidateKey {
				index = candidateIndex
				break
			}
		}
		if index < 0 {
			continue
		}
		if found[index] {
			return ModbusAlertSignature{}, fmt.Errorf("%w: duplicate register metric", ErrInvalidModbusAlertSignature)
		}
		if strings.TrimSpace(metric.Group) != "alert" {
			return ModbusAlertSignature{}, fmt.Errorf("%w: register metric is outside the alert group", ErrInvalidModbusAlertSignature)
		}
		if strings.TrimSpace(metric.Unit) != "" {
			return ModbusAlertSignature{}, fmt.Errorf("%w: raw register metric has a unit", ErrInvalidModbusAlertSignature)
		}
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) ||
			metric.Value < 0 || metric.Value > math.MaxUint16 || math.Trunc(metric.Value) != metric.Value {
			return ModbusAlertSignature{}, fmt.Errorf("%w: register metric is not an exact U16 integer", ErrInvalidModbusAlertSignature)
		}
		values[index] = uint16(metric.Value)
		found[index] = true
	}
	for _, ok := range found {
		if !ok {
			return ModbusAlertSignature{}, fmt.Errorf("%w: one or more register metrics are missing", ErrInvalidModbusAlertSignature)
		}
	}

	return ModbusAlertSignature{
		R553: values[0],
		R554: values[1],
		R555: values[2],
		R556: values[3],
		R557: values[4],
		R558: values[5],
	}, nil
}

type AlertCorrelationEventKind string

const (
	AlertCorrelationDetected  AlertCorrelationEventKind = "detected"
	AlertCorrelationRecovered AlertCorrelationEventKind = "recovered"
	AlertCorrelationComplete  AlertCorrelationEventKind = "correlation_complete"
)

type AlertCorrelationSourceStatus string

const (
	AlertCorrelationSourceOK         AlertCorrelationSourceStatus = "ok"
	AlertCorrelationSourceFailed     AlertCorrelationSourceStatus = "fetch_failed"
	AlertCorrelationSourceTimedOut   AlertCorrelationSourceStatus = "timeout"
	AlertCorrelationSourceCanceled   AlertCorrelationSourceStatus = "canceled"
	AlertCorrelationSourcePreempted  AlertCorrelationSourceStatus = "preempted"
	AlertCorrelationSourceEmpty      AlertCorrelationSourceStatus = "empty_snapshot"
	AlertCorrelationSourceUnverified AlertCorrelationSourceStatus = "identity_unverified"
	AlertCorrelationSourceMismatch   AlertCorrelationSourceStatus = "device_mismatch"
	AlertCorrelationSourceWrongType  AlertCorrelationSourceStatus = "source_mismatch"
	AlertCorrelationSourceStale      AlertCorrelationSourceStatus = "stale_snapshot"
	AlertCorrelationSourceBadTime    AlertCorrelationSourceStatus = "invalid_collected_at"
	AlertCorrelationSourceNoMetrics  AlertCorrelationSourceStatus = "no_safe_metrics"
)

// AlertCorrelationSourceSummary is safe for a notification. It contains only
// the configured source name and a closed status vocabulary, never an upstream
// error, response body, URL, or device identity.
type AlertCorrelationSourceSummary struct {
	Source string
	Status AlertCorrelationSourceStatus
}

// AlertCorrelationEvent is delivered asynchronously. The callback must honor
// ctx cancellation. Sources is populated only for the completion event.
type AlertCorrelationEvent struct {
	Kind       AlertCorrelationEventKind
	ObservedAt time.Time
	Signature  ModbusAlertSignature
	Sources    []AlertCorrelationSourceSummary
}

// AlertCorrelationMetric is either a normalized source-local alert or an
// ordinary cloud point whose key also existed in the triggering Modbus
// snapshot. Unknown cloud-only fields, names, metadata, raw payloads, and
// identities are excluded.
type AlertCorrelationMetric struct {
	Group string
	Key   string
	Unit  string
	Value float64
}

// AlertCorrelationSourceEvidence is deliberately sanitized before it crosses
// the source package boundary.
type AlertCorrelationSourceEvidence struct {
	Source      string
	Status      AlertCorrelationSourceStatus
	CollectedAt time.Time
	Duration    time.Duration
	Metrics     []AlertCorrelationMetric
	// TotalMetricCount counts the complete normalized finite set before the
	// bounded Metrics slice is truncated. MetricsSHA256 fingerprints that full
	// sorted set, including entries beyond the cap.
	TotalMetricCount int
	AlertMetricCount int
	Truncated        bool
	MetricsSHA256    string
}

type AlertCorrelationEvidence struct {
	ObservedAt time.Time
	Signature  ModbusAlertSignature
	Sources    []AlertCorrelationSourceEvidence
}

type AlertCorrelationNotifyFunc func(context.Context, AlertCorrelationEvent) error
type AlertCorrelationEvidenceFunc func(context.Context, AlertCorrelationEvidence) error

// AlertCorrelationConfig is independent of application/config packages so a
// caller can wire any bounded notifier and structured evidence sink.
type AlertCorrelationConfig struct {
	Cooldown       time.Duration
	NotifyTimeout  time.Duration
	JobTimeout     time.Duration
	Now            func() time.Time
	Notify         AlertCorrelationNotifyFunc
	RecordEvidence AlertCorrelationEvidenceFunc
}

type alertCorrelationTarget struct {
	name   string
	source Source
	gate   *sourceFetchGate
}

type alertCorrelationJob struct {
	kind             AlertCorrelationEventKind
	observedAt       time.Time
	signature        ModbusAlertSignature
	expectedDeviceSN string
	ordinaryKeys     map[string]alertCorrelationMetricIdentity
}

type alertCorrelationMetricIdentity struct {
	Group string
	Unit  string
}

// alertCorrelationWorker owns one bounded latest-wins queue. At most one job
// is correlated at a time; its two source reads may run concurrently so one
// unhealthy cloud cannot prevent the other from being sampled before timeout.
type alertCorrelationWorker struct {
	cfg     AlertCorrelationConfig
	targets []alertCorrelationTarget
	jobs    chan alertCorrelationJob

	mu              sync.Mutex
	running         bool
	active          bool
	lastSignature   ModbusAlertSignature
	lastDeviceSN    string
	lastTriggeredAt time.Time
}

var _ BackgroundMaintainer = (*alertCorrelationWorker)(nil)

// ConfigureAlertCorrelation attaches one optional worker to this Priority.
// The worker reuses the exact configured source instances and their Fetch
// gates; it never constructs a second cloud client. Configuration must happen
// during startup, before RunBackground begins.
func (p *Priority) ConfigureAlertCorrelation(cfg AlertCorrelationConfig) error {
	if p == nil {
		return errors.New("cannot configure alert correlation on a nil priority source")
	}

	p.correlationMu.Lock()
	defer p.correlationMu.Unlock()
	if p.alertCorrelation != nil {
		return errors.New("alert correlation is already configured")
	}

	modbusCount := 0
	targetsByName := make(map[string][]alertCorrelationTarget, 2)
	for index, src := range p.sources {
		if isNilInterface(src) {
			continue
		}
		name := strings.TrimSpace(src.Name())
		switch name {
		case "modbus":
			modbusCount++
		case "jinko", "solarman":
			targetsByName[name] = append(targetsByName[name], alertCorrelationTarget{
				name:   name,
				source: src,
				gate:   p.sourceGates[index],
			})
		}
	}
	if modbusCount != 1 {
		return fmt.Errorf("alert correlation requires exactly one configured Modbus source; got %d", modbusCount)
	}
	if len(targetsByName["jinko"]) != 1 || len(targetsByName["solarman"]) != 1 {
		return errors.New("alert correlation requires exactly one configured Jinko source and one configured Solarman source")
	}
	targets := []alertCorrelationTarget{targetsByName["jinko"][0], targetsByName["solarman"][0]}
	worker, err := newAlertCorrelationWorker(cfg, targets)
	if err != nil {
		return err
	}
	p.alertCorrelation = worker
	return nil
}

func (p *Priority) correlationWorker() *alertCorrelationWorker {
	if p == nil {
		return nil
	}
	p.correlationMu.RLock()
	defer p.correlationMu.RUnlock()
	return p.alertCorrelation
}

func newAlertCorrelationWorker(cfg AlertCorrelationConfig, targets []alertCorrelationTarget) (*alertCorrelationWorker, error) {
	if cfg.Cooldown < 0 {
		return nil, errors.New("alert correlation cooldown must be >= 0")
	}
	if cfg.JobTimeout <= 0 {
		return nil, errors.New("alert correlation job timeout must be > 0")
	}
	if cfg.NotifyTimeout <= 0 {
		return nil, errors.New("alert correlation notify timeout must be > 0")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Notify == nil && cfg.RecordEvidence == nil {
		return nil, errors.New("alert correlation requires a notifier or evidence callback")
	}
	if len(targets) != 2 || targets[0].name != "jinko" || targets[1].name != "solarman" {
		return nil, errors.New("alert correlation requires the configured Jinko and Solarman source instances")
	}
	return &alertCorrelationWorker{
		cfg:     cfg,
		targets: append([]alertCorrelationTarget(nil), targets...),
		jobs:    make(chan alertCorrelationJob, 1),
	}, nil
}

// observe is called on the normal polling path and never calls user code or a
// cloud source. A full queue has its pending (not running) job replaced.
func (w *alertCorrelationWorker) observe(snapshot *model.Snapshot) {
	signature, err := ModbusAlertSignatureFromSnapshot(snapshot)
	if err != nil {
		return
	}
	// Keep the clock value intact for cooldown arithmetic so time.Now's
	// monotonic component survives wall-clock corrections. Only the externally
	// reported timestamp is converted to UTC.
	now := w.cfg.Now()
	observedAt := now.UTC()

	w.mu.Lock()
	defer w.mu.Unlock()

	if !signature.Active() {
		if !w.active {
			return
		}
		w.active = false
		w.lastSignature = ModbusAlertSignature{}
		w.lastDeviceSN = ""
		w.lastTriggeredAt = time.Time{}
		w.enqueueLatestLocked(alertCorrelationJob{
			kind:             AlertCorrelationRecovered,
			observedAt:       observedAt,
			signature:        signature,
			expectedDeviceSN: strings.TrimSpace(snapshot.DeviceSN),
		})
		return
	}

	expectedDeviceSN := strings.TrimSpace(snapshot.DeviceSN)
	changed := !w.active || signature != w.lastSignature || expectedDeviceSN != w.lastDeviceSN
	cooldownElapsed := w.cfg.Cooldown == 0 || w.lastTriggeredAt.IsZero() || now.Sub(w.lastTriggeredAt) >= w.cfg.Cooldown
	if !changed && !cooldownElapsed {
		return
	}
	w.active = true
	w.lastSignature = signature
	w.lastDeviceSN = expectedDeviceSN
	w.lastTriggeredAt = now
	w.enqueueLatestLocked(alertCorrelationJob{
		kind:             AlertCorrelationDetected,
		observedAt:       observedAt,
		signature:        signature,
		expectedDeviceSN: expectedDeviceSN,
		ordinaryKeys:     cloneOrdinaryMetricKeys(snapshot.Metrics),
	})
}

func cloneOrdinaryMetricKeys(metrics []model.Metric) map[string]alertCorrelationMetricIdentity {
	keys := make(map[string]alertCorrelationMetricIdentity)
	for _, metric := range metrics {
		if strings.TrimSpace(metric.Group) == "alert" {
			continue
		}
		key := strings.TrimSpace(metric.Key)
		if key != "" {
			if _, exists := keys[key]; !exists {
				keys[key] = alertCorrelationMetricIdentity{
					Group: strings.TrimSpace(metric.Group),
					Unit:  strings.TrimSpace(metric.Unit),
				}
			}
		}
	}
	return keys
}

func (w *alertCorrelationWorker) enqueueLatestLocked(job alertCorrelationJob) {
	select {
	case w.jobs <- job:
		return
	default:
	}
	select {
	case <-w.jobs:
	default:
	}
	select {
	case w.jobs <- job:
	default:
		// The sole consumer can race between replacement operations. If it did,
		// it already made room and accepted the previous latest observation.
	}
}

func (w *alertCorrelationWorker) RunBackground(ctx context.Context) {
	if ctx == nil {
		return
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		<-ctx.Done()
		return
	}
	w.running = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.active = false
		w.lastSignature = ModbusAlertSignature{}
		w.lastDeviceSN = ""
		w.lastTriggeredAt = time.Time{}
		for {
			select {
			case <-w.jobs:
				continue
			default:
				w.mu.Unlock()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.jobs:
			w.process(ctx, job)
		}
	}
}

func (w *alertCorrelationWorker) process(parent context.Context, job alertCorrelationJob) {
	notifyCtx, cancelNotify := context.WithTimeout(parent, w.cfg.NotifyTimeout)
	if job.kind != AlertCorrelationDetected {
		w.notify(notifyCtx, AlertCorrelationEvent{
			Kind:       job.kind,
			ObservedAt: job.observedAt,
			Signature:  job.signature,
		})
		cancelNotify()
		return
	}
	w.notify(notifyCtx, AlertCorrelationEvent{
		Kind:       job.kind,
		ObservedAt: job.observedAt,
		Signature:  job.signature,
	})
	cancelNotify()

	// Notification and cloud evidence have independent budgets. A slow HA
	// request can consume only NotifyTimeout; both cloud reads still receive a
	// fresh full JobTimeout afterwards.
	jobCtx, cancelJob := context.WithTimeout(parent, w.cfg.JobTimeout)
	defer cancelJob()

	type indexedEvidence struct {
		index    int
		evidence AlertCorrelationSourceEvidence
	}
	results := make(chan indexedEvidence, len(w.targets))
	for index, target := range w.targets {
		go func() {
			results <- indexedEvidence{
				index: index,
				evidence: fetchAlertCorrelationEvidence(
					jobCtx,
					target,
					job.expectedDeviceSN,
					job.ordinaryKeys,
					w.cfg.Now,
				),
			}
		}()
	}
	evidence := AlertCorrelationEvidence{
		ObservedAt: job.observedAt,
		Signature:  job.signature,
		Sources:    make([]AlertCorrelationSourceEvidence, len(w.targets)),
	}
	for range w.targets {
		result := <-results
		evidence.Sources[result.index] = result.evidence
	}
	w.recordEvidence(jobCtx, evidence)

	summaries := make([]AlertCorrelationSourceSummary, len(evidence.Sources))
	for index, sourceEvidence := range evidence.Sources {
		summaries[index] = AlertCorrelationSourceSummary{
			Source: sourceEvidence.Source,
			Status: sourceEvidence.Status,
		}
	}
	completionCtx, cancelCompletion := context.WithTimeout(parent, w.cfg.NotifyTimeout)
	w.notify(completionCtx, AlertCorrelationEvent{
		Kind:       AlertCorrelationComplete,
		ObservedAt: job.observedAt,
		Signature:  job.signature,
		Sources:    summaries,
	})
	cancelCompletion()
}

func fetchAlertCorrelationEvidence(
	ctx context.Context,
	target alertCorrelationTarget,
	expectedDeviceSN string,
	ordinaryKeys map[string]alertCorrelationMetricIdentity,
	now func() time.Time,
) AlertCorrelationSourceEvidence {
	startedAt := time.Now()
	snapshot, err := target.gate.fetchDiagnostic(ctx, target.source)
	evidence := AlertCorrelationSourceEvidence{
		Source:   target.name,
		Status:   classifyAlertCorrelationStatus(err),
		Duration: time.Since(startedAt),
	}
	if err != nil {
		return evidence
	}
	if snapshot == nil {
		evidence.Status = AlertCorrelationSourceEmpty
		return evidence
	}
	evidence.CollectedAt = snapshot.CollectedAt.UTC()
	if strings.TrimSpace(snapshot.Source) != target.name {
		evidence.Status = AlertCorrelationSourceWrongType
		return evidence
	}
	expectedDeviceSN = strings.TrimSpace(expectedDeviceSN)
	cloudDeviceSN := strings.TrimSpace(snapshot.DeviceSN)
	if expectedDeviceSN == "" || cloudDeviceSN == "" {
		evidence.Status = AlertCorrelationSourceUnverified
		return evidence
	}
	if cloudDeviceSN != expectedDeviceSN {
		evidence.Status = AlertCorrelationSourceMismatch
		return evidence
	}
	collectedAt := snapshot.CollectedAt.UTC()
	if collectedAt.IsZero() {
		evidence.Status = AlertCorrelationSourceBadTime
		return evidence
	}
	validationTime := now().UTC()
	if collectedAt.After(validationTime.Add(maxCorrelationFutureSkew)) {
		evidence.Status = AlertCorrelationSourceBadTime
		return evidence
	}
	if validationTime.Sub(collectedAt) > maxCorrelationSnapshotAge {
		evidence.Status = AlertCorrelationSourceStale
		return evidence
	}
	evidence.Metrics, evidence.TotalMetricCount, evidence.AlertMetricCount, evidence.MetricsSHA256 =
		sanitizedCorrelationMetrics(snapshot.Metrics, ordinaryKeys)
	evidence.Truncated = evidence.TotalMetricCount > len(evidence.Metrics)
	if evidence.TotalMetricCount == 0 {
		evidence.Status = AlertCorrelationSourceNoMetrics
		evidence.MetricsSHA256 = ""
	}
	return evidence
}

func classifyAlertCorrelationStatus(err error) AlertCorrelationSourceStatus {
	switch {
	case err == nil:
		return AlertCorrelationSourceOK
	case errors.Is(err, errDiagnosticPreempted):
		return AlertCorrelationSourcePreempted
	case errors.Is(err, context.DeadlineExceeded):
		return AlertCorrelationSourceTimedOut
	case errors.Is(err, context.Canceled):
		return AlertCorrelationSourceCanceled
	default:
		return AlertCorrelationSourceFailed
	}
}

func sanitizedCorrelationMetrics(metrics []model.Metric, ordinaryKeys map[string]alertCorrelationMetricIdentity) ([]AlertCorrelationMetric, int, int, string) {
	normalized := make([]AlertCorrelationMetric, 0, len(metrics))
	alertCount := 0
	for _, metric := range metrics {
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			continue
		}
		group := strings.TrimSpace(metric.Group)
		key := strings.TrimSpace(metric.Key)
		unit := strings.TrimSpace(metric.Unit)
		if group == "alert" {
			// Unknown upstream fields can be classified as alerts from their
			// names. Never let such attacker-controlled labels cross the
			// structured evidence boundary: only the shared canonical lithium
			// alert surface is safe to identify in logs.
			if unit != "" || !isCanonicalCorrelationAlertKey(key) {
				continue
			}
		} else {
			identity, comparable := ordinaryKeys[key]
			if !comparable {
				continue
			}
			// A cloud value is evidence only for a metric identity already
			// accepted from Modbus. Never log upstream-controlled group/unit
			// labels, even when the cloud reuses an allowlisted key.
			group = identity.Group
			unit = identity.Unit
		}
		if !safeEvidenceLabel(group, 32, false) || !safeEvidenceLabel(key, 64, false) || !safeEvidenceLabel(unit, 16, true) {
			continue
		}
		value := metric.Value
		if value == 0 {
			value = 0 // Canonicalize negative zero before sorting and hashing.
		}
		normalized = append(normalized, AlertCorrelationMetric{
			Group: group,
			Key:   key,
			Unit:  unit,
			Value: value,
		})
		if group == "alert" {
			alertCount++
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		left, right := normalized[i], normalized[j]
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		if left.Key != right.Key {
			return left.Key < right.Key
		}
		if left.Unit != right.Unit {
			return left.Unit < right.Unit
		}
		return math.Float64bits(left.Value) < math.Float64bits(right.Value)
	})
	digest := correlationMetricDigest(normalized)
	total := len(normalized)
	if len(normalized) > maxCorrelationEvidenceMetrics {
		normalized = normalized[:maxCorrelationEvidenceMetrics]
	}
	return normalized, total, alertCount, digest
}

func isCanonicalCorrelationAlertKey(key string) bool {
	switch key {
	case "L_B_A_F", "L_B_F_F", "L_B_A_F2", "L_B_F_F2":
		return true
	default:
		return false
	}
}

func safeEvidenceLabel(value string, maxBytes int, allowEmpty bool) bool {
	if (!allowEmpty && value == "") || len(value) > maxBytes {
		return false
	}
	for _, character := range value {
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_' {
			continue
		}
		if allowEmpty && (character == '%' || character == '/' || character == '.' ||
			character == '-' || character == '\u00b0' || character == '\u2103') {
			continue
		}
		return false
	}
	return true
}

func correlationMetricDigest(metrics []AlertCorrelationMetric) string {
	var canonical bytes.Buffer
	for _, metric := range metrics {
		writeCorrelationDigestString(&canonical, metric.Group)
		writeCorrelationDigestString(&canonical, metric.Key)
		writeCorrelationDigestString(&canonical, metric.Unit)
		var bits [8]byte
		binary.BigEndian.PutUint64(bits[:], math.Float64bits(metric.Value))
		canonical.Write(bits[:])
	}
	sum := sha256.Sum256(canonical.Bytes())
	return hex.EncodeToString(sum[:])
}

func writeCorrelationDigestString(buffer *bytes.Buffer, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	buffer.Write(length[:])
	buffer.WriteString(value)
}

func (w *alertCorrelationWorker) notify(ctx context.Context, event AlertCorrelationEvent) {
	if w.cfg.Notify == nil {
		return
	}
	defer func() {
		// A callback is an integration boundary. Do not let it crash the source
		// maintainer, and never stringify a recovered value that could be secret.
		_ = recover()
	}()
	_ = w.cfg.Notify(ctx, event)
}

func (w *alertCorrelationWorker) recordEvidence(ctx context.Context, evidence AlertCorrelationEvidence) {
	if w.cfg.RecordEvidence == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = w.cfg.RecordEvidence(ctx, evidence)
}

// sourceFetchGate serializes Fetch calls for one concrete source instance.
// Normal polling increments normalWaiters before waiting and cancels an active
// diagnostic. Diagnostics cannot acquire the source while any normal call is
// queued, so failover always has precedence. If a client ignores cancellation,
// the normal call waits rather than issuing an unsafe overlapping request.
type sourceFetchGate struct {
	mu               sync.Mutex
	changed          chan struct{}
	active           bool
	diagnostic       bool
	cancelDiagnostic context.CancelCauseFunc
	normalWaiters    int
}

func newSourceFetchGate() *sourceFetchGate {
	return &sourceFetchGate{changed: make(chan struct{})}
}

func (g *sourceFetchGate) fetchNormal(ctx context.Context, src Source) (*model.Snapshot, error) {
	if ctx == nil {
		return nil, errors.New("normal source fetch requires a context")
	}
	g.mu.Lock()
	g.normalWaiters++
	for g.active {
		if g.diagnostic && g.cancelDiagnostic != nil {
			g.cancelDiagnostic(errDiagnosticPreempted)
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.mu.Lock()
			g.normalWaiters--
			g.signalLocked()
			g.mu.Unlock()
			return nil, ctx.Err()
		case <-changed:
		}
		g.mu.Lock()
	}
	g.normalWaiters--
	g.active = true
	g.diagnostic = false
	g.cancelDiagnostic = nil
	g.mu.Unlock()

	defer g.release()
	return src.Fetch(ctx)
}

func (g *sourceFetchGate) fetchDiagnostic(ctx context.Context, src Source) (*model.Snapshot, error) {
	if ctx == nil {
		return nil, errors.New("diagnostic source fetch requires a context")
	}
	diagnosticCtx, cancel := context.WithCancelCause(withDiagnosticFetch(ctx))
	defer cancel(nil)

	g.mu.Lock()
	for g.active || g.normalWaiters > 0 {
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-diagnosticCtx.Done():
			return nil, context.Cause(diagnosticCtx)
		case <-changed:
		}
		g.mu.Lock()
	}
	g.active = true
	g.diagnostic = true
	g.cancelDiagnostic = cancel
	g.mu.Unlock()

	defer g.release()
	snapshot, err := src.Fetch(diagnosticCtx)
	cause := context.Cause(diagnosticCtx)
	if cause != nil {
		return nil, cause
	}
	return snapshot, err
}

func (g *sourceFetchGate) release() {
	g.mu.Lock()
	g.active = false
	g.diagnostic = false
	g.cancelDiagnostic = nil
	g.signalLocked()
	g.mu.Unlock()
}

func (g *sourceFetchGate) signalLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func sameSourceInstance(first, second Source) bool {
	if isNilInterface(first) || isNilInterface(second) {
		return isNilInterface(first) && isNilInterface(second)
	}
	firstValue := reflect.ValueOf(first)
	secondValue := reflect.ValueOf(second)
	if firstValue.Type() != secondValue.Type() {
		return false
	}
	if firstValue.Type().Comparable() {
		return firstValue.Interface() == secondValue.Interface()
	}
	return firstValue.Kind() == reflect.Pointer && firstValue.Pointer() == secondValue.Pointer()
}
