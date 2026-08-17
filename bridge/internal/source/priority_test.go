package source

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestPriorityFirstSuccessWins(t *testing.T) {
	primary := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "PRIMARY")}
	fallback := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "FALLBACK")}
	priority := NewPriority([]Source{primary, fallback}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "jinko" || primary.calls != 1 || fallback.calls != 0 {
		t.Fatalf("snapshot source=%q primary calls=%d fallback calls=%d", snapshot.Source, primary.calls, fallback.calls)
	}
}

func TestPriorityFallsBackAfterFailure(t *testing.T) {
	primary := &stubSource{name: "jinko", err: errors.New("cloud down")}
	fallback := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "FALLBACK")}
	priority := NewPriority([]Source{primary, fallback}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "solarman" || primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("snapshot source=%q primary calls=%d fallback calls=%d", snapshot.Source, primary.calls, fallback.calls)
	}
}

func TestPriorityUsesThreeSourcesInConfiguredOrder(t *testing.T) {
	var order []string
	record := func(name string) func() {
		return func() { order = append(order, name) }
	}
	modbus := &stubSource{name: "modbus", err: errors.New("modbus unavailable"), onFetch: record("modbus")}
	jinko := &stubSource{name: "jinko", err: errors.New("jinko unavailable"), onFetch: record("jinko")}
	solarman := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "FALLBACK"), onFetch: record("solarman")}
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "solarman" || modbus.calls != 1 || jinko.calls != 1 || solarman.calls != 1 {
		t.Fatalf("source=%q calls modbus/jinko/solarman=%d/%d/%d", snapshot.Source, modbus.calls, jinko.calls, solarman.calls)
	}
	if got := strings.Join(order, ","); got != "modbus,jinko,solarman" {
		t.Fatalf("fetch order = %q, want modbus,jinko,solarman", got)
	}

	order = nil
	modbus.err = nil
	modbus.snapshot = testSourceSnapshot("modbus", "PRIMARY")
	if snapshot, err = priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	if snapshot.Source != "modbus" || modbus.calls != 2 || jinko.calls != 1 || solarman.calls != 1 {
		t.Fatalf("source=%q calls modbus/jinko/solarman=%d/%d/%d after primary recovery", snapshot.Source, modbus.calls, jinko.calls, solarman.calls)
	}
	if got := strings.Join(order, ","); got != "modbus" {
		t.Fatalf("fetch order after recovery = %q, want modbus only", got)
	}
}

func TestPriorityFallsBackFromNilSnapshotWithoutError(t *testing.T) {
	invalid := &stubSource{name: "modbus"}
	fallback := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "FALLBACK")}
	priority := NewPriority([]Source{invalid, fallback}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot == nil || snapshot.Source != "jinko" || invalid.calls != 1 || fallback.calls != 1 {
		t.Fatalf("snapshot=%#v calls invalid/fallback=%d/%d", snapshot, invalid.calls, fallback.calls)
	}
}

func TestPriorityRunsBackgroundMaintainersConcurrentlyAndOnce(t *testing.T) {
	first := newBackgroundStub("jinko")
	second := newBackgroundStub("solarman")
	priority := NewPriority([]Source{first, first, second}, false)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		priority.RunBackground(ctx)
		close(done)
	}()

	waitStarted(t, first.started)
	waitStarted(t, second.started)
	if first.backgroundCalls.Load() != 1 || second.backgroundCalls.Load() != 1 {
		t.Fatalf("background calls first/second=%d/%d, want 1/1", first.backgroundCalls.Load(), second.backgroundCalls.Load())
	}
	cancel()
	waitStarted(t, done)
}

func TestPriorityHealthyModbusSkipsCloudFetchWhileJinkoMaintenanceRuns(t *testing.T) {
	modbus := &stubSource{name: "modbus", snapshot: testSourceSnapshot("modbus", "SYNTHETIC_INV_001")}
	jinko := newBackgroundStub("jinko")
	solarman := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "SYNTHETIC_INV_001")}
	priority := NewPriority([]Source{modbus, jinko, solarman}, false)

	ctx, cancel := context.WithCancel(context.Background())
	backgroundDone := make(chan struct{})
	go func() {
		priority.RunBackground(ctx)
		close(backgroundDone)
	}()
	waitStarted(t, jinko.started)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot.Source != "modbus" {
		t.Fatalf("snapshot source = %q, want modbus", snapshot.Source)
	}
	if modbus.calls != 1 || jinko.fetchCalls.Load() != 0 || solarman.calls != 0 {
		t.Fatalf("fetch calls modbus/jinko/solarman = %d/%d/%d, want 1/0/0", modbus.calls, jinko.fetchCalls.Load(), solarman.calls)
	}
	if jinko.backgroundCalls.Load() != 1 {
		t.Fatalf("Jinko background calls = %d, want 1", jinko.backgroundCalls.Load())
	}

	cancel()
	waitStarted(t, backgroundDone)
}

