package modbus

import (
	"fmt"
	"math"
)

const (
	expectedMPPTCount  = uint16(2)
	expectedPhaseCount = uint16(3)

	maximumTemperatureRaw = uint16(3000)
	minimumTemperatureC   = -100.0
	maximumTemperatureC   = 100.0
	maximumVoltageV       = 1000.0

	maximumAbsoluteGridPowerW   = 65535.0
	maximumAbsoluteOutputPowerW = int64(0x7FFF)
	maximumOutputPhaseVoltageV  = 500.0
	maximumAbsoluteOutputAmps   = 100.0
	maximumOutputFrequencyHz    = 100.0
	maximumLoadPhaseVoltageV    = 500.0
	maximumLoadFrequencyHz      = 100.0
	maximumGridFrequencyHz      = 100.0
	maximumAbsoluteGridAmps     = 100.0
	maximumPVToRatedPowerRatio  = 2.0
	// The supported family spans several ratings and hardware revisions. Keep
	// the PV-current guard deliberately above their model-specific input-current
	// limits, using the same broad 100 A telemetry envelope as the validated AC
	// current fields, while still rejecting corrupt full-range U16 values.
	maximumPVInputCurrentA = 100.0
)

type profileCapabilities struct {
	ratedPowerW float64
	mpptCount   uint16
	phaseCount  uint16
}

type metricValue struct {
	key   string
	value float64
}

type outputScalars struct {
	voltages    [3]float64
	currents    [3]float64
	frequencyHz float64
}

type gridFrequencyCurrents struct {
	frequencyHz float64
	currents    [3]float64
}

type directLoadPowers struct {
	phasesW [3]float64
	totalW  float64
}

type pvInputs struct {
	powersW     [2]float64
	voltagesV   [2]float64
	currentsA   [2]float64
	totalPowerW float64
}

func decodeGeneratorPortModeZeroOnly(values []uint16) error {
	if err := requireWordCount("generator port-mode register 133", values, 1); err != nil {
		return err
	}
	if values[0] != 0 {
		return fmt.Errorf("generator port-mode register 133 is 0x%04X; only the live-verified zero domain is accepted", values[0])
	}
	return nil
}

func decodeGeneratorEnergyZeroOnly(values []uint16) ([]metricValue, error) {
	if err := requireWordCount("generator energy/runtime registers 536-539", values, 4); err != nil {
		return nil, err
	}
	if err := requireZeroGeneratorWords(values, 536); err != nil {
		return nil, err
	}

	// The target's exact local read was all zero, consistent with previously
	// observed cloud generator zeros.
	// Until a non-zero capture validates scale and word pairing, expose only
	// that observed domain and fail closed before publishing any other value.
	return []metricValue{
		{key: "R_T_D", value: 0},
		{key: "GEN_P_D", value: 0},
		{key: "GEN_P_TO", value: 0},
	}, nil
}

func decodeGeneratorElectricalZeroOnly(values []uint16) ([]metricValue, error) {
	if err := requireWordCount("generator voltage/power registers 661-671", values, 11); err != nil {
		return nil, err
	}
	if err := requireZeroGeneratorWords(values, 661); err != nil {
		return nil, err
	}

	// EG_P_CT1 is intentionally a zero-domain compatibility view of the same
	// observed total as GEN_P_T. No non-zero equivalence is claimed or accepted.
	return []metricValue{
		{key: "GEN_P_L1", value: 0},
		{key: "GEN_P_L2", value: 0},
		{key: "GEN_P_L3", value: 0},
		{key: "GEN_V_L1", value: 0},
		{key: "GEN_V_L2", value: 0},
		{key: "GEN_V_L3", value: 0},
		{key: "EG_P_CT1", value: 0},
		{key: "GEN_P_T", value: 0},
	}, nil
}

func requireZeroGeneratorWords(values []uint16, startRegister uint16) error {
	for index, value := range values {
		if value != 0 {
			return fmt.Errorf("generator register %d is 0x%04X; only the live-verified all-zero generator domain is accepted", int(startRegister)+index, value)
		}
	}
	return nil
}

