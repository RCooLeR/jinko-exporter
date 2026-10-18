package modbus

import "fmt"

// decodeBMSMirrors publishes the HV-scale subset correlated with a complete
// target capture and nearby cloud data on 2026-10-08. R211/R212/R218 were only
// observed at zero; R216 current signedness/sentinels remain unresolved. Those
// four words are deliberately ignored, even when nonzero or 0xFFFF.
func decodeBMSMirrors(words []uint16) ([]metricValue, error) {
	if err := requireWordCount("BMS telemetry mirrors registers 210-218", words, 9); err != nil {
		return nil, err
	}
	chargeVoltage := float64(words[0]) / 10
	voltage := float64(words[5]) / 10
	if chargeVoltage > maximumVoltageV {
		return nil, fmt.Errorf("BMS charge voltage register 210 %.1fV exceeds %.1fV validation maximum", chargeVoltage, maximumVoltageV)
	}
	if voltage > maximumVoltageV {
		return nil, fmt.Errorf("BMS voltage register 215 %.1fV exceeds %.1fV validation maximum", voltage, maximumVoltageV)
	}
	if words[4] > 100 {
		return nil, fmt.Errorf("BMS SOC register 214 %d%% exceeds documented maximum 100%%", words[4])
	}
	temperature := float64(int(words[7])-1000) / 10
	if words[7] > maximumTemperatureRaw || temperature < minimumTemperatureC || temperature > maximumTemperatureC {
		return nil, fmt.Errorf("BMS temperature register 217 %.1fC is outside plausible range %.1f..%.1fC", temperature, minimumTemperatureC, maximumTemperatureC)
	}
	return []metricValue{
		{key: "BMS_C_V", value: chargeVoltage},
		// The documented nonnegative U16 whole-amp limit is a wire domain,
		// not a battery capacity rating or an inferred sentinel convention.
		{key: "BMS_D_C_L", value: float64(words[3])},
		{key: "BMS_SOC", value: float64(words[4])},
		{key: "BMS_B_V1", value: voltage},
		{key: "BMST", value: temperature},
	}, nil
}
