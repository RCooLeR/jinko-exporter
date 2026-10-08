package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func TestAlertCorrelationNotificationRawBitsAndMaximumSize(t *testing.T) {
	signature := source.ModbusAlertSignature{R553: 0xffff, R554: 0xffff, R555: 0xffff, R556: 0xffff, R557: 0xffff, R558: 0xffff}
	notification, err := alertCorrelationNotification(source.AlertCorrelationEvent{
		Kind: source.AlertCorrelationDetected, Signature: signature,
	}, "unchanged-tag")
	if err != nil {
		t.Fatal(err)
	}
	if size := utf8.RuneCountInString(notification.Message); size >= 4000 {
		t.Fatalf("all-96-bit message has %d runes; must fit below 4000 without truncation", size)
	}
	const qualification = "Active raw bits (zero-based; fault names/meanings unverified): "
	if !strings.Contains(notification.Message, qualification) || !strings.Contains(notification.Message, formatCorrelationWords(signature)) {
		t.Fatalf("lost original words or interpretation disclaimer: %q", notification.Message)
	}
	parts := strings.Split(formatCorrelationActiveBits(signature), ", ")
	if len(parts) != 96 {
		t.Fatalf("raw bits=%d, want all 96", len(parts))
	}
	for index, part := range parts {
		want := fmt.Sprintf("R%d.b%d", 553+index/16, index%16)
		if part != want {
			t.Fatalf("bit %d=%q, want %q", index, part, want)
		}
	}
	if !strings.HasSuffix(notification.Message, "R558.b15.") || notification.Data["tag"] != "unchanged-tag" {
		t.Fatal("notification lost final bit or stable tag")
	}
	mixed := source.ModbusAlertSignature{R553: 0x8001, R555: 2}
	if got := formatCorrelationActiveBits(mixed); got != "R553.b0, R553.b15, R555.b1" {
		t.Fatalf("mixed bits = %q", got)
	}
	if got := formatCorrelationActiveBits(source.ModbusAlertSignature{}); got != "none" {
		t.Fatalf("empty bits = %q", got)
	}
	recovery, err := alertCorrelationNotification(source.AlertCorrelationEvent{Kind: source.AlertCorrelationRecovered}, "unchanged-tag")
	if err != nil || recovery.Message != "A complete Modbus snapshot reported R553-R558 as zero." {
		t.Fatalf("recovery behavior changed: %#v / %v", recovery, err)
	}
}

func TestAlertCorrelationAllLogPathsSerializeRawBits(t *testing.T) {
	observedAt := time.Unix(1_700_000_000, 123).UTC()
	signature := source.ModbusAlertSignature{R553: 0x8001, R555: 2, R558: 0x8000}
	tests := []struct {
		name          string
		kind          source.AlertCorrelationEventKind
		signature     source.ModbusAlertSignature
		deliveryError bool
		wantLogs      int
		wantSends     int
		wantError     bool
	}{
		{"detected processing and delivered", source.AlertCorrelationDetected, signature, false, 2, 1, false},
		{"detected processing and delivery failed", source.AlertCorrelationDetected, signature, true, 2, 1, true},
		{"recovered processing and delivered", source.AlertCorrelationRecovered, source.ModbusAlertSignature{}, false, 2, 1, false},
		{"completion only", source.AlertCorrelationComplete, signature, false, 1, 0, false},
		{"invalid event", "unsupported", signature, false, 1, 0, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			oldLogger := log.Logger
			log.Logger = zerolog.New(&output)
			t.Cleanup(func() { log.Logger = oldLogger })
			sender := &recordingHomeAssistantSender{}
			if test.deliveryError {
				sender.err = errors.New("synthetic delivery failure")
			}
			event := source.AlertCorrelationEvent{Kind: test.kind, ObservedAt: observedAt, Signature: test.signature}
			err := homeAssistantCorrelationCallback(sender, "stable-tag")(context.Background(), event)
			if (err != nil) != test.wantError || len(sender.notifications) != test.wantSends {
				t.Fatalf("err=%v sends=%d, want error=%t sends=%d", err, len(sender.notifications), test.wantError, test.wantSends)
			}
			assertCorrelationBitLogs(t, output.Bytes(), test.signature, observedAt, test.wantLogs)
		})
	}
}

