package modbus

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

const testLoggerSerial = uint32(0x12345678)

func TestBuildReadRequestGoldenFrames(t *testing.T) {
	tests := []struct {
		name       string
		id         readID
		wantHex    string
		wantSHA256 string
	}{
		{
			name:       "device type canary",
			id:         deviceTypeRead,
			wantHex:    "a517001045010078563412020000000000000000000000000000010300000001840a1615",
			wantSHA256: "3455245dcace59ac9ce4ab27e09faddf8533155c3fc83e4066fda1d3b159ea0d",
		},
		{
			name:       "UPS load power",
			id:         upsPowerRead,
			wantHex:    "a517001045020078563412020000000000000000000000000000010302830001745adc15",
			wantSHA256: "879b44a8bc8ba80a2d725f851bf781b7033844eb4ccaf264938233083a607aa7",
		},
		{
			name:       "load phase voltages block",
			id:         loadVoltageRead,
			wantHex:    "a51700104510007856341202000000000000000000000000000001030280000704587b15",
			wantSHA256: "c7f6f231263a34943fa691c46aa80753a4f029962797b6f811524341f8e00638",
		},
		{
			name:       "direct-load power low words",
			id:         directLoadPowerLowRead,
			wantHex:    "a5170010451200785634120200000000000000000000000000000103028a0004645be715",
			wantSHA256: "f76dc5dd06d842e5e0b642a41e853fa22d97f36a3cdb9d329f4cf2252bae29d8",
		},
		{
			name:       "load frequency block",
			id:         loadFrequencyRead,
			wantHex:    "a5170010451300785634120200000000000000000000000000000103028f0005b59a7e15",
			wantSHA256: "e3b8e671ad6933863a7135a8ac18c13487473008e9d7e356be12487b91f2119a",
		},
		{
			name:       "grid phase voltages",
			id:         gridVoltageRead,
			wantHex:    "a517001045030078563412020000000000000000000000000000010302560003e4632b15",
			wantSHA256: "d1c51f1fa76c155ec996b406a3fa59c085cb0723b8eab886c2289edaf5c6d2b4",
		},
		{
			name:       "rated power MPPT and phases",
			id:         capabilityRead,
			wantHex:    "a51700104509007856341202000000000000000000000000000001030014000345cfba15",
			wantSHA256: "f914909771049378623608f8cc54d1ba1d6adf1e63d49b26b99ddaefb44646ef",
		},
		{
			name:       "generator port mode zero-domain gate",
			id:         generatorPortModeRead,
			wantHex:    "a51700104516007856341202000000000000000000000000000001030085000195e39a15",
			wantSHA256: "32f03196517687dc31eeb2f59ea0129d418997b6bb05d259af14ddc9e8d627de",
		},
		{
			name:       "generator energy and runtime zero-domain block",
			id:         generatorEnergyRead,
			wantHex:    "a517001045170078563412020000000000000000000000000000010302180004c5b63615",
			wantSHA256: "551d085bea96c00c358baeadfb1c9f28653b6f97dc38aea39d69d7c0a2f8feaa",
		},
		{
			name:       "generator voltage and power zero-domain block",
			id:         generatorElectricalRead,
			wantHex:    "a51700104518007856341202000000000000000000000000000001030295000b1599ee15",
			wantSHA256: "c6bf93708fd3f02ab12cfc482902d243b66fa1337ddf6e76b2cfc9d4d926306b",
		},
		{
			name:       "battery temperature",
			id:         batteryTemperatureRead,
			wantHex:    "a5170010450600785634120200000000000000000000000000000103024a0001a464e115",
			wantSHA256: "4173e4b5c918afc591128630c768e194af7585fcf21d092fb6037eb198905a1d",
		},
		{
			name:       "battery voltage and SOC",
			id:         batteryVoltageSOCRead,
			wantHex:    "a5170010450500785634120200000000000000000000000000000103024b0002b5a53415",
			wantSHA256: "fca6bb89bed713a75479593c1e8a6ef85752672e40e31913dc84ed8ce507fcd4",
		},
		{
			name:       "battery power and current",
			id:         batteryFlowRead,
			wantHex:    "a5170010450a00785634120200000000000000000000000000000103024e0002a5a42b15",
			wantSHA256: "085261073960e155d7f90ef2f333289f8ee76dbe84749e87a00dd9ea3f232170",
		},
		{
			name:       "energy counters",
			id:         energyRead,
			wantHex:    "a517001045080078563412020000000000000000000000000000010302020010e47e0415",
			wantSHA256: "b2af0b3a6e7f1e4e25fc48244c766dc73ef426307f27b99b0a104b89b33980de",
		},
		{
			name:       "power and AC relay status",
			id:         relayStatusRead,
			wantHex:    "a51700104519007856341202000000000000000000000000000001030227000275b8f715",
			wantSHA256: "6ca3776bb36eade99c93a97ee34386f44951674e297b181ed6eaa043b989ac36",
		},
		{
			name:       "warning and fault raw words",
			id:         warningFaultRead,
			wantHex:    "a51700104514007856341202000000000000000000000000000001030229000615b89815",
			wantSHA256: "588be8efc528d704dd382b2f99d306f586c96ac39aa4d7ab69ecfd8dafcf7f20",
		},
		{
			name:       "run-state block",
			id:         runStateRead,
			wantHex:    "a517001045040078563412020000000000000000000000000000010301f4000685c6d015",
			wantSHA256: "3c57cbf873cef15fbc31aad6d36a29ee39a3cf31c41b739e2a3fb6f2c83932ae",
		},
		{
			name:       "grid frequency and current block",
			id:         gridFrequencyCurrentRead,
			wantHex:    "a5170010450b007856341202000000000000000000000000000001030261000b546bbe15",
			wantSHA256: "ed8576fde66ebf7c7a4828372005e0538b456dd46a80b11b70e3b21264483879",
		},
		{
			name:       "grid power low words",
			id:         gridPowerLowRead,
			wantHex:    "a5170010450c00785634120200000000000000000000000000000103026e0004246c9615",
			wantSHA256: "35b0424f361201eae18025888e1a70019605c161585fa6e21f704bb63786ced3",
		},
		{
			name:       "grid power high words",
			id:         gridPowerHighRead,
			wantHex:    "a5170010450d0078563412020000000000000000000000000000010302af000475904d15",
			wantSHA256: "c0e5ee9eea15964cf9e44add9fab5853f37c601dce608a94ef328e798b91b958",
		},
		{
			name:       "output scalar block",
			id:         outputScalarRead,
			wantHex:    "a5170010450e007856341202000000000000000000000000000001030273000cb5ac7615",
			wantSHA256: "8dcf382889cba089aa929b58610917d3709effb94f118737e34202c005133d20",
		},
		{
			name:       "output power high words",
			id:         outputPowerHighRead,
			wantHex:    "a5170010450f0078563412020000000000000000000000000000010302b3000575965a15",
			wantSHA256: "667ee1799c7734fb9ea8f2b3a2b9f470c3a0167e3bb7ab277cfe62add6e944d3",
		},
		{
			name:       "PV input block",
			id:         pvInputRead,
			wantHex:    "a517001045070078563412020000000000000000000000000000010302a0000845961215",
			wantSHA256: "4d6c1b7c7aec56497460b551cbee067c9a5246db72f27f242b557e176f999839",
		},
		{
			name:       "DC and AC temperatures",
			id:         inverterTemperatureRead,
			wantHex:    "a5170010451500785634120200000000000000000000000000000103021c000204753415",
			wantSHA256: "33d64b97de5f6f46f3905d1b12f654f58a40802f542c5e83b2ab55b53133ea4e",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := buildReadRequest(testLoggerSerial, modbusUnitID, tt.id)
			if err != nil {
				t.Fatalf("buildReadRequest() error = %v", err)
			}
			if got := hex.EncodeToString(frame); got != tt.wantHex {
				t.Fatalf("frame = %s, want %s", got, tt.wantHex)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(frame)); got != tt.wantSHA256 {
				t.Fatalf("SHA-256 = %s, want %s", got, tt.wantSHA256)
			}
			if frame[v5RequestRTUOffset+1] != readHoldingRegisters {
				t.Fatalf("function byte = 0x%02X, want FC03", frame[v5RequestRTUOffset+1])
			}
		})
	}
}

