package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestExternalCTReadIsExactlyOneReadOnlyHighWordBlock(t *testing.T) {
	spec, ok := approvedReadSpec(externalCTPowerHighRead)
	if !ok || spec.start != 705 || spec.quantity != 4 || spec.sequence != 0x1A {
		t.Fatalf("external-CT approved spec = %#v/%v, want 705+4/0x1A", spec, ok)
	}
	base, err := buildReadRequest(testLoggerSerial, modbusUnitID, externalCTPowerHighRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "FC06", mutate: func(frame []byte) { frame[v5RequestRTUOffset+1] = 6 }},
		{name: "FC10", mutate: func(frame []byte) { frame[v5RequestRTUOffset+1] = 0x10 }},
		{name: "neighbor address", mutate: func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:v5RequestRTUOffset+4], 704) }},
		{name: "broader range", mutate: func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:v5RequestRTUOffset+6], 5) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := append([]byte(nil), base...)
			tt.mutate(frame)
			rtu := frame[v5RequestRTUOffset : len(frame)-2]
			crc := modbusCRC(rtu[:len(rtu)-2])
			binary.LittleEndian.PutUint16(rtu[len(rtu)-2:], crc)
			refreshOuterChecksum(frame)
			if err := validateReadRequest(frame, testLoggerSerial, modbusUnitID, externalCTPowerHighRead); err == nil {
				t.Fatal("mutated request accepted")
			}
		})
	}
}

func TestDecodeExternalCTDocumentedSignedDomains(t *testing.T) {
	tests := []struct {
		name string
		grid []uint16
		high []uint16
		want externalCTReadings
	}{
		{
			name: "retained positive CT capture with independent internal currents",
			// Only 613-619 were read in the 2026-10-07 CT probe. The first
			// four words below are synthetic independent internal readings.
			grid: []uint16{5000, 123, 234, 345, 57, 62, 100, 101, 98, 172, 371},
			high: []uint16{0, 0, 0, 0},
			want: externalCTReadings{currentsA: [3]float64{0.57, 0.62, 1}, powersW: [4]float64{101, 98, 172, 371}},
		},
		{
			name: "synthetic signed currents and mixed signed powers with independent total",
			grid: syntheticSignedExternalCTWords(),
			high: []uint16{0xFFFF, 0, 0xFFFF, 0xFFFF},
			want: externalCTReadings{currentsA: [3]float64{-0.01, -327.68, 327.67}, powersW: [4]float64{-10, 20, -30, -21}},
		},
		{
			name: "zero with unused internal fields ignored",
			grid: []uint16{0xFFFF, 0x8000, 0x7FFF, 0xFFFF, 0, 0, 0, 0, 0, 0, 0},
			high: []uint16{0, 0, 0, 0},
			want: externalCTReadings{},
		},
		{
			name: "symmetric power coherence boundaries do not follow inverter rating",
			grid: []uint16{5000, 0, 0, 0, 10001, 0xD8EF, 0, 0x7FFF, 0x8001, 0xFFFF, 0x7FFF},
			high: []uint16{0, 0xFFFF, 0xFFFF, 0},
			want: externalCTReadings{currentsA: [3]float64{100.01, -100.01, 0}, powersW: [4]float64{32767, -32767, -1, 32767}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeExternalCT(tt.grid, tt.high)
			if err != nil || got != tt.want {
				t.Fatalf("decodeExternalCT() = %#v/%v, want %#v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeExternalCTRejectsLengthsAndEveryIncoherentPowerPair(t *testing.T) {
	for _, size := range []int{0, 10, 12} {
		if got, err := decodeExternalCT(make([]uint16, size), make([]uint16, 4)); err == nil || got != (externalCTReadings{}) {
			t.Fatalf("grid length %d decoded to %#v/%v", size, got, err)
		}
	}
	for _, size := range []int{0, 3, 5} {
		if got, err := decodeExternalCT(make([]uint16, 11), make([]uint16, size)); err == nil || got != (externalCTReadings{}) {
			t.Fatalf("high length %d decoded to %#v/%v", size, got, err)
		}
	}
	for index := range 4 {
		for _, tt := range []struct {
			name      string
			low, high uint16
			want      string
		}{
			{name: "torn import to export", low: 0xFFFF, high: 0, want: "safe signed coherence range"},
			{name: "torn export to import", low: 1, high: 0xFFFF, want: "safe signed coherence range"},
			{name: "positive boundary exceeded", low: 0x8000, high: 0, want: "safe signed coherence range"},
			{name: "negative boundary exceeded", low: 0x8000, high: 0xFFFF, want: "safe signed coherence range"},
			{name: "noncanonical positive high", low: 1, high: 1, want: "expected signed extension"},
			{name: "noncanonical negative high", low: 0xFFFF, high: 0xFFFE, want: "expected signed extension"},
		} {
			t.Run(fmt.Sprintf("pair%d/%s", index, tt.name), func(t *testing.T) {
				grid, high := make([]uint16, 11), make([]uint16, 4)
				grid[7+index], high[index] = tt.low, tt.high
				got, err := decodeExternalCT(grid, high)
				if err == nil || !strings.Contains(err.Error(), tt.want) || got != (externalCTReadings{}) {
					t.Fatalf("decodeExternalCT() = %#v/%v, want empty result and %q", got, err, tt.want)
				}
			})
		}
	}
}

func TestClientExternalCTFailureDoesNotPublishPartialSnapshotOrRetry(t *testing.T) {
	tests := []struct {
		name     string
		response func(*testing.T) []byte
		want     string
	}{
		{name: "exception", response: func(t *testing.T) []byte { return makeExceptionResponse(t, externalCTPowerHighRead, 2) }, want: "modbus exception"},
		{name: "wrong response sequence", response: func(t *testing.T) []byte { return makeReadResponse(t, gridPowerHighRead, []uint16{0, 0, 0, 0}) }, want: "sequence"},
		{name: "EOF", response: func(*testing.T) []byte { return nil }, want: "read V5 response header"},
		{name: "noncanonical high word", response: func(t *testing.T) []byte { return makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 1, 0, 0}) }, want: "signed extension"},
		{name: "torn total sign", response: func(t *testing.T) []byte {
			return makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 0, 0, 0xFFFF})
		}, want: "safe signed coherence range"},
		{name: "bad CRC", response: func(t *testing.T) []byte {
			frame := makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 0, 0, 0})
			frame[len(frame)-3] ^= 1
			refreshOuterChecksum(frame)
			return frame
		}, want: "Modbus response CRC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := responsesThroughExternalCTLow(t)
			responses = append(responses, tt.response(t), makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 0, 0, 0}))
			dialer := &scriptedDialer{responses: responses}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if snapshot != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("snapshot/error = %#v/%v, want nil/%q", snapshot, err, tt.want)
			}
			if dialer.attemptCount() != 1 || dialer.connectionCount() != 1 || dialer.responseCount() != 25 {
				t.Fatalf("attempts/connections/responses = %d/%d/%d, want 1/1/25", dialer.attemptCount(), dialer.connectionCount(), dialer.responseCount())
			}
			conn := dialer.connection(0)
			if len(conn.writes) != 25 || conn.closeCount != 1 {
				t.Fatalf("writes/closes = %d/%d, want 25/1", len(conn.writes), conn.closeCount)
			}
		})
	}
}

