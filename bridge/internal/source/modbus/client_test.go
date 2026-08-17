package modbus

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestClientFetchUsesOnlyLiveVerifiedReads(t *testing.T) {
	firstEnergy := liveEnergyWords()
	secondEnergy := liveEnergyWords()
	secondEnergy[15]++
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, []uint16{80, 80, 83, 251}),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, firstEnergy),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{2, 0x0001, 0x7FFF, 0x8000, 0xFFFE, 0xFFFF}),
		makeReadResponse(t, gridFrequencyCurrentRead, liveGridFrequencyCurrentWords()),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{193}),
		makeReadResponse(t, loadVoltageRead, nightLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, nightDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, nightLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2505, 2412, 2369}),
		makeReadResponse(t, gridPowerLowRead, exportGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF}),
		makeReadResponse(t, outputScalarRead, nightOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, nightOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, nightPVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, nightInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1281}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6018, 99}),
		makeReadResponse(t, batteryFlowRead, []uint16{0xFFF7, 0xFFF0}),
		makeReadResponse(t, energyRead, secondEnergy),
		makeReadResponse(t, relayStatusRead, []uint16{0xFFF0, 0xFFFF}),
		makeReadResponse(t, warningFaultRead, []uint16{1, 2, 3, 4, 0x8000, 0xFFFF}),
		makeReadResponse(t, runStateRead, []uint16{5, 5, 4, 3, 2, 1}),
		makeReadResponse(t, gridFrequencyCurrentRead, mutatedIgnoredGridFrequencyCurrentWords()),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	first, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	assertSnapshot(t, first, expectedSnapshotMetrics(
		192,
		[3]float64{238.9, 240.9, 236.2}, 49.95,
		[3]float64{80, 80, 83}, 251,
		[3]float64{250.4, 241.1, 236.8},
		49.95, [3]float64{0.53, 0.66, 1.55},
		[4]float64{101, 98, 117, 316},
		[3]float64{238.9, 240.9, 236.2},
		[3]float64{3.8, 3.8, 4.6},
		49.95,
		[4]float64{879, 883, 1092, 2854},
		pvInputs{
			powersW:     [2]float64{30, 40},
			voltagesV:   [2]float64{126.8, 130},
			currentsA:   [2]float64{0.2, 0.3},
			totalPowerW: 70,
		},
		[2]float64{25, 44},
		28, 601.7, 100, 90, 0.16,
		firstEnergy,
		1, 7,
		[6]uint16{},
		2,
	))
	if got := dialer.connectionCount(); got != 1 {
		t.Fatalf("first Fetch connections = %d, want 1", got)
	}
	firstDeadline := dialer.connection(0).deadline
	if firstDeadline.IsZero() {
		t.Fatal("first Fetch connection deadline is zero")
	}
	if got := len(dialer.connection(0).writes); got != 24 {
		t.Fatalf("first Fetch writes = %d, want 24", got)
	}
	if got := dialer.responseCount(); got != 24 {
		t.Fatalf("first Fetch responses consumed = %d, want 24", got)
	}

	second, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	assertSnapshot(t, second, expectedSnapshotMetrics(
		193,
		[3]float64{235.4, 240.1, 235.8}, 50,
		[3]float64{}, 0,
		[3]float64{250.5, 241.2, 236.9},
		49.95, [3]float64{0.53, 0.66, 1.55},
		[4]float64{-777, -780, -774, -2331},
		[3]float64{235.4, 240.1, 235.8},
		[3]float64{-0.1, -0.2, 0.6},
		50,
		[4]float64{-10, -20, 117, 87},
		pvInputs{},
		[2]float64{25.1, 44.1},
		28.1, 601.8, 99, -90, -0.16,
		secondEnergy,
		0, 0xFFFF,
		[6]uint16{1, 2, 3, 4, 0x8000, 0xFFFF},
		5,
	))
	if got := dialer.connectionCount(); got != 2 {
		t.Fatalf("total connections = %d, want 2 (one per Fetch)", got)
	}
	if got := dialer.attemptCount(); got != 2 {
		t.Fatalf("total dial attempts = %d, want 2", got)
	}
	secondDeadline := dialer.connection(1).deadline
	if secondDeadline.IsZero() || secondDeadline.Before(firstDeadline) {
		t.Fatalf("second Fetch deadline = %s, want a non-regressing deadline relative to %s", secondDeadline, firstDeadline)
	}
	if got := len(dialer.connection(1).writes); got != 22 {
		t.Fatalf("second Fetch writes = %d, want 22 (profile gates cached; generator mode uncached)", got)
	}
	if got := dialer.responseCount(); got != 46 {
		t.Fatalf("total responses consumed = %d, want 46", got)
	}

	expectedReads := []readID{
		deviceTypeRead, capabilityRead,
		generatorPortModeRead, generatorEnergyRead, generatorElectricalRead,
		upsPowerRead, loadVoltageRead, directLoadPowerLowRead, loadFrequencyRead, gridVoltageRead, gridPowerLowRead, gridPowerHighRead, outputScalarRead, outputPowerHighRead, pvInputRead, inverterTemperatureRead,
		batteryTemperatureRead, batteryVoltageSOCRead, batteryFlowRead, energyRead, relayStatusRead, warningFaultRead, runStateRead, gridFrequencyCurrentRead,
		generatorPortModeRead, generatorEnergyRead, generatorElectricalRead,
		upsPowerRead, loadVoltageRead, directLoadPowerLowRead, loadFrequencyRead, gridVoltageRead, gridPowerLowRead, gridPowerHighRead, outputScalarRead, outputPowerHighRead, pvInputRead, inverterTemperatureRead,
		batteryTemperatureRead, batteryVoltageSOCRead, batteryFlowRead, energyRead, relayStatusRead, warningFaultRead, runStateRead, gridFrequencyCurrentRead,
	}
	for index, id := range expectedReads {
		connIndex := 0
		writeIndex := index
		if index >= 24 {
			connIndex = 1
			writeIndex = index - 24
		}
		conn := dialer.connection(connIndex)
		want, err := buildReadRequest(testLoggerSerial, modbusUnitID, id)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(conn.writes[writeIndex], want) {
			t.Fatalf("connection %d write %d request = %x, want %x", connIndex, writeIndex, conn.writes[writeIndex], want)
		}
	}
	if dialer.connection(0).closeCount != 1 || dialer.connection(1).closeCount != 1 {
		t.Fatalf("connection close counts = %d/%d, want 1/1", dialer.connection(0).closeCount, dialer.connection(1).closeCount)
	}
}

