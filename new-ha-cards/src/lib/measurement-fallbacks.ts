export const completeSum = (values: Array<number | null>): number | null =>
  values.length > 0 && values.every((value) => value !== null && Number.isFinite(value))
    ? values.reduce<number>((sum, value) => sum + value!, 0)
    : null;

export const estimatedCurrent = (power: number | null, voltage: number | null): number | null =>
  power !== null && voltage !== null && Number.isFinite(power) && Number.isFinite(voltage) && voltage > 0
    ? Math.abs(power) / voltage
    : null;