func TestBuildReadRequestRejectsAnythingOutsideAllowlist(t *testing.T) {
	tests := []struct {
		name   string
		serial uint32
		unit   byte
		id     readID
	}{
		{name: "zero serial", serial: 0, unit: 1, id: deviceTypeRead},
		{name: "different unit", serial: testLoggerSerial, unit: 2, id: deviceTypeRead},
		{name: "unknown read ID", serial: testLoggerSerial, unit: 1, id: readID(0xFF)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildReadRequest(tt.serial, tt.unit, tt.id); err == nil {
				t.Fatal("buildReadRequest() error = nil")
			}
		})
	}
}

func TestValidateReadRequestRejectsFC06AndChangedRangeWithValidChecksums(t *testing.T) {
	base, err := buildReadRequest(testLoggerSerial, modbusUnitID, upsPowerRead)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "FC06", mutate: func(frame []byte) { frame[v5RequestRTUOffset+1] = 0x06 }},
		{name: "different register", mutate: func(frame []byte) {
			binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+2:v5RequestRTUOffset+4], 0x0062)
		}},
		{name: "different quantity", mutate: func(frame []byte) { binary.BigEndian.PutUint16(frame[v5RequestRTUOffset+4:v5RequestRTUOffset+6], 2) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := append([]byte(nil), base...)
			tt.mutate(frame)
			rtu := frame[v5RequestRTUOffset : len(frame)-2]
			crc := modbusCRC(rtu[:len(rtu)-2])
			rtu[len(rtu)-2] = byte(crc)
			rtu[len(rtu)-1] = byte(crc >> 8)
			refreshOuterChecksum(frame)
			if err := validateReadRequest(frame, testLoggerSerial, modbusUnitID, upsPowerRead); err == nil {
				t.Fatal("validateReadRequest() accepted mutated request")
			}
		})
	}
}