func TestClientExternalCTMetricDefinitionsStayDistinctFromInternalGrid(t *testing.T) {
	responses := append(responsesThroughExternalCTLow(t),
		makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 0, 0, 0}),
		makeReadResponse(t, internalGridPowerLowRead, []uint16{98, 86, 148, 332}),
		makeReadResponse(t, internalGridPowerHighRead, []uint16{0, 0, 0, 0}),
	)
	dialer := &scriptedDialer{responses: responses}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		name, unit string
		value      float64
	}{
		"JINKO_MODBUS_R613_EXTERNAL_CT_L1_CURRENT": {"External CT Current L1", "A", 0.57},
		"JINKO_MODBUS_R614_EXTERNAL_CT_L2_CURRENT": {"External CT Current L2", "A", 0.62},
		"JINKO_MODBUS_R615_EXTERNAL_CT_L3_CURRENT": {"External CT Current L3", "A", 1},
		"CT1_P_E": {"External\u00a0CT1\u00a0Power", "W", 101},
		"CT2_P_E": {"External\u00a0CT2\u00a0Power", "W", 98},
		"CT3_P_E": {"External\u00a0CT3\u00a0Power", "W", 172},
		"CT_T_E":  {"Total\u00a0External\u00a0CT\u00a0Power", "W", 371},
		"G_C_L1":  {"Grid\u00a0Current\u00a0L1", "A", 1.23},
		"G_C_L2":  {"Grid\u00a0Current\u00a0L2", "A", 2.34},
		"G_C_L3":  {"Grid\u00a0Current\u00a0L3", "A", 3.45},
		"PG_Pt1":  {"Total Grid Power", "W", 316},
	}
	if len(snapshot.Metrics) != 91 {
		t.Fatalf("metric count = %d, want 91", len(snapshot.Metrics))
	}
	for _, metric := range snapshot.Metrics {
		definition, ok := want[metric.Key]
		if !ok {
			continue
		}
		if metric.Group != "grid" || metric.Name != definition.name || metric.Unit != definition.unit || metric.Value != definition.value {
			t.Fatalf("metric = %#v, want grid/%#v", metric, definition)
		}
		delete(want, metric.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing metric definitions: %#v", want)
	}
}

func responsesThroughExternalCTLow(t *testing.T) [][]byte {
	t.Helper()
	return append(responsesThroughEnergy(t),
		makeReadResponse(t, relayStatusRead, []uint16{1, 7}),
		makeReadResponse(t, warningFaultRead, make([]uint16, 6)),
		makeReadResponse(t, runStateRead, []uint16{2, 0, 0, 0, 0, 0}),
		// Independent synthetic 609-612 fields followed by retained CT words.
		makeReadResponse(t, gridFrequencyCurrentRead, []uint16{5000, 123, 234, 345, 57, 62, 100, 101, 98, 172, 371}),
	)
}
