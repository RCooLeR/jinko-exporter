import type { EntityKey } from "./entity-model";

type Reading = number | null;

export interface MeasuredGridLoad {
  voltage: Reading;
  current: Reading;
  power: Reading;
  voltagePhases: Reading[];
  currentPhases: Reading[];
  powerPhases: Reading[];
  available: boolean;
}

// These entities belong to the independent Shelly meter, not the inverter.
export const measuredGridLoadFor = (value: (key: EntityKey) => Reading): MeasuredGridLoad => {
  const voltagePhases = [1, 2, 3].map((phase) => value(`grid_load_l${phase}_voltage` as EntityKey));
  const currentPhases = [1, 2, 3].map((phase) => value(`grid_load_l${phase}_current` as EntityKey));
  const powerPhases = [1, 2, 3].map((phase) => value(`grid_load_l${phase}_power` as EntityKey));
  const voltages = voltagePhases.filter(isReading);
  const voltage = voltages.length ? total(voltages) / voltages.length : null;
  // Dedicated totals must never silently become sums of partially available phases.
  const current = value("grid_load_total_current") ?? (currentPhases.every(isReading) ? total(currentPhases.map(Math.abs)) : null);
  const power = value("grid_load_total_power") ?? (powerPhases.every(isReading) ? total(powerPhases) : null);

  return {
    voltage,
    current,
    power,
    voltagePhases,
    currentPhases,
    powerPhases,
    available: [voltage, current, power, ...currentPhases, ...powerPhases].some(isReading)
  };
};

const isReading = (value: Reading): value is number => Number.isFinite(value);
const total = (values: number[]): number => values.reduce((sum, value) => sum + value, 0);