func TestParseReadResponse(t *testing.T) {
	frame := makeReadResponse(t, gridVoltageRead, []uint16{2504, 2411, 2368})
	frame[6] = 0xA7 // Logger-owned sequence metadata is intentionally not echoed.
	frame[len(frame)-2] = checksum8(frame[1 : len(frame)-2])

	values, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, gridVoltageRead)
	if err != nil {
		t.Fatalf("parseReadResponse() error = %v", err)
	}
	want := []uint16{2504, 2411, 2368}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("value[%d] = %d, want %d", index, values[index], want[index])
		}
	}
}

func TestParseSanitizedLiveCanaryResponse(t *testing.T) {
	// This is the successful live canary response with only the logger serial
	// replaced by the synthetic test serial and the outer checksum recomputed.
	// The remaining bytes, offsets, RTU payload, and logger-owned sequence byte
	// are preserved so this fixture does not depend on our response generator.
	frame, err := hex.DecodeString("a515001015014b785634120201f98b07015c1800009bd37869010302000638467615")
	if err != nil {
		t.Fatal(err)
	}
	values, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, deviceTypeRead)
	if err != nil {
		t.Fatalf("parseReadResponse() error = %v", err)
	}
	if len(values) != 1 || values[0] != expectedDeviceType {
		t.Fatalf("values = %v, want [0x%04X]", values, expectedDeviceType)
	}
}