func TestClientFetchAcceptsTwentyKilowattJKSFamilyMemberEndToEnd(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0x0D40, 0x0003, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{0xFFFF}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, []uint16{20000, 20000, 25535, 65535}),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{2, 0, 0, 0, 0, 0}),
		makeReadResponse(t, gridFrequencyCurrentRead, liveGridFrequencyCurrentWords()),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err != nil || snapshot == nil {
		t.Fatalf("snapshot/error = %#v/%v", snapshot, err)
	}
	if snapshot.Meta["profile"] != profileName || snapshot.Meta["rated_power_w"] != "20000" || snapshot.Meta["mppt_count"] != "2" || snapshot.Meta["phase_count"] != "3" {
		t.Fatalf("snapshot meta = %#v", snapshot.Meta)
	}
	wantBoundaryMetrics := map[string]float64{
		"Pr1":       20000,
		"UPS_P":     65535,
		"LPP_A":     20000,
		"LPP_B":     20000,
		"LPP_C":     25535,
		"E_Puse_t1": 65535,
	}
	for _, metric := range snapshot.Metrics {
		want, ok := wantBoundaryMetrics[metric.Key]
		if !ok {
			continue
		}
		if metric.Value != want {
			t.Fatalf("%s = %v, want %v", metric.Key, metric.Value, want)
		}
		delete(wantBoundaryMetrics, metric.Key)
	}
	if len(wantBoundaryMetrics) != 0 {
		t.Fatalf("boundary metrics are missing: %#v", wantBoundaryMetrics)
	}

	expectedReads := []readID{
		deviceTypeRead, capabilityRead,
		generatorPortModeRead, generatorEnergyRead, generatorElectricalRead,
		upsPowerRead, loadVoltageRead, directLoadPowerLowRead, loadFrequencyRead, gridVoltageRead, gridPowerLowRead, gridPowerHighRead, outputScalarRead, outputPowerHighRead, pvInputRead, inverterTemperatureRead,
		batteryTemperatureRead, batteryVoltageSOCRead, batteryFlowRead, energyRead, relayStatusRead, warningFaultRead, runStateRead, gridFrequencyCurrentRead,
	}
	if dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("connections/attempts = %d/%d, want 1/1", dialer.connectionCount(), dialer.attemptCount())
	}
	conn := dialer.connection(0)
	if len(conn.writes) != len(expectedReads) {
		t.Fatalf("writes = %d, want %d", len(conn.writes), len(expectedReads))
	}
	for index, id := range expectedReads {
		want, err := buildReadRequest(testLoggerSerial, modbusUnitID, id)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(conn.writes[index], want) {
			t.Fatalf("write %d request = %x, want %x", index, conn.writes[index], want)
		}
	}
}

func TestClientFetchCapabilityMismatchStopsAndDoesNotCacheDeviceCanary(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0103}),
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{2, 0, 0, 0, 0, 0}),
		makeReadResponse(t, gridFrequencyCurrentRead, liveGridFrequencyCurrentWords()),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot, err := client.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "capability profile") || snapshot != nil {
		t.Fatalf("first snapshot/error = %#v/%v, want nil capability error", snapshot, err)
	}
	if got := dialer.connectionCount(); got != 1 {
		t.Fatalf("first Fetch connections = %d, want 1", got)
	}
	if got := len(dialer.connection(0).writes); got != 2 {
		t.Fatalf("first Fetch writes = %d, want 2", got)
	}

	snapshot, err := client.Fetch(context.Background())
	if err != nil || snapshot == nil {
		t.Fatalf("second snapshot/error = %#v/%v", snapshot, err)
	}
	if got := dialer.connectionCount(); got != 2 {
		t.Fatalf("total connections = %d, want 2 (both gates rerun on the second session)", got)
	}
	if got := len(dialer.connection(1).writes); got != 24 {
		t.Fatalf("second Fetch writes = %d, want 24", got)
	}
}

func TestClientFetchWrongCanaryStopsBeforeTelemetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0007}),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing telemetry") {
		t.Fatalf("snapshot/error = %#v/%v", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
	}
}

func TestClientFetchRejectsGeneratorOutsideExactZeroDomainWithoutPartialSnapshot(t *testing.T) {
	tests := []struct {
		name       string
		responses  func(*testing.T) [][]byte
		wantError  string
		wantWrites int
	}{
		{
			name: "port mode",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{1}),
				}
			},
			wantError:  "register 133",
			wantWrites: 3,
		},
		{
			name: "energy and runtime",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, []uint16{0, 0, 1, 0}),
				}
			},
			wantError:  "register 538",
			wantWrites: 4,
		},
		{
			name: "voltage and power",
			responses: func(t *testing.T) [][]byte {
				values := make([]uint16, 11)
				values[10] = 1
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, values),
				}
			},
			wantError:  "register 671",
			wantWrites: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &scriptedDialer{responses: tt.responses(t)}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("snapshot/error = %#v/%v, want %q", snapshot, err, tt.wantError)
			}
			if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
				t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
			}
			if writes := len(dialer.connection(0).writes); writes != tt.wantWrites {
				t.Fatalf("writes = %d, want %d", writes, tt.wantWrites)
			}
			if closes := dialer.connection(0).closeCount; closes != 1 {
				t.Fatalf("connection closes = %d, want 1", closes)
			}
		})
	}
}