func TestPriorityAllFail(t *testing.T) {
	priority := NewPriority([]Source{
		&stubSource{name: "jinko", err: errors.New("cloud down")},
		&stubSource{name: "solarman", err: errors.New("quota down")},
	}, false)

	_, err := priority.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "all priority sources failed") {
		t.Fatalf("Fetch() error = %v, want all-failed error", err)
	}
	if got := err.Error(); !strings.Contains(got, "jinko: cloud down") || !strings.Contains(got, "solarman: quota down") || strings.Index(got, "jinko: cloud down") > strings.Index(got, "solarman: quota down") {
		t.Fatalf("Fetch() error = %q, want both failures in configured order", got)
	}
}

func TestPriorityCancellationStopsFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	primary := &stubSource{
		name: "modbus",
		err:  errors.New("read canceled"),
		onFetch: func() {
			cancel()
		},
	}
	fallback := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "FALLBACK")}
	priority := NewPriority([]Source{primary, fallback}, false)

	snapshot, err := priority.Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
	if snapshot != nil || primary.calls != 1 || fallback.calls != 0 {
		t.Fatalf("snapshot=%#v calls primary/fallback=%d/%d, want nil and 1/0", snapshot, primary.calls, fallback.calls)
	}
}

func TestPriorityWithoutProjectionKeepsFullFallbackSnapshot(t *testing.T) {
	primary := &stubSource{name: "modbus", err: errors.New("logger unavailable")}
	fallbackSnapshot := &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "SYNTHETIC_INV_001",
		CollectedAt: time.Unix(20, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "PV 1", Unit: "W", Value: 200},
			{Group: "status", Key: "CLOUD_ONLY", Name: "Cloud only", Unit: "", Value: 1},
		},
	}
	priority := NewPriority([]Source{primary, &stubSource{name: "jinko", snapshot: fallbackSnapshot}}, false)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snapshot != fallbackSnapshot || len(snapshot.Metrics) != 2 || snapshot.Metrics[1].Key != "CLOUD_ONLY" {
		t.Fatalf("fallback snapshot = %#v, want complete unprojected snapshot", snapshot)
	}
}

func TestPriorityProjectsFallbackMetricsToPrimarySurface(t *testing.T) {
	canonicalSurface := []model.Metric{
		{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 100},
		{Group: "electric", Key: "DP2", Name: "DC Power PV2", Unit: "W", Value: 101},
		{Group: "electric", Key: "DV1", Name: "DC Voltage PV1", Unit: "V", Value: 102},
		{Group: "electric", Key: "DC1", Name: "DC Current PV1", Unit: "A", Value: 103},
		{Group: "electric", Key: "DV2", Name: "DC Voltage PV2", Unit: "V", Value: 104},
		{Group: "electric", Key: "DC2", Name: "DC Current PV2", Unit: "A", Value: 105},
		{Group: "electric", Key: "INV_O_P_L1", Name: "Inverter Output Power L1", Unit: "W", Value: 106},
		{Group: "electric", Key: "INV_O_P_L2", Name: "Inverter Output Power L2", Unit: "W", Value: 107},
		{Group: "electric", Key: "INV_O_P_L3", Name: "Inverter Output Power L3", Unit: "W", Value: 108},
		{Group: "electric", Key: "INV_O_P_T", Name: "Total Inverter Output Power", Unit: "W", Value: 109},
	}
	primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
		Source:      "modbus",
		DeviceSN:    "PRIMARY_SN",
		CollectedAt: time.Unix(10, 0).UTC(),
		Metrics:     canonicalSurface,
	}}
	fallbackMetrics := make([]model.Metric, 0, len(canonicalSurface)+1)
	for _, metric := range canonicalSurface {
		fallbackMetrics = append(fallbackMetrics, model.Metric{
			Group: "other",
			Key:   metric.Key,
			Name:  "Fallback " + metric.Key,
			Unit:  "source-unit",
			Value: metric.Value + 100,
		})
	}
	fallbackMetrics = append(fallbackMetrics, model.Metric{Group: "other", Key: "EXTRA", Name: "Extra", Unit: "W", Value: 300})
	fallback := &stubSource{name: "jinko", snapshot: &model.Snapshot{
		Source:      "jinko",
		DeviceSN:    "PRIMARY_SN",
		CollectedAt: time.Unix(20, 0).UTC(),
		Metrics:     fallbackMetrics,
	}}
	priority := NewPriority([]Source{primary, fallback}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("output active power is outside the safe signed domain")

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	if snapshot.DeviceSN != "PRIMARY_SN" {
		t.Fatalf("DeviceSN = %q, want PRIMARY_SN", snapshot.DeviceSN)
	}
	if len(snapshot.Metrics) != len(canonicalSurface) {
		t.Fatalf("metric count = %d, want %d", len(snapshot.Metrics), len(canonicalSurface))
	}
	for index, metric := range snapshot.Metrics {
		want := canonicalSurface[index]
		if metric.Group != want.Group || metric.Key != want.Key || metric.Name != want.Name || metric.Unit != want.Unit || metric.Value != want.Value+100 {
			t.Fatalf("projected metric[%d] = %#v, want canonical labels %#v and value %v", index, metric, want, want.Value+100)
		}
	}
}

