package modbus

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Register words from the complete 2026-10-08T06:01:07Z target capture.
// No logger or device identity is retained in this decoder fixture.
func capturedBMSMirrorWords() []uint16 {
	return []uint16{0x1950, 0, 0, 0x0016, 0x0064, 0x1808, 1, 0x04C4, 0}
}

func TestBMSReadExactIndependentWireVectors(t *testing.T) {
	// Independently assembled with a separate JavaScript CRC/checksum routine.
	// The response uses captured words inside a wholly synthetic V5 envelope.
	const tx = "a517001045230078563412020000000000000000000000000000010300d2000925f59e15"
	const rx = "a5250010152357785634120201000000000000000000000000010312195000000000001600641808000104c400003d251f15"
	spec, ok := approvedReadSpec(bmsMirrorsRead)
	if !ok || spec.start != 210 || spec.quantity != 9 || spec.sequence != 0x23 {
		t.Fatalf("BMS read spec = %#v/%v", spec, ok)
	}
	request, err := buildReadRequest(testLoggerSerial, modbusUnitID, bmsMirrorsRead)
	if err != nil || hex.EncodeToString(request) != tx {
		t.Fatalf("BMS request = %x/%v, want %s", request, err, tx)
	}
	response, err := hex.DecodeString(rx)
	if err != nil {
		t.Fatal(err)
	}
	words, err := parseReadResponse(response, testLoggerSerial, modbusUnitID, bmsMirrorsRead)
	if err != nil || !reflect.DeepEqual(words, capturedBMSMirrorWords()) {
		t.Fatalf("BMS response = %v/%v", words, err)
	}
	for offset := range request {
		bad := bytes.Clone(request)
		bad[offset] ^= 1
		if validateReadRequest(bad, testLoggerSerial, modbusUnitID, bmsMirrorsRead) == nil {
			t.Fatalf("accepted request byte %d mutation", offset)
		}
	}
	for _, mutate := range []func([]byte){
		func(f []byte) { f[27] = 6 },
		func(f []byte) { f[27] = 0x10 },
		func(f []byte) { binary.BigEndian.PutUint16(f[28:30], 209) },
		func(f []byte) { binary.BigEndian.PutUint16(f[30:32], 10) },
	} {
		bad := bytes.Clone(request)
		mutate(bad)
		binary.LittleEndian.PutUint16(bad[32:34], modbusCRC(bad[26:32]))
		refreshOuterChecksum(bad)
		if validateReadRequest(bad, testLoggerSerial, modbusUnitID, bmsMirrorsRead) == nil {
			t.Fatal("accepted write function or altered range with valid checksums")
		}
	}
}

func TestDecodeBMSCaptureAndUninterpretedWords(t *testing.T) {
	want := []metricValue{
		{key: "BMS_C_V", value: 648}, {key: "BMS_D_C_L", value: 22},
		{key: "BMS_SOC", value: 100}, {key: "BMS_B_V1", value: 615.2}, {key: "BMST", value: 22},
	}
	for _, ignoredValue := range []uint16{0, 1, 0x7FFF, 0x8000, 0xFFFF} {
		words := capturedBMSMirrorWords()
		for _, index := range []int{1, 2, 6, 8} {
			words[index] = ignoredValue
		}
		got, err := decodeBMSMirrors(words)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("ignored value %04X: %v/%v, want %v", ignoredValue, got, err, want)
		}
	}
}

func TestDecodeBMSAcceptedBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		words []uint16
		want  []metricValue
	}{
		{"lower boundaries", make([]uint16, 9), []metricValue{
			{"BMS_C_V", 0}, {"BMS_D_C_L", 0}, {"BMS_SOC", 0}, {"BMS_B_V1", 0}, {"BMST", -100},
		}},
		{"upper boundaries and full U16 discharge limit", []uint16{10000, 0xFFFF, 0xFFFF, 0xFFFF, 100, 10000, 0xFFFF, 2000, 0xFFFF}, []metricValue{
			{"BMS_C_V", 1000}, {"BMS_D_C_L", 65535}, {"BMS_SOC", 100}, {"BMS_B_V1", 1000}, {"BMST", 100},
		}},
		{"fractional values and negative offset temperature", []uint16{6481, 0, 0, 32768, 49, 6153, 0, 999, 0}, []metricValue{
			{"BMS_C_V", 648.1}, {"BMS_D_C_L", 32768}, {"BMS_SOC", 49}, {"BMS_B_V1", 615.3}, {"BMST", -0.1},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeBMSMirrors(tt.words)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("decoded = %v/%v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestDecodeBMSRejectsUnsupportedPublishedValuesWithoutPartialMetrics(t *testing.T) {
	for _, count := range []int{0, 8, 10} {
		if got, err := decodeBMSMirrors(make([]uint16, count)); err == nil || got != nil {
			t.Fatalf("word count %d = %v/%v", count, got, err)
		}
	}
	for _, mutation := range []struct {
		index int
		raw   uint16
	}{
		{0, 10001}, {0, 0xFFFF}, {4, 101}, {4, 0xFFFF},
		{5, 10001}, {5, 0xFFFF}, {7, 2001}, {7, 0xFFFF},
	} {
		words := capturedBMSMirrorWords()
		words[mutation.index] = mutation.raw
		if got, err := decodeBMSMirrors(words); err == nil || got != nil {
			t.Fatalf("R%d=%d = %v/%v", 210+mutation.index, mutation.raw, got, err)
		}
	}
}

func TestClientBMSMirrorsAreDistinctUncachedAndKeepExactBudgets(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("metadata=%t", metadata), func(t *testing.T) {
			firstResponses := baselineMetadataTestResponses(t)
			if metadata {
				firstResponses = enabledMetadataTestResponses(t)
			}
			secondResponses := baselineMetadataTestResponses(t)[2:]
			secondWords := []uint16{6401, 0xFFFF, 0xFFFF, 31, 42, 6201, 0xFFFF, 1205, 0xFFFF}
			secondResponses[bmsPlanIndex(t, false, metadata)] = makeReadResponse(t, bmsMirrorsRead, secondWords)
			dialer := &scriptedDialer{responses: append(firstResponses, secondResponses...)}
			cfg := testModbusConfig()
			cfg.MetadataEnabled = metadata
			client, err := newClient(cfg, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			for fetch := range 2 {
				snapshot, err := client.Fetch(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.Metrics) != 97 {
					t.Fatalf("metrics = %d, want 97", len(snapshot.Metrics))
				}
				want := map[string]float64{"B_T1": 28, "B_V1": 601.7, "B_left_cap1": 100, "B_C1": 0.16,
					"BMS_C_V": 648, "BMS_D_C_L": 22, "BMS_SOC": 100, "BMS_B_V1": 615.2, "BMST": 22}
				if fetch == 1 {
					want["BMS_C_V"], want["BMS_D_C_L"], want["BMS_SOC"], want["BMS_B_V1"], want["BMST"] = 640.1, 31, 42, 620.1, 20.5
				}
				seen := map[string]bool{}
				for _, metric := range snapshot.Metrics {
					if seen[metric.Key] {
						t.Fatalf("duplicate key %s", metric.Key)
					}
					seen[metric.Key] = true
					if value, ok := want[metric.Key]; ok {
						if metric.Value != value {
							t.Fatalf("%s = %v, want %v", metric.Key, metric.Value, value)
						}
						delete(want, metric.Key)
					}
					if metric.Group == "bms" {
						canonical, err := canonicalMetric(metric.Key, metric.Value)
						if err != nil || metric != canonical {
							t.Fatalf("noncanonical BMS metric %#v", metric)
						}
					}
				}
				if len(want) != 0 {
					t.Fatalf("missing fields: %v", want)
				}
				for _, key := range []string{"BMS_D_V", "BMS_C_C_L", "BMS_B_C1", "BMS_CC1", "BMS_DC1"} {
					if seen[key] {
						t.Fatalf("unvalidated field %s published", key)
					}
				}
				wantReads := 26
				if fetch == 0 {
					wantReads = 28
					if metadata {
						wantReads = 32
					}
				}
				conn := dialer.connection(fetch)
				if len(conn.writes) != wantReads || conn.closeCount != 1 || conn.deadlineCalls != 1 {
					t.Fatalf("fetch%d writes/closes/deadlines = %d/%d/%d", fetch, len(conn.writes), conn.closeCount, conn.deadlineCalls)
				}
			}
			if dialer.attemptCount() != 2 {
				t.Fatalf("dial attempts = %d", dialer.attemptCount())
			}
		})
	}
}

func TestClientBMSFailureStopsWithoutPartialSnapshot(t *testing.T) {
	for _, mode := range []string{"exception", "EOF", "short body", "sequence", "CRC", "word count", "charge voltage", "SOC", "voltage", "temperature"} {
		t.Run(mode, func(t *testing.T) {
			responses := baselineMetadataTestResponses(t)
			index := bmsPlanIndex(t, true, false)
			bad := bytes.Clone(responses[index])
			wantError := mode
			switch mode {
			case "exception":
				bad = makeExceptionResponse(t, bmsMirrorsRead, 2)
			case "EOF":
				bad = nil
				wantError = "response header"
			case "short body":
				bad = bad[:12]
				wantError = "unexpected EOF"
			case "sequence":
				bad[5] ^= 1
				refreshOuterChecksum(bad)
			case "CRC":
				bad[len(bad)-3] ^= 1
				refreshOuterChecksum(bad)
			case "word count":
				bad = makeReadResponseForFuzz(bmsMirrorsRead, make([]uint16, 8))
				wantError = "response length"
			default:
				words := capturedBMSMirrorWords()
				switch mode {
				case "charge voltage":
					words[0] = 10001
				case "SOC":
					words[4] = 101
				case "voltage":
					words[5] = 10001
				case "temperature":
					words[7] = 2001
				}
				bad = makeReadResponse(t, bmsMirrorsRead, words)
			}
			responses[index] = bad
			dialer := &scriptedDialer{responses: responses}
			client, err := newClient(testModbusConfig(), dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if snapshot != nil || err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("snapshot/error = %v/%v, want nil/%s", snapshot, err, wantError)
			}
			conn := dialer.connection(0)
			if dialer.attemptCount() != 1 || dialer.responseCount() != index+1 || len(conn.writes) != index+1 || conn.closeCount != 1 {
				t.Fatalf("failure continued: attempts/responses/writes/closes = %d/%d/%d/%d", dialer.attemptCount(), dialer.responseCount(), len(conn.writes), conn.closeCount)
			}
		})
	}
}

func TestClientBMSUninterpretedWordsDoNotChangeSnapshotMetrics(t *testing.T) {
	responses := baselineMetadataTestResponses(t)
	second := baselineMetadataTestResponses(t)[2:]
	words := capturedBMSMirrorWords()
	words[1], words[2], words[6], words[8] = 0xFFFF, 1, 0xFFFF, 32768
	second[bmsPlanIndex(t, false, false)] = makeReadResponse(t, bmsMirrorsRead, words)
	dialer := &scriptedDialer{responses: append(responses, second...)}
	client, err := newClient(testModbusConfig(), dialer.DialContext)
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, err := client.Fetch(context.Background())
	if err != nil || after == nil {
		t.Fatalf("ignored nonzero/current FFFF rejected the snapshot: %v", err)
	}
	if !reflect.DeepEqual(before.Metrics, after.Metrics) {
		t.Fatal("uninterpreted BMS words changed published metrics")
	}
}

func TestClientBMSCancellationClosesBlockedReadWithoutNextRequest(t *testing.T) {
	responses := baselineMetadataTestResponses(t)
	index := bmsPlanIndex(t, true, false)
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	conn := &countingCloseConn{Conn: clientSide}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dials := 0
	client, err := newClient(testModbusConfig(), func(context.Context, string, string) (net.Conn, error) { dials++; return conn, nil })
	if err != nil {
		t.Fatal(err)
	}
	serverDone := make(chan error, 1)
	go func() {
		for step := 0; step <= index; step++ {
			request := make([]byte, v5RequestLength)
			if _, err := io.ReadFull(serverSide, request); err != nil {
				serverDone <- err
				return
			}
			if step == index {
				cancel()
				if _, err := serverSide.Read(make([]byte, 1)); err != io.EOF {
					serverDone <- fmt.Errorf("request after cancellation: %v", err)
					return
				}
				serverDone <- nil
				return
			}
			if _, err := serverSide.Write(responses[step]); err != nil {
				serverDone <- err
				return
			}
		}
	}()
	snapshot, err := client.Fetch(ctx)
	if snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot/error = %v/%v", snapshot, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if dials != 1 || conn.closeCount() != 1 {
		t.Fatalf("dials/closes = %d/%d", dials, conn.closeCount())
	}
}

func bmsPlanIndex(t *testing.T, first, metadata bool) int {
	t.Helper()
	for index, id := range fetchReadPlan(first, metadata) {
		if id == bmsMirrorsRead {
			return index
		}
	}
	t.Fatal("missing BMS read")
	return 0
}