func TestClientFetchRejectsOutputPowerOutsideSafeSignedDomainWithoutPartialSnapshot(t *testing.T) {
	tests := []struct {
		name      string
		responses func(*testing.T) [][]byte
		wantError string
	}{
		{
			name: "high-word read exception",
			responses: func(t *testing.T) [][]byte {
				responses := responsesThroughOutputScalar(t, liveOutputScalarWords())
				return append(responses, makeExceptionResponse(t, outputPowerHighRead, 0x02))
			},
			wantError: "read output power high words",
		},
		{
			name: "torn positive low with negative high word",
			responses: func(t *testing.T) [][]byte {
				responses := responsesThroughOutputScalar(t, liveOutputScalarWords())
				return append(responses, makeReadResponse(t, outputPowerHighRead, []uint16{0, 0xFFFF, 0, 0, 0}))
			},
			wantError: "registers 634/692",
		},
		{
			name: "torn negative to zero crossing",
			responses: func(t *testing.T) [][]byte {
				words := liveOutputScalarWords()
				copy(words[6:10], []uint16{65436, 20, 30, 65486})
				responses := responsesThroughOutputScalar(t, words)
				return append(responses, makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()))
			},
			wantError: "registers 633/691",
		},
		{
			name: "phase sum mismatch",
			responses: func(t *testing.T) [][]byte {
				words := liveOutputScalarWords()
				words[9]++
				responses := responsesThroughOutputScalar(t, words)
				return append(responses, makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()))
			},
			wantError: "phase sum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &scriptedDialer{responses: tt.responses(t)}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("snapshot/error = %#v/%v, want %q", snapshot, err, tt.wantError)
			}
			if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
				t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
			}
			if writes := len(dialer.connection(0).writes); writes != 14 {
				t.Fatalf("writes = %d, want exactly 14 through the paired output reads", writes)
			}
			if dialer.connection(0).closeCount != 1 {
				t.Fatalf("connection closes = %d, want 1", dialer.connection(0).closeCount)
			}
		})
	}
}

func TestClientFetchReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		{}, // Grid response closes immediately.
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grid phase voltages") {
		t.Fatalf("snapshot/error = %#v/%v", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 {
		t.Fatalf("snapshot/connections = %#v/%d, want nil/1", snapshot, dialer.connectionCount())
	}
	if got := dialer.attemptCount(); got != 1 {
		t.Fatalf("dial attempts = %d, want exactly 1 (no reconnect)", got)
	}
	if got := len(dialer.connection(0).writes); got != 10 {
		t.Fatalf("writes = %d, want exactly 10 before failure", got)
	}
}

func TestClientFetchDirectLoadPowerLowFailureReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeExceptionResponse(t, directLoadPowerLowRead, 0x02),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "direct-load power low words") || !strings.Contains(err.Error(), "modbus exception 0x02") {
		t.Fatalf("snapshot/error = %#v/%v, want direct-load low-word exception", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
	}
	if writes := len(dialer.connection(0).writes); writes != 8 {
		t.Fatalf("writes = %d, want exactly 8", writes)
	}
}

func TestClientFetchWarningFaultFailureReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		{}, // The warning/fault response closes immediately.
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "warning/fault raw words") {
		t.Fatalf("snapshot/error = %#v/%v, want warning/fault read error", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
	}
	if writes := len(dialer.connection(0).writes); writes != 22 {
		t.Fatalf("writes = %d, want exactly 22", writes)
	}
}

func TestClientFetchRelayStatusFailureReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	tests := []struct {
		name      string
		response  func(*testing.T) []byte
		wantError string
	}{
		{
			name: "FC03 exception",
			response: func(t *testing.T) []byte {
				return makeExceptionResponse(t, relayStatusRead, 0x02)
			},
			wantError: "read power and AC relay status",
		},
		{
			name: "undocumented power-switch code",
			response: func(t *testing.T) []byte {
				return makeReadResponse(t, relayStatusRead, []uint16{2, 7})
			},
			wantError: "power status register 551",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := responsesThroughEnergy(t)
			responses = append(responses, tt.response(t))
			dialer := &scriptedDialer{responses: responses}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}

			snapshot, err := client.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("snapshot/error = %#v/%v, want %q", snapshot, err, tt.wantError)
			}
			if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
				t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
			}
			if writes := len(dialer.connection(0).writes); writes != 21 {
				t.Fatalf("writes = %d, want exactly 21 through the relay-status read", writes)
			}
			if closes := dialer.connection(0).closeCount; closes != 1 {
				t.Fatalf("connection closes = %d, want 1", closes)
			}
		})
	}
}

func TestClientFetchRunStateFailureReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{6, 0xFFFF, 0x8000, 3, 2, 1}),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "run-state block") || !strings.Contains(err.Error(), "documented maximum 5") {
		t.Fatalf("snapshot/error = %#v/%v, want run-state decode error", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
	}
	if writes := len(dialer.connection(0).writes); writes != 23 {
		t.Fatalf("writes = %d, want exactly 23", writes)
	}
}