func decodeCapabilities(values []uint16) (profileCapabilities, error) {
	if err := requireWordCount("capability registers 20-22", values, 3); err != nil {
		return profileCapabilities{}, err
	}
	ratedPowerRaw := uint32(values[0]) | uint32(values[1])<<16
	mpptCount := (values[2] >> 8) & 0x0F
	phaseCount := values[2] & 0x0F
	if !isSupportedJKSFamilyRatedPowerRaw(ratedPowerRaw) || mpptCount != expectedMPPTCount || phaseCount != expectedPhaseCount || values[2] != 0x0203 {
		return profileCapabilities{}, fmt.Errorf(
			"refusing capability profile rated=%0.1fW mppt=%d phases=%d raw22=0x%04X; expected JKS H-EI family rated power 6000/8000/10000/12000/15000/20000W with 2 MPPT/3 phases/0x0203",
			float64(ratedPowerRaw)/10, mpptCount, phaseCount, values[2],
		)
	}
	return profileCapabilities{
		ratedPowerW: float64(ratedPowerRaw) / 10,
		mpptCount:   mpptCount,
		phaseCount:  phaseCount,
	}, nil
}

func isSupportedJKSFamilyRatedPowerRaw(raw uint32) bool {
	switch raw {
	case 60000, 80000, 100000, 120000, 150000, 200000: // 0.1 W/count
		return true
	default:
		return false
	}
}

func decodeUPSPower(values []uint16) (float64, error) {
	if err := requireWordCount("UPS load power register 643", values, 1); err != nil {
		return 0, err
	}
	return float64(values[0]), nil
}

func decodeLoadVoltages(values []uint16) ([3]float64, error) {
	if err := requireWordCount("UPS/load registers 640-646", values, 7); err != nil {
		return [3]float64{}, err
	}

	result := [3]float64{}
	for index := range result {
		// Registers 640-643 are intentionally ignored incomplete power words.
		result[index] = float64(values[index+4]) / 10
		if result[index] > maximumLoadPhaseVoltageV {
			return [3]float64{}, fmt.Errorf("load voltage L%d %.1fV exceeds %.1fV validation maximum", index+1, result[index], maximumLoadPhaseVoltageV)
		}
	}
	return result, nil
}

func decodeLoadFrequency(values []uint16) (float64, error) {
	if err := requireWordCount("load registers 655-659", values, 5); err != nil {
		return 0, err
	}

	// This scalar decoder uses register 655 only. The same response's phase and
	// total high words 656-659 are validated by decodeDirectLoadPowers.
	frequencyHz := float64(values[0]) / 100
	if frequencyHz > maximumLoadFrequencyHz {
		return 0, fmt.Errorf("load frequency %.2fHz exceeds %.2fHz validation maximum", frequencyHz, maximumLoadFrequencyHz)
	}
	return frequencyHz, nil
}

func decodeDirectLoadPowers(lowWords, frequencyWords []uint16) (directLoadPowers, error) {
	if err := requireWordCount("direct-load power low-word registers 650-653", lowWords, 4); err != nil {
		return directLoadPowers{}, err
	}
	if err := requireWordCount("load frequency/high-word registers 655-659", frequencyWords, 5); err != nil {
		return directLoadPowers{}, err
	}

	// The primary maps pair low words 650-653 with high words 656-659 as
	// low-word-first signed 32-bit values. Live verification currently covers
	// only the non-negative zero-high-word domain, so every pair fails closed
	// when its high word is non-zero (including 0xFFFF sign extension).
	labels := [...]string{"phase A", "phase B", "phase C", "total"}
	values := [4]float64{}
	for index := range values {
		high := frequencyWords[index+1]
		if high != 0 {
			return directLoadPowers{}, fmt.Errorf("direct-load %s high word register %d is 0x%04X; only verified non-negative zero-high-word values are accepted", labels[index], 656+index, high)
		}
		raw := uint32(lowWords[index]) | uint32(high)<<16
		values[index] = float64(int32(raw))
	}

	// The dedicated total is independent. A same-frame live capture disproved
	// exact phase-sum equality, so it must not be used as a validation gate.
	return directLoadPowers{
		phasesW: [3]float64{values[0], values[1], values[2]},
		totalW:  values[3],
	}, nil
}

