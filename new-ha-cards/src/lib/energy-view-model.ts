import fakeEnergyData from "../data/fake-energy-data.json";
import type { EntityKey } from "./entity-model";
import { calculateDailyGridBalanceUAH } from "./energy-tariff";
import { average, first, formatNumber, isFiniteNumber, splitValueUnit } from "./format";
import { completeSum, estimatedCurrent } from "./measurement-fallbacks";

export type EnergyMetricKey = EntityKey | "daily_cost";
export type EnergyValueGetter = (key: EnergyMetricKey) => number | null;

export interface EnergyViewModelOptions {
  batteryNegativeIsCharging: boolean;
  stabilizers?: boolean;
  stabilizersTestMirrorGrid?: boolean;
  now?: Date;
}

export interface EnergyViewModel {
  values: Record<string, string>;
  flags: Record<string, boolean>;
  missing: EntityKey[];
}

const POWER_EPSILON = 1;
const VOLTAGE_EPSILON = 1;
const CURRENT_EPSILON = 0.01;
const CRITICAL_KEYS: EntityKey[] = [
  "pv1_power",
  "pv2_power",
  "grid_total_power",
  "home_total_power",
  "ups_total_power",
  "battery_power",
  "battery_soc",
  "grid_buy_today",
  "grid_sell_today"
];

export const fakeValueFor: EnergyValueGetter = (key) => {
  const values = fakeEnergyData as Partial<Record<EnergyMetricKey, number>>;
  return isFiniteNumber(values[key]) ? values[key] : null;
};

