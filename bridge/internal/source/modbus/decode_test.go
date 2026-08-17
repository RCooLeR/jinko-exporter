package modbus

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

func TestDecodeGeneratorZeroOnlyAcceptsExactLiveDomain(t *testing.T) {
	if err := decodeGeneratorPortModeZeroOnly([]uint16{0}); err != nil {
		t.Fatalf("decodeGeneratorPortModeZeroOnly() error = %v", err)
	}
	energy, err := decodeGeneratorEnergyZeroOnly(make([]uint16, 4))
	if err != nil {
		t.Fatalf("decodeGeneratorEnergyZeroOnly() error = %v", err)
	}
	electrical, err := decodeGeneratorElectricalZeroOnly(make([]uint16, 11))
	if err != nil {
		t.Fatalf("decodeGeneratorElectricalZeroOnly() error = %v", err)
	}

	got := append(energy, electrical...)
	wantKeys := []string{
		"R_T_D", "GEN_P_D", "GEN_P_TO",
		"GEN_P_L1", "GEN_P_L2", "GEN_P_L3",
		"GEN_V_L1", "GEN_V_L2", "GEN_V_L3",
		"EG_P_CT1", "GEN_P_T",
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("generator metrics = %d, want %d", len(got), len(wantKeys))
	}
	for index, metric := range got {
		if metric.key != wantKeys[index] || metric.value != 0 {
			t.Fatalf("generator metric[%d] = %#v, want key=%q/value=0", index, metric, wantKeys[index])
		}
	}
}

func TestDecodeGeneratorZeroOnlyRejectsEveryNonzeroWord(t *testing.T) {
	if err := decodeGeneratorPortModeZeroOnly([]uint16{1}); err == nil || !strings.Contains(err.Error(), "register 133") {
		t.Fatalf("nonzero generator mode error = %v", err)
	}

	tests := []struct {
		name  string
		start int
		words int
		call  func([]uint16) error
	}{
		{
			name:  "energy and runtime",
			start: 536,
			words: 4,
			call: func(values []uint16) error {
				_, err := decodeGeneratorEnergyZeroOnly(values)
				return err
			},
		},
		{
			name:  "voltage and power",
			start: 661,
			words: 11,
			call: func(values []uint16) error {
				_, err := decodeGeneratorElectricalZeroOnly(values)
				return err
			},
		},
	}
	for _, tt := range tests {
		for index := 0; index < tt.words; index++ {
			t.Run(fmt.Sprintf("%s register %d", tt.name, tt.start+index), func(t *testing.T) {
				values := make([]uint16, tt.words)
				values[index] = 1
				err := tt.call(values)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("register %d", tt.start+index)) {
					t.Fatalf("error = %v, want exact nonzero-register rejection", err)
				}
			})
		}
	}
}

func TestDecodeGeneratorZeroOnlyRequiresExactWordCounts(t *testing.T) {
	for _, words := range [][]uint16{nil, {0, 0}} {
		if err := decodeGeneratorPortModeZeroOnly(words); err == nil {
			t.Fatalf("decodeGeneratorPortModeZeroOnly(%d words) accepted", len(words))
		}
	}
	for _, words := range [][]uint16{make([]uint16, 3), make([]uint16, 5)} {
		if _, err := decodeGeneratorEnergyZeroOnly(words); err == nil {
			t.Fatalf("decodeGeneratorEnergyZeroOnly(%d words) accepted", len(words))
		}
	}
	for _, words := range [][]uint16{make([]uint16, 10), make([]uint16, 12)} {
		if _, err := decodeGeneratorElectricalZeroOnly(words); err == nil {
			t.Fatalf("decodeGeneratorElectricalZeroOnly(%d words) accepted", len(words))
		}
	}
}

func TestDecodeCapabilitiesRequiresSupportedJKSFamilyProfile(t *testing.T) {
	words := func(ratedPowerW uint32, raw22 uint16) []uint16 {
		raw := ratedPowerW * 10
		return []uint16{uint16(raw), uint16(raw >> 16), raw22}
	}
	for _, ratedPowerW := range []uint32{6000, 8000, 10000, 12000, 15000, 20000} {
		got, err := decodeCapabilities(words(ratedPowerW, 0x0203))
		if err != nil {
			t.Fatalf("decodeCapabilities(%dW) error = %v", ratedPowerW, err)
		}
		if got.ratedPowerW != float64(ratedPowerW) || got.mpptCount != 2 || got.phaseCount != 3 {
			t.Fatalf("decodeCapabilities(%dW) = %#v", ratedPowerW, got)
		}
	}

	for _, values := range [][]uint16{
		words(5000, 0x0203),
		words(25000, 0x0203),
		words(29900, 0x0203),
		words(30000, 0x0203),
		words(30000, 0x0303),
		words(12000, 0x0103),
		words(12000, 0x0201),
		words(12000, 0x1203),
		words(12000, 0x0203)[:2],
	} {
		if _, err := decodeCapabilities(values); err == nil {
			t.Fatalf("decodeCapabilities(%#v) error = nil", values)
		}
	}
}