func TestParseIndependentSanitizedPromotedResponseFixtures(t *testing.T) {
	// These fixed fixtures are independent protocol oracles. The logger serial
	// is synthetic and only the outer checksum was recomputed after sanitizing
	// it; none is produced by makeReadResponse or the production frame builder.
	tests := []struct {
		name       string
		id         readID
		frameHex   string
		want       []uint16
		wantSHA256 string
	}{
		{
			name: "capability", id: capabilityRead,
			frameHex: "a5190010150957785634120201000000000000000000000000010306d4c00001020323d14d15",
			want:     []uint16{0xD4C0, 0x0001, 0x0203},
		},
		{
			name: "battery temperature", id: batteryTemperatureRead,
			frameHex: "a5150010150656785634120201000000000000000000000000010302046a3aab0615",
			want:     []uint16{0x046A},
		},
		{
			name: "battery voltage SOC", id: batteryVoltageSOCRead,
			frameHex: "a517001015054e78563412020100000000000000000000000001030417fc00643e5cbf15",
			want:     []uint16{0x17FC, 0x0064},
		},
		{
			name: "battery flow", id: batteryFlowRead,
			frameHex: "a5170010150a577856341202010000000000000000000000000103048000ffffd2434f15",
			want:     []uint16{0x8000, 0xFFFF},
		},
		{
			name: "load phase voltage block", id: loadVoltageRead,
			frameHex: "a521001015105778563412020100000000000000000000000001030e00000001ffff123408fd08fe08ff2674c715",
			want:     []uint16{0x0000, 0x0001, 0xFFFF, 0x1234, 2301, 2302, 2303},
		},
		{
			// The payload preserves the reviewed live low words. Logger identity
			// is synthetic and every logger-owned opaque byte, including byte 6,
			// is zero.
			name: "direct-load power low words", id: directLoadPowerLowRead,
			frameHex:   "a51b00101512007856341202010000000000000000000000000103080013002200f8012d0f6d4c15",
			want:       []uint16{19, 34, 248, 301},
			wantSHA256: "601191fe780da1811d8a81e58b91e873de3963921ce5b24eb9199f25a8c75b01",
		},
		{
			name: "load frequency block", id: loadFrequencyRead,
			frameHex: "a51d001015135778563412020100000000000000000000000001030a13880000ffff12348000f6476d15",
			want:     []uint16{5000, 0x0000, 0xFFFF, 0x1234, 0x8000},
		},
		{
			name: "energy", id: energyRead,
			frameHex: "a53300101508577856341202010000000000000000000000000103200001000a006400e6012c03e807d012345678ffff80007fff00ffff000102abcdec570215",
			want: []uint16{
				0x0001, 0x000A, 0x0064, 0x00E6, 0x012C, 0x03E8, 0x07D0, 0x1234,
				0x5678, 0xFFFF, 0x8000, 0x7FFF, 0x00FF, 0xFF00, 0x0102, 0xABCD,
			},
		},
		{
			name: "grid power low words", id: gridPowerLowRead,
			frameHex: "a51b0010150c57785634120201000000000000000000000000010308fcf7fcf4fcfaf6e52faa5315",
			want:     []uint16{0xFCF7, 0xFCF4, 0xFCFA, 0xF6E5},
		},
		{
			name: "grid power high words", id: gridPowerHighRead,
			frameHex: "a51b0010150d57785634120201000000000000000000000000010308ffffffffffffffffd453e615",
			want:     []uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
		},
		{
			name: "output scalar block", id: outputScalarRead,
			frameHex: "a52b0010150e5778563412020100000000000000000000000001031809550969093a017c017c01cc036f037304440b260b261383844bb915",
			want: []uint16{
				0x0955, 0x0969, 0x093A, 0x017C, 0x017C, 0x01CC,
				0x036F, 0x0373, 0x0444, 0x0B26, 0x0B26, 0x1383,
			},
		},
		{
			// The signed mix is synthetic and logger identity is sanitized. A
			// production log independently observed R691=0xFFFF, but did not retain
			// the remaining words, so this fixture is a protocol boundary oracle,
			// not a claimed live magnitude capture.
			name:       "output power signed high words",
			id:         outputPowerHighRead,
			frameHex:   "a51d0010150f4b78563412020100000000000000000000000001030affffffff00000000ffff150ada15",
			want:       []uint16{0xFFFF, 0xFFFF, 0, 0, 0xFFFF},
			wantSHA256: "c7f948fb86ca8dda4365843fc9840bf705574e599056b6dbf91f4b4f4bd420b2",
		},
		{
			name: "PV input block", id: pvInputRead,
			frameHex: "a5230010150757785634120201000000000000000000000000010310000300040000000004f400020514000364e93b15",
			want:     []uint16{3, 4, 0, 0, 1268, 2, 1300, 3},
		},
		{
			// The raw words retain the live bracketed 25.0/44.0 C sample;
			// logger identity is synthetic and the outer checksum is recomputed.
			name: "DC and AC temperatures", id: inverterTemperatureRead,
			frameHex: "a517001015155778563412020100000000000000000000000001030404e205a0581dc715",
			want:     []uint16{1250, 1440},
		},
		{
			// Synthetic R551=on/R552=7 response. The relay value matches the
			// retained canonical cloud fixture; all identity bytes are synthetic.
			name:       "power and AC relay status",
			id:         relayStatusRead,
			frameHex:   "a517001015195778563412020100000000000000000000000001030400010007ea31ee15",
			want:       []uint16{1, 7},
			wantSHA256: "509c179e86d3fb18fd6cd2073097e193492bee72267704bf81ef46d3e8101c89",
		},
		{
			// Fully synthetic all-clear response: logger identity and every
			// logger-owned opaque metadata byte are sanitized.
			name: "warning and fault raw words all clear", id: warningFaultRead,
			frameHex: "a51f001015145778563412020100000000000000000000000001030c0000000000000000000000009370d915",
			want:     []uint16{0, 0, 0, 0, 0, 0},
		},
		{
			name: "warning and fault raw word boundaries", id: warningFaultRead,
			frameHex: "a51f001015145778563412020100000000000000000000000001030c000ec0001240004e000080006a7ba915",
			want:     []uint16{0x000E, 0xC000, 0x1240, 0x004E, 0x0000, 0x8000},
		},
		{
			name: "run-state normal with ignored energy words", id: runStateRead,
			frameHex: "a51f001015045778563412020100000000000000000000000001030c00020000000000000000000098c82815",
			want:     []uint16{2, 0, 0, 0, 0, 0},
		},
		{
			// The payload preserves the reviewed live 609-619 words. Logger
			// identity is synthetic and all logger-owned opaque bytes are zero.
			name: "grid frequency and current live values", id: gridFrequencyCurrentRead,
			frameHex: "a5290010150b00785634120201000000000000000000000000010316138300350042009b0035003f009d0064006600780142fc965a15",
			want:     []uint16{4995, 53, 66, 155, 53, 63, 157, 100, 102, 120, 322},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := hex.DecodeString(tt.frameHex)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantSHA256 != "" {
				if got := fmt.Sprintf("%x", sha256.Sum256(frame)); got != tt.wantSHA256 {
					t.Fatalf("fixture SHA-256 = %s, want %s", got, tt.wantSHA256)
				}
			}
			got, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, tt.id)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("values = %d, want %d", len(got), len(tt.want))
			}
			for index := range tt.want {
				if got[index] != tt.want[index] {
					t.Fatalf("value[%d] = 0x%04X, want 0x%04X", index, got[index], tt.want[index])
				}
			}
		})
	}
}