func TestPrioritySkipsFallbackForDifferentLearnedDeviceAndContinues(t *testing.T) {
	primary := &stubSource{name: "modbus", snapshot: testSourceSnapshot("modbus", "INVERTER_A")}
	differentDevice := &stubSource{name: "jinko", snapshot: testSourceSnapshot("jinko", "INVERTER_B")}
	matchingTertiary := &stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", "INVERTER_A")}
	priority := NewPriority([]Source{primary, differentDevice, matchingTertiary}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("logger unavailable")

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	if snapshot.Source != "solarman" || snapshot.DeviceSN != "INVERTER_A" {
		t.Fatalf("selected snapshot source/device = %q/%q, want matching tertiary solarman/INVERTER_A", snapshot.Source, snapshot.DeviceSN)
	}
	if primary.calls != 2 || differentDevice.calls != 1 || matchingTertiary.calls != 1 {
		t.Fatalf("calls primary/different/tertiary = %d/%d/%d, want 2/1/1", primary.calls, differentDevice.calls, matchingTertiary.calls)
	}
}

func TestPriorityProjectionPreservesFallbackSourceAlertMetrics(t *testing.T) {
	primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
		Source:   "modbus",
		DeviceSN: "SYNTHETIC_INV_001",
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "PV Power", Unit: "W", Value: 100},
			{Group: "alert", Key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", Name: "Deye Warning Word", Value: 0},
		},
	}}
	fallback := &stubSource{name: "jinko", snapshot: &model.Snapshot{
		Source:   "jinko",
		DeviceSN: "SYNTHETIC_INV_001",
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "Cloud PV", Unit: "W", Value: 90},
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 1},
			{Group: "status", Key: "CLOUD_ONLY", Name: "Cloud only", Value: 7},
		},
	}}
	priority := NewPriority([]Source{primary, fallback}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("logger unavailable")
	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	if len(snapshot.Metrics) != 2 {
		t.Fatalf("projected metrics = %#v, want common telemetry plus source-local alert", snapshot.Metrics)
	}
	if got := snapshot.Metrics[0]; got.Key != "DP1" || got.Name != "PV Power" || got.Value != 90 {
		t.Fatalf("projected common metric = %#v", got)
	}
	if got := snapshot.Metrics[1]; got.Key != "L_B_F_F" || got.Group != "alert" || got.Value != 1 {
		t.Fatalf("preserved fallback alert metric = %#v", got)
	}
}