export const buildEnergyViewModel = (valueFor: EnergyValueGetter, options: EnergyViewModelOptions): EnergyViewModel => {
  const v = valueFor;
  const testMirrorGrid = options.stabilizers === true && options.stabilizersTestMirrorGrid === true;
  const gridPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`grid_l${phase}_voltage` as EntityKey),
    current: v(`grid_l${phase}_current` as EntityKey),
    power: v(`grid_l${phase}_power` as EntityKey)
  }));
  const gridTotalPower = v("grid_total_power");
  // The input meter is on the other side of the stabilizers. Never fill a
  // missing reading from downstream in normal operation. The explicit test
  // mode copies display inputs only, never HA states or entity bindings.
  const gridInputPhases = testMirrorGrid ? gridPhases : [1, 2, 3].map((phase) => ({
    voltage: v(`grid_input_l${phase}_voltage` as EntityKey),
    current: v(`grid_input_l${phase}_current` as EntityKey),
    power: v(`grid_input_l${phase}_power` as EntityKey)
  }));
  const gridInputPower = testMirrorGrid ? gridTotalPower : v("grid_input_total_power");
  const gridInputCurrent = testMirrorGrid ? null : v("grid_input_total_current");
  const gridInputFrequency = v(testMirrorGrid ? "grid_frequency" : "grid_input_frequency");
  const measuredGridLoadPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`grid_load_l${phase}_voltage` as EntityKey),
    current: v(`grid_load_l${phase}_current` as EntityKey),
    power: v(`grid_load_l${phase}_power` as EntityKey)
  }));
  const loadPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`home_l${phase}_voltage` as EntityKey),
    power: v(`home_l${phase}_power` as EntityKey)
  }));
  const inverterPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`inverter_l${phase}_voltage` as EntityKey),
    current: v(`inverter_l${phase}_current` as EntityKey),
    power: v(`inverter_l${phase}_power` as EntityKey)
  }));
  const generatorPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`generator_l${phase}_voltage` as EntityKey),
    power: v(`generator_l${phase}_power` as EntityKey)
  }));

  const pv1Power = v("pv1_power");
  const pv2Power = v("pv2_power");
  const pvTotalPower = first(v("pv_total_power"), completeSum([pv1Power, pv2Power]));
  const gridAverageVoltage = average(gridPhases.map((phase) => phase.voltage));
  const homeTotalPower = v("home_total_power");
  const upsTotalPower = first(v("ups_total_power"), completeSum(loadPhases.map((phase) => phase.power)));
  const calculatedGridLoadPower =
    isFiniteNumber(homeTotalPower) && isFiniteNumber(upsTotalPower) ? homeTotalPower - upsTotalPower : null;
  const gridLoadPhasePower = measuredGridLoadPhases.map((phase) => phase.power);
  const measuredGridLoadPower = first(v("grid_load_total_power"), completeSum(gridLoadPhasePower));
  const gridLoadPower = first(measuredGridLoadPower, calculatedGridLoadPower);
  const gridLoadPhases = gridPhases.map((phase, index) => ({
    voltage: first(measuredGridLoadPhases[index]?.voltage ?? null, phase.voltage),
    current: measuredGridLoadPhases[index]?.current ?? null,
    power: gridLoadPhasePower[index] ?? null
  }));
  const generatorTotalPower = first(v("generator_total_power"), completeSum(generatorPhases.map((phase) => phase.power)));
  const batteryPower = v("battery_power");
  const batterySoc = v("battery_soc");
  const inverterOutputPower = first(v("inverter_total_power"), completeSum(inverterPhases.map((phase) => phase.power)));
  const inverterDcPower = v("inverter_dc_power");
  const dailyCost = first(v("daily_cost"), calculateDailyGridBalanceUAH(v("grid_buy_today"), v("grid_sell_today")));
  const now = options.now ?? new Date();

  const batteryCharging =
    isFiniteNumber(batteryPower) && Math.abs(batteryPower) >= POWER_EPSILON
      ? options.batteryNegativeIsCharging
        ? batteryPower < 0
        : batteryPower > 0
      : false;
  const batteryMode = isFiniteNumber(batteryPower) ? (Math.abs(batteryPower) >= POWER_EPSILON ? (batteryCharging ? "Заряд" : "Розряд") : "Очікування") : "Немає даних";

  const pv1Active = meaningful(pv1Power, POWER_EPSILON);
  const pv2Active = meaningful(pv2Power, POWER_EPSILON);
  const gridImporting = isFiniteNumber(gridTotalPower) && gridTotalPower > POWER_EPSILON;
  const gridExporting = isFiniteNumber(gridTotalPower) && gridTotalPower < -POWER_EPSILON;
  const gridFlowActive = meaningful(gridTotalPower, POWER_EPSILON);
  const batteryFlowActive = meaningful(batteryPower, POWER_EPSILON);
  const generatorFlowActive = meaningful(generatorTotalPower, POWER_EPSILON);
  const upsFlowActive = meaningful(upsTotalPower, POWER_EPSILON);
  const gridLoadFlowActive = meaningful(gridLoadPower, POWER_EPSILON);
  const inverterOnline = [inverterOutputPower, v("inverter_frequency"), ...inverterPhases.flatMap((phase) => [phase.voltage, phase.current])].some(isFiniteNumber);
  const gridOnline = [gridTotalPower, ...gridPhases.flatMap((phase) => [phase.voltage, phase.current, phase.power])].some(isFiniteNumber);
  const gridInputOnline = testMirrorGrid ? gridOnline : [gridInputPower, gridInputCurrent, gridInputFrequency,
    ...gridInputPhases.flatMap((phase) => [phase.voltage, phase.current, phase.power])].some(isFiniteNumber);
  const gridLoadOnline = [gridLoadPower, v("grid_load_total_current"), ...measuredGridLoadPhases.flatMap((phase) => [phase.voltage, phase.current, phase.power])].some(isFiniteNumber);
  const batteryOnline = [batterySoc, batteryPower, v("battery_voltage"), v("battery_current"), v("battery_temp")].some(isFiniteNumber);

  const values: Record<string, string> = {
    "daily.production.value": energyParts(v("pv_daily_energy")).value,
    "daily.production.unit": energyParts(v("pv_daily_energy")).unit,
    "daily.export.value": energyParts(v("grid_sell_today")).value,
    "daily.export.unit": energyParts(v("grid_sell_today")).unit,
    "daily.import.value": energyParts(v("grid_buy_today")).value,
    "daily.import.unit": energyParts(v("grid_buy_today")).unit,
    "daily.generator.value": energyParts(v("generator_daily_energy")).value,
    "daily.generator.unit": energyParts(v("generator_daily_energy")).unit,
    "daily.cost.value": costParts(dailyCost).value,
    "daily.cost.unit": costParts(dailyCost).unit,

    "status.grid": "Робота від\nмережі",
    "status.solar": "Робота від\nсонця",
    "status.battery": "Заряд АКБ",
    "status.generator": "Генератор\nstandby",

    "pv.total.power": power(pvTotalPower),
    "pv.total.power.value": powerParts(pvTotalPower).value,
    "pv.total.power.unit": powerParts(pvTotalPower).unit,
    "pv1.voltage": voltage(v("pv1_voltage")),
    "pv1.current": current(v("pv1_current")),
    "pv1.power.value": powerParts(pv1Power).value,
    "pv1.power.unit": powerParts(pv1Power).unit,
    "pv2.voltage": voltage(v("pv2_voltage")),
    "pv2.current": current(v("pv2_current")),
    "pv2.power.value": powerParts(pv2Power).value,
    "pv2.power.unit": powerParts(pv2Power).unit,

    "grid.phases": phaseVoltage(gridPhases.map((phase) => phase.voltage), gridAverageVoltage),
    "grid.current": phaseCurrent(gridPhases.map((phase) => phase.current), null),
    "grid.phase_power": phasePower(gridPhases.map((phase) => phase.power)),
    "grid.power.value": powerParts(gridTotalPower).value,
    "grid.power.unit": powerParts(gridTotalPower).unit,
    "grid.status": !isFiniteNumber(gridTotalPower) ? "Немає даних" : gridTotalPower < 0 ? "Експорт в мережу" : gridTotalPower > 0 ? "Імпорт з мережі" : "Немає потоку",

    "grid_input.phases": phaseVoltage(gridInputPhases.map((phase) => phase.voltage), null),
    "grid_input.current": phaseCurrent(gridInputPhases.map((phase) => phase.current), gridInputCurrent),
    "grid_input.phase_power": phasePower(gridInputPhases.map((phase) => phase.power)),
    "grid_input.power.value": powerParts(gridInputPower).value,
    "grid_input.power.unit": powerParts(gridInputPower).unit,
    "grid_input.frequency": frequency(gridInputFrequency),
    "grid_input.energy_today": signedDailyGridEnergyBalance(
      v(testMirrorGrid ? "grid_buy_today" : "grid_input_buy_today"),
      v(testMirrorGrid ? "grid_sell_today" : "grid_input_sell_today")
    ),
    "grid_input.status": !isFiniteNumber(gridInputPower) ? "Немає даних" : gridInputPower < 0 ? "Експорт в мережу" : gridInputPower > 0 ? "Імпорт з мережі" : "Немає потоку",

    "grid_load.phases": phaseVoltage(gridLoadPhases.map((phase) => phase.voltage), gridAverageVoltage),
    "grid_load.current": phaseCurrent(gridLoadPhases.map((phase) => phase.current), first(v("grid_load_total_current"), null)),
    "grid_load.power.value": `${measuredGridLoadPower === null && gridLoadPower !== null ? "≈ " : ""}${powerParts(gridLoadPower).value}`,
    "grid_load.power.unit": powerParts(gridLoadPower).unit,
    "grid_load.phase_power": phasePower(gridLoadPhasePower),

    "load.total_power.value": powerParts(homeTotalPower).value,
    "load.total_power.unit": powerParts(homeTotalPower).unit,

    "ups.phases": phaseVoltage(loadPhases.map((phase) => phase.voltage), average(loadPhases.map((phase) => phase.voltage))),
    "ups.current": estimatedPhaseCurrent(loadPhases),
    "ups.power.value": powerParts(upsTotalPower).value,
    "ups.power.unit": powerParts(upsTotalPower).unit,
    "ups.phase_power": phasePower(loadPhases.map((phase) => phase.power)),

    "inverter.ac_power.value": powerParts(inverterOutputPower).value,
    "inverter.ac_power.unit": powerParts(inverterOutputPower).unit,
    "inverter.dc_power.value": powerParts(inverterDcPower).value,
    "inverter.dc_power.unit": powerParts(inverterDcPower).unit,
    "inverter.output_power": power(inverterOutputPower),
    "inverter.output_power.value": powerParts(inverterOutputPower).value,
    "inverter.output_power.unit": powerParts(inverterOutputPower).unit,
    "inverter.ac_temperature": temperature(v("ac_temperature")),
    "inverter.dc_temperature": temperature(v("dc_temperature")),
    "inverter.ac_temperature_compact": compactTemperature(v("ac_temperature")),
    "inverter.dc_temperature_compact": compactTemperature(v("dc_temperature")),
    "inverter.phases": phaseVoltage(inverterPhases.map((phase) => phase.voltage), average(inverterPhases.map((phase) => phase.voltage))),
    "inverter.current": phaseCurrent(inverterPhases.map((phase) => phase.current), null),
    "inverter.frequency": frequency(v("inverter_frequency")),
    "inverter.temperature": temperature(first(v("dc_temperature"), v("ac_temperature"))),
    "inverter.status": inverterOnline ? "Дані доступні" : "Немає даних",

    "battery.soc.value": percentParts(batterySoc).value,
    "battery.soc.unit": "%",
    "battery.voltage": voltage(v("battery_voltage")),
    "battery.current": current(v("battery_current"), true),
    "battery.power.value": powerParts(batteryPower).value,
    "battery.power.unit": powerParts(batteryPower).unit,
    "battery.mode": `(${batteryMode})`,
    "battery.temperature": temperature(v("battery_temp")),

    "generator.phases": phaseVoltage(generatorPhases.map((phase) => phase.voltage), average(generatorPhases.map((phase) => phase.voltage))),
    "generator.current": estimatedPhaseCurrent(generatorPhases),
    "generator.power.value": powerParts(generatorTotalPower).value,
    "generator.power.unit": powerParts(generatorTotalPower).unit,
    "generator.phase_power": phasePower(generatorPhases.map((phase) => phase.power)),
    "generator.status": "standby",

    "system.online": inverterOnline ? "Онлайн" : gridLoadOnline || gridOnline || batteryOnline || (options.stabilizers === true && gridInputOnline) ? "Часткові дані" : "Немає даних",
    "system.frequency": frequency(v("grid_frequency")),
    "system.dc_bus": voltage(v("dc_bus_voltage")),
    "system.generation": preciseEnergy(v("pv_daily_energy")),
    "system.self_consumption": preciseEnergy(v("home_daily_energy")),
    "system.export": preciseEnergy(v("grid_sell_today")),
    "system.import": preciseEnergy(v("grid_buy_today")),
    "system.updated_at": now.toLocaleTimeString("uk-UA", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
  };

  const flags = {
    test_mirror_grid: testMirrorGrid,
    online: inverterOnline || gridLoadOnline || gridOnline || batteryOnline || (options.stabilizers === true && gridInputOnline),
    grid_input_offline: !gridInputOnline,
    pv1_offline: !pv1Active && !meaningful(v("pv1_voltage"), VOLTAGE_EPSILON),
    pv2_offline: !pv2Active && !meaningful(v("pv2_voltage"), VOLTAGE_EPSILON),
    pv_offline: !meaningful(pvTotalPower, POWER_EPSILON),
    grid_offline: !gridOnline,
    battery_offline: !batteryOnline,
    generator_offline: !meaningful(generatorTotalPower, POWER_EPSILON),
    ups_offline: !isFiniteNumber(upsTotalPower) && !loadPhases.some((phase) => isFiniteNumber(phase.voltage)),
    grid_load_offline: !gridLoadOnline,
    battery_charging: batteryCharging,
    battery_empty: isFiniteNumber(batterySoc) && batterySoc <= 10,
    battery_low: isFiniteNumber(batterySoc) && batterySoc > 10 && batterySoc <= 35,
    battery_half: isFiniteNumber(batterySoc) && batterySoc > 35 && batterySoc <= 75,
    battery_full: isFiniteNumber(batterySoc) && batterySoc > 75,
    battery_q1: isFiniteNumber(batterySoc) && batterySoc > 0,
    battery_q2: isFiniteNumber(batterySoc) && batterySoc >= 25,
    battery_q3: isFiniteNumber(batterySoc) && batterySoc >= 50,
    battery_q4: isFiniteNumber(batterySoc) && batterySoc >= 75,
    flow_pv1_inverter_active: pv1Active,
    flow_pv2_inverter_active: pv2Active,
    flow_pv_inverter_active: pv1Active || pv2Active,
    flow_battery_inverter_active: batteryFlowActive,
    flow_battery_inverter_reverse: batteryCharging,
    flow_inverter_grid_active: gridFlowActive,
    flow_inverter_grid_reverse: gridImporting,
    flow_inverter_grid_forward: gridExporting,
    flow_generator_inverter_active: generatorFlowActive,
    flow_inverter_ups_active: upsFlowActive,
    flow_inverter_load_active: meaningful(homeTotalPower, POWER_EPSILON),
    flow_grid_load_active: gridLoadFlowActive,
    flow_grid_input_stabilized_active: meaningful(gridInputPower, POWER_EPSILON),
    flow_grid_input_stabilized_reverse: isFiniteNumber(gridInputPower) && gridInputPower < -POWER_EPSILON,
    flow_grid_input_stabilized_forward: isFiniteNumber(gridInputPower) && gridInputPower > POWER_EPSILON
  };

  const missing = CRITICAL_KEYS.filter((key) => v(key) === null);
  if (options.stabilizers === true && !testMirrorGrid && gridInputPower === null) missing.push("grid_input_total_power");

  return { values, flags, missing };
};

