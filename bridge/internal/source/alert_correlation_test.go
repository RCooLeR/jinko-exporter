package source

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestModbusAlertSignatureFromSnapshotRequiresExactU16Surface(t *testing.T) {
	snapshot := correlationModbusSnapshot(ModbusAlertSignature{
		R553: 0,
		R554: 1,
		R555: 0x7fff,
		R556: 0x8000,
		R557: 0xfffe,
		R558: 0xffff,
	})
	// Order and unrelated telemetry do not affect the keyed signature.
	snapshot.Metrics[0], snapshot.Metrics[5] = snapshot.Metrics[5], snapshot.Metrics[0]
	snapshot.Metrics = append(snapshot.Metrics, model.Metric{Group: "electric", Key: "DP1", Value: 123})

	got, err := ModbusAlertSignatureFromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("ModbusAlertSignatureFromSnapshot() error = %v", err)
	}
	want := ModbusAlertSignature{R553: 0, R554: 1, R555: 0x7fff, R556: 0x8000, R557: 0xfffe, R558: 0xffff}
	if got != want || !got.Active() {
		t.Fatalf("signature = %#v active=%t, want %#v active", got, got.Active(), want)
	}
	if (ModbusAlertSignature{}).Active() {
		t.Fatal("zero signature is active")
	}
}

func TestModbusAlertSignatureFromSnapshotRejectsInvalidSurface(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Snapshot)
	}{
		{name: "nil", mutate: func(snapshot *model.Snapshot) { *snapshot = model.Snapshot{} }},
		{name: "wrong source", mutate: func(snapshot *model.Snapshot) { snapshot.Source = "jinko" }},
		{name: "trimmed but wrong source case", mutate: func(snapshot *model.Snapshot) { snapshot.Source = "Modbus" }},
		{name: "missing", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics = snapshot.Metrics[:5] }},
		{name: "duplicate", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics = append(snapshot.Metrics, snapshot.Metrics[0]) }},
		{name: "wrong group", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Group = "status" }},
		{name: "unexpected unit", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Unit = "W" }},
		{name: "negative", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Value = -1 }},
		{name: "too large", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Value = 65536 }},
		{name: "fractional", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Value = 1.5 }},
		{name: "nan", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Value = math.NaN() }},
		{name: "positive infinity", mutate: func(snapshot *model.Snapshot) { snapshot.Metrics[0].Value = math.Inf(1) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := correlationModbusSnapshot(ModbusAlertSignature{R553: 1})
			test.mutate(snapshot)
			if _, err := ModbusAlertSignatureFromSnapshot(snapshot); !errors.Is(err, ErrInvalidModbusAlertSignature) {
				t.Fatalf("error = %v, want ErrInvalidModbusAlertSignature", err)
			}
		})
	}

	if _, err := ModbusAlertSignatureFromSnapshot(nil); !errors.Is(err, ErrInvalidModbusAlertSignature) {
		t.Fatalf("nil snapshot error = %v, want ErrInvalidModbusAlertSignature", err)
	}
}

func TestConfigureAlertCorrelationValidatesSourcesAndOptions(t *testing.T) {
	validSources := []Source{
		newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{})),
		newCorrelationSource("jinko", correlationCloudSnapshot("jinko")),
		newCorrelationSource("solarman", correlationCloudSnapshot("solarman")),
	}
	validConfig := AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify:        func(context.Context, AlertCorrelationEvent) error { return nil },
	}

	tests := []struct {
		name    string
		sources []Source
		config  AlertCorrelationConfig
	}{
		{name: "missing modbus", sources: validSources[1:], config: validConfig},
		{name: "missing jinko", sources: []Source{validSources[0], validSources[2]}, config: validConfig},
		{name: "missing solarman", sources: validSources[:2], config: validConfig},
		{name: "duplicate jinko", sources: append(append([]Source{}, validSources...), newCorrelationSource("jinko", nil)), config: validConfig},
		{name: "negative cooldown", sources: validSources, config: AlertCorrelationConfig{Cooldown: -1, NotifyTimeout: time.Second, JobTimeout: time.Second, Notify: validConfig.Notify}},
		{name: "zero job timeout", sources: validSources, config: AlertCorrelationConfig{NotifyTimeout: time.Second, Notify: validConfig.Notify}},
		{name: "zero notify timeout", sources: validSources, config: AlertCorrelationConfig{JobTimeout: time.Second, Notify: validConfig.Notify}},
		{name: "no callbacks", sources: validSources, config: AlertCorrelationConfig{NotifyTimeout: time.Second, JobTimeout: time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := NewPriority(test.sources, false).ConfigureAlertCorrelation(test.config); err == nil {
				t.Fatal("ConfigureAlertCorrelation() error = nil")
			}
		})
	}

	priority := NewPriority(validSources, false)
	if err := priority.ConfigureAlertCorrelation(validConfig); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	if err := priority.ConfigureAlertCorrelation(validConfig); err == nil {
		t.Fatal("second ConfigureAlertCorrelation() error = nil")
	}
}