func TestPrioritySkipsFallbackWithNoProjectedTelemetryIntersection(t *testing.T) {
	primary := &stubSource{name: "modbus", snapshot: &model.Snapshot{
		Source:   "modbus",
		DeviceSN: "SYNTHETIC_INV_001",
		Metrics:  []model.Metric{{Group: "electric", Key: "DP1", Name: "PV Power", Unit: "W", Value: 100}},
	}}
	incompatible := &stubSource{name: "jinko", snapshot: &model.Snapshot{
		Source:   "jinko",
		DeviceSN: "SYNTHETIC_INV_001",
		Metrics: []model.Metric{
			{Group: "status", Key: "CLOUD_ONLY", Name: "Cloud only", Value: 7},
			{Group: "alert", Key: "L_B_F_F", Name: "Lithium battery fault flag", Value: 1},
		},
	}}
	tertiary := &stubSource{name: "solarman", snapshot: &model.Snapshot{
		Source:   "solarman",
		DeviceSN: "SYNTHETIC_INV_001",
		Metrics:  []model.Metric{{Group: "other", Key: "DP1", Name: "Fallback PV", Unit: "watts", Value: 80}},
	}}
	priority := NewPriority([]Source{primary, incompatible, tertiary}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("logger unavailable")
	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	if snapshot.Source != "solarman" || len(snapshot.Metrics) != 1 || snapshot.Metrics[0].Key != "DP1" || snapshot.Metrics[0].Value != 80 {
		t.Fatalf("selected snapshot = %#v, want compatible tertiary telemetry", snapshot)
	}
	if primary.calls != 2 || incompatible.calls != 1 || tertiary.calls != 1 {
		t.Fatalf("calls primary/incompatible/tertiary = %d/%d/%d, want 2/1/1", primary.calls, incompatible.calls, tertiary.calls)
	}
}

func TestPriorityColdStartFallbackKeepsAvailableIdentity(t *testing.T) {
	fallbackSnapshot := testSourceSnapshot("solarman", "SYNTHETIC_INV_001")
	priority := NewPriority([]Source{
		&stubSource{name: "jinko", err: errors.New("expired token")},
		&stubSource{name: "solarman", snapshot: fallbackSnapshot},
	}, true)

	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	wantIdentity := [4]string{"SYNTHETIC_INV_001", "", "", ""}
	if got := snapshotIdentity(snapshot); got != wantIdentity {
		t.Fatalf("identity = %#v, want %#v", got, wantIdentity)
	}
	if snapshot.Source != "solarman" || len(snapshot.Metrics) != len(fallbackSnapshot.Metrics) {
		t.Fatalf("cold fallback snapshot = %#v", snapshot)
	}
}

func TestPriorityJinkoSolarmanJinkoReusesMatchingDeviceIdentity(t *testing.T) {
	primarySnapshot := testSourceSnapshot("jinko", "SYNTHETIC_INV_001")
	primarySnapshot.ParentSN = "LOGGER_SN"
	primarySnapshot.DeviceID = "DEVICE_ID"
	primarySnapshot.SiteID = "SITE_ID"
	fallbackSnapshot := testSourceSnapshot("solarman", "SYNTHETIC_INV_001")

	primary := &stubSource{name: "jinko", snapshot: primarySnapshot}
	fallback := &stubSource{name: "solarman", snapshot: fallbackSnapshot}
	priority := NewPriority([]Source{primary, fallback}, true)
	wantIdentity := snapshotIdentity(primarySnapshot)

	first, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("initial Jinko Fetch() error = %v", err)
	}
	if first.Source != "jinko" || snapshotIdentity(first) != wantIdentity {
		t.Fatalf("initial snapshot = %#v, want complete Jinko identity", first)
	}

	primary.err = errors.New("expired token")
	second, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Solarman fallback Fetch() error = %v", err)
	}
	if second.Source != "solarman" || snapshotIdentity(second) != wantIdentity {
		t.Fatalf("fallback snapshot = %#v, want reused matching identity %#v", second, wantIdentity)
	}

	primary.err = nil
	third, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("recovered Jinko Fetch() error = %v", err)
	}
	if third.Source != "jinko" || snapshotIdentity(third) != wantIdentity {
		t.Fatalf("recovered snapshot = %#v, want complete Jinko identity", third)
	}
	if primary.calls != 3 || fallback.calls != 1 {
		t.Fatalf("calls = primary %d fallback %d, want 3/1", primary.calls, fallback.calls)
	}
}

func TestPriorityMatchingDeviceFillsOnlyEmptyFallbackIdentity(t *testing.T) {
	primarySnapshot := testSourceSnapshot("jinko", "SYNTHETIC_INV_001")
	primarySnapshot.ParentSN = "PRIMARY_PARENT"
	primarySnapshot.DeviceID = "PRIMARY_DEVICE_ID"
	primarySnapshot.SiteID = "PRIMARY_SITE_ID"
	fallbackSnapshot := testSourceSnapshot("solarman", "SYNTHETIC_INV_001")
	fallbackSnapshot.ParentSN = "FALLBACK_PARENT"
	fallbackSnapshot.SiteID = "   "

	primary := &stubSource{name: "jinko", snapshot: primarySnapshot}
	priority := NewPriority([]Source{
		primary,
		&stubSource{name: "solarman", snapshot: fallbackSnapshot},
	}, true)

	if _, err := priority.Fetch(context.Background()); err != nil {
		t.Fatalf("primary Fetch() error = %v", err)
	}
	primary.err = errors.New("cloud down")
	snapshot, err := priority.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fallback Fetch() error = %v", err)
	}
	wantIdentity := [4]string{"SYNTHETIC_INV_001", "FALLBACK_PARENT", "PRIMARY_DEVICE_ID", "PRIMARY_SITE_ID"}
	if got := snapshotIdentity(snapshot); got != wantIdentity {
		t.Fatalf("identity = %#v, want fill-empty-only identity %#v", got, wantIdentity)
	}
}