func TestParseIndependentSanitizedPromotedExceptionFixtures(t *testing.T) {
	tests := []struct {
		id         readID
		frameHex   string
		wantSHA256 string
	}{
		{id: capabilityRead, frameHex: "a5130010150957785634120201000000000000000000000000018302c0f1e615"},
		{id: batteryTemperatureRead, frameHex: "a5130010150656785634120201000000000000000000000000018302c0f1e215"},
		{id: batteryVoltageSOCRead, frameHex: "a513001015054e785634120201000000000000000000000000018302c0f1d915"},
		{id: batteryFlowRead, frameHex: "a5130010150a57785634120201000000000000000000000000018302c0f1e715"},
		{id: energyRead, frameHex: "a5130010150857785634120201000000000000000000000000018302c0f1e515"},
		{id: gridPowerLowRead, frameHex: "a5130010150c57785634120201000000000000000000000000018302c0f1e915"},
		{id: gridPowerHighRead, frameHex: "a5130010150d57785634120201000000000000000000000000018302c0f1ea15"},
		{id: outputScalarRead, frameHex: "a5130010150e57785634120201000000000000000000000000018302c0f1eb15"},
		{id: outputPowerHighRead, frameHex: "a5130010150f57785634120201000000000000000000000000018302c0f1ec15"},
		{id: loadVoltageRead, frameHex: "a5130010151057785634120201000000000000000000000000018302c0f1ed15"},
		{id: directLoadPowerLowRead, frameHex: "a5130010151200785634120201000000000000000000000000018302c0f19815", wantSHA256: "9014c5904584b0439103e3f04910a934d7dd33d0e313b569c60ab6de1f72840c"},
		{id: loadFrequencyRead, frameHex: "a5130010151357785634120201000000000000000000000000018302c0f1f015"},
		{id: pvInputRead, frameHex: "a5130010150757785634120201000000000000000000000000018302c0f1e415"},
		{id: inverterTemperatureRead, frameHex: "a5130010151557785634120201000000000000000000000000018302c0f1f215"},
		{id: relayStatusRead, frameHex: "a5130010151957785634120201000000000000000000000000018302c0f1f615", wantSHA256: "40950ff784256db70daae36f6320b126f4917ff649f6bb996ec5982f80306ad0"},
		{id: warningFaultRead, frameHex: "a5130010151457785634120201000000000000000000000000018302c0f1f115"},
		{id: runStateRead, frameHex: "a5130010150457785634120201000000000000000000000000018302c0f1e115"},
		{id: gridFrequencyCurrentRead, frameHex: "a5130010150b00785634120201000000000000000000000000018302c0f19115"},
	}
	for _, tt := range tests {
		frame, err := hex.DecodeString(tt.frameHex)
		if err != nil {
			t.Fatal(err)
		}
		if tt.wantSHA256 != "" {
			if got := fmt.Sprintf("%x", sha256.Sum256(frame)); got != tt.wantSHA256 {
				t.Fatalf("fixture SHA-256 = %s, want %s", got, tt.wantSHA256)
			}
		}
		if _, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, tt.id); err == nil || !strings.Contains(err.Error(), "modbus exception 0x02") {
			t.Fatalf("read %d error = %v", tt.id, err)
		}
	}
}