func TestPriorityOneShotFetchDoesNotRunAlertCorrelationWithoutMaintainer(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	jinko := newCorrelationSource("jinko", correlationCloudSnapshot("jinko"))
	solarman := newCorrelationSource("solarman", correlationCloudSnapshot("solarman"))
	var callbacks atomic.Int32
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify: func(context.Context, AlertCorrelationEvent) error {
			callbacks.Add(1)
			return nil
		},
		RecordEvidence: func(context.Context, AlertCorrelationEvidence) error {
			callbacks.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}

	snapshot, err := priority.Fetch(t.Context())
	if err != nil || snapshot.Source != "modbus" {
		t.Fatalf("Fetch() snapshot/error = %#v/%v", snapshot, err)
	}
	time.Sleep(20 * time.Millisecond)
	if modbus.calls.Load() != 1 || jinko.calls.Load() != 0 || solarman.calls.Load() != 0 || callbacks.Load() != 0 {
		t.Fatalf("calls modbus/jinko/solarman/callbacks = %d/%d/%d/%d, want 1/0/0/0",
			modbus.calls.Load(), jinko.calls.Load(), solarman.calls.Load(), callbacks.Load())
	}
}

func TestPriorityRejectedModbusFallbackDoesNotQueueAlertCorrelation(t *testing.T) {
	jinko := newCorrelationSource("jinko", &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "EXPECTED_DEVICE",
		CollectedAt: time.Now().UTC(),
		Metrics: []model.Metric{
			{Group: "inverter", Key: "power", Unit: "W", Value: 10},
		},
	})
	modbusSnapshot := correlationModbusSnapshot(ModbusAlertSignature{R553: 1})
	modbusSnapshot.DeviceSN = "DIFFERENT_DEVICE"
	modbus := newCorrelationSource("modbus", modbusSnapshot)
	solarman := newCorrelationSource("solarman", nil)
	solarman.SetError(errors.New("offline"))
	priority := NewPriority([]Source{jinko, modbus, solarman}, true)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Minute,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify:        func(context.Context, AlertCorrelationEvent) error { return nil },
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}

	// The first configured source establishes Priority's accepted device
	// identity. No Modbus signature is observed during this call.
	fetchPriority(t, priority)
	jinko.SetError(errors.New("offline"))
	if _, err := priority.Fetch(t.Context()); err == nil {
		t.Fatal("Priority.Fetch() error = nil, want mismatched fallback rejection")
	}

	worker := priority.correlationWorker()
	if got := len(worker.jobs); got != 0 {
		t.Fatalf("queued correlation jobs = %d, want 0 for a rejected Modbus fallback", got)
	}
}