func TestClientFetchGridFrequencyCurrentFailureReturnsNoPartialSnapshotAndDoesNotRetry(t *testing.T) {
	dialer := &scriptedDialer{responses: [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{2, 0, 0, 0, 0, 0}),
		makeExceptionResponse(t, gridFrequencyCurrentRead, 0x02),
	}}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := client.Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grid frequency and currents") || !strings.Contains(err.Error(), "modbus exception 0x02") {
		t.Fatalf("snapshot/error = %#v/%v, want final grid block exception", snapshot, err)
	}
	if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
		t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
	}
	if writes := len(dialer.connection(0).writes); writes != 24 {
		t.Fatalf("writes = %d, want exactly 24", writes)
	}
}

func TestClientFetchRangeFailureStopsWithoutPartialOrExtraDial(t *testing.T) {
	tests := []struct {
		name       string
		responses  func(*testing.T) [][]byte
		wantError  string
		wantWrites int
	}{
		{
			name: "load voltage",
			responses: func(t *testing.T) [][]byte {
				invalid := liveLoadVoltageWords()
				invalid[5] = 5001
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, invalid),
				}
			},
			wantError:  "load voltage",
			wantWrites: 7,
		},
		{
			name: "load frequency",
			responses: func(t *testing.T) [][]byte {
				invalid := liveLoadFrequencyWords()
				invalid[0] = 10001
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, invalid),
				}
			},
			wantError:  "load frequency",
			wantWrites: 9,
		},
		{
			name: "direct-load total high word",
			responses: func(t *testing.T) [][]byte {
				invalid := liveLoadFrequencyWords()
				invalid[4] = 0xFFFF
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, invalid),
				}
			},
			wantError:  "only verified non-negative",
			wantWrites: 9,
		},
		{
			name: "grid power phase sum",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, []uint16{100, 100, 100, 301}),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
				}
			},
			wantError:  "phase sum",
			wantWrites: 12,
		},
		{
			name: "output voltage",
			responses: func(t *testing.T) [][]byte {
				invalidOutput := liveOutputScalarWords()
				invalidOutput[0] = 5001
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
					makeReadResponse(t, outputScalarRead, invalidOutput),
					makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
				}
			},
			wantError:  "output voltage",
			wantWrites: 14,
		},
		{
			name: "PV extra channel",
			responses: func(t *testing.T) [][]byte {
				invalid := livePVInputWords()
				invalid[2] = 1
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
					makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
					makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
					makeReadResponse(t, pvInputRead, invalid),
				}
			},
			wantError:  "non-zero PV3/PV4",
			wantWrites: 15,
		},
		{
			name: "DC temperature",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
					makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
					makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
					makeReadResponse(t, pvInputRead, livePVInputWords()),
					makeReadResponse(t, inverterTemperatureRead, []uint16{2001, 1440}),
				}
			},
			wantError:  "DC temperature",
			wantWrites: 16,
		},
		{
			name: "temperature",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
					makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
					makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
					makeReadResponse(t, pvInputRead, livePVInputWords()),
					makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
					makeReadResponse(t, batteryTemperatureRead, []uint16{2001}),
				}
			},
			wantError:  "battery temperature",
			wantWrites: 17,
		},
		{
			name: "SOC",
			responses: func(t *testing.T) [][]byte {
				return [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
					makeReadResponse(t, upsPowerRead, []uint16{192}),
					makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
					makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
					makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
					makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
					makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
					makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
					makeReadResponse(t, outputScalarRead, liveOutputScalarWords()),
					makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
					makeReadResponse(t, pvInputRead, livePVInputWords()),
					makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
					makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
					makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 101}),
				}
			},
			wantError:  "SOC",
			wantWrites: 18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialer := &scriptedDialer{responses: tt.responses(t)}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("snapshot/error = %#v/%v, want %q", snapshot, err, tt.wantError)
			}
			if snapshot != nil || dialer.connectionCount() != 1 || dialer.attemptCount() != 1 {
				t.Fatalf("snapshot/connections/attempts = %#v/%d/%d, want nil/1/1", snapshot, dialer.connectionCount(), dialer.attemptCount())
			}
			if writes := len(dialer.connection(0).writes); writes != tt.wantWrites {
				t.Fatalf("writes before failure = %d, want %d", writes, tt.wantWrites)
			}
			if closes := dialer.connection(0).closeCount; closes != 1 {
				t.Fatalf("connection closes = %d, want 1", closes)
			}
		})
	}
}

func TestClientFetchGateHonorsCancellation(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return nil, errors.New("released")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	cfg := testModbusConfig()
	cfg.Timeout = 5 * time.Second
	client, err := newClient(cfg, dial)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	go func() {
		_, err := client.Fetch(firstCtx)
		firstDone <- err
	}()
	<-entered

	secondCtx, cancelSecond := context.WithCancel(context.Background())
	cancelSecond()
	if _, err := client.Fetch(secondCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Fetch() error = %v, want context.Canceled", err)
	}
	cancelFirst()
	close(release)
	if err := <-firstDone; err == nil {
		t.Fatal("first Fetch() error = nil")
	}
}

