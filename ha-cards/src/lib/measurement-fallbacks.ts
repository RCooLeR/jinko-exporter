// Do not present a partial phase sum as a complete measurement.
export const completeSum = (values: Array<number | null>): number | null =>
  values.length > 0 && values.every((value) => value !== null && Number.isFinite(value))
    ? values.reduce<number>((sum, value) => sum + value!, 0)
    : null;

// An active-power estimate, not an RMS current measurement: callers label it ≈.
export const estimatedCurrent = (power: number | null, voltage: number | null): number | null =>
  power !== null && voltage !== null && Number.isFinite(power) && Number.isFinite(voltage) && voltage > 0
    ? Math.abs(power) / voltage
    : null;