func decodePVInputs(values []uint16, ratedPowerW float64) (pvInputs, error) {
	if err := requireWordCount("PV input registers 672-679", values, 8); err != nil {
		return pvInputs{}, err
	}
	if values[2] != 0 || values[3] != 0 {
		return pvInputs{}, fmt.Errorf("refusing PV input profile with non-zero PV3/PV4 power words 0x%04X/0x%04X", values[2], values[3])
	}
	if ratedPowerW <= 0 {
		return pvInputs{}, fmt.Errorf("rated power must be positive for PV power validation")
	}

	// Device type 0x0006 uses 10 W/count. Registers 676/678 are PV1/PV2
	// voltage at 0.1 V/count; 677/679 are PV1/PV2 current at 0.1 A/count.
	result := pvInputs{
		powersW:   [2]float64{float64(values[0]) * 10, float64(values[1]) * 10},
		voltagesV: [2]float64{float64(values[4]) / 10, float64(values[6]) / 10},
		currentsA: [2]float64{float64(values[5]) / 10, float64(values[7]) / 10},
	}
	result.totalPowerW = result.powersW[0] + result.powersW[1]

	// Apply the existing conservative two-times-rated envelope both to each
	// channel and their aggregate. The per-channel check rejects a corrupt word
	// independently while allowing either MPPT to carry the full accepted total.
	maximumPowerW := ratedPowerW * maximumPVToRatedPowerRatio
	for index := range result.powersW {
		if result.powersW[index] > maximumPowerW {
			return pvInputs{}, fmt.Errorf("PV%d power %.0fW exceeds %.0fW profile-gated validation maximum", index+1, result.powersW[index], maximumPowerW)
		}
		if result.voltagesV[index] > maximumVoltageV {
			return pvInputs{}, fmt.Errorf("PV%d voltage %.1fV exceeds %.1fV validation maximum", index+1, result.voltagesV[index], maximumVoltageV)
		}
		if result.currentsA[index] > maximumPVInputCurrentA {
			return pvInputs{}, fmt.Errorf("PV%d current %.1fA exceeds %.1fA validation maximum", index+1, result.currentsA[index], maximumPVInputCurrentA)
		}
	}
	if result.totalPowerW > maximumPowerW {
		return pvInputs{}, fmt.Errorf("total PV power %.0fW exceeds %.0fW profile-gated validation maximum", result.totalPowerW, maximumPowerW)
	}
	return result, nil
}

func decodeInverterTemperatures(values []uint16) ([2]float64, error) {
	if err := requireWordCount("DC/AC temperature registers 540-541", values, 2); err != nil {
		return [2]float64{}, err
	}

	labels := [...]string{"DC", "AC"}
	result := [2]float64{}
	for index, raw := range values {
		if raw > maximumTemperatureRaw {
			return [2]float64{}, fmt.Errorf("%s temperature raw value %d exceeds documented maximum %d", labels[index], raw, maximumTemperatureRaw)
		}
		result[index] = float64(int(raw)-1000) / 10
		if result[index] < minimumTemperatureC || result[index] > maximumTemperatureC {
			return [2]float64{}, fmt.Errorf("%s temperature %.1fC is outside plausible range %.1f..%.1fC", labels[index], result[index], minimumTemperatureC, maximumTemperatureC)
		}
	}
	return result, nil
}

func decodeGridVoltages(values []uint16) ([3]float64, error) {
	if err := requireWordCount("grid voltage registers 598-600", values, 3); err != nil {
		return [3]float64{}, err
	}
	result := [3]float64{}
	for index, raw := range values {
		result[index] = float64(raw) / 10
		if result[index] > maximumVoltageV {
			return [3]float64{}, fmt.Errorf("grid voltage L%d %.1fV exceeds %.1fV validation maximum", index+1, result[index], maximumVoltageV)
		}
	}
	return result, nil
}

func decodeGridPowers(lowWords, highWords []uint16) ([4]float64, error) {
	if err := requireWordCount("grid power low-word registers 622-625", lowWords, 4); err != nil {
		return [4]float64{}, err
	}
	if err := requireWordCount("grid power high-word registers 687-690", highWords, 4); err != nil {
		return [4]float64{}, err
	}

	labels := [...]string{"L1", "L2", "L3", "total"}
	result := [4]float64{}
	rawValues := [4]int32{}
	for index := range lowWords {
		low := lowWords[index]
		high := highWords[index]
		if high != 0x0000 && high != 0xFFFF {
			return [4]float64{}, fmt.Errorf("grid power %s high word 0x%04X is outside the verified JKS family envelope; expected 0x0000 or 0xFFFF", labels[index], high)
		}
		raw := int32(uint32(low) | uint32(high)<<16)
		if math.Abs(float64(raw)) > maximumAbsoluteGridPowerW {
			return [4]float64{}, fmt.Errorf("grid power %s %dW exceeds %.0fW JKS family validation maximum", labels[index], raw, maximumAbsoluteGridPowerW)
		}
		rawValues[index] = raw
		result[index] = float64(raw)
	}

	phaseSum := int64(rawValues[0]) + int64(rawValues[1]) + int64(rawValues[2])
	if phaseSum != int64(rawValues[3]) {
		return [4]float64{}, fmt.Errorf("grid power phase sum %dW does not equal total %dW", phaseSum, rawValues[3])
	}
	return result, nil
}