func TestNewClientRejectsUnsafeConfiguration(t *testing.T) {
	valid := testModbusConfig()
	tests := []struct {
		name   string
		mutate func(*config.ModbusConfig)
		want   string
	}{
		{name: "hostname", mutate: func(cfg *config.ModbusConfig) { cfg.Host = "logger.local" }, want: "literal IPv4"},
		{name: "IPv6", mutate: func(cfg *config.ModbusConfig) { cfg.Host = "::1" }, want: "literal IPv4"},
		{name: "IPv4-mapped IPv6", mutate: func(cfg *config.ModbusConfig) { cfg.Host = "::ffff:192.168.50.25" }, want: "literal IPv4"},
		{name: "public IPv4", mutate: func(cfg *config.ModbusConfig) { cfg.Host = "1.1.1.1" }, want: "private literal IPv4"},
		{name: "zero port", mutate: func(cfg *config.ModbusConfig) { cfg.Port = 0 }, want: "port"},
		{name: "missing logger serial", mutate: func(cfg *config.ModbusConfig) { cfg.LoggerSerial = "" }, want: "serial"},
		{name: "overflow logger serial", mutate: func(cfg *config.ModbusConfig) { cfg.LoggerSerial = "4294967296" }, want: "serial"},
		{name: "wrong unit", mutate: func(cfg *config.ModbusConfig) { cfg.UnitID = 2 }, want: "unit ID"},
		{name: "zero timeout", mutate: func(cfg *config.ModbusConfig) { cfg.Timeout = 0 }, want: "timeout"},
		{name: "excessive timeout", mutate: func(cfg *config.ModbusConfig) { cfg.Timeout = 31 * time.Second }, want: "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			_, err := newClient(cfg, defaultDialContext)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestClientFetchRejectsNilContext(t *testing.T) {
	client, err := newClient(testModbusConfig(), defaultDialContext)
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if _, err := client.Fetch(nilContext); err == nil {
		t.Fatal("Fetch(nil) error = nil")
	}
}

func testModbusConfig() config.ModbusConfig {
	return config.ModbusConfig{
		Host:         "192.168.50.25",
		Port:         8899,
		LoggerSerial: "305419896",
		DeviceSN:     "INVERTER_SN_EXAMPLE",
		UnitID:       1,
		Timeout:      time.Second,
	}
}

func assertSnapshot(t *testing.T, snapshot *model.Snapshot, want map[string]float64) {
	t.Helper()
	if snapshot == nil {
		t.Fatal("snapshot = nil")
	}
	if snapshot.Source != "modbus" || snapshot.DeviceSN != "INVERTER_SN_EXAMPLE" || snapshot.ParentSN != "305419896" {
		t.Fatalf("snapshot identity = %q/%q/%q", snapshot.Source, snapshot.DeviceSN, snapshot.ParentSN)
	}
	if snapshot.Meta["profile"] != profileName || snapshot.Meta["modbus_function"] != "03" ||
		snapshot.Meta["rated_power_w"] != "12000" || snapshot.Meta["mppt_count"] != "2" || snapshot.Meta["phase_count"] != "3" {
		t.Fatalf("snapshot meta = %#v", snapshot.Meta)
	}
	if len(snapshot.Metrics) != len(want) {
		t.Fatalf("metrics = %d, want %d", len(snapshot.Metrics), len(want))
	}
	if len(snapshot.Metrics) != 80 {
		t.Fatalf("metrics = %d, want exact 80-key Modbus surface", len(snapshot.Metrics))
	}
	wantAlertNames := map[string]string{
		"DEYE_MODBUS_R553_WARNING_WORD_1_RAW": "Deye Inverter Warning Word 1 (Raw U16)",
		"DEYE_MODBUS_R554_WARNING_WORD_2_RAW": "Deye Inverter Warning Word 2 (Raw U16)",
		"DEYE_MODBUS_R555_FAULT_WORD_1_RAW":   "Deye Inverter Fault Word 1 (Raw U16)",
		"DEYE_MODBUS_R556_FAULT_WORD_2_RAW":   "Deye Inverter Fault Word 2 (Raw U16)",
		"DEYE_MODBUS_R557_FAULT_WORD_3_RAW":   "Deye Inverter Fault Word 3 (Raw U16)",
		"DEYE_MODBUS_R558_FAULT_WORD_4_RAW":   "Deye Inverter Fault Word 4 (Raw U16)",
	}
	const runStateKey = "DEYE_MODBUS_R500_RUN_STATE"
	const runStateName = "Deye Inverter Run State Code"
	const powerSwitchStateKey = "DEYE_MODBUS_R551_POWER_SWITCH_STATE"
	const powerSwitchStateName = "Deye Inverter Power Switch State"
	wantGridNames := map[string]string{
		"PG_F1":  "Grid Frequency",
		"G_C_L1": "Grid\u00a0Current\u00a0L1",
		"G_C_L2": "Grid\u00a0Current\u00a0L2",
		"G_C_L3": "Grid\u00a0Current\u00a0L3",
	}
	wantPVDefinitions := map[string]struct {
		name string
		unit string
	}{
		"DP1": {name: "DC Power PV1", unit: "W"},
		"DP2": {name: "DC Power PV2", unit: "W"},
		"DV1": {name: "DC Voltage PV1", unit: "V"},
		"DC1": {name: "DC Current PV1", unit: "A"},
		"DV2": {name: "DC Voltage PV2", unit: "V"},
		"DC2": {name: "DC Current PV2", unit: "A"},
	}
	wantDirectLoadDefinitions := map[string]string{
		"LPP_A": "Load phase power A",
		"LPP_B": "Load phase power B",
		"LPP_C": "Load phase power C",
	}
	wantOutputDefinitions := map[string]string{
		"INV_O_P_L1": "Inverter Output Power L1",
		"INV_O_P_L2": "Inverter Output Power L2",
		"INV_O_P_L3": "Inverter Output Power L3",
		"INV_O_P_T":  "Total Inverter Output Power",
	}
	wantGeneratorDefinitions := map[string]struct {
		name string
		unit string
	}{
		"GEN_P_L1": {name: "Gen Power L1", unit: "W"},
		"GEN_P_L2": {name: "Gen Power L2", unit: "W"},
		"GEN_P_L3": {name: "Gen Power L3", unit: "W"},
		"GEN_V_L1": {name: "Gen Voltage L1", unit: "V"},
		"GEN_V_L2": {name: "Gen Voltage L2", unit: "V"},
		"GEN_V_L3": {name: "Gen Voltage L3", unit: "V"},
		"R_T_D":    {name: "Gen\u00a0Daily\u00a0Run\u00a0Time", unit: "h"},
		"EG_P_CT1": {name: "Generator Active Power", unit: "W"},
		"GEN_P_T":  {name: "Total Gen Power", unit: "W"},
		"GEN_P_D":  {name: "Daily Production Generator", unit: "kWh"},
		"GEN_P_TO": {name: "Total Production Generator", unit: "kWh"},
	}
	seen := make(map[string]struct{}, len(snapshot.Metrics))
	for _, metric := range snapshot.Metrics {
		if _, duplicate := seen[metric.Key]; duplicate {
			t.Fatalf("duplicate metric key %q", metric.Key)
		}
		seen[metric.Key] = struct{}{}
		if metric.Key == "C_P_L1" || metric.Key == "C_P_L2" || metric.Key == "C_P_L3" || metric.Key == "O_P" {
			t.Fatalf("output/direct-load decoding emitted an excluded alias: %#v", metric)
		}
		if metric.Key == "DP3" || metric.Key == "DP4" || metric.Key == "DV3" || metric.Key == "DV4" || metric.Key == "DC3" || metric.Key == "DC4" {
			t.Fatalf("two-MPPT profile emitted a forbidden PV3/PV4 alias: %#v", metric)
		}
		if metric.Key == "CT1_P_E" || metric.Key == "CT2_P_E" || metric.Key == "CT3_P_E" || metric.Key == "CT_T_E" ||
			strings.Contains(metric.Key, "R613") || strings.Contains(metric.Key, "R614") || strings.Contains(metric.Key, "R615") ||
			strings.Contains(metric.Key, "R616") || strings.Contains(metric.Key, "R617") || strings.Contains(metric.Key, "R618") || strings.Contains(metric.Key, "R619") {
			t.Fatalf("grid frequency/current block emitted an ignored external-CT register metric: %#v", metric)
		}
		value, ok := want[metric.Key]
		if !ok {
			t.Fatalf("unexpected metric %#v", metric)
		}
		if metric.Value != value {
			t.Fatalf("metric %s = %v, want %v", metric.Key, metric.Value, value)
		}
		if alertName, isModbusAlert := wantAlertNames[metric.Key]; isModbusAlert {
			if metric.Group != "alert" || metric.Name != alertName || metric.Unit != "" {
				t.Fatalf("Modbus-local alert metric %s = %#v, want group=alert/name=%q/empty unit", metric.Key, metric, alertName)
			}
			continue
		}
		if metric.Key == runStateKey {
			if metric.Group != "status" || metric.Name != runStateName || metric.Unit != "" {
				t.Fatalf("Modbus-local run-state metric = %#v, want group=status/name=%q/empty unit", metric, runStateName)
			}
			continue
		}
		if metric.Key == powerSwitchStateKey {
			if metric.Group != "status" || metric.Name != powerSwitchStateName || metric.Unit != "" || (metric.Value != 0 && metric.Value != 1) {
				t.Fatalf("Modbus-local power-switch state metric = %#v, want group=status/name=%q/empty unit/value 0 or 1", metric, powerSwitchStateName)
			}
			continue
		}
		if metric.Key == "AC" {
			if metric.Group != "status" || metric.Name != "AC side relay status" || metric.Unit != "" {
				t.Fatalf("canonical AC relay metric = %#v", metric)
			}
			continue
		}
		if gridName, isPromotedGridScalar := wantGridNames[metric.Key]; isPromotedGridScalar {
			wantUnit := "A"
			if metric.Key == "PG_F1" {
				wantUnit = "Hz"
			}
			if metric.Group != "grid" || metric.Name != gridName || metric.Unit != wantUnit {
				t.Fatalf("canonical grid scalar %s = %#v, want group=grid/name=%q/unit=%q", metric.Key, metric, gridName, wantUnit)
			}
			continue
		}
		if pvDefinition, isPromotedPVScalar := wantPVDefinitions[metric.Key]; isPromotedPVScalar {
			if metric.Group != "electric" || metric.Name != pvDefinition.name || metric.Unit != pvDefinition.unit {
				t.Fatalf("canonical PV scalar %s = %#v, want group=electric/name=%q/unit=%q", metric.Key, metric, pvDefinition.name, pvDefinition.unit)
			}
			continue
		}
		if directLoadName, isDirectLoadPhase := wantDirectLoadDefinitions[metric.Key]; isDirectLoadPhase {
			if metric.Group != "consumption" || metric.Name != directLoadName || metric.Unit != "W" {
				t.Fatalf("canonical direct-load phase %s = %#v, want group=consumption/name=%q/unit=W", metric.Key, metric, directLoadName)
			}
			continue
		}
		if outputName, isOutputPower := wantOutputDefinitions[metric.Key]; isOutputPower {
			if metric.Group != "electric" || metric.Name != outputName || metric.Unit != "W" {
				t.Fatalf("canonical output power %s = %#v, want group=electric/name=%q/unit=W", metric.Key, metric, outputName)
			}
			continue
		}
		if definition, isGenerator := wantGeneratorDefinitions[metric.Key]; isGenerator {
			if metric.Group != "generator" || metric.Name != definition.name || metric.Unit != definition.unit || metric.Value != 0 {
				t.Fatalf("canonical zero-domain generator metric %s = %#v, want group=generator/name=%q/unit=%q/value=0", metric.Key, metric, definition.name, definition.unit)
			}
			continue
		}
		if metric.Key == "E_Puse_t1" {
			if metric.Group != "consumption" || metric.Name != "Total Consumption Power" || metric.Unit != "W" {
				t.Fatalf("canonical direct-load total metric = %#v", metric)
			}
			continue
		}
		if metric.Key == "BMS_SOC" {
			if metric.Group != "bms" || metric.Name != "BMS_SOC" || metric.Unit != "%" {
				t.Fatalf("canonical BMS SOC metric = %#v", metric)
			}
			continue
		}
		if metric.Key == "BMST" {
			if metric.Group != "bms" || metric.Name != "BMS Temperature" || metric.Unit != "C" {
				t.Fatalf("canonical BMS temperature metric = %#v", metric)
			}
			continue
		}
		if metric.Key == "ST_PG1" || metric.Key == "INV_MOD1" || strings.Contains(metric.Key, "R501") || strings.Contains(metric.Key, "R502") || strings.Contains(metric.Key, "R503") || strings.Contains(metric.Key, "R504") || strings.Contains(metric.Key, "R505") {
			t.Fatalf("run-state block emitted an excluded alias/register metric: %#v", metric)
		}
		if metric.Name == "" || metric.Group == "" || metric.Unit == "" {
			t.Fatalf("metric %s is not canonical: %#v", metric.Key, metric)
		}
		if metric.Key == "B_T1" && metric.Unit != "C" {
			t.Fatalf("metric B_T1 unit = %q, want canonical C", metric.Unit)
		}
	}
}

func expectedSnapshotMetrics(
	ups float64,
	loadVoltages [3]float64,
	loadFrequency float64,
	directLoadPhasePowers [3]float64,
	directLoadPower float64,
	gridVoltages [3]float64,
	gridFrequency float64,
	gridCurrents [3]float64,
	gridPowers [4]float64,
	outputVoltages [3]float64,
	outputCurrents [3]float64,
	outputFrequency float64,
	outputPowers [4]float64,
	pv pvInputs,
	inverterTemperatures [2]float64,
	temperature, voltage, soc, batteryPower, batteryCurrent float64,
	energy []uint16,
	powerSwitchState uint16,
	acRelayStatus uint16,
	warningFaultWords [6]uint16,
	runState uint16,
) map[string]float64 {
	result := map[string]float64{
		"Pr1":                                 12000,
		"UPS_P":                               ups,
		"C_V_L1":                              loadVoltages[0],
		"C_V_L2":                              loadVoltages[1],
		"C_V_L3":                              loadVoltages[2],
		"L_F":                                 loadFrequency,
		"LPP_A":                               directLoadPhasePowers[0],
		"LPP_B":                               directLoadPhasePowers[1],
		"LPP_C":                               directLoadPhasePowers[2],
		"E_Puse_t1":                           directLoadPower,
		"G_V_L1":                              gridVoltages[0],
		"G_V_L2":                              gridVoltages[1],
		"G_V_L3":                              gridVoltages[2],
		"PG_F1":                               gridFrequency,
		"G_C_L1":                              gridCurrents[0],
		"G_C_L2":                              gridCurrents[1],
		"G_C_L3":                              gridCurrents[2],
		"G_P_L1":                              gridPowers[0],
		"G_P_L2":                              gridPowers[1],
		"G_P_L3":                              gridPowers[2],
		"PG_Pt1":                              gridPowers[3],
		"AV1":                                 outputVoltages[0],
		"AV2":                                 outputVoltages[1],
		"AV3":                                 outputVoltages[2],
		"AC1":                                 outputCurrents[0],
		"AC2":                                 outputCurrents[1],
		"AC3":                                 outputCurrents[2],
		"A_Fo1":                               outputFrequency,
		"INV_O_P_L1":                          outputPowers[0],
		"INV_O_P_L2":                          outputPowers[1],
		"INV_O_P_L3":                          outputPowers[2],
		"INV_O_P_T":                           outputPowers[3],
		"DP1":                                 pv.powersW[0],
		"DP2":                                 pv.powersW[1],
		"DV1":                                 pv.voltagesV[0],
		"DC1":                                 pv.currentsA[0],
		"DV2":                                 pv.voltagesV[1],
		"DC2":                                 pv.currentsA[1],
		"S_P_T":                               pv.totalPowerW,
		"T_DC":                                inverterTemperatures[0],
		"AC_T":                                inverterTemperatures[1],
		"B_T1":                                temperature,
		"BMST":                                temperature,
		"B_V1":                                voltage,
		"B_left_cap1":                         soc,
		"BMS_SOC":                             soc,
		"B_P1":                                batteryPower,
		"B_C1":                                batteryCurrent,
		"AC":                                  float64(acRelayStatus),
		"DEYE_MODBUS_R551_POWER_SWITCH_STATE": float64(powerSwitchState),
		"GEN_P_L1":                            0,
		"GEN_P_L2":                            0,
		"GEN_P_L3":                            0,
		"GEN_V_L1":                            0,
		"GEN_V_L2":                            0,
		"GEN_V_L3":                            0,
		"R_T_D":                               0,
		"EG_P_CT1":                            0,
		"GEN_P_T":                             0,
		"GEN_P_D":                             0,
		"GEN_P_TO":                            0,
	}
	decoded, err := decodeEnergyMetrics(energy)
	if err != nil {
		panic(err)
	}
	for _, metric := range decoded {
		result[metric.key] = metric.value
	}
	for _, metric := range modbusWarningFaultMetrics(warningFaultWords) {
		result[metric.Key] = metric.Value
	}
	result["DEYE_MODBUS_R500_RUN_STATE"] = float64(runState)
	return result
}

func importGridPowerLowWords() []uint16 {
	return []uint16{101, 98, 117, 316}
}

func exportGridPowerLowWords() []uint16 {
	return []uint16{0xFCF7, 0xFCF4, 0xFCFA, 0xF6E5}
}

func liveLoadVoltageWords() []uint16 {
	return []uint16{0xFFFF, 0x8000, 0x7FFF, 0x1234, 2389, 2409, 2362}
}

func nightLoadVoltageWords() []uint16 {
	return []uint16{0x0000, 0xFFFF, 0x8000, 0x7FFF, 2354, 2401, 2358}
}

func liveLoadFrequencyWords() []uint16 {
	return []uint16{4995, 0, 0, 0, 0}
}

func nightLoadFrequencyWords() []uint16 {
	return []uint16{5000, 0, 0, 0, 0}
}

func liveDirectLoadPowerLowWords() []uint16 {
	return []uint16{19, 34, 248, 301}
}

func nightDirectLoadPowerLowWords() []uint16 {
	return []uint16{0, 0, 0, 0}
}

func liveGridFrequencyCurrentWords() []uint16 {
	return []uint16{4995, 53, 66, 155, 53, 63, 157, 100, 102, 120, 322}
}

func mutatedIgnoredGridFrequencyCurrentWords() []uint16 {
	return []uint16{4995, 53, 66, 155, 0xFFFF, 0x8000, 0x7FFF, 0x1234, 0xABCD, 0x0001, 0xFFFE}
}

func livePVInputWords() []uint16 {
	return []uint16{3, 4, 0, 0, 1268, 2, 1300, 3}
}

func nightPVInputWords() []uint16 {
	return []uint16{0, 0, 0, 0, 0, 0, 0, 0}
}

func liveInverterTemperatureWords() []uint16 {
	return []uint16{1250, 1440}
}

func nightInverterTemperatureWords() []uint16 {
	return []uint16{1251, 1441}
}

func liveOutputScalarWords() []uint16 {
	return []uint16{
		0x0955, 0x0969, 0x093A,
		0x017C, 0x017C, 0x01CC,
		0x036F, 0x0373, 0x0444, 0x0B26, 0x0B26,
		0x1383,
	}
}

func responsesThroughOutputScalar(t *testing.T, outputWords []uint16) [][]byte {
	t.Helper()
	return [][]byte{
		makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
		makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
		makeReadResponse(t, generatorPortModeRead, []uint16{0}),
		makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
		makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
		makeReadResponse(t, upsPowerRead, []uint16{192}),
		makeReadResponse(t, loadVoltageRead, liveLoadVoltageWords()),
		makeReadResponse(t, directLoadPowerLowRead, liveDirectLoadPowerLowWords()),
		makeReadResponse(t, loadFrequencyRead, liveLoadFrequencyWords()),
		makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368}),
		makeReadResponse(t, gridPowerLowRead, importGridPowerLowWords()),
		makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, outputScalarRead, outputWords),
	}
}