func TestAlertCorrelationCloudEvidenceLogsRawBitsWithoutChangingMetrics(t *testing.T) {
	var output bytes.Buffer
	oldLogger := log.Logger
	log.Logger = zerolog.New(&output)
	t.Cleanup(func() { log.Logger = oldLogger })
	observedAt := time.Unix(1_700_000_000, 123).UTC()
	signature := source.ModbusAlertSignature{R554: 0x8000, R556: 5}
	evidence := source.AlertCorrelationEvidence{
		ObservedAt: observedAt,
		Signature:  signature,
		Sources: []source.AlertCorrelationSourceEvidence{
			{Source: "jinko", Status: source.AlertCorrelationSourceOK, TotalMetricCount: 1, Metrics: []source.AlertCorrelationMetric{{Group: "bms", Key: "BMS_SOC", Unit: "%", Value: 73}}},
			{Source: "solarman", Status: source.AlertCorrelationSourceTimedOut},
		},
	}
	if err := recordAlertCorrelationEvidence(context.Background(), evidence); err != nil {
		t.Fatal(err)
	}
	entries := assertCorrelationBitLogs(t, output.Bytes(), signature, observedAt, 2)
	var metrics []correlationLogMetric
	if err := json.Unmarshal(entries[0]["metrics"], &metrics); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(metrics, []correlationLogMetric{{Group: "bms", Key: "BMS_SOC", Unit: "%", Value: 73}}) || string(entries[0]["metric_count"]) != "1" {
		t.Fatalf("evidence metrics changed: %s", entries[0]["metrics"])
	}
	if string(entries[1]["metrics"]) != "[]" || string(entries[1]["metric_count"]) != "0" {
		t.Fatal("failed source gained synthetic metrics")
	}
}

func assertCorrelationBitLogs(t *testing.T, encoded []byte, signature source.ModbusAlertSignature, observedAt time.Time, wantCount int) []map[string]json.RawMessage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var entries []map[string]json.RawMessage
	for {
		var entry map[string]json.RawMessage
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("invalid log JSON: %v", err)
		}
		entries = append(entries, entry)
		encodedBits, ok := entry["modbus_active_bits"]
		if !ok || string(encodedBits) == "null" {
			t.Fatalf("log lacks an explicit raw-bit array: %s", encoded)
		}
		var got []source.ModbusAlertBit
		if err := json.Unmarshal(encodedBits, &got); err != nil || !reflect.DeepEqual(got, signature.ActiveBits()) {
			t.Fatalf("bits=%s, error=%v, want %#v", encodedBits, err, signature.ActiveBits())
		}
		var objects []map[string]json.RawMessage
		if err := json.Unmarshal(encodedBits, &objects); err != nil {
			t.Fatal(err)
		}
		for _, object := range objects {
			if len(object) != 2 || object["register"] == nil || object["bit"] == nil {
				t.Fatalf("raw-bit schema must contain only register and bit: %s", encodedBits)
			}
		}
		if words, ok := entry["modbus_words"]; ok {
			var gotWords correlationLogWords
			if err := json.Unmarshal(words, &gotWords); err != nil || gotWords != correlationWords(signature) {
				t.Fatalf("original raw words changed: %s / %v", words, err)
			}
		}
		var id string
		if err := json.Unmarshal(entry["correlation_id"], &id); err != nil || id != alertCorrelationID(observedAt, signature) {
			t.Fatalf("correlation identity changed: %q / %v", id, err)
		}
	}
	if len(entries) != wantCount {
		t.Fatalf("log count=%d, want %d: %s", len(entries), wantCount, encoded)
	}
	return entries
}