func decodeOutputScalars(values []uint16) (outputScalars, error) {
	if err := requireWordCount("output registers 627-638", values, 12); err != nil {
		return outputScalars{}, err
	}

	result := outputScalars{}
	for index := range 3 {
		result.voltages[index] = float64(values[index]) / 10
		if result.voltages[index] > maximumOutputPhaseVoltageV {
			return outputScalars{}, fmt.Errorf("output voltage L%d %.1fV exceeds %.1fV validation maximum", index+1, result.voltages[index], maximumOutputPhaseVoltageV)
		}

		result.currents[index] = float64(int16(values[index+3])) / 100
		if math.Abs(result.currents[index]) > maximumAbsoluteOutputAmps {
			return outputScalars{}, fmt.Errorf("output current L%d %.2fA exceeds %.2fA absolute validation maximum", index+1, result.currents[index], maximumAbsoluteOutputAmps)
		}
	}

	// Output powers are decoded separately with their paired high-word block so
	// these scalar fields never interpret an incomplete 32-bit value.
	result.frequencyHz = float64(values[11]) / 100
	if result.frequencyHz > maximumOutputFrequencyHz {
		return outputScalars{}, fmt.Errorf("output frequency %.2fHz exceeds %.2fHz validation maximum", result.frequencyHz, maximumOutputFrequencyHz)
	}
	return result, nil
}

func decodeOutputActivePowers(outputWords, highWords []uint16) ([4]float64, error) {
	if err := requireWordCount("output registers 627-638", outputWords, 12); err != nil {
		return [4]float64{}, err
	}
	if err := requireWordCount("output power high-word registers 691-695", highWords, 5); err != nil {
		return [4]float64{}, err
	}

	// Registers 633-636 pair low-word-first with 691-694 as signed 32-bit
	// values. The symmetric 0x7FFF envelope is deliberately narrower than the
	// wire type: because the low and high blocks require separate reads, it
	// makes a stale sign-extension word fail closed if the reads straddle zero.
	// This is an encoding-coherence guard, not an inverter-rating limit.
	labels := [...]string{"L1", "L2", "L3", "total"}
	lowWords := outputWords[6:10]
	result := [4]float64{}
	rawValues := [4]int32{}
	for index, low := range lowWords {
		high := highWords[index]
		if high != 0x0000 && high != 0xFFFF {
			return [4]float64{}, fmt.Errorf("output active power %s high word register %d is 0x%04X; expected signed extension 0x0000 or 0xFFFF", labels[index], 691+index, high)
		}
		raw := int32(uint32(low) | uint32(high)<<16)
		if int64(raw) < -maximumAbsoluteOutputPowerW || int64(raw) > maximumAbsoluteOutputPowerW {
			return [4]float64{}, fmt.Errorf("output active power %s registers %d/%d decode to %dW; safe signed validation range is -%d..%dW", labels[index], 633+index, 691+index, raw, maximumAbsoluteOutputPowerW, maximumAbsoluteOutputPowerW)
		}
		rawValues[index] = raw
		result[index] = float64(raw)
	}

	phaseSum := int64(rawValues[0]) + int64(rawValues[1]) + int64(rawValues[2])
	if phaseSum != int64(rawValues[3]) {
		return [4]float64{}, fmt.Errorf("output active power signed phase sum %dW does not equal total %dW", phaseSum, rawValues[3])
	}

	// R637/R695 are an unverified apparent-power candidate. They are read only
	// because both fixed blocks are contiguous and are intentionally ignored.
	return result, nil
}

func decodeBatteryTemperature(values []uint16) (float64, error) {
	if err := requireWordCount("battery temperature register 586", values, 1); err != nil {
		return 0, err
	}
	if values[0] > maximumTemperatureRaw {
		return 0, fmt.Errorf("battery temperature raw value %d exceeds documented maximum %d", values[0], maximumTemperatureRaw)
	}
	temperatureC := float64(int(values[0])-1000) / 10
	if temperatureC < minimumTemperatureC || temperatureC > maximumTemperatureC {
		return 0, fmt.Errorf("battery temperature %.1fC is outside plausible range %.1f..%.1fC", temperatureC, minimumTemperatureC, maximumTemperatureC)
	}
	return temperatureC, nil
}

func decodeBatteryVoltageSOC(values []uint16) (voltageV float64, socPercent float64, err error) {
	if err := requireWordCount("battery voltage/SOC registers 587-588", values, 2); err != nil {
		return 0, 0, err
	}
	voltageV = float64(values[0]) / 10
	if voltageV > maximumVoltageV {
		return 0, 0, fmt.Errorf("battery voltage %.1fV exceeds %.1fV validation maximum", voltageV, maximumVoltageV)
	}
	if values[1] > 100 {
		return 0, 0, fmt.Errorf("battery SOC %d%% exceeds documented maximum 100%%", values[1])
	}
	return voltageV, float64(values[1]), nil
}

