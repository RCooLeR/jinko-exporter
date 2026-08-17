package modbus

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/RCooLeR/jinko-exporter/bridge/internal/config"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/model"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source"
	"github.com/RCooLeR/jinko-exporter/bridge/internal/source/jinko"
)

const (
	profileName        = "jks-6-20h-ei-readonly-v1"
	expectedDeviceType = uint16(0x0006)
)

var _ source.Source = (*Client)(nil)

// Client is deliberately a closed, read-only profile. It has no API for raw
// Modbus frames, arbitrary register addresses, or Modbus write functions.
type Client struct {
	cfg          config.ModbusConfig
	host         netip.Addr
	loggerSerial uint32
	dial         dialContextFunc
	gate         chan struct{}
	capabilities *profileCapabilities
}

func New(cfg config.ModbusConfig) (*Client, error) {
	return newClient(cfg, defaultDialContext)
}

func newClient(cfg config.ModbusConfig, dial dialContextFunc) (*Client, error) {
	host, err := netip.ParseAddr(strings.TrimSpace(cfg.Host))
	if err != nil || !host.Is4() {
		return nil, fmt.Errorf("modbus host must be a literal IPv4 address")
	}
	if !host.IsPrivate() {
		return nil, fmt.Errorf("modbus host must be a private literal IPv4 address")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("modbus port must be between 1 and 65535")
	}
	serial, err := strconv.ParseUint(strings.TrimSpace(cfg.LoggerSerial), 10, 32)
	if err != nil || serial == 0 {
		return nil, fmt.Errorf("modbus logger serial must be a non-zero decimal uint32")
	}
	if cfg.UnitID != uint(modbusUnitID) {
		return nil, fmt.Errorf("modbus unit ID must be 1 for the locked %s profile", profileName)
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 30*time.Second {
		return nil, fmt.Errorf("modbus timeout must be greater than zero and at most 30s")
	}
	if dial == nil {
		return nil, fmt.Errorf("modbus dialer is required")
	}

	return &Client{
		cfg:          cfg,
		host:         host,
		loggerSerial: uint32(serial),
		dial:         dial,
		gate:         make(chan struct{}, 1),
	}, nil
}

func (c *Client) Name() string {
	return "modbus"
}

func (c *Client) Fetch(ctx context.Context) (*model.Snapshot, error) {
	if ctx == nil {
		return nil, fmt.Errorf("modbus fetch context is required")
	}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	operationCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	includeProfileGates := c.capabilities == nil
	session, err := openReadSession(
		operationCtx,
		c.dial,
		c.host,
		c.cfg.Port,
		c.loggerSerial,
		modbusUnitID,
		includeProfileGates,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	// Both target-profile gates run together and are cached only after both
	// succeed. A failed or interrupted gate leaves no partially validated state.
	if includeProfileGates {
		capabilities, err := c.validateProfile(session)
		if err != nil {
			return nil, err
		}
		c.capabilities = &capabilities
	}

	// Register 133 is mutable operating configuration rather than an inherent
	// device capability, so it is deliberately re-read and gated on every Fetch.
	generatorPortModeWords, err := session.readApproved(generatorPortModeRead)
	if err != nil {
		return nil, fmt.Errorf("read generator port mode: %w", err)
	}
	if err := decodeGeneratorPortModeZeroOnly(generatorPortModeWords); err != nil {
		return nil, fmt.Errorf("decode generator port mode: %w", err)
	}

	generatorEnergyWords, err := session.readApproved(generatorEnergyRead)
	if err != nil {
		return nil, fmt.Errorf("read generator energy and runtime: %w", err)
	}
	generatorEnergyMetrics, err := decodeGeneratorEnergyZeroOnly(generatorEnergyWords)
	if err != nil {
		return nil, fmt.Errorf("decode generator energy and runtime: %w", err)
	}

	generatorElectricalWords, err := session.readApproved(generatorElectricalRead)
	if err != nil {
		return nil, fmt.Errorf("read generator voltage and power: %w", err)
	}
	generatorElectricalMetrics, err := decodeGeneratorElectricalZeroOnly(generatorElectricalWords)
	if err != nil {
		return nil, fmt.Errorf("decode generator voltage and power: %w", err)
	}

	upsWords, err := session.readApproved(upsPowerRead)
	if err != nil {
		return nil, fmt.Errorf("read UPS load power: %w", err)
	}
	upsPower, err := decodeUPSPower(upsWords)
	if err != nil {
		return nil, fmt.Errorf("decode UPS load power: %w", err)
	}

	loadVoltageWords, err := session.readApproved(loadVoltageRead)
	if err != nil {
		return nil, fmt.Errorf("read load phase voltages: %w", err)
	}
	loadVoltages, err := decodeLoadVoltages(loadVoltageWords)
	if err != nil {
		return nil, fmt.Errorf("decode load phase voltages: %w", err)
	}

	directLoadPowerLowWords, err := session.readApproved(directLoadPowerLowRead)
	if err != nil {
		return nil, fmt.Errorf("read direct-load power low words: %w", err)
	}

	loadFrequencyWords, err := session.readApproved(loadFrequencyRead)
	if err != nil {
		return nil, fmt.Errorf("read load frequency block: %w", err)
	}
	loadFrequency, err := decodeLoadFrequency(loadFrequencyWords)
	if err != nil {
		return nil, fmt.Errorf("decode load frequency: %w", err)
	}
	directLoadPowers, err := decodeDirectLoadPowers(directLoadPowerLowWords, loadFrequencyWords)
	if err != nil {
		return nil, fmt.Errorf("decode direct-load powers: %w", err)
	}

	gridWords, err := session.readApproved(gridVoltageRead)
	if err != nil {
		return nil, fmt.Errorf("read grid phase voltages: %w", err)
	}
	gridVoltages, err := decodeGridVoltages(gridWords)
	if err != nil {
		return nil, fmt.Errorf("decode grid phase voltages: %w", err)
	}

	gridPowerLowWords, err := session.readApproved(gridPowerLowRead)
	if err != nil {
		return nil, fmt.Errorf("read grid power low words: %w", err)
	}
	gridPowerHighWords, err := session.readApproved(gridPowerHighRead)
	if err != nil {
		return nil, fmt.Errorf("read grid power high words: %w", err)
	}
	gridPowers, err := decodeGridPowers(gridPowerLowWords, gridPowerHighWords)
	if err != nil {
		return nil, fmt.Errorf("decode grid powers: %w", err)
	}

	outputWords, err := session.readApproved(outputScalarRead)
	if err != nil {
		return nil, fmt.Errorf("read output scalars: %w", err)
	}
	outputPowerHighWords, err := session.readApproved(outputPowerHighRead)
	if err != nil {
		return nil, fmt.Errorf("read output power high words: %w", err)
	}
	output, err := decodeOutputScalars(outputWords)
	if err != nil {
		return nil, fmt.Errorf("decode output scalars: %w", err)
	}
	outputPowers, err := decodeOutputActivePowers(outputWords, outputPowerHighWords)
	if err != nil {
		return nil, fmt.Errorf("decode output active powers: %w", err)
	}

	pvWords, err := session.readApproved(pvInputRead)
	if err != nil {
		return nil, fmt.Errorf("read PV input block: %w", err)
	}
	pv, err := decodePVInputs(pvWords, c.capabilities.ratedPowerW)
	if err != nil {
		return nil, fmt.Errorf("decode PV inputs: %w", err)
	}

	inverterTemperatureWords, err := session.readApproved(inverterTemperatureRead)
	if err != nil {
		return nil, fmt.Errorf("read DC/AC temperatures: %w", err)
	}
	inverterTemperatures, err := decodeInverterTemperatures(inverterTemperatureWords)
	if err != nil {
		return nil, fmt.Errorf("decode DC/AC temperatures: %w", err)
	}

	temperatureWords, err := session.readApproved(batteryTemperatureRead)
	if err != nil {
		return nil, fmt.Errorf("read battery temperature: %w", err)
	}
	batteryTemperature, err := decodeBatteryTemperature(temperatureWords)
	if err != nil {
		return nil, fmt.Errorf("decode battery temperature: %w", err)
	}

	voltageSOCWords, err := session.readApproved(batteryVoltageSOCRead)
	if err != nil {
		return nil, fmt.Errorf("read battery voltage and SOC: %w", err)
	}
	batteryVoltage, batterySOC, err := decodeBatteryVoltageSOC(voltageSOCWords)
	if err != nil {
		return nil, fmt.Errorf("decode battery voltage and SOC: %w", err)
	}

	flowWords, err := session.readApproved(batteryFlowRead)
	if err != nil {
		return nil, fmt.Errorf("read battery power and current: %w", err)
	}
	batteryPower, batteryCurrent, err := decodeBatteryFlow(flowWords)
	if err != nil {
		return nil, fmt.Errorf("decode battery power and current: %w", err)
	}

	energyWords, err := session.readApproved(energyRead)
	if err != nil {
		return nil, fmt.Errorf("read energy counters: %w", err)
	}
	energyMetrics, err := decodeEnergyMetrics(energyWords)
	if err != nil {
		return nil, fmt.Errorf("decode energy counters: %w", err)
	}

	relayStatusWords, err := session.readApproved(relayStatusRead)
	if err != nil {
		return nil, fmt.Errorf("read power and AC relay status: %w", err)
	}
	powerSwitchState, acRelayStatus, err := decodePowerAndACRelayStatus(relayStatusWords)
	if err != nil {
		return nil, fmt.Errorf("decode power and AC relay status: %w", err)
	}

	warningFaultWords, err := session.readApproved(warningFaultRead)
	if err != nil {
		return nil, fmt.Errorf("read warning/fault raw words: %w", err)
	}
	warningFaultValues, err := decodeWarningFaultWords(warningFaultWords)
	if err != nil {
		return nil, fmt.Errorf("decode warning/fault raw words: %w", err)
	}

	runStateWords, err := session.readApproved(runStateRead)
	if err != nil {
		return nil, fmt.Errorf("read run-state block: %w", err)
	}
	runState, err := decodeRunState(runStateWords)
	if err != nil {
		return nil, fmt.Errorf("decode run-state block: %w", err)
	}

	gridFrequencyCurrentWords, err := session.readApproved(gridFrequencyCurrentRead)
	if err != nil {
		return nil, fmt.Errorf("read grid frequency and currents: %w", err)
	}
	gridFrequencyCurrent, err := decodeGridFrequencyCurrents(gridFrequencyCurrentWords)
	if err != nil {
		return nil, fmt.Errorf("decode grid frequency and currents: %w", err)
	}

	metricValues := []metricValue{
		{key: "Pr1", value: c.capabilities.ratedPowerW},
		{key: "UPS_P", value: upsPower},
		{key: "C_V_L1", value: loadVoltages[0]},
		{key: "C_V_L2", value: loadVoltages[1]},
		{key: "C_V_L3", value: loadVoltages[2]},
		{key: "L_F", value: loadFrequency},
		{key: "LPP_A", value: directLoadPowers.phasesW[0]},
		{key: "LPP_B", value: directLoadPowers.phasesW[1]},
		{key: "LPP_C", value: directLoadPowers.phasesW[2]},
		{key: "E_Puse_t1", value: directLoadPowers.totalW},
		{key: "G_V_L1", value: gridVoltages[0]},
		{key: "G_V_L2", value: gridVoltages[1]},
		{key: "G_V_L3", value: gridVoltages[2]},
		{key: "PG_F1", value: gridFrequencyCurrent.frequencyHz},
		{key: "G_C_L1", value: gridFrequencyCurrent.currents[0]},
		{key: "G_C_L2", value: gridFrequencyCurrent.currents[1]},
		{key: "G_C_L3", value: gridFrequencyCurrent.currents[2]},
		{key: "G_P_L1", value: gridPowers[0]},
		{key: "G_P_L2", value: gridPowers[1]},
		{key: "G_P_L3", value: gridPowers[2]},
		{key: "PG_Pt1", value: gridPowers[3]},
		{key: "AV1", value: output.voltages[0]},
		{key: "AV2", value: output.voltages[1]},
		{key: "AV3", value: output.voltages[2]},
		{key: "AC1", value: output.currents[0]},
		{key: "AC2", value: output.currents[1]},
		{key: "AC3", value: output.currents[2]},
		{key: "A_Fo1", value: output.frequencyHz},
		{key: "INV_O_P_L1", value: outputPowers[0]},
		{key: "INV_O_P_L2", value: outputPowers[1]},
		{key: "INV_O_P_L3", value: outputPowers[2]},
		{key: "INV_O_P_T", value: outputPowers[3]},
		{key: "DP1", value: pv.powersW[0]},
		{key: "DP2", value: pv.powersW[1]},
		{key: "DV1", value: pv.voltagesV[0]},
		{key: "DC1", value: pv.currentsA[0]},
		{key: "DV2", value: pv.voltagesV[1]},
		{key: "DC2", value: pv.currentsA[1]},
		{key: "S_P_T", value: pv.totalPowerW},
		{key: "T_DC", value: inverterTemperatures[0]},
		{key: "AC_T", value: inverterTemperatures[1]},
		{key: "B_T1", value: batteryTemperature},
		{key: "BMST", value: batteryTemperature},
		{key: "B_V1", value: batteryVoltage},
		{key: "B_left_cap1", value: batterySOC},
		{key: "BMS_SOC", value: batterySOC},
		{key: "B_P1", value: batteryPower},
		{key: "B_C1", value: batteryCurrent},
		{key: "AC", value: float64(acRelayStatus)},
	}
	metricValues = append(metricValues, energyMetrics...)
	metricValues = append(metricValues, generatorEnergyMetrics...)
	metricValues = append(metricValues, generatorElectricalMetrics...)
	metrics := make([]model.Metric, 0, len(metricValues)+len(warningFaultValues)+2)
	for _, item := range metricValues {
		metric, err := canonicalMetric(item.key, item.value)
		if err != nil {
			return nil, err
		}
		metrics = append(metrics, metric)
	}
	metrics = append(metrics, modbusWarningFaultMetrics(warningFaultValues)...)
	metrics = append(metrics, model.Metric{
		Group: "status",
		Key:   "DEYE_MODBUS_R551_POWER_SWITCH_STATE",
		Name:  "Deye Inverter Power Switch State",
		Unit:  "",
		Value: float64(powerSwitchState),
	})
	metrics = append(metrics, model.Metric{
		Group: "status",
		Key:   "DEYE_MODBUS_R500_RUN_STATE",
		Name:  "Deye Inverter Run State Code",
		Unit:  "",
		Value: float64(runState),
	})
	if err := session.complete(); err != nil {
		return nil, fmt.Errorf("complete fixed Modbus Fetch plan: %w", err)
	}

	return &model.Snapshot{
		Source:      c.Name(),
		DeviceSN:    strings.TrimSpace(c.cfg.DeviceSN),
		ParentSN:    strings.TrimSpace(c.cfg.LoggerSerial),
		CollectedAt: time.Now().UTC(),
		Metrics:     metrics,
		Meta: map[string]string{
			"profile":          profileName,
			"transport":        "solarman-v5-over-tcp",
			"modbus_function":  "03",
			"device_type_code": "0x0006",
			"rated_power_w":    strconv.FormatFloat(c.capabilities.ratedPowerW, 'f', 0, 64),
			"mppt_count":       strconv.FormatUint(uint64(c.capabilities.mpptCount), 10),
			"phase_count":      strconv.FormatUint(uint64(c.capabilities.phaseCount), 10),
		},
	}, nil
}

func modbusWarningFaultMetrics(values [6]uint16) []model.Metric {
	definitions := [...]struct {
		key  string
		name string
	}{
		{key: "DEYE_MODBUS_R553_WARNING_WORD_1_RAW", name: "Deye Inverter Warning Word 1 (Raw U16)"},
		{key: "DEYE_MODBUS_R554_WARNING_WORD_2_RAW", name: "Deye Inverter Warning Word 2 (Raw U16)"},
		{key: "DEYE_MODBUS_R555_FAULT_WORD_1_RAW", name: "Deye Inverter Fault Word 1 (Raw U16)"},
		{key: "DEYE_MODBUS_R556_FAULT_WORD_2_RAW", name: "Deye Inverter Fault Word 2 (Raw U16)"},
		{key: "DEYE_MODBUS_R557_FAULT_WORD_3_RAW", name: "Deye Inverter Fault Word 3 (Raw U16)"},
		{key: "DEYE_MODBUS_R558_FAULT_WORD_4_RAW", name: "Deye Inverter Fault Word 4 (Raw U16)"},
	}
	metrics := make([]model.Metric, 0, len(definitions))
	for index, definition := range definitions {
		metrics = append(metrics, model.Metric{
			Group: "alert",
			Key:   definition.key,
			Name:  definition.name,
			Unit:  "",
			Value: float64(values[index]),
		})
	}
	return metrics
}

func (c *Client) validateProfile(session *readSession) (profileCapabilities, error) {
	values, err := session.readApproved(deviceTypeRead)
	if err != nil {
		return profileCapabilities{}, fmt.Errorf("validate Modbus device type: %w", err)
	}
	if len(values) != 1 || values[0] != expectedDeviceType {
		got := uint16(0)
		if len(values) == 1 {
			got = values[0]
		}
		return profileCapabilities{}, fmt.Errorf("refusing telemetry for device type 0x%04X; expected 0x%04X", got, expectedDeviceType)
	}

	values, err = session.readApproved(capabilityRead)
	if err != nil {
		return profileCapabilities{}, fmt.Errorf("validate Modbus capability profile: %w", err)
	}
	capabilities, err := decodeCapabilities(values)
	if err != nil {
		return profileCapabilities{}, fmt.Errorf("validate Modbus capability profile: %w", err)
	}
	return capabilities, nil
}

func canonicalMetric(key string, value float64) (model.Metric, error) {
	metric, ok := jinko.CanonicalizeMetric(model.Metric{Key: key, Value: value})
	if !ok {
		return model.Metric{}, fmt.Errorf("missing canonical metric definition for %s", key)
	}
	return metric, nil
}