const meaningful = (value: number | null | undefined, epsilon: number): value is number => isFiniteNumber(value) && Math.abs(value) >= epsilon;

const preciseEnergy = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, 2)} kWh` : "--");

const signedDailyGridEnergyBalance = (imported: number | null, returned: number | null): string => {
  if (!isFiniteNumber(imported) || !isFiniteNumber(returned)) return "--";
  const balance = imported - returned;
  if (!isFiniteNumber(balance)) return "--";
  const magnitude = formatNumber(Math.abs(balance), 2);
  // Normalize both exact and rounded zero; do not show misleading +0 or -0.
  if (Number(magnitude) === 0) return "0 kWh";
  return `${balance > 0 ? "+" : "-"}${magnitude} kWh`;
};

const energyParts = (value: number | null | undefined): { value: string; unit: string } => splitValueUnit(preciseEnergy(value));

const costParts = (value: number | null | undefined): { value: string; unit: string } =>
  isFiniteNumber(value)
    ? { value: `${value > 0 ? "+" : ""}${formatNumber(value, 2)}`, unit: "грн" }
    : { value: "--", unit: "" };

const percentParts = (value: number | null | undefined): { value: string; unit: string } =>
  isFiniteNumber(value) ? { value: formatNumber(value, value >= 10 ? 0 : 1), unit: "%" } : { value: "--", unit: "" };

const power = (value: number | null | undefined, signed = false): string => {
  const parts = powerParts(value, signed);
  return parts.unit ? `${parts.value} ${parts.unit}` : parts.value;
};

const powerParts = (value: number | null | undefined, signed = false): { value: string; unit: string } => {
  if (!isFiniteNumber(value)) return { value: "--", unit: "" };
  const sign = signed && value > 0 ? "+" : value < 0 ? "-" : "";
  const abs = Math.abs(value);
  if (abs >= 1000) return { value: `${sign}${formatNumber(abs / 1000, abs >= 10000 ? 1 : 2)}`, unit: "kW" };
  return { value: `${sign}${formatNumber(abs, abs >= 100 ? 0 : 1)}`, unit: "W" };
};

const voltage = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, value >= 100 ? 0 : 1)} V` : "--");