func responsesThroughEnergy(t *testing.T) [][]byte {
	t.Helper()
	responses := responsesThroughOutputScalar(t, liveOutputScalarWords())
	return append(responses,
		makeReadResponse(t, outputPowerHighRead, liveOutputPowerHighWords()),
		makeReadResponse(t, pvInputRead, livePVInputWords()),
		makeReadResponse(t, inverterTemperatureRead, liveInverterTemperatureWords()),
		makeReadResponse(t, batteryTemperatureRead, []uint16{1280}),
		makeReadResponse(t, batteryVoltageSOCRead, []uint16{6017, 100}),
		makeReadResponse(t, batteryFlowRead, []uint16{9, 16}),
		makeReadResponse(t, energyRead, liveEnergyWords()),
	)
}

func nightOutputScalarWords() []uint16 {
	return []uint16{
		2354, 2401, 2358,
		0xFFF6, 0xFFEC, 60,
		// These active-power magnitudes are a synthetic signed fixture. A live
		// production log has established R691=0xFFFF, but did not preserve the
		// paired low words or the other three active high words.
		0xFFF6, 0xFFEC, 117, 87, 0xFFFD,
		5000,
	}
}

func liveOutputPowerHighWords() []uint16 {
	return []uint16{0, 0, 0, 0, 0}
}