func TestDecodeBatteryTelemetryScalesAndSignedValues(t *testing.T) {
	temperature, err := decodeBatteryTemperature([]uint16{1280})
	if err != nil || temperature != 28 {
		t.Fatalf("temperature/error = %v/%v", temperature, err)
	}
	voltage, soc, err := decodeBatteryVoltageSOC([]uint16{6017, 100})
	if err != nil || voltage != 601.7 || soc != 100 {
		t.Fatalf("voltage/SOC/error = %v/%v/%v", voltage, soc, err)
	}
	power, current, err := decodeBatteryFlow([]uint16{0xFFF7, 0xFFEF})
	if err != nil || power != -90 || current != -0.17 {
		t.Fatalf("power/current/error = %v/%v/%v", power, current, err)
	}
}

func TestDecodeUPSPowerAcceptsFullU16Boundary(t *testing.T) {
	got, err := decodeUPSPower([]uint16{0xFFFF})
	if err != nil || got != 65535 {
		t.Fatalf("decodeUPSPower(0xFFFF) = %v/%v, want 65535/nil", got, err)
	}
}

func TestDecodeBatteryAndGridRangeGuards(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "temperature raw", run: func() error { _, err := decodeBatteryTemperature([]uint16{3001}); return err }, want: "documented maximum"},
		{name: "temperature plausible", run: func() error { _, err := decodeBatteryTemperature([]uint16{2001}); return err }, want: "plausible range"},
		{name: "battery voltage", run: func() error { _, _, err := decodeBatteryVoltageSOC([]uint16{10001, 50}); return err }, want: "voltage"},
		{name: "battery SOC", run: func() error { _, _, err := decodeBatteryVoltageSOC([]uint16{6000, 101}); return err }, want: "SOC"},
		{name: "grid voltage", run: func() error { _, err := decodeGridVoltages([]uint16{2300, 10001, 2300}); return err }, want: "grid voltage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeLoadScalarsUsesOnlyApprovedValues(t *testing.T) {
	voltages, err := decodeLoadVoltages([]uint16{
		0xFFFF, 0x8000, 0x7FFF, 0x1234,
		2389, 2409, 2362,
	})
	if err != nil {
		t.Fatal(err)
	}
	if voltages != [3]float64{238.9, 240.9, 236.2} {
		t.Fatalf("load voltages = %#v", voltages)
	}

	frequency, err := decodeLoadFrequency([]uint16{4995, 0xFFFF, 0x8000, 0x7FFF, 0x1234})
	if err != nil {
		t.Fatal(err)
	}
	if frequency != 49.95 {
		t.Fatalf("load frequency = %v, want 49.95", frequency)
	}

	// Zero voltage and frequency are valid during an outage or inactive state.
	if zeroVoltages, err := decodeLoadVoltages(make([]uint16, 7)); err != nil || zeroVoltages != [3]float64{} {
		t.Fatalf("zero load voltages/error = %#v/%v", zeroVoltages, err)
	}
	if zeroFrequency, err := decodeLoadFrequency(make([]uint16, 5)); err != nil || zeroFrequency != 0 {
		t.Fatalf("zero load frequency/error = %v/%v", zeroFrequency, err)
	}
}

