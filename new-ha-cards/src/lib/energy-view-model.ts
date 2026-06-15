import fakeEnergyData from "../data/fake-energy-data.json";
import type { EntityKey } from "./entity-model";
import { average, first, formatNumber, isFiniteNumber, splitValueUnit, sum } from "./format";

export type EnergyMetricKey = EntityKey | "daily_cost";
export type EnergyValueGetter = (key: EnergyMetricKey) => number | null;

export interface EnergyViewModelOptions {
  batteryNegativeIsCharging: boolean;
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
const ENERGY_EPSILON = 0.01;
const CRITICAL_KEYS: EntityKey[] = [
  "pv1_power",
  "pv2_power",
  "grid_total_power",
  "home_total_power",
  "ups_total_power",
  "battery_power",
  "battery_soc"
];

export const fakeValueFor: EnergyValueGetter = (key) => {
  const values = fakeEnergyData as Partial<Record<EnergyMetricKey, number>>;
  return isFiniteNumber(values[key]) ? values[key] : null;
};

export const buildEnergyViewModel = (valueFor: EnergyValueGetter, options: EnergyViewModelOptions): EnergyViewModel => {
  const v = valueFor;
  const gridPhases = [1, 2, 3].map((phase) => ({
    voltage: v(`grid_l${phase}_voltage` as EntityKey),
    current: v(`grid_l${phase}_current` as EntityKey),
    power: v(`grid_l${phase}_power` as EntityKey)
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
  const pvTotalPower = first(v("pv_total_power"), sum([pv1Power, pv2Power]));
  const gridTotalPower = first(v("grid_total_power"), sum(gridPhases.map((phase) => phase.power)));
  const gridAverageVoltage = average(gridPhases.map((phase) => phase.voltage));
  const loadPhasePower = sum(loadPhases.map((phase) => phase.power));
  const homeTotalPower = first(v("home_total_power"), loadPhasePower);
  const upsTotalPower = v("ups_total_power");
  const gridLoadPower =
    isFiniteNumber(homeTotalPower) && isFiniteNumber(upsTotalPower) ? Math.max(homeTotalPower - upsTotalPower, 0) : null;
  const gridLoadPhasePower = splitTotalPower(gridLoadPower);
  const gridLoadPhases = gridPhases.map((phase, index) => ({
    voltage: phase.voltage,
    power: gridLoadPhasePower[index] ?? null
  }));
  const generatorTotalPower = first(v("generator_total_power"), sum(generatorPhases.map((phase) => phase.power)));
  const batteryPower = v("battery_power");
  const batterySoc = v("battery_soc");
  const inverterOutputPower = first(v("inverter_total_power"), upsTotalPower, sum(inverterPhases.map((phase) => phase.power)));
  const inverterDcPower = pvTotalPower;
  const dailyCost = first(v("daily_cost"), estimateDailyCost(v("grid_buy_today"), v("grid_sell_today")));
  const now = options.now ?? new Date();

  const batteryCharging =
    isFiniteNumber(batteryPower) && Math.abs(batteryPower) >= POWER_EPSILON
      ? options.batteryNegativeIsCharging
        ? batteryPower < 0
        : batteryPower > 0
      : false;
  const batteryMode = isFiniteNumber(batteryPower) && Math.abs(batteryPower) >= POWER_EPSILON ? (batteryCharging ? "Заряд" : "Розряд") : "Очікування";

  const pv1Active = meaningful(pv1Power, POWER_EPSILON);
  const pv2Active = meaningful(pv2Power, POWER_EPSILON);
  const gridImporting = isFiniteNumber(gridTotalPower) && gridTotalPower > POWER_EPSILON;
  const gridExporting = isFiniteNumber(gridTotalPower) && gridTotalPower < -POWER_EPSILON;
  const gridFlowActive = meaningful(gridTotalPower, POWER_EPSILON) && meaningful(gridAverageVoltage, VOLTAGE_EPSILON);
  const batteryFlowActive = meaningful(batteryPower, POWER_EPSILON);
  const generatorFlowActive = meaningful(generatorTotalPower, POWER_EPSILON);
  const upsFlowActive = meaningful(upsTotalPower, POWER_EPSILON);
  const gridLoadFlowActive = meaningful(gridLoadPower, POWER_EPSILON);

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
    "grid.status": isFiniteNumber(gridTotalPower) && gridTotalPower < 0 ? "Експорт в мережу" : "Імпорт з мережі",

    "grid_load.phases": phaseVoltage(gridPhases.map((phase) => phase.voltage), gridAverageVoltage),
    "grid_load.current": phaseCurrent(phaseCurrentsFromPower(gridLoadPhases), null),
    "grid_load.power.value": powerParts(gridLoadPower).value,
    "grid_load.power.unit": powerParts(gridLoadPower).unit,
    "grid_load.phase_power": phasePower(gridLoadPhasePower),

    "load.total_power.value": powerParts(homeTotalPower).value,
    "load.total_power.unit": powerParts(homeTotalPower).unit,

    "ups.phases": phaseVoltage(loadPhases.map((phase) => phase.voltage), average(loadPhases.map((phase) => phase.voltage))),
    "ups.current": phaseCurrent(phaseCurrentsFromPower(loadPhases), null),
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
    "inverter.status": "Нормально",

    "battery.soc.value": percentParts(batterySoc).value,
    "battery.soc.unit": "%",
    "battery.voltage": voltage(v("battery_voltage")),
    "battery.current": current(v("battery_current"), true),
    "battery.power.value": powerParts(batteryPower).value,
    "battery.power.unit": powerParts(batteryPower).unit,
    "battery.mode": `(${batteryMode})`,
    "battery.temperature": temperature(v("battery_temp")),

    "generator.phases": phaseVoltage(generatorPhases.map((phase) => phase.voltage), average(generatorPhases.map((phase) => phase.voltage))),
    "generator.current": phaseCurrent(phaseCurrentsFromPower(generatorPhases), null),
    "generator.power.value": powerParts(generatorTotalPower).value,
    "generator.power.unit": powerParts(generatorTotalPower).unit,
    "generator.phase_power": phasePower(generatorPhases.map((phase) => phase.power)),
    "generator.status": "standby",

    "system.online": "Онлайн",
    "system.frequency": frequency(v("grid_frequency")),
    "system.dc_bus": voltage(v("dc_bus_voltage")),
    "system.generation": preciseEnergy(v("pv_daily_energy")),
    "system.self_consumption": preciseEnergy(v("home_daily_energy")),
    "system.export": preciseEnergy(v("grid_sell_today")),
    "system.import": preciseEnergy(v("grid_buy_today")),
    "system.updated_at": now.toLocaleTimeString("uk-UA", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
  };

  const flags = {
    online: true,
    pv1_offline: !pv1Active && !meaningful(v("pv1_voltage"), VOLTAGE_EPSILON),
    pv2_offline: !pv2Active && !meaningful(v("pv2_voltage"), VOLTAGE_EPSILON),
    pv_offline: !meaningful(pvTotalPower, POWER_EPSILON),
    grid_offline: !meaningful(gridTotalPower, POWER_EPSILON) && !meaningful(gridAverageVoltage, VOLTAGE_EPSILON),
    battery_offline: !meaningful(batterySoc, CURRENT_EPSILON) && !meaningful(batteryPower, POWER_EPSILON),
    generator_offline: !meaningful(generatorTotalPower, POWER_EPSILON),
    ups_offline: !meaningful(upsTotalPower, POWER_EPSILON),
    grid_load_offline: !meaningful(gridLoadPower, POWER_EPSILON),
    battery_charging: batteryCharging,
    battery_empty: isFiniteNumber(batterySoc) && batterySoc <= 10,
    battery_low: isFiniteNumber(batterySoc) && batterySoc > 10 && batterySoc <= 35,
    battery_half: isFiniteNumber(batterySoc) && batterySoc > 35 && batterySoc <= 75,
    battery_full: !isFiniteNumber(batterySoc) || batterySoc > 75,
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
    flow_grid_load_active: gridLoadFlowActive
  };

  const missing = CRITICAL_KEYS.filter((key) => v(key) === null);

  return { values, flags, missing };
};

const estimateDailyCost = (buyToday: number | null, sellToday: number | null): number | null => {
  if (!isFiniteNumber(buyToday) && !isFiniteNumber(sellToday)) return null;
  const buy = buyToday ?? 0;
  const sell = sellToday ?? 0;
  return sell > buy ? (sell - buy) * 6.515 : -((buy - sell) * 4.32);
};

const meaningful = (value: number | null | undefined, epsilon: number): value is number => isFiniteNumber(value) && Math.abs(value) >= epsilon;

const preciseEnergy = (value: number | null | undefined): string => (isFiniteNumber(value) ? `${formatNumber(value, 2)} kWh` : "--");

const energyParts = (value: number | null | undefined): { value: string; unit: string } => splitValueUnit(preciseEnergy(value));

const costParts = (value: number | null | undefined): { value: string; unit: string } =>
  isFiniteNumber(value) ? { value: formatNumber(Math.abs(value), 2), unit: "грн" } : { value: "--", unit: "" };

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
  if (!phases.some((value) => meaningful(value, VOLTAGE_EPSILON))) return voltage(fallback);
  return `${phases.map((value) => (meaningful(value, VOLTAGE_EPSILON) ? `${formatNumber(value, 0)}V` : "--")).join(" / ")}`;
};

const phaseCurrent = (phases: Array<number | null>, fallback: number | null): string => {
  if (!phases.some((value) => meaningful(value, CURRENT_EPSILON))) return current(fallback);
  return `${phases.map((value) => (meaningful(value, CURRENT_EPSILON) ? `${Number(value).toFixed(1)} A` : "--")).join(" / ")}`;
};

const phasePower = (phases: Array<number | null>): string => {
  if (!phases.some((value) => meaningful(value, POWER_EPSILON))) return "--";
  return phases
    .map((value) => (meaningful(value, POWER_EPSILON) ? `${formatNumber(Math.abs(value) / 1000, 2)} kW` : "--"))
    .join(" / ");
};

const splitTotalPower = (totalPower: number | null): Array<number | null> => {
  if (!isFiniteNumber(totalPower)) return [null, null, null];
  const third = totalPower / 3;
  return [third * 0.98, third * 1.01, third * 1.01];
};

const phaseCurrentsFromPower = (phases: Array<{ voltage: number | null; power: number | null }>): Array<number | null> =>
  phases.map((phase) =>
    isFiniteNumber(phase.voltage) && phase.voltage !== 0 && isFiniteNumber(phase.power) ? Math.abs(phase.power) / phase.voltage : null
  );
