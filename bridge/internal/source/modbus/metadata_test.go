package modbus

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"testing"
)

// Synthetic vectors were assembled independently of the production frame
// builder/parser. They are not captures or proof of target firmware support.
var metadataVectors = []struct {
	id              readID
	start, quantity uint16
	sequence        byte
	tx, rx          string
	words           []uint16
}{
	{protocolVersionRead, 2, 1, 0x1F,
		"a5170010451f007856341202000000000000000000000000000001030002000125ca9715",
		"a5150010151f577856341202010000000000000000000000000103020104b817a115", []uint16{0x0104}},
	{controllerVersionsRead, 11, 1, 0x20,
		"a5170010452000785634120200000000000000000000000000000103000b0001f5c86f15",
		"a5150010152057785634120201000000000000000000000000010302ab0cc771bd15", []uint16{0xAB0C}},
	{firmwareWordsRead, 14, 5, 0x21,
		"a5170010452100785634120200000000000000000000000000000103000e0005e40aa815",
		"a51d001015215778563412020100000000000000000000000001030a300110870000200220172c133f15", []uint16{0x3001, 0x1087, 0, 0x2002, 0x2017}},
	{batteryInputCodeRead, 24, 1, 0x22,
		"a517001045220078563412020000000000000000000000000000010300180001040dd215",
		"a51500101522577856341202010000000000000000000000000103020000b844cc15", []uint16{0}},
}

func TestMetadataExactReadOnlyVectors(t *testing.T) {
	for _, vector := range metadataVectors {
		t.Run(fmt.Sprintf("R%d", vector.start), func(t *testing.T) {
			spec, ok := approvedReadSpec(vector.id)
			if !ok || spec.start != vector.start || spec.quantity != vector.quantity || spec.sequence != vector.sequence {
				t.Fatalf("unexpected read spec: %#v", spec)
			}
			request, err := buildReadRequest(testLoggerSerial, modbusUnitID, vector.id)
			if err != nil || hex.EncodeToString(request) != vector.tx {
				t.Fatalf("request = %x / %v, want %s", request, err, vector.tx)
			}
			response, err := hex.DecodeString(vector.rx)
			if err != nil {
				t.Fatal(err)
			}
			words, err := parseReadResponse(response, testLoggerSerial, modbusUnitID, vector.id)
			if err != nil || !reflect.DeepEqual(words, vector.words) {
				t.Fatalf("response = %v / %v, want %v", words, err, vector.words)
			}
			for index := range request {
				mutated := append([]byte(nil), request...)
				mutated[index] ^= 1
				if validateReadRequest(mutated, testLoggerSerial, modbusUnitID, vector.id) == nil {
					t.Fatalf("accepted request mutation at byte %d", index)
				}
			}
			for _, mutation := range []func([]byte){
				func(frame []byte) { frame[v5RequestRTUOffset+1] = 6 },
				func(frame []byte) { frame[v5RequestRTUOffset+1] = 0x10 },
				func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:], vector.start+1) },
				func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:], vector.quantity+1) },
			} {
				mutated := append([]byte(nil), request...)
				mutation(mutated)
				rtu := mutated[v5RequestRTUOffset : len(mutated)-2]
				binary.LittleEndian.PutUint16(rtu[len(rtu)-2:], modbusCRC(rtu[:len(rtu)-2]))
				refreshOuterChecksum(mutated)
				if validateReadRequest(mutated, testLoggerSerial, modbusUnitID, vector.id) == nil {
					t.Fatal("accepted write/expanded range with valid checksums")
				}
			}
		})
	}
}

func expectedRawMetadata() map[string]string {
	return map[string]string{
		"protocol_version_hex":         "0104",
		"control_firmware_words":       "3001-1087",
		"communication_firmware_words": "0000-2002-2017",
		"bootloader_version_hex":       "0C",
		"assistant_version_hex":        "AB",
		"battery_input_code":           "0",
	}
}

func TestDecodeMetadataPreservesExactRawValues(t *testing.T) {
	got, err := decodeMetadata(metadataVectors[0].words, metadataVectors[1].words, metadataVectors[2].words, metadataVectors[3].words)
	if err != nil || !reflect.DeepEqual(got, expectedRawMetadata()) {
		t.Fatalf("metadata = %v / %v", got, err)
	}
	for code := uint16(0); code <= 4; code++ {
		got, err := decodeMetadata([]uint16{0xFFFF}, []uint16{0x0001}, []uint16{0, 0xFFFF, 0xABCD, 1, 0}, []uint16{code})
		want := map[string]string{
			"protocol_version_hex": "FFFF", "control_firmware_words": "0000-FFFF",
			"communication_firmware_words": "ABCD-0001-0000", "bootloader_version_hex": "01",
			"assistant_version_hex": "00", "battery_input_code": fmt.Sprint(code),
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("code %d: %v / %v", code, got, err)
		}
	}
}