func TestDecodeLoadScalarsRangeGuards(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "load voltage word count", run: func() error { _, err := decodeLoadVoltages(make([]uint16, 6)); return err }, want: "returned 6 words"},
		{name: "load voltage", run: func() error { _, err := decodeLoadVoltages([]uint16{0, 0, 0, 0, 2300, 5001, 2300}); return err }, want: "load voltage"},
		{name: "load frequency word count", run: func() error { _, err := decodeLoadFrequency(make([]uint16, 4)); return err }, want: "returned 4 words"},
		{name: "load frequency", run: func() error { _, err := decodeLoadFrequency([]uint16{10001, 0, 0, 0, 0}); return err }, want: "load frequency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeDirectLoadPowersAcceptOnlyVerifiedNonNegativeDomain(t *testing.T) {
	tests := []struct {
		name  string
		lows  []uint16
		highs []uint16
		want  directLoadPowers
	}{
		{
			name:  "reviewed live values",
			lows:  []uint16{19, 34, 248, 301},
			highs: []uint16{4995, 0, 0, 0, 0},
			want:  directLoadPowers{phasesW: [3]float64{19, 34, 248}, totalW: 301},
		},
		{
			name:  "live arbitrary phase mismatch regression",
			lows:  []uint16{80, 80, 83, 251},
			highs: []uint16{5000, 0, 0, 0, 0},
			want:  directLoadPowers{phasesW: [3]float64{80, 80, 83}, totalW: 251},
		},
		{
			name:  "low word sign bit is positive in joined signed32 value",
			lows:  []uint16{0xFFFF, 0x8000, 0x1234, 1},
			highs: []uint16{5000, 0, 0, 0, 0},
			want:  directLoadPowers{phasesW: [3]float64{65535, 32768, 4660}, totalW: 1},
		},
		{name: "zero", lows: make([]uint16, 4), highs: make([]uint16, 5), want: directLoadPowers{}},
		{
			name:  "full verified U16 boundary for every pair",
			lows:  []uint16{65535, 65535, 65535, 65535},
			highs: []uint16{5000, 0, 0, 0, 0},
			want:  directLoadPowers{phasesW: [3]float64{65535, 65535, 65535}, totalW: 65535},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeDirectLoadPowers(tt.lows, tt.highs)
			if err != nil || got != tt.want {
				t.Fatalf("decodeDirectLoadPowers() = %#v/%v, want %#v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeDirectLoadPowersFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		lows  []uint16
		highs []uint16
		want  string
	}{
		{name: "short low block", lows: make([]uint16, 3), highs: make([]uint16, 5), want: "expected 4"},
		{name: "long low block", lows: make([]uint16, 5), highs: make([]uint16, 5), want: "expected 4"},
		{name: "short high block", lows: make([]uint16, 4), highs: make([]uint16, 4), want: "expected 5"},
		{name: "long high block", lows: make([]uint16, 4), highs: make([]uint16, 6), want: "expected 5"},
		{name: "phase A above verified U16 boundary", lows: []uint16{1, 2, 3, 6}, highs: []uint16{5000, 1, 0, 0, 0}, want: "phase A high word register 656"},
		{name: "negative phase B encoding", lows: []uint16{1, 2, 3, 6}, highs: []uint16{5000, 0, 0xFFFF, 0, 0}, want: "phase B high word register 657"},
		{name: "phase C above verified U16 boundary", lows: []uint16{1, 2, 3, 6}, highs: []uint16{5000, 0, 0, 1, 0}, want: "phase C high word register 658"},
		{name: "negative total encoding", lows: []uint16{1, 2, 3, 6}, highs: []uint16{5000, 0, 0, 0, 0xFFFF}, want: "only verified non-negative"},
		{name: "total above verified U16 boundary", lows: []uint16{1, 2, 3, 6}, highs: []uint16{5000, 0, 0, 0, 1}, want: "total high word register 659"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeDirectLoadPowers(tt.lows, tt.highs); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeDirectLoadPowers(%#v, %#v) error = %v, want %q", tt.lows, tt.highs, err, tt.want)
			}
		})
	}
}

func TestDecodePVInputsUsesTwoCapabilityGatedChannels(t *testing.T) {
	want := pvInputs{
		powersW:     [2]float64{30, 40},
		voltagesV:   [2]float64{126.8, 130},
		currentsA:   [2]float64{0.2, 0.3},
		totalPowerW: 70,
	}
	got, err := decodePVInputs([]uint16{3, 4, 0, 0, 1268, 2, 1300, 3}, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PV inputs = %#v, want %#v", got, want)
	}

	if got, err := decodePVInputs(make([]uint16, 8), 12000); err != nil || got != (pvInputs{}) {
		t.Fatalf("zero PV inputs/error = %#v/%v", got, err)
	}
}

func TestDecodePVInputsRegisterMutationsStayChannelLocal(t *testing.T) {
	baseWords := []uint16{3, 4, 0, 0, 1268, 2, 1300, 3}
	base, err := decodePVInputs(baseWords, 12000)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		index int
		want  pvInputs
	}{
		{name: "PV1 power", index: 0, want: pvInputs{powersW: [2]float64{40, 40}, voltagesV: base.voltagesV, currentsA: base.currentsA, totalPowerW: 80}},
		{name: "PV2 power", index: 1, want: pvInputs{powersW: [2]float64{30, 50}, voltagesV: base.voltagesV, currentsA: base.currentsA, totalPowerW: 80}},
		{name: "PV1 voltage", index: 4, want: pvInputs{powersW: base.powersW, voltagesV: [2]float64{126.9, 130}, currentsA: base.currentsA, totalPowerW: 70}},
		{name: "PV1 current", index: 5, want: pvInputs{powersW: base.powersW, voltagesV: base.voltagesV, currentsA: [2]float64{0.3, 0.3}, totalPowerW: 70}},
		{name: "PV2 voltage", index: 6, want: pvInputs{powersW: base.powersW, voltagesV: [2]float64{126.8, 130.1}, currentsA: base.currentsA, totalPowerW: 70}},
		{name: "PV2 current", index: 7, want: pvInputs{powersW: base.powersW, voltagesV: base.voltagesV, currentsA: [2]float64{0.2, 0.4}, totalPowerW: 70}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := append([]uint16(nil), baseWords...)
			words[tt.index]++
			got, err := decodePVInputs(words, 12000)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("PV inputs = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodePVInputsAcceptsValidationBoundaries(t *testing.T) {
	for _, ratedPowerW := range []uint16{6000, 8000, 10000, 12000, 15000, 20000} {
		t.Run(fmt.Sprintf("%dW exact aggregate boundary", ratedPowerW), func(t *testing.T) {
			perChannelRaw := ratedPowerW / 10
			want := float64(ratedPowerW) * maximumPVToRatedPowerRatio
			got, err := decodePVInputs([]uint16{perChannelRaw, perChannelRaw, 0, 0, 10000, 1000, 10000, 1000}, float64(ratedPowerW))
			if err != nil || got.totalPowerW != want || got.voltagesV != [2]float64{maximumVoltageV, maximumVoltageV} || got.currentsA != [2]float64{maximumPVInputCurrentA, maximumPVInputCurrentA} {
				t.Fatalf("boundary PV inputs/error = %#v/%v, want total=%v voltage=%v current=%v", got, err, want, maximumVoltageV, maximumPVInputCurrentA)
			}
		})
		t.Run(fmt.Sprintf("%dW exact per-channel boundary", ratedPowerW), func(t *testing.T) {
			maximumRaw := uint16(float64(ratedPowerW) * maximumPVToRatedPowerRatio / 10)
			got, err := decodePVInputs([]uint16{maximumRaw, 0, 0, 0, 0, 0, 0, 0}, float64(ratedPowerW))
			if err != nil || got.powersW != [2]float64{float64(ratedPowerW) * maximumPVToRatedPowerRatio, 0} || got.totalPowerW != float64(ratedPowerW)*maximumPVToRatedPowerRatio {
				t.Fatalf("per-channel boundary PV inputs/error = %#v/%v", got, err)
			}
		})
	}
}

func TestDecodePVInputsFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		rated  float64
		want   string
	}{
		{name: "short word count", values: make([]uint16, 7), rated: 12000, want: "returned 7 words"},
		{name: "long word count", values: make([]uint16, 9), rated: 12000, want: "returned 9 words"},
		{name: "PV3 non-zero", values: []uint16{1, 2, 1, 0, 0, 0, 0, 0}, rated: 12000, want: "non-zero PV3/PV4"},
		{name: "PV4 non-zero", values: []uint16{1, 2, 0, 1, 0, 0, 0, 0}, rated: 12000, want: "non-zero PV3/PV4"},
		{name: "missing rated power", values: make([]uint16, 8), rated: 0, want: "rated power"},
		{name: "PV1 power bound", values: []uint16{2401, 0, 0, 0, 0, 0, 0, 0}, rated: 12000, want: "PV1 power"},
		{name: "PV2 power bound", values: []uint16{0, 2401, 0, 0, 0, 0, 0, 0}, rated: 12000, want: "PV2 power"},
		{name: "profile-gated bound", values: []uint16{1200, 1201, 0, 0, 0, 0, 0, 0}, rated: 12000, want: "profile-gated validation maximum"},
		{name: "PV1 voltage bound", values: []uint16{0, 0, 0, 0, 10001, 0, 0, 0}, rated: 12000, want: "PV1 voltage"},
		{name: "PV2 voltage bound", values: []uint16{0, 0, 0, 0, 0, 0, 10001, 0}, rated: 12000, want: "PV2 voltage"},
		{name: "PV1 current bound", values: []uint16{0, 0, 0, 0, 0, 1001, 0, 0}, rated: 12000, want: "PV1 current"},
		{name: "PV2 current bound", values: []uint16{0, 0, 0, 0, 0, 0, 0, 1001}, rated: 12000, want: "PV2 current"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodePVInputs(tt.values, tt.rated)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeInverterTemperaturesUsesDocumentedOffset(t *testing.T) {
	got, err := decodeInverterTemperatures([]uint16{1250, 1440})
	if err != nil {
		t.Fatal(err)
	}
	if got != [2]float64{25, 44} {
		t.Fatalf("DC/AC temperatures = %#v, want [25 44]", got)
	}

	for _, tt := range []struct {
		name   string
		values []uint16
		want   [2]float64
	}{
		{name: "lower boundary", values: []uint16{0, 0}, want: [2]float64{-100, -100}},
		{name: "upper boundary", values: []uint16{2000, 2000}, want: [2]float64{100, 100}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeInverterTemperatures(tt.values)
			if err != nil || got != tt.want {
				t.Fatalf("temperatures/error = %#v/%v, want %#v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeInverterTemperaturesFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   string
	}{
		{name: "word count", values: []uint16{1250}, want: "returned 1 words"},
		{name: "DC raw maximum", values: []uint16{3001, 1440}, want: "DC temperature raw"},
		{name: "AC raw maximum", values: []uint16{1250, 3001}, want: "AC temperature raw"},
		{name: "DC plausible maximum", values: []uint16{2001, 1440}, want: "DC temperature"},
		{name: "AC plausible maximum", values: []uint16{1250, 2001}, want: "AC temperature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeInverterTemperatures(tt.values)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeWarningFaultWordsPreservesExactU16Values(t *testing.T) {
	values := []uint16{0x0000, 0x0001, 0x7FFF, 0x8000, 0xFFFE, 0xFFFF}
	got, err := decodeWarningFaultWords(values)
	if err != nil {
		t.Fatal(err)
	}
	want := [6]uint16{0x0000, 0x0001, 0x7FFF, 0x8000, 0xFFFE, 0xFFFF}
	if got != want {
		t.Fatalf("warning/fault words = %#v, want %#v", got, want)
	}
}

func TestDecodeWarningFaultWordsRequiresExactlySixWords(t *testing.T) {
	for _, values := range [][]uint16{make([]uint16, 5), make([]uint16, 7)} {
		if _, err := decodeWarningFaultWords(values); err == nil || !strings.Contains(err.Error(), "expected 6") {
			t.Fatalf("decodeWarningFaultWords(%d words) error = %v, want exact-six error", len(values), err)
		}
	}
}

func TestDecodeRunStateAcceptsDocumentedCodesAndIgnoresRegisters501To505(t *testing.T) {
	for code := uint16(0); code <= 5; code++ {
		for _, trailing := range [][5]uint16{
			{},
			{0x0001, 0x7FFF, 0x8000, 0xFFFE, 0xFFFF},
		} {
			values := []uint16{code, trailing[0], trailing[1], trailing[2], trailing[3], trailing[4]}
			got, err := decodeRunState(values)
			if err != nil || got != code {
				t.Fatalf("decodeRunState(code=%d, trailing=%#v) = %d/%v, want %d/nil", code, trailing, got, err, code)
			}
		}
	}
}

func TestDecodeRunStateRejectsUnknownCodeAndWrongWordCount(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   string
	}{
		{name: "unknown code", values: []uint16{6, 0, 0, 0, 0, 0}, want: "documented maximum 5"},
		{name: "short block", values: make([]uint16, 5), want: "expected 6"},
		{name: "long block", values: make([]uint16, 7), want: "expected 6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeRunState(tt.values); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeRunState(%#v) error = %v, want %q", tt.values, err, tt.want)
			}
		})
	}
}

func TestDecodeGridFrequencyCurrentsUsesSignedScalesAndIgnoresRegisters613To619(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   gridFrequencyCurrents
	}{
		{
			name:   "reviewed live values",
			values: []uint16{4995, 53, 66, 155, 53, 63, 157, 100, 102, 120, 322},
			want:   gridFrequencyCurrents{frequencyHz: 49.95, currents: [3]float64{0.53, 0.66, 1.55}},
		},
		{
			name:   "signed boundaries and arbitrary ignored words",
			values: []uint16{10000, 10000, 0xD8F0, 0, 0x0001, 0x7FFF, 0x8000, 0xFFFE, 0xFFFF, 0x1234, 0xABCD},
			want:   gridFrequencyCurrents{frequencyHz: 100, currents: [3]float64{100, -100, 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeGridFrequencyCurrents(tt.values)
			if err != nil || got != tt.want {
				t.Fatalf("decodeGridFrequencyCurrents() = %#v/%v, want %#v/nil", got, err, tt.want)
			}
		})
	}

	base := []uint16{5000, 123, 0xFF16, 345, 0, 0, 0, 0, 0, 0, 0}
	mutated := append([]uint16(nil), base...)
	copy(mutated[4:], []uint16{0xFFFF, 0x8000, 0x7FFF, 0x1234, 0xABCD, 0x0001, 0xFFFE})
	first, err := decodeGridFrequencyCurrents(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := decodeGridFrequencyCurrents(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("ignored registers changed decode: base=%#v mutated=%#v", first, second)
	}
}

func TestDecodeGridFrequencyCurrentsFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   string
	}{
		{name: "short block", values: make([]uint16, 10), want: "expected 11"},
		{name: "long block", values: make([]uint16, 12), want: "expected 11"},
		{name: "frequency", values: []uint16{10001, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, want: "grid frequency"},
		{name: "positive current", values: []uint16{5000, 10001, 0, 0, 0, 0, 0, 0, 0, 0, 0}, want: "grid current L1"},
		{name: "negative current", values: []uint16{5000, 0, 0xD8EF, 0, 0, 0, 0, 0, 0, 0, 0}, want: "grid current L2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeGridFrequencyCurrents(tt.values); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeGridFrequencyCurrents(%#v) error = %v, want %q", tt.values, err, tt.want)
			}
		})
	}
}

func TestDecodeGridPowersUsesSignedLowWordFirstValues(t *testing.T) {
	tests := []struct {
		name string
		low  []uint16
		high []uint16
		want [4]float64
	}{
		{
			name: "positive import",
			low:  []uint16{101, 98, 117, 316},
			high: []uint16{0, 0, 0, 0},
			want: [4]float64{101, 98, 117, 316},
		},
		{
			name: "negative export",
			low:  []uint16{0xFCF7, 0xFCF4, 0xFCFA, 0xF6E5},
			high: []uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
			want: [4]float64{-777, -780, -774, -2331},
		},
		{
			name: "positive above int16 range",
			low:  []uint16{40000, 10000, 5000, 55000},
			high: []uint16{0, 0, 0, 0},
			want: [4]float64{40000, 10000, 5000, 55000},
		},
		{
			name: "negative below int16 range",
			low:  []uint16{0x63C0, 0xD8F0, 0xEC78, 0x2928},
			high: []uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
			want: [4]float64{-40000, -10000, -5000, -55000},
		},
		{
			name: "positive family-envelope boundary",
			low:  []uint16{40000, 20000, 5535, 65535},
			high: []uint16{0, 0, 0, 0},
			want: [4]float64{40000, 20000, 5535, 65535},
		},
		{
			name: "negative family-envelope boundary",
			low:  []uint16{0x63C0, 0xB1E0, 0xEA61, 0x0001},
			high: []uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF},
			want: [4]float64{-40000, -20000, -5535, -65535},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeGridPowers(tt.low, tt.high)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("grid powers = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDecodeGridPowersFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		low  []uint16
		high []uint16
		want string
	}{
		{name: "short low block", low: []uint16{1, 2, 3}, high: []uint16{0, 0, 0, 0}, want: "returned 3 words"},
		{name: "short high block", low: []uint16{1, 2, 3, 6}, high: []uint16{0, 0, 0}, want: "returned 3 words"},
		{name: "unsupported high word", low: []uint16{1, 2, 3, 6}, high: []uint16{0, 1, 0, 0}, want: "expected 0x0000 or 0xFFFF"},
		{name: "family envelope", low: []uint16{0, 0, 0, 0}, high: []uint16{0xFFFF, 0, 0, 0xFFFF}, want: "JKS family validation maximum"},
		{name: "phase sum", low: []uint16{100, 100, 100, 301}, high: []uint16{0, 0, 0, 0}, want: "phase sum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeGridPowers(tt.low, tt.high)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeOutputScalarsUsesOnlyApprovedValues(t *testing.T) {
	values := []uint16{
		0x0955, 0x0969, 0x093A,
		0x017C, 0x017C, 0x01CC,
		0x036F, 0x0373, 0x0444, 0x0B26, 0x0B26,
		0x1383,
	}
	got, err := decodeOutputScalars(values)
	if err != nil {
		t.Fatal(err)
	}
	if got.voltages != [3]float64{238.9, 240.9, 236.2} {
		t.Fatalf("voltages = %#v", got.voltages)
	}
	if got.currents != [3]float64{3.8, 3.8, 4.6} {
		t.Fatalf("currents = %#v", got.currents)
	}
	if got.frequencyHz != 49.95 {
		t.Fatalf("frequency = %v, want 49.95", got.frequencyHz)
	}

	// Signed current decoding is independent of the five intentionally ignored
	// output-power words in the middle of the fixed contiguous read.
	values = []uint16{2354, 2401, 2358, 0xFFF6, 0xFFEC, 60, 0xFFFF, 0x8000, 0x7FFF, 0xFFFF, 0x0000, 5000}
	got, err = decodeOutputScalars(values)
	if err != nil {
		t.Fatal(err)
	}
	if got.currents != [3]float64{-0.1, -0.2, 0.6} || got.frequencyHz != 50 {
		t.Fatalf("signed currents/frequency = %#v/%v", got.currents, got.frequencyHz)
	}
}

func TestDecodeOutputScalarsRangeGuards(t *testing.T) {
	valid := []uint16{2300, 2300, 2300, 0, 0, 0, 0, 0, 0, 0, 0, 5000}
	tests := []struct {
		name   string
		mutate func([]uint16) []uint16
		want   string
	}{
		{name: "word count", mutate: func(values []uint16) []uint16 { return values[:11] }, want: "returned 11 words"},
		{name: "voltage", mutate: func(values []uint16) []uint16 { values[1] = 5001; return values }, want: "output voltage"},
		{name: "positive current", mutate: func(values []uint16) []uint16 { values[4] = 10001; return values }, want: "output current"},
		{name: "negative current", mutate: func(values []uint16) []uint16 { values[4] = 0xD8EF; return values }, want: "output current"},
		{name: "frequency", mutate: func(values []uint16) []uint16 { values[11] = 10001; return values }, want: "output frequency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := append([]uint16(nil), valid...)
			_, err := decodeOutputScalars(tt.mutate(values))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeOutputActivePowersAcceptsConservativeSignedDomain(t *testing.T) {
	tests := []struct {
		name  string
		lows  [5]uint16
		highs [5]uint16
		want  [4]float64
	}{
		{
			name: "reviewed live values",
			lows: [5]uint16{879, 883, 1092, 2854, 2854},
			want: [4]float64{879, 883, 1092, 2854},
		},
		{
			// The exact negative magnitudes are synthetic. Production has observed
			// R691=0xFFFF, which establishes that this signed-extension shape occurs.
			name:  "mixed signed values with observed negative high-word shape",
			lows:  [5]uint16{0xFFF6, 0xFFEC, 117, 87, 0xFFFD},
			highs: [5]uint16{0xFFFF, 0xFFFF, 0, 0, 0xFFFF},
			want:  [4]float64{-10, -20, 117, 87},
		},
		{
			name:  "all phases negative",
			lows:  [5]uint16{0xFFFF, 0xFFFE, 0xFFFD, 0xFFFA, 0},
			highs: [5]uint16{0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0},
			want:  [4]float64{-1, -2, -3, -6},
		},
		{name: "zero", want: [4]float64{}},
		{
			name: "symmetric signed boundaries",
			lows: [5]uint16{0x7FFF, 0x8001, 0, 0, 0xFFFF},
			// R695 is deliberately arbitrary: apparent power is outside the
			// approved semantic surface and cannot affect active power.
			highs: [5]uint16{0, 0xFFFF, 0, 0, 0xFFFF},
			want:  [4]float64{32767, -32767, 0, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words := make([]uint16, 12)
			copy(words[6:11], tt.lows[:])
			got, err := decodeOutputActivePowers(words, tt.highs[:])
			if err != nil || got != tt.want {
				t.Fatalf("decodeOutputActivePowers() = %#v/%v, want %#v/nil", got, err, tt.want)
			}
		})
	}
}

func TestDecodeOutputActivePowersIgnoresApparentPowerPair(t *testing.T) {
	words := liveOutputScalarWords()
	highs := make([]uint16, 5)
	want, err := decodeOutputActivePowers(words, highs)
	if err != nil {
		t.Fatal(err)
	}

	words[10] = 0xFFFF // R637 apparent-power low candidate.
	highs[4] = 0xFFFF  // R695 apparent-power high candidate.
	got, err := decodeOutputActivePowers(words, highs)
	if err != nil || got != want {
		t.Fatalf("apparent-power mutation changed active powers: got=%#v/%v want=%#v/nil", got, err, want)
	}
}

func TestDecodeOutputActivePowersFailsClosed(t *testing.T) {
	validWords := func() []uint16 {
		values := make([]uint16, 12)
		copy(values[6:11], []uint16{1, 2, 3, 6, 0xFFFF})
		return values
	}
	tests := []struct {
		name  string
		words []uint16
		high  []uint16
		want  string
	}{
		{name: "short scalar block", words: make([]uint16, 11), high: make([]uint16, 5), want: "expected 12"},
		{name: "long scalar block", words: make([]uint16, 13), high: make([]uint16, 5), want: "expected 12"},
		{name: "short high block", words: validWords(), high: make([]uint16, 4), want: "expected 5"},
		{name: "long high block", words: validWords(), high: make([]uint16, 6), want: "expected 5"},
		{name: "noncanonical positive high word", words: validWords(), high: []uint16{0, 0, 1, 0, 0}, want: "register 693"},
		{name: "noncanonical negative high word", words: validWords(), high: []uint16{0, 0, 0xFFFE, 0, 0}, want: "register 693"},
		{name: "positive value above safe boundary", words: func() []uint16 { values := validWords(); values[6] = 0x8000; values[9] = 0x8005; return values }(), high: make([]uint16, 5), want: "registers 633/691"},
		{name: "negative value below safe boundary", words: func() []uint16 { values := validWords(); values[6] = 0x8000; values[9] = 0x8000; return values }(), high: []uint16{0xFFFF, 0, 0, 0xFFFF, 0}, want: "registers 633/691"},
		{name: "phase sum mismatch", words: func() []uint16 { values := validWords(); values[9] = 7; return values }(), high: make([]uint16, 5), want: "phase sum"},
		{
			name: "torn negative to zero crossing regression",
			words: func() []uint16 {
				values := make([]uint16, 12)
				copy(values[6:10], []uint16{65436, 20, 30, 65486}) // -100 + 20 + 30 = -50 before highs changed.
				return values
			}(),
			high: make([]uint16, 5),
			want: "registers 633/691",
		},
		{
			name: "torn positive to negative crossing",
			words: func() []uint16 {
				values := make([]uint16, 12)
				copy(values[6:10], []uint16{100, 20, 30, 150})
				return values
			}(),
			high: []uint16{0xFFFF, 0, 0, 0xFFFF, 0},
			want: "registers 633/691",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeOutputActivePowers(tt.words, tt.high); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodeOutputActivePowers() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeEnergyMetricsUsesLowWordFirstAndDuplicatesDailyPV(t *testing.T) {
	metrics, err := decodeEnergyMetrics(liveEnergyWords())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"Etdy_cg1": 1.2, "Etdy_dcg1": 0.8,
		"t_cg_n1": 1038.7, "t_dcg_n1": 1059.4,
		"E_B_D": 3.5, "E_S_D": 57.4,
		"E_B_TO": 2504.3, "E_S_TO": 16770.4,
		"Etdy_use1": 6.2, "E_C_T": 2769.7,
		"PV_D_P_G": 67.1, "Etdy_ge1": 67.1,
	}
	if len(metrics) != len(want) {
		t.Fatalf("metrics = %d, want %d", len(metrics), len(want))
	}
	for _, metric := range metrics {
		if metric.value != want[metric.key] {
			t.Fatalf("metric %s = %v, want %v", metric.key, metric.value, want[metric.key])
		}
		delete(want, metric.key)
	}
	if len(want) != 0 {
		t.Fatalf("missing metrics: %#v", want)
	}
	if _, err := decodeEnergyMetrics(make([]uint16, 15)); err == nil {
		t.Fatal("15-word energy block accepted")
	}
}

func TestDecodePowerAndACRelayStatusValidatesPowerCodeAndPreservesRawMask(t *testing.T) {
	tests := []struct {
		name            string
		values          []uint16
		wantPowerStatus uint16
		wantRelayMask   uint16
	}{
		{name: "cloud-correlated mask", values: []uint16{1, 7}, wantPowerStatus: 1, wantRelayMask: 7},
		{name: "off with zero mask", values: []uint16{0, 0}},
		{name: "upper power bits ignored and relay bits preserved", values: []uint16{0xFFF1, 0xFFFF}, wantPowerStatus: 1, wantRelayMask: 0xFFFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPowerStatus, gotRelayMask, err := decodePowerAndACRelayStatus(tt.values)
			if err != nil || gotPowerStatus != tt.wantPowerStatus || gotRelayMask != tt.wantRelayMask {
				t.Fatalf("decodePowerAndACRelayStatus() = 0x%04X/0x%04X/%v, want 0x%04X/0x%04X/nil", gotPowerStatus, gotRelayMask, err, tt.wantPowerStatus, tt.wantRelayMask)
			}
		})
	}
}

func TestDecodePowerAndACRelayStatusFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		values []uint16
		want   string
	}{
		{name: "short block", values: []uint16{1}, want: "expected 2"},
		{name: "long block", values: []uint16{1, 7, 0}, want: "expected 2"},
		{name: "undocumented low-nibble code", values: []uint16{0xFFF2, 7}, want: "register 551"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := decodePowerAndACRelayStatus(tt.values); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("decodePowerAndACRelayStatus() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCanonicalMetricDelegatesToJinkoCanonicalization(t *testing.T) {
	tests := []struct {
		key   string
		value float64
		group string
		unit  string
	}{
		{key: "B_T1", value: 28, group: "temperature", unit: "C"},
		{key: "BMST", value: 28, group: "bms", unit: "C"},
		{key: "AC", value: 7, group: "status", unit: ""},
		{key: "C_V_L1", value: 230.1, group: "consumption", unit: "V"},
		{key: "C_V_L2", value: 230.2, group: "consumption", unit: "V"},
		{key: "C_V_L3", value: 230.3, group: "consumption", unit: "V"},
		{key: "L_F", value: 50, group: "consumption", unit: "Hz"},
		{key: "S_P_T", value: 70, group: "electric", unit: "W"},
		{key: "T_DC", value: 25, group: "temperature", unit: "C"},
		{key: "AC_T", value: 44, group: "temperature", unit: "C"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			want, ok := jinko.CanonicalizeMetric(model.Metric{Key: tt.key, Value: tt.value})
			if !ok {
				t.Fatalf("%s missing from Jinko schema", tt.key)
			}
			got, err := canonicalMetric(tt.key, tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != want || got.Group != tt.group || got.Unit != tt.unit {
				t.Fatalf("metric = %#v, want Jinko canonical %#v with group/unit %q/%q", got, want, tt.group, tt.unit)
			}
		})
	}
}
