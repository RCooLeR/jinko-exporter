package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestInternalGridReadsAreExactReadOnlyBlocks(t *testing.T) {
	for _, expected := range []struct {
		id       readID
		start    uint16
		sequence byte
	}{{internalGridPowerLowRead, 604, 0x1B}, {internalGridPowerHighRead, 700, 0x1C}} {
		spec, ok := approvedReadSpec(expected.id)
		if !ok || spec.start != expected.start || spec.quantity != 4 || spec.sequence != expected.sequence {
			t.Fatalf("internal-grid spec = %#v/%v, want start=%d quantity=4 sequence=%d", spec, ok, expected.start, expected.sequence)
		}
		base, err := buildReadRequest(testLoggerSerial, modbusUnitID, expected.id)
		if err != nil {
			t.Fatal(err)
		}
		for _, mutation := range []struct {
			name   string
			mutate func([]byte)
		}{
			{"FC06", func(frame []byte) { frame[v5RequestRTUOffset+1] = 6 }},
			{"FC10", func(frame []byte) { frame[v5RequestRTUOffset+1] = 0x10 }},
			{"shifted range", func(frame []byte) {
				binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:v5RequestRTUOffset+4], expected.start+1)
			}},
			{"broader range", func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:v5RequestRTUOffset+6], 5) }},
		} {
			t.Run(fmt.Sprintf("read%d/%s", expected.start, mutation.name), func(t *testing.T) {
				frame := append([]byte(nil), base...)
				mutation.mutate(frame)
				rtu := frame[v5RequestRTUOffset : len(frame)-2]
				binary.LittleEndian.PutUint16(rtu[len(rtu)-2:], modbusCRC(rtu[:len(rtu)-2]))
				refreshOuterChecksum(frame)
				if err := validateReadRequest(frame, testLoggerSerial, modbusUnitID, expected.id); err == nil {
					t.Fatal("mutated internal-grid request accepted")
				}
			})
		}
	}
}

