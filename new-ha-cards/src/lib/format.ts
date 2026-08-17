export const isFiniteNumber = (value: number | null | undefined): value is number => Number.isFinite(value);

export const clamp = (value: number, min: number, max: number): number => Math.min(Math.max(value, min), max);

export const sum = (values: Array<number | null | undefined>): number | null => {
  const filtered = values.filter(isFiniteNumber);
  return filtered.length ? filtered.reduce((acc, value) => acc + value, 0) : null;
};

export const average = (values: Array<number | null | undefined>): number | null => {
  const filtered = values.filter((value): value is number => isFiniteNumber(value) && value !== 0);
  return filtered.length ? filtered.reduce((acc, value) => acc + value, 0) / filtered.length : null;
};

export const first = <T>(...values: Array<T | null | undefined>): T | null =>
  values.find((value) => value !== null && value !== undefined) ?? null;

export const formatNumber = (value: number, digits = 1): string => {
  if (!Number.isFinite(value)) return "--";
  const fixed = value.toFixed(digits);
  return fixed.replace(/\.0+$|(\.\d*[1-9])0+$/, "$1");
};

export const formatCurrent = (value: number | null | undefined): string => {
  if (!isFiniteNumber(value)) return "--";
  const abs = Math.abs(value);
  if (abs < 1) return `${formatNumber(abs * 1000, abs < 0.1 ? 0 : 1)} mA`;
  return `${formatNumber(abs, abs >= 10 ? 1 : 2)} A`;
};

export const formatEnergy = (value: number | null | undefined): string =>
  isFiniteNumber(value) ? `${formatNumber(value, value >= 10 ? 1 : 2)} kWh` : "--";

export const formatPercent = (value: number | null | undefined): string =>
  isFiniteNumber(value) ? `${formatNumber(value, value >= 10 ? 0 : 1)}%` : "--";

export const formatPower = (value: number | null | undefined, signed = false): string => {
  if (!isFiniteNumber(value)) return "--";
  const sign = signed && value > 0 ? "+" : "";
  const abs = Math.abs(value);
  if (abs >= 1000) return `${sign}${value < 0 ? "-" : ""}${formatNumber(abs / 1000, abs >= 10000 ? 1 : 2)} kW`;
  return `${sign}${value < 0 ? "-" : ""}${formatNumber(abs, abs >= 100 ? 0 : 1)} W`;
};

export const formatTemperature = (value: number | null | undefined): string =>
  isFiniteNumber(value) ? `${formatNumber(value, 1)} °C` : "--";

export const splitValueUnit = (formatted: string): { value: string; unit: string } => {
  const match = formatted.match(/^([+-]?(?:\d+(?:\.\d+)?|--))(?:\s+(.+))?$/);
  if (!match) return { value: formatted, unit: "" };
  return { value: match[1] ?? formatted, unit: match[2] ?? "" };
};
