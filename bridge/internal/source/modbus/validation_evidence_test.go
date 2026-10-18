package modbus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
)

func TestValidationEvidenceOptInDoesNotChangeMetricsMetadataOrRequests(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("metadata=%t", metadata), func(t *testing.T) {
			var disabledSnapshots [2]*model.Snapshot
			var disabledRequests [2][][]byte
			for _, enabled := range []bool{false, true} {
				cfg := testModbusConfig()
				cfg.MetadataEnabled = metadata
				cfg.ValidationEvidenceEnabled = enabled
				firstResponses := validationEvidenceResponses(t, metadata)
				secondResponses := validationEvidenceResponses(t, false)[2:]
				dialer := &scriptedDialer{responses: append(firstResponses, secondResponses...)}
				client, err := newClient(cfg, dialer.DialContext)
				if err != nil {
					t.Fatal(err)
				}
				var firstEvidence *model.ModbusEvidence
				for fetch := range 2 {
					before := time.Now().UTC()
					snapshot, err := client.Fetch(context.Background())
					after := time.Now().UTC()
					if err != nil || snapshot == nil {
						t.Fatalf("Fetch(): %v / %v", snapshot, err)
					}
					conn := dialer.connection(fetch)
					wantReads := 26
					if fetch == 0 {
						wantReads = 28
						if metadata {
							wantReads = 32
						}
					}
					if len(snapshot.Metrics) != 97 || len(conn.writes) != wantReads || conn.closeCount != 1 || conn.deadlineCalls != 1 {
						t.Fatalf("enabled=%t fetch=%d metrics/reads/closes/deadlines=%d/%d/%d/%d", enabled, fetch, len(snapshot.Metrics), len(conn.writes), conn.closeCount, conn.deadlineCalls)
					}
					if !enabled {
						if snapshot.ModbusEvidence != nil {
							t.Fatal("disabled diagnostic produced a sidecar")
						}
						disabledSnapshots[fetch] = snapshot
						disabledRequests[fetch] = conn.writes
						continue
					}
					if !reflect.DeepEqual(conn.writes, disabledRequests[fetch]) {
						t.Fatal("opt-in changed the exact approved request bytes/order")
					}
					withoutEvidence := *snapshot
					withoutEvidence.ModbusEvidence = nil
					withoutEvidence.CollectedAt = disabledSnapshots[fetch].CollectedAt
					if !reflect.DeepEqual(&withoutEvidence, disabledSnapshots[fetch]) {
						t.Fatal("opt-in changed metrics, metadata, identity, or source")
					}
					assertValidationEvidence(t, snapshot, before, after)
					assertEvidenceExcludedFromSnapshotJSON(t, snapshot)
					if fetch == 0 {
						firstEvidence = snapshot.ModbusEvidence
						// Caller changes must not alter later captures or cached data.
						firstEvidence.BMSWords[1] = 42
						firstEvidence.RelayWords[0] = 42
						firstEvidence.AlertWords[0] = 42
						firstEvidence.RunState = 42
						firstEvidence.BMSWindow.StartedAt = time.Time{}
					} else if snapshot.ModbusEvidence == firstEvidence {
						t.Fatal("successive snapshots share the same evidence instance")
					}
				}
				wantResponses := 54
				if metadata {
					wantResponses = 58
				}
				if dialer.attemptCount() != 2 || dialer.responseCount() != wantResponses {
					t.Fatal("validation evidence added a connection or response")
				}
			}
		})
	}
}