func TestDecodeInternalGridPowersSignedDomainsAndIndependentTotal(t *testing.T) {
	for _, tt := range []struct {
		name      string
		low, high []uint16
		want      [4]float64
	}{
		{"retained positive 2026-10-07 capture", []uint16{98, 86, 148, 332}, []uint16{0, 0, 0, 0}, [4]float64{98, 86, 148, 332}},
		{"synthetic mixed signed and independent total", []uint16{0xFFF6, 20, 0xFFE2, 0xFFEB}, []uint16{0xFFFF, 0, 0xFFFF, 0xFFFF}, [4]float64{-10, 20, -30, -21}},
		{"synthetic zero", []uint16{0, 0, 0, 0}, []uint16{0, 0, 0, 0}, [4]float64{}},
		{"synthetic coherence edges above rated inverter power", []uint16{0x7FFF, 0x8001, 0xFFFF, 0x8001}, []uint16{0, 0xFFFF, 0xFFFF, 0xFFFF}, [4]float64{32767, -32767, -1, -32767}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeInternalGridPowers(tt.low, tt.high)
			if err != nil || got != tt.want {
				t.Fatalf("decoded = %v/%v, want %v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeInternalGridPowersRejectsLengthsAndEveryIncoherentPair(t *testing.T) {
	for _, size := range []int{0, 3, 5} {
		for _, words := range [][2][]uint16{{make([]uint16, size), make([]uint16, 4)}, {make([]uint16, 4), make([]uint16, size)}} {
			if got, err := decodeInternalGridPowers(words[0], words[1]); err == nil || got != ([4]float64{}) {
				t.Fatalf("invalid count decoded = %v/%v", got, err)
			}
		}
	}
	for index := range 4 {
		for _, tt := range []struct {
			name      string
			low, high uint16
			want      string
		}{
			{"torn import to export", 0xFFFF, 0, "safe signed coherence range"},
			{"torn export to import", 1, 0xFFFF, "safe signed coherence range"},
			{"positive boundary exceeded", 0x8000, 0, "safe signed coherence range"},
			{"negative boundary exceeded", 0x8000, 0xFFFF, "safe signed coherence range"},
			{"noncanonical positive high", 1, 1, "expected signed extension"},
			{"noncanonical negative high", 0xFFFF, 0xFFFE, "expected signed extension"},
		} {
			t.Run(fmt.Sprintf("pair%d/%s", index, tt.name), func(t *testing.T) {
				low, high := []uint16{98, 86, 148, 332}, make([]uint16, 4)
				low[index], high[index] = tt.low, tt.high
				got, err := decodeInternalGridPowers(low, high)
				if err == nil || !strings.Contains(err.Error(), tt.want) || got != ([4]float64{}) {
					t.Fatalf("decoded = %v/%v, want empty result and %q", got, err, tt.want)
				}
			})
		}
	}
}

func TestClientInternalGridFailureIsTerminalWithNoPartialSnapshot(t *testing.T) {
	for _, id := range []readID{internalGridPowerLowRead, internalGridPowerHighRead} {
		for _, mode := range []string{"exception", "EOF", "sequence", "CRC", "noncanonical high", "torn total"} {
			if id == internalGridPowerLowRead && (mode == "noncanonical high" || mode == "torn total") {
				continue
			}
			t.Run(fmt.Sprintf("read%d/%s", id, mode), func(t *testing.T) {
				responses := responsesThroughExternalCTHigh(t)
				if id == internalGridPowerHighRead {
					responses = append(responses, makeReadResponse(t, internalGridPowerLowRead, []uint16{98, 86, 148, 332}))
				}
				wantReads := len(responses) + 1
				bad := makeReadResponse(t, id, []uint16{0, 0, 0, 0})
				wantError := mode
				switch mode {
				case "exception":
					bad = makeExceptionResponse(t, id, 2)
				case "EOF":
					bad = nil
					wantError = "read V5 response header"
				case "sequence":
					bad[5] ^= 1
					refreshOuterChecksum(bad)
				case "CRC":
					bad[len(bad)-3] ^= 1
					refreshOuterChecksum(bad)
				case "noncanonical high":
					bad = makeReadResponse(t, id, []uint16{0, 1, 0, 0})
					wantError = "signed extension"
				case "torn total":
					bad = makeReadResponse(t, id, []uint16{0, 0, 0, 0xFFFF})
					wantError = "safe signed coherence range"
				}
				responses = append(responses, bad, makeReadResponse(t, id, []uint16{0, 0, 0, 0}))
				dialer := &scriptedDialer{responses: responses}
				client, err := newClient(testModbusConfig(), dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := client.Fetch(context.Background())
				if snapshot != nil || err == nil || !strings.Contains(err.Error(), wantError) {
					t.Fatalf("snapshot/error = %#v/%v, want nil/%q", snapshot, err, wantError)
				}
				if dialer.attemptCount() != 1 || dialer.connectionCount() != 1 || dialer.responseCount() != wantReads {
					t.Fatalf("attempts/connections/responses = %d/%d/%d, want 1/1/%d", dialer.attemptCount(), dialer.connectionCount(), dialer.responseCount(), wantReads)
				}
				conn := dialer.connection(0)
				if len(conn.writes) != wantReads || conn.closeCount != 1 {
					t.Fatalf("writes/closes = %d/%d, want %d/1", len(conn.writes), conn.closeCount, wantReads)
				}
			})
		}
	}
}

func TestClientInternalGridPowersRemainDistinctFromSelectedGridAndExternalCT(t *testing.T) {
	responses := append(responsesThroughExternalCTHigh(t),
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
	if len(snapshot.Metrics) != 91 {
		t.Fatalf("metric count = %d, want 91", len(snapshot.Metrics))
	}
	want := map[string]struct {
		name  string
		value float64
	}{
		"GS_A":    {"Internal L1 Power", 98},
		"GS_B":    {"Internal L2 Power", 86},
		"GS_C":    {"Internal L3 Power", 148},
		"GS_T":    {"Internal Power", 332},
		"G_P_L1":  {"Grid Power L1", 101},
		"G_P_L2":  {"Grid Power L2", 98},
		"G_P_L3":  {"Grid Power L3", 117},
		"PG_Pt1":  {"Total Grid Power", 316},
		"CT1_P_E": {"External\u00a0CT1\u00a0Power", 101},
		"CT2_P_E": {"External\u00a0CT2\u00a0Power", 98},
		"CT3_P_E": {"External\u00a0CT3\u00a0Power", 172},
		"CT_T_E":  {"Total\u00a0External\u00a0CT\u00a0Power", 371},
	}
	for _, metric := range snapshot.Metrics {
		definition, ok := want[metric.Key]
		if !ok {
			continue
		}
		if metric.Group != "grid" || metric.Unit != "W" || metric.Name != definition.name || metric.Value != definition.value {
			t.Fatalf("metric = %#v, want grid/W/%#v", metric, definition)
		}
		delete(want, metric.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing distinct powers: %#v", want)
	}
}

func responsesThroughExternalCTHigh(t *testing.T) [][]byte {
	t.Helper()
	return append(responsesThroughExternalCTLow(t), makeReadResponse(t, externalCTPowerHighRead, []uint16{0, 0, 0, 0}))
}