func TestAlertCorrelationProcessesPreStartObservationWhenMaintainerStarts(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 9}))
	jinko := newCorrelationSource("jinko", correlationCloudSnapshot("jinko"))
	solarman := newCorrelationSource("solarman", correlationCloudSnapshot("solarman"))
	events := make(chan AlertCorrelationEvent, 4)
	evidenceCh := make(chan AlertCorrelationEvidence, 1)
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify: func(_ context.Context, event AlertCorrelationEvent) error {
			events <- event
			return nil
		},
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}

	// Normal startup can complete its first poll before the maintenance goroutine
	// is scheduled. The observation may be queued, but cannot call user code or
	// either cloud source until RunBackground starts.
	fetchPriority(t, priority)
	time.Sleep(20 * time.Millisecond)
	if jinko.calls.Load() != 0 || solarman.calls.Load() != 0 || len(events) != 0 || len(evidenceCh) != 0 {
		t.Fatalf("pre-start calls/events/evidence = %d/%d/%d/%d, want all zero",
			jinko.calls.Load(), solarman.calls.Load(), len(events), len(evidenceCh))
	}

	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)
	if event := receiveWithin(t, events); event.Kind != AlertCorrelationDetected || event.Signature.R553 != 9 {
		t.Fatalf("first event = %#v, want detected R553=9", event)
	}
	if evidence := receiveWithin(t, evidenceCh); evidence.Signature.R553 != 9 {
		t.Fatalf("evidence signature = %#v, want R553=9", evidence.Signature)
	}
	if event := receiveWithin(t, events); event.Kind != AlertCorrelationComplete {
		t.Fatalf("second event = %#v, want completion", event)
	}
	if jinko.calls.Load() != 1 || solarman.calls.Load() != 1 {
		t.Fatalf("post-start cloud calls = %d/%d, want 1/1", jinko.calls.Load(), solarman.calls.Load())
	}
}

func TestAlertCorrelationNotifiesDetectedBeforeStartingCloudFetches(t *testing.T) {
	notified := make(chan struct{})
	cloudObservedOrder := make(chan bool, 2)
	cloud := func(name string) *correlationSource {
		src := newCorrelationSource(name, correlationCloudSnapshot(name))
		src.fetch = func(context.Context, int32) (*model.Snapshot, error) {
			select {
			case <-notified:
				cloudObservedOrder <- true
			default:
				cloudObservedOrder <- false
			}
			return correlationCloudSnapshot(name), nil
		}
		return src
	}
	priority := NewPriority([]Source{
		newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1})),
		cloud("jinko"),
		cloud("solarman"),
	}, false)
	var notifiedOnce sync.Once
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify: func(_ context.Context, event AlertCorrelationEvent) error {
			if event.Kind == AlertCorrelationDetected {
				notifiedOnce.Do(func() { close(notified) })
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)
	fetchPriority(t, priority)
	if first, second := receiveWithin(t, cloudObservedOrder), receiveWithin(t, cloudObservedOrder); !first || !second {
		t.Fatalf("cloud observed Detected notification = %t/%t, want true/true", first, second)
	}
}

func TestAlertCorrelationNotifyTimeoutLeavesFreshBudgetForBothClouds(t *testing.T) {
	notifyTimedOut := make(chan struct{})
	attempted := make(chan string, 2)
	cloud := func(name string) *correlationSource {
		src := newCorrelationSource(name, correlationCloudSnapshot(name))
		src.fetch = func(context.Context, int32) (*model.Snapshot, error) {
			select {
			case <-notifyTimedOut:
			case <-time.After(time.Second):
				return nil, errors.New("cloud started before notification phase completed")
			}
			attempted <- name
			return correlationCloudSnapshot(name), nil
		}
		return src
	}
	evidenceCh := make(chan AlertCorrelationEvidence, 1)
	priority := NewPriority([]Source{
		newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1})),
		cloud("jinko"),
		cloud("solarman"),
	}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: 20 * time.Millisecond,
		JobTimeout:    100 * time.Millisecond,
		Notify: func(ctx context.Context, event AlertCorrelationEvent) error {
			if event.Kind != AlertCorrelationDetected {
				return nil
			}
			<-ctx.Done()
			close(notifyTimedOut)
			return ctx.Err()
		},
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	fetchPriority(t, priority)
	first, second := receiveWithin(t, attempted), receiveWithin(t, attempted)
	if (first != "jinko" && second != "jinko") || (first != "solarman" && second != "solarman") {
		t.Fatalf("cloud attempts = %q/%q, want Jinko and Solarman", first, second)
	}
	evidence := receiveWithin(t, evidenceCh)
	if len(evidence.Sources) != 2 || evidence.Sources[0].Status != AlertCorrelationSourceOK || evidence.Sources[1].Status != AlertCorrelationSourceOK {
		t.Fatalf("cloud evidence after notify timeout = %#v", evidence.Sources)
	}
	cancel()
	receiveWithin(t, done)
}