func TestDecodeMetadataRejectsIncompleteOrUnsupportedVector(t *testing.T) {
	for block := range metadataVectors {
		for _, lengthDelta := range []int{-1, 1} {
			inputs := [4][]uint16{}
			for index, vector := range metadataVectors {
				inputs[index] = vector.words
			}
			inputs[block] = make([]uint16, len(inputs[block])+lengthDelta)
			got, err := decodeMetadata(inputs[0], inputs[1], inputs[2], inputs[3])
			if err == nil || got != nil {
				t.Fatalf("block %d delta %d: %v / %v", block, lengthDelta, got, err)
			}
		}
	}
	for _, code := range []uint16{5, 255, 0xFFFF} {
		got, err := decodeMetadata([]uint16{0}, []uint16{0}, make([]uint16, 5), []uint16{code})
		if err == nil || got != nil {
			t.Fatalf("accepted unsupported code %d: %v / %v", code, got, err)
		}
	}
}

func TestMetadataPlanHonorsOptionAndCache(t *testing.T) {
	baseline := fetchReadPlan(true, false)
	steady := fetchReadPlan(false, false)
	optIn := fetchReadPlan(true, true)
	if len(baseline) != 28 || len(steady) != 26 || len(optIn) != 32 {
		t.Fatal("wrong read budgets")
	}
	if !reflect.DeepEqual(steady, fetchReadPlan(false, true)) || !reflect.DeepEqual(optIn[:2], baseline[:2]) || !reflect.DeepEqual(optIn[6:], baseline[2:]) {
		t.Fatal("metadata changed the gate order or existing telemetry plan")
	}
	for index, vector := range metadataVectors {
		if optIn[index+2] != vector.id {
			t.Fatal("metadata read order changed")
		}
	}
	sequences := map[byte]bool{}
	for _, id := range optIn {
		spec, ok := approvedReadSpec(id)
		if !ok || sequences[spec.sequence] {
			t.Fatalf("invalid/duplicate sequence for %d", id)
		}
		sequences[spec.sequence] = true
	}
}

func baselineMetadataTestResponses(t *testing.T) [][]byte {
	t.Helper()
	return append(responsesThroughExternalCTHigh(t),
		makeReadResponse(t, internalGridPowerLowRead, []uint16{98, 86, 148, 332}),
		makeReadResponse(t, internalGridPowerHighRead, []uint16{0, 0, 0, 0}))
}

func enabledMetadataTestResponses(t *testing.T) [][]byte {
	t.Helper()
	baseline := baselineMetadataTestResponses(t)
	result := append([][]byte(nil), baseline[:2]...)
	for _, vector := range metadataVectors {
		frame, err := hex.DecodeString(vector.rx)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, frame)
	}
	return append(result, baseline[2:]...)
}

func TestClientMetadataIsTextOnlyCachedAndSnapshotOwned(t *testing.T) {
	cfg := testModbusConfig()
	baselineDialer := &scriptedDialer{responses: baselineMetadataTestResponses(t)}
	baselineClient, err := newClient(cfg, baselineDialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := baselineClient.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Meta) != 7 || baselineClient.metadata != nil {
		t.Fatal("metadata enabled when explicitly disabled")
	}
	cfg.MetadataEnabled = true
	responses := append(enabledMetadataTestResponses(t), baselineMetadataTestResponses(t)[2:]...)
	dialer := &scriptedDialer{responses: responses}
	client, err := newClient(cfg, dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	for fetch := range 2 {
		snapshot, err := client.Fetch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Metrics) != 97 || !reflect.DeepEqual(snapshot.Metrics, baseline.Metrics) {
			t.Fatal("metadata altered numeric telemetry")
		}
		if len(snapshot.Meta) != 13 {
			t.Fatalf("metadata field count = %d", len(snapshot.Meta))
		}
		for key, want := range expectedRawMetadata() {
			if snapshot.Meta[key] != want {
				t.Fatalf("Fetch %d: %s = %q, want %q", fetch, key, snapshot.Meta[key], want)
			}
			snapshot.Meta[key] = "caller mutation"
		}
		snapshot.Meta["unexpected"] = "caller mutation"
		conn := dialer.connection(fetch)
		wantReads := 32
		if fetch > 0 {
			wantReads = 26
		}
		if len(conn.writes) != wantReads || conn.closeCount != 1 || conn.deadlineCalls != 1 || conn.deadline.IsZero() {
			t.Fatalf("Fetch %d: writes=%d closes=%d deadlines=%d", fetch, len(conn.writes), conn.closeCount, conn.deadlineCalls)
		}
	}
	if dialer.attemptCount() != 2 || dialer.responseCount() != 58 {
		t.Fatal("extra dial/read or missing metadata cache")
	}
}

