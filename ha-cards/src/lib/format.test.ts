import assert from "node:assert/strict";
import test from "node:test";

import { formatCurrent, formatEnergy, formatNumber, formatPercent, formatPower, formatTemperature, formatVoltage } from "./format.ts";

test("formatNumber trims trailing zeroes and rejects non-finite values", () => {
  assert.equal(formatNumber(12.3, 2), "12.3");
  assert.equal(formatNumber(12, 2), "12");
  assert.equal(formatNumber(Number.NaN), "--");
});

test("formatPower scales and signs power values", () => {
  assert.equal(formatPower(999), "999 W");
  assert.equal(formatPower(1234), "1.23 kW");
  assert.equal(formatPower(-1234, true), "-1.23 kW");
  assert.equal(formatPower(1234, true), "+1.23 kW");
  assert.equal(formatPower(null), "--");
});

test("formatters never render invalid numeric text", () => {
  assert.equal(formatVoltage(undefined), "--");
  assert.equal(formatCurrent(Number.POSITIVE_INFINITY), "--");
  assert.equal(formatEnergy(Number.NaN), "--");
  assert.equal(formatPercent(null), "--");
  assert.equal(formatTemperature(undefined), "--");
});