func TestAlertCorrelationFetchesBothCloudsAndEmitsSanitizedEvidence(t *testing.T) {
	modbusSnapshot := correlationModbusSnapshot(ModbusAlertSignature{R554: 0x8000, R558: 7})
	modbusSnapshot.Metrics = append(modbusSnapshot.Metrics, model.Metric{Group: "electric", Key: "DP1", Unit: "W", Value: 100})
	modbus := newCorrelationSource("modbus", modbusSnapshot)
	jinkoSnapshot := correlationCloudSnapshot("jinko")
	jinkoSnapshot.CollectedAt = time.Now().UTC()
	jinkoSnapshot.Meta = map[string]string{"token": "MUST_NOT_CROSS_BOUNDARY"}
	jinkoSnapshot.Metrics = append(jinkoSnapshot.Metrics,
		model.Metric{Group: "alert", Key: "unsafe-key", Value: 9},
		model.Metric{Group: "alert", Key: "device_123456789", Value: 17},
		model.Metric{Group: "alert", Key: "NOT_FINITE", Value: math.NaN()},
		model.Metric{Group: "device_123456789", Key: "DP1", Unit: "id_123456789", Value: 88},
		model.Metric{Group: "electric", Key: "CLOUD_ONLY_SENTINEL", Value: 123456789},
		model.Metric{Group: "alert", Key: "L_B_F_F", Value: 99},
	)
	jinko := newCorrelationSource("jinko", jinkoSnapshot)
	solarman := newCorrelationSource("solarman", correlationCloudSnapshot("solarman"))
	events := make(chan AlertCorrelationEvent, 4)
	evidenceCh := make(chan AlertCorrelationEvidence, 1)
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify: func(_ context.Context, event AlertCorrelationEvent) error {
			events <- event
			return nil
		},
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)

	if _, err := priority.Fetch(t.Context()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	evidence := receiveWithin(t, evidenceCh)
	if evidence.Signature.R554 != 0x8000 || evidence.Signature.R558 != 7 || len(evidence.Sources) != 2 {
		t.Fatalf("evidence = %#v", evidence)
	}
	if got := evidence.Sources[0]; got.Source != "jinko" || got.Status != AlertCorrelationSourceOK ||
		len(got.Metrics) != 3 || got.TotalMetricCount != 3 || got.AlertMetricCount != 2 || got.Truncated || len(got.MetricsSHA256) != 64 ||
		got.Metrics[0].Group != "alert" || got.Metrics[0].Key != "L_B_F_F" || got.Metrics[0].Value != 1 ||
		got.Metrics[1].Key != "L_B_F_F" || got.Metrics[1].Value != 99 ||
		got.Metrics[2].Group != "electric" || got.Metrics[2].Key != "DP1" || got.Metrics[2].Unit != "W" || got.Metrics[2].Value != 88 {
		t.Fatalf("sanitized Jinko evidence = %#v", got)
	}
	if got := evidence.Sources[1]; got.Source != "solarman" || got.Status != AlertCorrelationSourceOK || len(got.Metrics) != 1 || got.Metrics[0].Key != "L_B_F_F" {
		t.Fatalf("sanitized Solarman evidence = %#v", got)
	}
	firstEvent := receiveWithin(t, events)
	secondEvent := receiveWithin(t, events)
	if firstEvent.Kind != AlertCorrelationDetected || secondEvent.Kind != AlertCorrelationComplete {
		t.Fatalf("event kinds = %q, %q", firstEvent.Kind, secondEvent.Kind)
	}
	if len(secondEvent.Sources) != 2 || secondEvent.Sources[0].Source != "jinko" || secondEvent.Sources[1].Source != "solarman" {
		t.Fatalf("completion summaries = %#v", secondEvent.Sources)
	}
}