func TestValidationEvidencePreservesAllUninterpretedBMSBits(t *testing.T) {
	for _, raw := range []uint16{0, 1, 0x7fff, 0x8000, 0x8001, 0xfffe, 0xffff} {
		t.Run(fmt.Sprintf("raw=%04X", raw), func(t *testing.T) {
			cfg := testModbusConfig()
			cfg.ValidationEvidenceEnabled = true
			responses := baselineMetadataTestResponses(t)
			words := capturedBMSMirrorWords()
			for _, index := range []int{1, 2, 6, 8} {
				words[index] = raw
			}
			responses[validationReadIndex(t, bmsMirrorsRead, false)] = makeReadResponse(t, bmsMirrorsRead, words)
			dialer := &scriptedDialer{responses: responses}
			client, err := newClient(cfg, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err != nil || snapshot == nil || snapshot.ModbusEvidence == nil {
				t.Fatalf("Fetch() failed or lost evidence: %v", err)
			}
			if !reflect.DeepEqual(snapshot.ModbusEvidence.BMSWords[:], words) || len(snapshot.Metrics) != 97 {
				t.Fatal("raw bits were discarded, interpreted, or added as metrics")
			}
			for _, metric := range snapshot.Metrics {
				switch metric.Key {
				case "BMS_D_V", "BMS_C_C_L", "BMS_B_C1", "BMS_CC1":
					t.Fatalf("unvalidated key %s became a metric", metric.Key)
				}
			}
			copyOfEvidence := *snapshot.ModbusEvidence
			copyOfEvidence.BMSWords[1] ^= 0xffff
			copyOfEvidence.RelayWords[0] ^= 0xffff
			copyOfEvidence.AlertWords[0] ^= 0xffff
			if snapshot.ModbusEvidence.BMSWords[1] != raw || snapshot.ModbusEvidence.RelayWords[0] == copyOfEvidence.RelayWords[0] || snapshot.ModbusEvidence.AlertWords[0] == copyOfEvidence.AlertWords[0] {
				t.Fatal("copying evidence retained aliases to raw arrays")
			}
		})
	}
}

func TestValidationEvidenceNeverEscapesPartialOrFailedFetch(t *testing.T) {
	plan := fetchReadPlan(true, false)
	for failAt := validationReadIndex(t, bmsMirrorsRead, false); failAt < len(plan); failAt++ {
		t.Run(fmt.Sprintf("failed_read=%d", plan[failAt]), func(t *testing.T) {
			cfg := testModbusConfig()
			cfg.ValidationEvidenceEnabled = true
			responses := validationEvidenceResponses(t, false)
			responses[failAt] = makeExceptionResponse(t, plan[failAt], 2)
			dialer := &scriptedDialer{responses: responses}
			client, err := newClient(cfg, dialer.DialContext)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := client.Fetch(context.Background())
			if err == nil || snapshot != nil {
				t.Fatalf("partial snapshot/evidence returned: %#v / %v", snapshot, err)
			}
			if dialer.attemptCount() != 1 || dialer.responseCount() != failAt+1 || len(dialer.connection(0).writes) != failAt+1 {
				t.Fatal("diagnostic failure continued or retried the read plan")
			}
		})
	}
}

func TestValidationEvidenceCancellationAfterFinalResponseReturnsNoSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responses := validationEvidenceResponses(t, false)
	index := 0
	conn := &validationCancelConn{
		memoryConn: &memoryConn{responseForWrite: func() []byte {
			response := responses[index]
			index++
			return response
		}},
		cancel: cancel,
		reads:  len(responses),
	}
	cfg := testModbusConfig()
	cfg.ValidationEvidenceEnabled = true
	dials := 0
	client, err := newClient(cfg, func(context.Context, string, string) (net.Conn, error) {
		dials++
		return conn, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Fetch(ctx)
	if snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation after final response returned snapshot/evidence: %#v / %v", snapshot, err)
	}
	if dials != 1 || len(conn.writes) != len(responses) || index != len(responses) {
		t.Fatal("cancellation test did not consume exactly the existing fixed plan")
	}
}

func validationEvidenceResponses(t *testing.T, metadata bool) [][]byte {
	t.Helper()
	responses := baselineMetadataTestResponses(t)
	if metadata {
		responses = enabledMetadataTestResponses(t)
	}
	words := capturedBMSMirrorWords()
	words[1], words[2], words[6], words[8] = 0xffff, 0x8000, 0xfffe, 0x8001
	responses[validationReadIndex(t, bmsMirrorsRead, metadata)] = makeReadResponse(t, bmsMirrorsRead, words)
	responses[validationReadIndex(t, relayStatusRead, metadata)] = makeReadResponse(t, relayStatusRead, []uint16{0xfff1, 0xa5a5})
	responses[validationReadIndex(t, warningFaultRead, metadata)] = makeReadResponse(t, warningFaultRead, []uint16{0, 1, 0x7fff, 0x8000, 0xfffe, 0xffff})
	responses[validationReadIndex(t, runStateRead, metadata)] = makeReadResponse(t, runStateRead, []uint16{5, 1, 2, 3, 4, 5})
	return responses
}

func validationReadIndex(t *testing.T, id readID, metadata bool) int {
	t.Helper()
	for index, candidate := range fetchReadPlan(true, metadata) {
		if candidate == id {
			return index
		}
	}
	t.Fatalf("read %d missing from fixed plan", id)
	return -1
}

func assertValidationEvidence(t *testing.T, snapshot *model.Snapshot, before, after time.Time) {
	t.Helper()
	evidence := snapshot.ModbusEvidence
	if evidence == nil {
		t.Fatal("enabled complete Fetch omitted sidecar")
	}
	wantBMS := [9]uint16{0x1950, 0xffff, 0x8000, 0x0016, 0x0064, 0x1808, 0xfffe, 0x04c4, 0x8001}
	if evidence.BMSWords != wantBMS || evidence.RelayWords != [2]uint16{0xfff1, 0xa5a5} || evidence.AlertWords != [6]uint16{0, 1, 0x7fff, 0x8000, 0xfffe, 0xffff} || evidence.RunState != 5 {
		t.Fatalf("raw words were altered: %#v", evidence)
	}
	if evidence.StartedAt.Before(before) || evidence.FinishedAt.After(after) || !evidence.FinishedAt.Equal(snapshot.CollectedAt) {
		t.Fatal("whole-fetch window does not match this snapshot")
	}
	previous := evidence.StartedAt
	for _, window := range []model.ModbusReadWindow{evidence.BMSWindow, evidence.RelayWindow, evidence.AlertWindow, evidence.RunStateWindow} {
		if window.StartedAt.Before(previous) || window.FinishedAt.Before(window.StartedAt) || window.FinishedAt.After(evidence.FinishedAt) {
			t.Fatalf("unordered/read window outside Fetch bounds: %#v", evidence)
		}
		previous = window.FinishedAt
		for _, value := range []time.Time{window.StartedAt, window.FinishedAt} {
			if value.IsZero() || value.Location() != time.UTC {
				t.Fatalf("read timestamp is missing or not UTC: %v", value)
			}
		}
	}
	if evidence.StartedAt.IsZero() || evidence.FinishedAt.IsZero() || evidence.StartedAt.Location() != time.UTC || evidence.FinishedAt.Location() != time.UTC {
		t.Fatal("whole-fetch timestamps missing or not UTC")
	}
}

func assertEvidenceExcludedFromSnapshotJSON(t *testing.T, snapshot *model.Snapshot) {
	t.Helper()
	withEvidence, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	without := *snapshot
	without.ModbusEvidence = nil
	withoutEvidence, err := json.Marshal(&without)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(withEvidence, withoutEvidence) {
		t.Fatal("internal evidence escaped through public snapshot JSON")
	}
	var roundtrip model.Snapshot
	if err := json.Unmarshal(withEvidence, &roundtrip); err != nil || roundtrip.ModbusEvidence != nil {
		t.Fatal("serialized telemetry roundtrip contains raw diagnostic evidence")
	}
}

type validationCancelConn struct {
	*memoryConn
	cancel context.CancelFunc
	reads  int
}

func (conn *validationCancelConn) Read(data []byte) (int, error) {
	n, err := conn.memoryConn.Read(data)
	if len(conn.writes) == conn.reads && conn.responseBuffer.Len() == 0 {
		conn.cancel()
	}
	return n, err
}
