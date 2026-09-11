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
  const currents = currentPhases.filter(isReading).map(Math.abs);
  const powers = powerPhases.filter(isReading);
  const voltage = voltages.length ? total(voltages) / voltages.length : null;
  const current = value("grid_load_total_current") ?? (currents.length ? total(currents) : null);
  const power = value("grid_load_total_power") ?? (powers.length ? total(powers) : null);

  return {
    voltage,
    current,
    power,
    voltagePhases,
    currentPhases,
    powerPhases,
    available: [voltage, current, power].some(isReading)
  };
};

const isReading = (value: Reading): value is number => Number.isFinite(value);
const total = (values: number[]): number => values.reduce((sum, value) => sum + value, 0);