func TestAlertCorrelationEvidenceRejectsWrongIdentityAndStaleSnapshots(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	tests := []struct {
		name       string
		expectedSN string
		mutate     func(*model.Snapshot)
		want       AlertCorrelationSourceStatus
	}{
		{name: "missing expected identity", expectedSN: "", want: AlertCorrelationSourceUnverified},
		{name: "missing cloud identity", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.DeviceSN = "" }, want: AlertCorrelationSourceUnverified},
		{name: "different device", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.DeviceSN = "OTHER_DEVICE" }, want: AlertCorrelationSourceMismatch},
		{name: "wrong snapshot source", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.Source = "solarman" }, want: AlertCorrelationSourceWrongType},
		{name: "missing collection time", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.CollectedAt = time.Time{} }, want: AlertCorrelationSourceBadTime},
		{name: "stale collection time", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) {
			snapshot.CollectedAt = now.Add(-maxCorrelationSnapshotAge - time.Second)
		}, want: AlertCorrelationSourceStale},
		{name: "future collection time", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.CollectedAt = now.Add(maxCorrelationFutureSkew + time.Second) }, want: AlertCorrelationSourceBadTime},
		{name: "no safe metrics", expectedSN: "SYNTHETIC_DEVICE", mutate: func(snapshot *model.Snapshot) { snapshot.CollectedAt = now; snapshot.Metrics[0].Group = "bad group" }, want: AlertCorrelationSourceNoMetrics},
		{name: "trimmed matching identity", expectedSN: " SYNTHETIC_DEVICE ", mutate: func(snapshot *model.Snapshot) { snapshot.DeviceSN = " SYNTHETIC_DEVICE "; snapshot.CollectedAt = now }, want: AlertCorrelationSourceOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := correlationCloudSnapshot("jinko")
			snapshot.CollectedAt = now
			if test.mutate != nil {
				test.mutate(snapshot)
			}
			src := newCorrelationSource("jinko", snapshot)
			evidence := fetchAlertCorrelationEvidence(t.Context(), alertCorrelationTarget{
				name:   "jinko",
				source: src,
				gate:   newSourceFetchGate(),
			}, test.expectedSN, nil, func() time.Time { return now })
			if evidence.Status != test.want {
				t.Fatalf("status = %q, want %q; evidence=%#v", evidence.Status, test.want, evidence)
			}
			if test.want != AlertCorrelationSourceOK && (len(evidence.Metrics) != 0 || evidence.TotalMetricCount != 0 || evidence.MetricsSHA256 != "") {
				t.Fatalf("rejected snapshot leaked correlated metrics: %#v", evidence)
			}
			if test.want == AlertCorrelationSourceOK && (len(evidence.Metrics) != 1 || evidence.TotalMetricCount != 1 || len(evidence.MetricsSHA256) != 64) {
				t.Fatalf("accepted evidence = %#v", evidence)
			}
		})
	}
}

func TestSanitizedCorrelationMetricsAreBoundedSortedAndHashFullSet(t *testing.T) {
	metrics := make([]model.Metric, 0, 303)
	for index := range 300 {
		metrics = append(metrics, model.Metric{
			Group: "electric",
			Key:   fmt.Sprintf("K%03d", index),
			Unit:  "W",
			Value: float64(index),
		})
	}
	metrics = append(metrics,
		model.Metric{Group: "alert", Key: "L_B_F_F", Unit: "", Value: 1},
		model.Metric{Group: "bad group", Key: "REJECTED", Value: 1},
		model.Metric{Group: "electric", Key: "NOT_FINITE", Value: math.Inf(1)},
	)
	reversed := append([]model.Metric(nil), metrics...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}

	ordinaryKeys := cloneOrdinaryMetricKeys(metrics)
	first, firstTotal, firstAlerts, firstDigest := sanitizedCorrelationMetrics(metrics, ordinaryKeys)
	second, secondTotal, secondAlerts, secondDigest := sanitizedCorrelationMetrics(reversed, ordinaryKeys)
	if firstTotal != 301 || firstAlerts != 1 || len(first) != maxCorrelationEvidenceMetrics {
		t.Fatalf("first count/alerts/bounded = %d/%d/%d, want 301/1/%d", firstTotal, firstAlerts, len(first), maxCorrelationEvidenceMetrics)
	}
	if secondTotal != firstTotal || secondAlerts != firstAlerts || secondDigest != firstDigest || !reflect.DeepEqual(first, second) {
		t.Fatalf("permuted normalization differs: digest %q/%q", firstDigest, secondDigest)
	}
	if len(firstDigest) != 64 || first[0].Group != "alert" || first[0].Key != "L_B_F_F" || first[1].Key != "K000" {
		t.Fatalf("sorted metrics/digest = %#v/%q", first[:2], firstDigest)
	}

	// K299 is beyond the bounded slice, but changing it must still change the
	// digest of the complete normalized evidence set.
	metrics[299].Value++
	changed, changedTotal, _, changedDigest := sanitizedCorrelationMetrics(metrics, ordinaryKeys)
	if changedTotal != firstTotal || reflect.DeepEqual(changed, first) == false || changedDigest == firstDigest {
		t.Fatalf("uncapped mutation: bounded_equal=%t totals=%d/%d digest_equal=%t",
			reflect.DeepEqual(changed, first), changedTotal, firstTotal, changedDigest == firstDigest)
	}
}