func TestPriorityReusesIdentityOnlyForMatchingDeviceSN(t *testing.T) {
	tests := []struct {
		name         string
		primarySN    string
		fallbackSN   string
		wantOptional [3]string
		wantConflict bool
	}{
		{name: "trimmed match", primarySN: " SYNTHETIC_INV_001 ", fallbackSN: "SYNTHETIC_INV_001 ", wantOptional: [3]string{"PRIMARY_PARENT", "PRIMARY_DEVICE_ID", "PRIMARY_SITE_ID"}},
		{name: "different serial", primarySN: "SYNTHETIC_INV_001", fallbackSN: "DIFFERENT_SN", wantConflict: true},
		{name: "case mismatch", primarySN: "device-sn", fallbackSN: "DEVICE-SN", wantConflict: true},
		{name: "missing fallback serial", primarySN: "SYNTHETIC_INV_001", fallbackSN: ""},
		{name: "missing primary serial", primarySN: "", fallbackSN: "SYNTHETIC_INV_001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			primarySnapshot := testSourceSnapshot("jinko", tc.primarySN)
			primarySnapshot.ParentSN = "PRIMARY_PARENT"
			primarySnapshot.DeviceID = "PRIMARY_DEVICE_ID"
			primarySnapshot.SiteID = "PRIMARY_SITE_ID"
			primary := &stubSource{name: "jinko", snapshot: primarySnapshot}
			priority := NewPriority([]Source{
				primary,
				&stubSource{name: "solarman", snapshot: testSourceSnapshot("solarman", tc.fallbackSN)},
			}, true)

			if _, err := priority.Fetch(context.Background()); err != nil {
				t.Fatalf("primary Fetch() error = %v", err)
			}
			primary.err = errors.New("cloud down")
			snapshot, err := priority.Fetch(context.Background())
			if tc.wantConflict {
				if err == nil || !strings.Contains(err.Error(), "does not match the learned primary device serial") {
					t.Fatalf("fallback Fetch() error = %v, want learned-primary identity conflict", err)
				}
				if snapshot != nil {
					t.Fatalf("conflicting fallback snapshot = %#v, want nil", snapshot)
				}
				return
			}
			if err != nil {
				t.Fatalf("fallback Fetch() error = %v", err)
			}
			wantIdentity := [4]string{tc.fallbackSN, tc.wantOptional[0], tc.wantOptional[1], tc.wantOptional[2]}
			if got := snapshotIdentity(snapshot); got != wantIdentity {
				t.Fatalf("identity = %#v, want %#v", got, wantIdentity)
			}
		})
	}
}

type stubSource struct {
	name     string
	snapshot *model.Snapshot
	err      error
	calls    int
	onFetch  func()
}

func (s *stubSource) Name() string {
	return s.name
}

func (s *stubSource) Fetch(context.Context) (*model.Snapshot, error) {
	s.calls++
	if s.onFetch != nil {
		s.onFetch()
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.snapshot, nil
}

func testSourceSnapshot(sourceName string, deviceSN string) *model.Snapshot {
	return &model.Snapshot{
		Source:      sourceName,
		DeviceSN:    deviceSN,
		CollectedAt: time.Unix(1775145150, 0).UTC(),
		Metrics: []model.Metric{
			{Group: "electric", Key: "DP1", Name: "DC Power PV1", Unit: "W", Value: 100},
		},
	}
}

func snapshotIdentity(snapshot *model.Snapshot) [4]string {
	return [4]string{snapshot.DeviceSN, snapshot.ParentSN, snapshot.DeviceID, snapshot.SiteID}
}

type backgroundStub struct {
	name            string
	started         chan struct{}
	startedOnce     sync.Once
	fetchCalls      atomic.Int32
	backgroundCalls atomic.Int32
}

func newBackgroundStub(name string) *backgroundStub {
	return &backgroundStub{name: name, started: make(chan struct{})}
}

func (s *backgroundStub) Name() string {
	return s.name
}

func (s *backgroundStub) Fetch(context.Context) (*model.Snapshot, error) {
	s.fetchCalls.Add(1)
	return testSourceSnapshot(s.name, s.name), nil
}

func (s *backgroundStub) RunBackground(ctx context.Context) {
	s.backgroundCalls.Add(1)
	s.startedOnce.Do(func() { close(s.started) })
	<-ctx.Done()
}

func waitStarted(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background lifecycle signal")
	}
}
