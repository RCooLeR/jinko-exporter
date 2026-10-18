import assert from "node:assert/strict";
import test from "node:test";

import { formatCurrent, formatEnergy, formatSignedEnergy, formatNumber, formatPercent, formatPower, formatTemperature, formatVoltage } from "./format.ts";

test("signed daily energy preserves direction with two decimals and unsigned zero", () => {
  for (const [value, expected] of [
    [12.34, "+12.34 kWh"], [-12.34, "-12.34 kWh"],
    [12.346, "+12.35 kWh"], [-12.346, "-12.35 kWh"],
    [5, "+5 kWh"], [-5, "-5 kWh"], [0, "0 kWh"], [-0, "0 kWh"],
    [0.001, "0 kWh"], [-0.001, "0 kWh"]
  ] as const) assert.equal(formatSignedEnergy(value), expected);
  for (const missing of [null, undefined, NaN, Infinity, -Infinity]) {
    assert.equal(formatSignedEnergy(missing), "--");
  }
});

test("current formatting preserves direction at milliamp and amp scales", () => {
  assert.equal(formatCurrent(-0.16), "-160 mA");
  assert.equal(formatCurrent(-0.001), "-1 mA");
  assert.equal(formatCurrent(-1.25), "-1.25 A");
  assert.equal(formatCurrent(-12.5), "-12.5 A");
  assert.equal(formatCurrent(0), "0 mA");
  assert.equal(formatCurrent(0.16), "160 mA");
  assert.equal(formatCurrent(null), "--");
});

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