func TestAlertCorrelationCooldownChangeAndZeroRearm(t *testing.T) {
	clock := newCorrelationClock(time.Unix(1000, 0))
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	jinkoSnapshot := correlationCloudSnapshot("jinko")
	jinkoSnapshot.CollectedAt = clock.Now()
	solarmanSnapshot := correlationCloudSnapshot("solarman")
	solarmanSnapshot.CollectedAt = clock.Now()
	jinko := newCorrelationSource("jinko", jinkoSnapshot)
	solarman := newCorrelationSource("solarman", solarmanSnapshot)
	evidenceCh := make(chan AlertCorrelationEvidence, 8)
	events := make(chan AlertCorrelationEvent, 16)
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Now:           clock.Now,
		Notify: func(_ context.Context, event AlertCorrelationEvent) error {
			events <- event
			return nil
		},
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)

	fetchPriority(t, priority)
	if got := receiveWithin(t, evidenceCh).Signature; got.R553 != 1 {
		t.Fatalf("first signature = %#v", got)
	}
	fetchPriority(t, priority)
	assertNoEvidence(t, evidenceCh)

	// A backward wall-clock step must not be interpreted as cooldown expiry.
	clock.Set(time.Unix(900, 0))
	fetchPriority(t, priority)
	assertNoEvidence(t, evidenceCh)
	clock.Set(time.Unix(1000, 0).Add(time.Hour))
	fetchPriority(t, priority)
	if got := receiveWithin(t, evidenceCh).Signature; got.R553 != 1 {
		t.Fatalf("cooldown signature = %#v", got)
	}

	clock.Advance(time.Second)
	modbus.SetSnapshot(correlationModbusSnapshot(ModbusAlertSignature{R553: 1, R556: 2}))
	fetchPriority(t, priority)
	if got := receiveWithin(t, evidenceCh).Signature; got.R556 != 2 {
		t.Fatalf("changed signature = %#v", got)
	}

	modbus.SetSnapshot(correlationModbusSnapshot(ModbusAlertSignature{}))
	fetchPriority(t, priority)
	for {
		event := receiveWithin(t, events)
		if event.Kind == AlertCorrelationRecovered {
			break
		}
	}
	assertNoEvidence(t, evidenceCh)

	// The all-zero observation rearms the same signature immediately, even
	// though its previous trigger remains inside the configured cooldown.
	modbus.SetSnapshot(correlationModbusSnapshot(ModbusAlertSignature{R553: 1, R556: 2}))
	fetchPriority(t, priority)
	if got := receiveWithin(t, evidenceCh).Signature; got.R553 != 1 || got.R556 != 2 {
		t.Fatalf("rearmed signature = %#v", got)
	}
	if jinko.calls.Load() != 4 || solarman.calls.Load() != 4 {
		t.Fatalf("cloud calls jinko/solarman = %d/%d, want 4/4", jinko.calls.Load(), solarman.calls.Load())
	}
}

func TestAlertCorrelationPendingQueueIsBoundedLatestWins(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	blockingCloud := func(name string) *correlationSource {
		source := newCorrelationSource(name, correlationCloudSnapshot(name))
		source.fetch = func(ctx context.Context, call int32) (*model.Snapshot, error) {
			if call == 1 {
				started <- struct{}{}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
				}
			}
			return correlationCloudSnapshot(name), nil
		}
		return source
	}
	jinko := blockingCloud("jinko")
	solarman := blockingCloud("solarman")
	evidenceCh := make(chan AlertCorrelationEvidence, 4)
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    5 * time.Second,
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)

	fetchPriority(t, priority)
	receiveWithin(t, started)
	receiveWithin(t, started)
	modbus.SetSnapshot(correlationModbusSnapshot(ModbusAlertSignature{R553: 2}))
	fetchPriority(t, priority)
	modbus.SetSnapshot(correlationModbusSnapshot(ModbusAlertSignature{R553: 3}))
	fetchPriority(t, priority)
	close(release)

	first := receiveWithin(t, evidenceCh)
	latest := receiveWithin(t, evidenceCh)
	if first.Signature.R553 != 1 || latest.Signature.R553 != 3 {
		t.Fatalf("processed signatures = %d then %d, want 1 then latest 3", first.Signature.R553, latest.Signature.R553)
	}
	assertNoEvidence(t, evidenceCh)
	if jinko.calls.Load() != 2 || solarman.calls.Load() != 2 {
		t.Fatalf("cloud calls jinko/solarman = %d/%d, want 2/2", jinko.calls.Load(), solarman.calls.Load())
	}
}