func TestParseReadResponseRejectsMutations(t *testing.T) {
	base := makeReadResponse(t, gridVoltageRead, []uint16{2301, 2302, 2303})
	tests := []struct {
		name   string
		mutate func([]byte)
		want   string
	}{
		{name: "start marker", mutate: func(frame []byte) { frame[0] = 0 }, want: "markers"},
		{name: "end marker", mutate: func(frame []byte) { frame[len(frame)-1] = 0 }, want: "markers"},
		{name: "length", mutate: func(frame []byte) { frame[1]++ }, want: "length mismatch"},
		{name: "outer checksum", mutate: func(frame []byte) { frame[len(frame)-2]++ }, want: "V5 response checksum"},
		{name: "control", mutate: func(frame []byte) { frame[4] = 0x47; refreshOuterChecksum(frame) }, want: "control"},
		{name: "sequence", mutate: func(frame []byte) { frame[5] = 0x44; refreshOuterChecksum(frame) }, want: "sequence"},
		{name: "serial", mutate: func(frame []byte) { frame[7] ^= 1; refreshOuterChecksum(frame) }, want: "different logger serial"},
		{name: "type", mutate: func(frame []byte) { frame[11] = 1; refreshOuterChecksum(frame) }, want: "type/status"},
		{name: "status", mutate: func(frame []byte) { frame[12] = 0; refreshOuterChecksum(frame) }, want: "type/status"},
		{name: "unit with valid CRC", mutate: func(frame []byte) { frame[25] = 2; refreshRTUAndOuterChecksums(frame) }, want: "response header"},
		{name: "function with valid CRC", mutate: func(frame []byte) { frame[26] = 4; refreshRTUAndOuterChecksums(frame) }, want: "response header"},
		{name: "byte count with valid CRC", mutate: func(frame []byte) { frame[27] = 4; refreshRTUAndOuterChecksums(frame) }, want: "response header"},
		{name: "inner CRC", mutate: func(frame []byte) { frame[len(frame)-3] ^= 1; refreshOuterChecksum(frame) }, want: "Modbus response CRC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := append([]byte(nil), base...)
			tt.mutate(frame)
			_, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, gridVoltageRead)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseReadResponseRejectsModbusException(t *testing.T) {
	frame := makeExceptionResponse(t, upsPowerRead, 0x02)
	_, err := parseReadResponse(frame, testLoggerSerial, modbusUnitID, upsPowerRead)
	if err == nil || !strings.Contains(err.Error(), "modbus exception 0x02") {
		t.Fatalf("error = %v", err)
	}
}

func TestModbusCRCVector(t *testing.T) {
	raw, err := hex.DecodeString("010302830001")
	if err != nil {
		t.Fatal(err)
	}
	if got := modbusCRC(raw); got != 0x5A74 {
		t.Fatalf("CRC = 0x%04X, want 0x5A74", got)
	}
}

func FuzzParseReadResponseDoesNotPanic(f *testing.F) {
	f.Add(makeReadResponseForFuzz(gridVoltageRead, []uint16{2300, 2300, 2300}))
	f.Add([]byte{0xA5, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, frame []byte) {
		_, _ = parseReadResponse(frame, testLoggerSerial, modbusUnitID, gridVoltageRead)
	})
}

func makeReadResponse(t *testing.T, id readID, values []uint16) []byte {
	t.Helper()
	spec, ok := approvedReadSpec(id)
	if !ok {
		t.Fatalf("unknown read ID %d", id)
	}
	if len(values) != int(spec.quantity) {
		t.Fatalf("response values = %d, want %d", len(values), spec.quantity)
	}
	return makeReadResponseForFuzz(id, values)
}

func makeReadResponseForFuzz(id readID, values []uint16) []byte {
	rtu := make([]byte, 5+len(values)*2)
	rtu[0] = modbusUnitID
	rtu[1] = readHoldingRegisters
	rtu[2] = byte(len(values) * 2)
	for index, value := range values {
		binary.BigEndian.PutUint16(rtu[3+index*2:5+index*2], value)
	}
	crc := modbusCRC(rtu[:len(rtu)-2])
	rtu[len(rtu)-2] = byte(crc)
	rtu[len(rtu)-1] = byte(crc >> 8)
	return wrapResponseRTU(id, rtu)
}

func makeExceptionResponse(t *testing.T, id readID, code byte) []byte {
	t.Helper()
	rtu := []byte{modbusUnitID, readHoldingRegisters | 0x80, code, 0, 0}
	crc := modbusCRC(rtu[:3])
	rtu[3] = byte(crc)
	rtu[4] = byte(crc >> 8)
	return wrapResponseRTU(id, rtu)
}

func wrapResponseRTU(id readID, rtu []byte) []byte {
	spec, ok := approvedReadSpec(id)
	if !ok {
		panic("test helper received unknown read ID")
	}
	frame := make([]byte, v5ResponseRTUOffset+len(rtu)+2)
	frame[0] = 0xA5
	binary.LittleEndian.PutUint16(frame[1:3], uint16(len(frame)-13))
	frame[3] = 0x10
	frame[4] = 0x15
	frame[5] = spec.sequence
	frame[6] = 0x4B
	binary.LittleEndian.PutUint32(frame[7:11], testLoggerSerial)
	frame[11] = 0x02
	frame[12] = 0x01
	copy(frame[v5ResponseRTUOffset:], rtu)
	frame[len(frame)-2] = checksum8(frame[1 : len(frame)-2])
	frame[len(frame)-1] = 0x15
	return frame
}

func refreshOuterChecksum(frame []byte) {
	frame[len(frame)-2] = checksum8(frame[1 : len(frame)-2])
}

func refreshRTUAndOuterChecksums(frame []byte) {
	rtu := frame[v5ResponseRTUOffset : len(frame)-2]
	crc := modbusCRC(rtu[:len(rtu)-2])
	rtu[len(rtu)-2] = byte(crc)
	rtu[len(rtu)-1] = byte(crc >> 8)
	refreshOuterChecksum(frame)
}
