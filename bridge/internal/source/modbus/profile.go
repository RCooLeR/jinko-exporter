package modbus

const (
	modbusUnitID         = byte(1)
	readHoldingRegisters = byte(0x03)
)

type readSpec struct {
	name     string
	sequence byte
	start    uint16
	quantity uint16
}

type readID byte

const (
	deviceTypeRead readID = iota + 1
	upsPowerRead
	gridVoltageRead
	capabilityRead
	batteryTemperatureRead
	batteryVoltageSOCRead
	batteryFlowRead
	energyRead
	gridPowerLowRead
	gridPowerHighRead
	outputScalarRead
	loadVoltageRead
	directLoadPowerLowRead
	loadFrequencyRead
	pvInputRead
	inverterTemperatureRead
	warningFaultRead
	runStateRead
	gridFrequencyCurrentRead
	generatorPortModeRead
	generatorEnergyRead
	generatorElectricalRead
	outputPowerHighRead
	relayStatusRead
)

// approvedReadSpec is the immutable compile-time allowlist. Every range is
// compile-time reviewed against the evidence recorded in the validation
// document; each decoder states its narrower correlation and accepted-domain
// contract. There is deliberately no generic register entry point.
func approvedReadSpec(id readID) (readSpec, bool) {
	switch id {
	case deviceTypeRead:
		return readSpec{name: "device-type", sequence: 0x01, start: 0x0000, quantity: 1}, true
	case upsPowerRead:
		return readSpec{name: "ups-load-power", sequence: 0x02, start: 0x0283, quantity: 1}, true
	case gridVoltageRead:
		return readSpec{name: "grid-phase-voltages", sequence: 0x03, start: 0x0256, quantity: 3}, true
	case capabilityRead:
		return readSpec{name: "rated-power-mppt-phases", sequence: 0x09, start: 0x0014, quantity: 3}, true
	case batteryTemperatureRead:
		return readSpec{name: "battery-temperature", sequence: 0x06, start: 0x024A, quantity: 1}, true
	case batteryVoltageSOCRead:
		return readSpec{name: "battery-voltage-soc", sequence: 0x05, start: 0x024B, quantity: 2}, true
	case batteryFlowRead:
		return readSpec{name: "battery-power-current", sequence: 0x0A, start: 0x024E, quantity: 2}, true
	case energyRead:
		return readSpec{name: "energy-counters", sequence: 0x08, start: 0x0202, quantity: 16}, true
	case gridPowerLowRead:
		return readSpec{name: "grid-power-low-words", sequence: 0x0C, start: 0x026E, quantity: 4}, true
	case gridPowerHighRead:
		return readSpec{name: "grid-power-high-words", sequence: 0x0D, start: 0x02AF, quantity: 4}, true
	case outputScalarRead:
		return readSpec{name: "output-scalars", sequence: 0x0E, start: 0x0273, quantity: 12}, true
	case outputPowerHighRead:
		return readSpec{name: "output-power-high-words", sequence: 0x0F, start: 0x02B3, quantity: 5}, true
	case relayStatusRead:
		return readSpec{name: "power-and-ac-relay-status", sequence: 0x19, start: 0x0227, quantity: 2}, true
	case loadVoltageRead:
		return readSpec{name: "ups-load-voltage-block", sequence: 0x10, start: 0x0280, quantity: 7}, true
	case directLoadPowerLowRead:
		return readSpec{name: "direct-load-power-low-words", sequence: 0x12, start: 0x028A, quantity: 4}, true
	case loadFrequencyRead:
		return readSpec{name: "load-frequency-high-word-block", sequence: 0x13, start: 0x028F, quantity: 5}, true
	case pvInputRead:
		return readSpec{name: "pv-input-block", sequence: 0x07, start: 0x02A0, quantity: 8}, true
	case inverterTemperatureRead:
		return readSpec{name: "dc-ac-temperatures", sequence: 0x15, start: 0x021C, quantity: 2}, true
	case warningFaultRead:
		return readSpec{name: "warning-fault-raw-words", sequence: 0x14, start: 0x0229, quantity: 6}, true
	case runStateRead:
		return readSpec{name: "run-state-block", sequence: 0x04, start: 0x01F4, quantity: 6}, true
	case gridFrequencyCurrentRead:
		return readSpec{name: "grid-frequency-current-block", sequence: 0x0B, start: 0x0261, quantity: 11}, true
	case generatorPortModeRead:
		return readSpec{name: "generator-port-mode", sequence: 0x16, start: 0x0085, quantity: 1}, true
	case generatorEnergyRead:
		return readSpec{name: "generator-energy-runtime", sequence: 0x17, start: 0x0218, quantity: 4}, true
	case generatorElectricalRead:
		return readSpec{name: "generator-voltage-power", sequence: 0x18, start: 0x0295, quantity: 11}, true
	default:
		return readSpec{}, false
	}
}