func nightOutputPowerHighWords() []uint16 {
	return []uint16{0xFFFF, 0xFFFF, 0, 0, 0xFFFF}
}

func liveEnergyWords() []uint16 {
	return []uint16{
		12, 8,
		10387, 0,
		10594, 0,
		35, 574,
		25043, 0,
		0x8F18, 2,
		62,
		27697, 0,
		671,
	}
}

type scriptedDialer struct {
	mu            sync.Mutex
	responses     [][]byte
	responseIndex int
	conns         []*memoryConn
	attempts      int
}

func (dialer *scriptedDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	dialer.attempts++
	if network != "tcp4" || address != "192.168.50.25:8899" {
		return nil, errors.New("unexpected target")
	}
	if dialer.responseIndex >= len(dialer.responses) {
		return nil, errors.New("unexpected extra dial")
	}
	conn := &memoryConn{responseForWrite: dialer.nextResponse}
	dialer.conns = append(dialer.conns, conn)
	return conn, nil
}

func (dialer *scriptedDialer) nextResponse() []byte {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if dialer.responseIndex >= len(dialer.responses) {
		return nil
	}
	response := bytes.Clone(dialer.responses[dialer.responseIndex])
	dialer.responseIndex++
	return response
}

func (dialer *scriptedDialer) connectionCount() int {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return len(dialer.conns)
}

func (dialer *scriptedDialer) attemptCount() int {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return dialer.attempts
}

func (dialer *scriptedDialer) responseCount() int {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return dialer.responseIndex
}

func (dialer *scriptedDialer) connection(index int) *memoryConn {
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	return dialer.conns[index]
}