func TestNormalFailoverPreemptsDiagnosticFetchWithoutOverlap(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	diagnosticStarted := make(chan struct{})
	jinko := newCorrelationSource("jinko", correlationCloudSnapshot("jinko"))
	var active atomic.Int32
	var maxActive atomic.Int32
	jinko.fetch = func(ctx context.Context, call int32) (*model.Snapshot, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		if call == 1 {
			close(diagnosticStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return correlationCloudSnapshot("jinko"), nil
	}
	solarman := newCorrelationSource("solarman", correlationCloudSnapshot("solarman"))
	evidenceCh := make(chan AlertCorrelationEvidence, 1)
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    5 * time.Second,
		RecordEvidence: func(_ context.Context, evidence AlertCorrelationEvidence) error {
			evidenceCh <- evidence
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)

	fetchPriority(t, priority)
	receiveWithin(t, diagnosticStarted)
	modbus.SetError(errors.New("modbus unavailable"))
	snapshot, err := priority.Fetch(t.Context())
	if err != nil || snapshot == nil || snapshot.Source != "jinko" {
		t.Fatalf("normal failover snapshot/error = %#v/%v", snapshot, err)
	}
	evidence := receiveWithin(t, evidenceCh)
	if evidence.Sources[0].Source != "jinko" || evidence.Sources[0].Status != AlertCorrelationSourcePreempted {
		t.Fatalf("Jinko diagnostic evidence = %#v, want preempted", evidence.Sources[0])
	}
	if maxActive.Load() != 1 || jinko.calls.Load() != 2 {
		t.Fatalf("Jinko max concurrent/calls = %d/%d, want 1/2", maxActive.Load(), jinko.calls.Load())
	}
}

func TestDiagnosticFetchContextMarkerDoesNotLeakToNormalFetch(t *testing.T) {
	parent := t.Context()
	seen := make(chan bool, 2)
	src := newCorrelationSource("jinko", correlationCloudSnapshot("jinko"))
	src.fetch = func(ctx context.Context, _ int32) (*model.Snapshot, error) {
		seen <- IsDiagnosticFetch(ctx)
		return correlationCloudSnapshot("jinko"), nil
	}
	gate := newSourceFetchGate()
	if _, err := gate.fetchDiagnostic(parent, src); err != nil {
		t.Fatalf("diagnostic fetch error = %v", err)
	}
	if _, err := gate.fetchNormal(parent, src); err != nil {
		t.Fatalf("normal fetch error = %v", err)
	}
	if first, second := receiveWithin(t, seen), receiveWithin(t, seen); !first || second {
		t.Fatalf("diagnostic/normal markers = %t/%t, want true/false", first, second)
	}
	if IsDiagnosticFetch(parent) {
		t.Fatal("diagnostic marker leaked into parent context")
	}
}

func TestAlertCorrelationCancellationJoinsInFlightCloudFetches(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	started := make(chan struct{}, 2)
	waitingCloud := func(name string) *correlationSource {
		source := newCorrelationSource(name, nil)
		source.fetch = func(ctx context.Context, _ int32) (*model.Snapshot, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return source
	}
	priority := NewPriority([]Source{modbus, waitingCloud("jinko"), waitingCloud("solarman")}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      time.Hour,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Hour,
		Notify:        func(context.Context, AlertCorrelationEvent) error { return nil },
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	fetchPriority(t, priority)
	receiveWithin(t, started)
	receiveWithin(t, started)
	cancel()
	receiveWithin(t, done)
}

func TestAlertCorrelationCallbackPanicDoesNotStopWorker(t *testing.T) {
	modbus := newCorrelationSource("modbus", correlationModbusSnapshot(ModbusAlertSignature{R553: 1}))
	evidenceCalls := make(chan struct{}, 2)
	priority := NewPriority([]Source{
		modbus,
		newCorrelationSource("jinko", correlationCloudSnapshot("jinko")),
		newCorrelationSource("solarman", correlationCloudSnapshot("solarman")),
	}, false)
	if err := priority.ConfigureAlertCorrelation(AlertCorrelationConfig{
		Cooldown:      0,
		NotifyTimeout: time.Second,
		JobTimeout:    time.Second,
		Notify: func(context.Context, AlertCorrelationEvent) error {
			panic("synthetic callback panic")
		},
		RecordEvidence: func(context.Context, AlertCorrelationEvidence) error {
			evidenceCalls <- struct{}{}
			return nil
		},
	}); err != nil {
		t.Fatalf("ConfigureAlertCorrelation() error = %v", err)
	}
	cancel, done := startCorrelationPriority(t, priority)
	defer stopCorrelationPriority(t, cancel, done)

	fetchPriority(t, priority)
	receiveWithin(t, evidenceCalls)
	fetchPriority(t, priority)
	receiveWithin(t, evidenceCalls)
}

type correlationSource struct {
	name     string
	calls    atomic.Int32
	mu       sync.RWMutex
	snapshot *model.Snapshot
	err      error
	fetch    func(context.Context, int32) (*model.Snapshot, error)
}

func newCorrelationSource(name string, snapshot *model.Snapshot) *correlationSource {
	return &correlationSource{name: name, snapshot: snapshot}
}

func (s *correlationSource) Name() string { return s.name }

func (s *correlationSource) Fetch(ctx context.Context) (*model.Snapshot, error) {
	call := s.calls.Add(1)
	if s.fetch != nil {
		return s.fetch(ctx, call)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot, s.err
}

func (s *correlationSource) SetSnapshot(snapshot *model.Snapshot) {
	s.mu.Lock()
	s.snapshot = snapshot
	s.err = nil
	s.mu.Unlock()
}

func (s *correlationSource) SetError(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

type correlationClock struct {
	mu  sync.Mutex
	now time.Time
}

func newCorrelationClock(now time.Time) *correlationClock { return &correlationClock{now: now} }

func (c *correlationClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *correlationClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func (c *correlationClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

func correlationModbusSnapshot(signature ModbusAlertSignature) *model.Snapshot {
	values := signature.values()
	metrics := make([]model.Metric, len(modbusAlertMetricKeys))
	for index, key := range modbusAlertMetricKeys {
		metrics[index] = model.Metric{Group: "alert", Key: key, Value: float64(values[index])}
	}
	return &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "SYNTHETIC_DEVICE",
		CollectedAt: time.Unix(100, 0).UTC(),
		Metrics:     metrics,
	}
}

func correlationCloudSnapshot(name string) *model.Snapshot {
	return &model.Snapshot{
		Source:      name,
		DeviceSN:    "SYNTHETIC_DEVICE",
		CollectedAt: time.Now().UTC(),
		Metrics: []model.Metric{
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium fault flag", Value: 1},
		},
	}
}

func startCorrelationPriority(t *testing.T, priority *Priority) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		priority.RunBackground(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		worker := priority.correlationWorker()
		worker.mu.Lock()
		running := worker.running
		worker.mu.Unlock()
		if running {
			return cancel, done
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("timed out waiting for correlation maintainer startup")
		}
		time.Sleep(time.Millisecond)
	}
}

func stopCorrelationPriority(t *testing.T, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()
	cancel()
	receiveWithin(t, done)
}

func fetchPriority(t *testing.T, priority *Priority) {
	t.Helper()
	if _, err := priority.Fetch(t.Context()); err != nil {
		t.Fatalf("Priority.Fetch() error = %v", err)
	}
}

func receiveWithin[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for asynchronous result")
		return zero
	}
}

func assertNoEvidence(t *testing.T, evidence <-chan AlertCorrelationEvidence) {
	t.Helper()
	select {
	case got := <-evidence:
		t.Fatalf("unexpected evidence = %#v", got)
	case <-time.After(25 * time.Millisecond):
	}
}
