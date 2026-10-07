package modbus

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestUPSReadPlanRetiresRedundantTotalOnlyRead(t *testing.T) {
	low, ok := approvedReadSpec(loadVoltageRead)
	if !ok || low.start != 640 || low.quantity != 7 || low.sequence != 0x10 {
		t.Fatalf("UPS power/voltage spec = %#v/%v, want 640x7/0x10", low, ok)
	}
	high, ok := approvedReadSpec(upsPowerHighRead)
	if !ok || high.start != 696 || high.quantity != 4 || high.sequence != 0x1E {
		t.Fatalf("UPS power high spec = %#v/%v, want 696x4/0x1E", high, ok)
	}
	for id := readID(1); id < readID(0xFF); id++ {
		if spec, ok := approvedReadSpec(id); ok && (spec.sequence == 0x02 || (spec.start == 643 && spec.quantity == 1)) {
			t.Fatalf("retired UPS total-only request remains approved: %#v", spec)
		}
	}
	base, err := buildReadRequest(testLoggerSerial, modbusUnitID, upsPowerHighRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"FC06", func(frame []byte) { frame[v5RequestRTUOffset+1] = 6 }},
		{"FC10", func(frame []byte) { frame[v5RequestRTUOffset+1] = 0x10 }},
		{"shifted range", func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:v5RequestRTUOffset+4], 697) }},
		{"broader range", func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:v5RequestRTUOffset+6], 5) }},
		{"retired total-only range", func(frame []byte) {
			binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:v5RequestRTUOffset+4], 643)
			binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:v5RequestRTUOffset+6], 1)
			frame[5] = 0x02
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := append([]byte(nil), base...)
			tt.mutate(frame)
			rtu := frame[v5RequestRTUOffset : len(frame)-2]
			binary.LittleEndian.PutUint16(rtu[len(rtu)-2:], modbusCRC(rtu[:len(rtu)-2]))
			refreshOuterChecksum(frame)
			if validateReadRequest(frame, testLoggerSerial, modbusUnitID, upsPowerHighRead) == nil {
				t.Fatal("unapproved UPS request accepted")
			}
		})
	}
}

