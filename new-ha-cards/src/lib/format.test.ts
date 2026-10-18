import assert from "node:assert/strict";
import test from "node:test";
import { formatCurrent, formatPower } from "./format.ts";

test("current formatting preserves direction at milliamp and amp scales", () => {
  assert.equal(formatCurrent(-0.16), "-160 mA");
  assert.equal(formatCurrent(-0.001), "-1 mA");
  assert.equal(formatCurrent(-1.25), "-1.25 A");
  assert.equal(formatCurrent(-12.5), "-12.5 A");
  assert.equal(formatCurrent(0), "0 mA");
  assert.equal(formatCurrent(0.16), "160 mA");
  assert.equal(formatCurrent(null), "--");
});

test("power formatting preserves signed values and valid zero", () => {
  assert.equal(formatPower(-1234), "-1.23 kW");
  assert.equal(formatPower(0), "0 W");
  assert.equal(formatPower(null), "--");
});