const current = (value: number | null | undefined, signed = false): string => {
  if (!isFiniteNumber(value)) return "--";
  const sign = signed && value > 0 ? "+" : value < 0 ? "-" : "";
  return `${sign}${formatNumber(Math.abs(value), Math.abs(value) >= 10 ? 1 : 2)} A`;
};

const temperature = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, 1)} °C` : "--");

const compactTemperature = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, 0)}°C` : "--");

const frequency = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, 1)} Hz` : "--");

const phaseVoltage = (phases: Array<number | null>, fallback: number | null): string => {
  if (!phases.some(isFiniteNumber)) return voltage(fallback);
  return `${phases.map((value) => (isFiniteNumber(value) ? `${formatNumber(value, 0)}V` : "--")).join(" / ")}`;
};

const phaseCurrent = (phases: Array<number | null>, fallback: number | null): string => {
  if (!phases.some(isFiniteNumber)) return current(fallback);
  return `${phases.map((value) => (isFiniteNumber(value) ? `${formatNumber(value, 2)} A` : "--")).join(" / ")}`;
};

const phasePower = (phases: Array<number | null>): string => {
  if (!phases.some(isFiniteNumber)) return "--";
  return phases
    .map((value) => (isFiniteNumber(value) ? power(value) : "--"))
    .join(" / ");
};

const estimatedPhaseCurrent = (phases: Array<{ voltage: number | null; power: number | null }>): string => {
  const formatted = phaseCurrent(phases.map((phase) => estimatedCurrent(phase.power, phase.voltage)), null);
  return formatted === "--" ? formatted : `≈ ${formatted}`;
};