func TestDecodeUPSPowersAcceptsUnsignedZeroHighSubsetAndIndependentTotal(t *testing.T) {
	for _, tt := range []struct {
		name string
		lows []uint16
		want [4]float64
	}{
		{"retained UPS words plus independent earlier voltages", liveLoadVoltageWords(), [4]float64{34, 72, 295, 401}},
		{"synthetic non-summing total", []uint16{34, 72, 295, 399, 0, 0, 0}, [4]float64{34, 72, 295, 399}},
		{"synthetic zero powers ignore voltage words", []uint16{0, 0, 0, 0, 0xFFFF, 0x8000, 0x7FFF}, [4]float64{}},
		{"synthetic full unsigned low domain", []uint16{0x7FFF, 0x8000, 0xFFFF, 0xFFFF, 0, 0, 0}, [4]float64{32767, 32768, 65535, 65535}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeUPSPowers(tt.lows, []uint16{0, 0, 0, 0})
			if err != nil || got != tt.want {
				t.Fatalf("decoded = %v/%v, want %v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeUPSPowersRejectsEveryNonzeroHighAndInvalidWordCounts(t *testing.T) {
	for _, size := range []int{0, 4, 6, 8} {
		if got, err := decodeUPSPowers(make([]uint16, size), make([]uint16, 4)); err == nil || got != ([4]float64{}) {
			t.Fatalf("invalid low count %d decoded = %v/%v", size, got, err)
		}
	}
	for _, size := range []int{0, 3, 5} {
		if got, err := decodeUPSPowers(make([]uint16, 7), make([]uint16, size)); err == nil || got != ([4]float64{}) {
			t.Fatalf("invalid high count %d decoded = %v/%v", size, got, err)
		}
	}
	for index := range 4 {
		for _, high := range []uint16{1, 0x7FFF, 0x8000, 0xFFFF} {
			t.Run(fmt.Sprintf("R%d/0x%04X", 696+index, high), func(t *testing.T) {
				highs := make([]uint16, 4)
				highs[index] = high
				got, err := decodeUPSPowers(boundaryUPSLoadWords(), highs)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("register %d", 696+index)) || !strings.Contains(err.Error(), "zero-high-word unsigned domain") || got != ([4]float64{}) {
					t.Fatalf("unsupported high decoded = %v/%v", got, err)
				}
			})
		}
	}
}

func TestClientUPSPowerFailureStopsWithoutPartialSnapshotOrRetry(t *testing.T) {
	for _, id := range []readID{loadVoltageRead, upsPowerHighRead} {
		for _, mode := range []string{"exception", "EOF", "sequence", "CRC", "nonzero high", "FFFF high"} {
			if id == loadVoltageRead && (mode == "nonzero high" || mode == "FFFF high") {
				continue
			}
			t.Run(fmt.Sprintf("read%d/%s", id, mode), func(t *testing.T) {
				responses := [][]byte{
					makeReadResponse(t, deviceTypeRead, []uint16{0x0006}),
					makeReadResponse(t, capabilityRead, []uint16{0xD4C0, 0x0001, 0x0203}),
					makeReadResponse(t, generatorPortModeRead, []uint16{0}),
					makeReadResponse(t, generatorEnergyRead, make([]uint16, 4)),
					makeReadResponse(t, generatorElectricalRead, make([]uint16, 11)),
				}
				words := liveLoadVoltageWords()
				if id == upsPowerHighRead {
					responses = append(responses, makeReadResponse(t, loadVoltageRead, words))
					words = []uint16{0, 0, 0, 0}
				}
				wantReads := len(responses) + 1
				bad := makeReadResponse(t, id, words)
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
				case "nonzero high":
					bad = makeReadResponse(t, id, []uint16{0, 1, 0, 0})
					wantError = "zero-high-word unsigned domain"
				case "FFFF high":
					bad = makeReadResponse(t, id, []uint16{0, 0, 0, 0xFFFF})
					wantError = "zero-high-word unsigned domain"
				}
				responses = append(responses, bad, makeReadResponse(t, id, words))
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

func TestClientUPSPowerDefinitionsRemainIndependentFromDirectLoad(t *testing.T) {
	responses := append(responsesThroughExternalCTHigh(t),
		makeReadResponse(t, internalGridPowerLowRead, []uint16{98, 86, 148, 332}),
		makeReadResponse(t, internalGridPowerHighRead, []uint16{0, 0, 0, 0}),
	)
	// Deliberately synthetic independent total tests the entire Fetch path.
	responses[5] = makeReadResponse(t, loadVoltageRead, []uint16{34, 72, 295, 399, 2389, 2409, 2362})
	dialer := &scriptedDialer{responses: responses}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Metrics) != 94 {
		t.Fatalf("metric count = %d, want 94", len(snapshot.Metrics))
	}
	want := map[string]struct {
		group, name string
		value       float64
	}{
		"C_P_L1":    {"consumption", "Load  Power L1", 34},
		"C_P_L2":    {"consumption", "Load  Power L2", 72},
		"C_P_L3":    {"consumption", "Load  Power L3", 295},
		"UPS_P":     {"ups", "UPS Load Power", 399},
		"LPP_A":     {"consumption", "Load phase power A", 19},
		"LPP_B":     {"consumption", "Load phase power B", 34},
		"LPP_C":     {"consumption", "Load phase power C", 248},
		"E_Puse_t1": {"consumption", "Total Consumption Power", 301},
	}
	for _, metric := range snapshot.Metrics {
		definition, ok := want[metric.Key]
		if !ok {
			continue
		}
		if metric.Group != definition.group || metric.Unit != "W" || metric.Name != definition.name || metric.Value != definition.value {
			t.Fatalf("metric = %#v, want W/%#v", metric, definition)
		}
		delete(want, metric.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing distinct powers: %#v", want)
	}
}
