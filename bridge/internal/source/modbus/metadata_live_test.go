package modbus

import (
	"encoding/hex"
	"reflect"
	"testing"
)

// Complete target responses captured 2026-10-08T06:01:25.5299573Z through
// 06:01:25.9868958Z. Only logger identity and its outer checksum were replaced;
// the RTU payload/CRC, response sequence halves and time fields are retained.
// These fixtures are not generated with the production request/response code.
func TestMetadataCapturedResponsesPreserveRawFirmwareAndInputCode(t *testing.T) {
	fixtures := []struct {
		id    readID
		frame string
		words []uint16
	}{
		{protocolVersionRead, "a5150010151f3c78563412020196713701081e000018c08f690103020104b817bb15", []uint16{0x0104}},
		{controllerVersionsRead, "a515001015203d78563412020196713701081e000018c08f690103021c08b1420015", []uint16{0x1C08}},
		{firmwareWordsRead, "a51d001015213e78563412020196713701081e000018c08f6901030a300110870000200220172c135b15", []uint16{0x3001, 0x1087, 0, 0x2002, 0x2017}},
		{batteryInputCodeRead, "a515001015223f78563412020196713701081e000018c08f6901030200017984eb15", []uint16{1}},
	}
	var decoded [4][]uint16
	for i, fixture := range fixtures {
		frame, err := hex.DecodeString(fixture.frame)
		if err != nil {
			t.Fatal(err)
		}
		decoded[i], err = parseReadResponse(frame, testLoggerSerial, modbusUnitID, fixture.id)
		if err != nil || !reflect.DeepEqual(decoded[i], fixture.words) {
			t.Fatalf("capture %d = %v / %v, want %v", i, decoded[i], err, fixture.words)
		}
	}
	got, err := decodeMetadata(decoded[0], decoded[1], decoded[2], decoded[3])
	want := map[string]string{
		"protocol_version_hex":         "0104",
		"control_firmware_words":       "3001-1087",
		"communication_firmware_words": "0000-2002-2017",
		"bootloader_version_hex":       "08",
		"assistant_version_hex":        "1C",
		"battery_input_code":           "1",
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("captured metadata = %v / %v, want %v", got, err, want)
	}
}