func decodeBatteryFlow(values []uint16) (powerW float64, currentA float64, err error) {
	if err := requireWordCount("battery power/current registers 590-591", values, 2); err != nil {
		return 0, 0, err
	}
	return float64(int16(values[0])) * 10, float64(int16(values[1])) / 100, nil
}

func decodeEnergyMetrics(values []uint16) ([]metricValue, error) {
	if err := requireWordCount("energy registers 514-529", values, 16); err != nil {
		return nil, err
	}
	u16 := func(index int) float64 {
		return float64(values[index]) / 10
	}
	u32LowFirst := func(lowIndex int) float64 {
		raw := uint32(values[lowIndex]) | uint32(values[lowIndex+1])<<16
		return float64(raw) / 10
	}
	dailyPV := u16(15)
	return []metricValue{
		{key: "Etdy_cg1", value: u16(0)},
		{key: "Etdy_dcg1", value: u16(1)},
		{key: "t_cg_n1", value: u32LowFirst(2)},
		{key: "t_dcg_n1", value: u32LowFirst(4)},
		{key: "E_B_D", value: u16(6)},
		{key: "E_S_D", value: u16(7)},
		{key: "E_B_TO", value: u32LowFirst(8)},
		{key: "E_S_TO", value: u32LowFirst(10)},
		{key: "Etdy_use1", value: u16(12)},
		{key: "E_C_T", value: u32LowFirst(13)},
		// The two cloud schemas intentionally expose register 529 under both
		// canonical keys. Keeping both preserves source failover compatibility.
		{key: "PV_D_P_G", value: dailyPV},
		{key: "Etdy_ge1", value: dailyPV},
	}, nil
}

func decodeWarningFaultWords(values []uint16) ([6]uint16, error) {
	if err := requireWordCount("warning/fault registers 553-558", values, 6); err != nil {
		return [6]uint16{}, err
	}
	result := [6]uint16{}
	copy(result[:], values)
	return result, nil
}

func decodePowerAndACRelayStatus(values []uint16) (uint16, uint16, error) {
	if err := requireWordCount("power/relay status registers 551-552", values, 2); err != nil {
		return 0, 0, err
	}

	// The primary map defines only the low nibble of register 551 as the
	// turn-off/on signal and documents 0/1. Upper bits are deliberately ignored
	// because the map does not define them. Register 552 is itself the canonical
	// raw AC relay bitmask, so every U16 bit is preserved, including reserved or
	// firmware-specific bits observed by the cloud source.
	powerStatus := values[0] & 0x000F
	if powerStatus > 1 {
		return 0, 0, fmt.Errorf("power status register 551 low-nibble code %d exceeds documented maximum 1", powerStatus)
	}
	return powerStatus, values[1], nil
}

func decodeRunState(values []uint16) (uint16, error) {
	if err := requireWordCount("run-state registers 500-505", values, 6); err != nil {
		return 0, err
	}
	if values[0] > 5 {
		return 0, fmt.Errorf("run-state register 500 code %d exceeds documented maximum 5", values[0])
	}
	// Registers 501-505 are intentionally ignored. Their energy semantics did
	// not correlate with the target cloud data and are not production metrics.
	return values[0], nil
}

func decodeGridFrequencyCurrents(values []uint16) (gridFrequencyCurrents, error) {
	if err := requireWordCount("grid registers 609-619", values, 11); err != nil {
		return gridFrequencyCurrents{}, err
	}

	result := gridFrequencyCurrents{frequencyHz: float64(values[0]) / 100}
	if result.frequencyHz > maximumGridFrequencyHz {
		return gridFrequencyCurrents{}, fmt.Errorf("grid frequency %.2fHz exceeds %.2fHz validation maximum", result.frequencyHz, maximumGridFrequencyHz)
	}
	for index := range result.currents {
		result.currents[index] = float64(int16(values[index+1])) / 100
		if math.Abs(result.currents[index]) > maximumAbsoluteGridAmps {
			return gridFrequencyCurrents{}, fmt.Errorf("grid current L%d %.2fA exceeds %.2fA absolute validation maximum", index+1, result.currents[index], maximumAbsoluteGridAmps)
		}
	}

	// Registers 613-619 are intentionally ignored. They contain external-CT
	// currents and incomplete low-word-only power values, so they cannot affect
	// either this decoder or the emitted canonical metrics.
	return result, nil
}

func requireWordCount(block string, values []uint16, expected int) error {
	if len(values) != expected {
		return fmt.Errorf("%s returned %d words; expected %d", block, len(values), expected)
	}
	return nil
}