func TestClientMetadataFailureStopsAndDoesNotPartiallyCache(t *testing.T) {
	for index, vector := range metadataVectors {
		for _, kind := range []string{"exception", "eof", "checksum", "wrong sequence", "wrong word count"} {
			t.Run(fmt.Sprintf("R%d/%s", vector.start, kind), func(t *testing.T) {
				good := enabledMetadataTestResponses(t)
				bad := append([]byte(nil), good[index+2]...)
				switch kind {
				case "exception":
					bad = makeExceptionResponse(t, vector.id, 2)
				case "eof":
					bad = nil
				case "checksum":
					bad[len(bad)-4] ^= 1
					refreshOuterChecksum(bad)
				case "wrong sequence":
					bad[5]++
					refreshOuterChecksum(bad)
				case "wrong word count":
					bad = makeReadResponseForFuzz(vector.id, make([]uint16, int(vector.quantity)+1))
				}
				responses := append([][]byte(nil), good[:index+2]...)
				responses = append(responses, bad)
				responses = append(responses, good...) // only the next explicit poll may consume these
				dialer := &scriptedDialer{responses: responses}
				cfg := testModbusConfig()
				cfg.MetadataEnabled = true
				client, err := newClient(cfg, dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := client.Fetch(context.Background())
				if err == nil || snapshot != nil || client.capabilities != nil || client.metadata != nil {
					t.Fatalf("partial snapshot/cache after error: %v / %v", snapshot, err)
				}
				conn := dialer.connection(0)
				if dialer.attemptCount() != 1 || dialer.responseCount() != index+3 || len(conn.writes) != index+3 || conn.closeCount != 1 {
					t.Fatal("failed acquisition retried or continued after error")
				}
				if snapshot, err = client.Fetch(context.Background()); err != nil || snapshot == nil {
					t.Fatalf("next poll failed: %v", err)
				}
				if len(dialer.connection(1).writes) != 32 || !reflect.DeepEqual(client.metadata, expectedRawMetadata()) {
					t.Fatal("next poll used partial cache")
				}
			})
		}
	}
}

func TestClientMetadataUnsupportedBatteryCodeLeavesNoCache(t *testing.T) {
	responses := enabledMetadataTestResponses(t)
	responses[5] = makeReadResponse(t, batteryInputCodeRead, []uint16{0xFFFF})
	dialer := &scriptedDialer{responses: responses}
	cfg := testModbusConfig()
	cfg.MetadataEnabled = true
	client, err := newClient(cfg, dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(context.Background())
	if err == nil || snapshot != nil || client.capabilities != nil || client.metadata != nil || dialer.responseCount() != 6 || dialer.attemptCount() != 1 {
		t.Fatalf("unsupported code accepted, cached or retried: %v / %v", snapshot, err)
	}
}

func TestClientMetadataCancellationLeavesNoCache(t *testing.T) {
	for cancelAt := 3; cancelAt <= 6; cancelAt++ {
		t.Run(fmt.Sprint(cancelAt), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			responses := enabledMetadataTestResponses(t)
			index := 0
			conn := &memoryConn{responseForWrite: func() []byte {
				response := responses[index]
				index++
				if index == cancelAt {
					cancel()
				}
				return response
			}}
			cfg := testModbusConfig()
			cfg.MetadataEnabled = true
			client, err := newClient(cfg, func(context.Context, string, string) (net.Conn, error) { return conn, nil })
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(ctx)
			if err == nil || snapshot != nil || client.capabilities != nil || client.metadata != nil || len(conn.writes) != cancelAt {
				t.Fatalf("cancellation committed/continued acquisition: %v / %v, writes %d", snapshot, err, len(conn.writes))
			}
		})
	}
}
