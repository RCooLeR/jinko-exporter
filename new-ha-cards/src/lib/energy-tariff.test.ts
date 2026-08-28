import assert from "node:assert/strict";
import test from "node:test";

import {
  calculateDailyGridBalanceUAH,
  GREEN_TARIFF_NET_RATE_UAH_PER_KWH
} from "./energy-tariff.ts";

test("uses the taxed green tariff for net export", () => {
  assert.ok(Math.abs(GREEN_TARIFF_NET_RATE_UAH_PER_KWH - 5.235307) < 1e-12);
  assert.ok(Math.abs(Number(calculateDailyGridBalanceUAH(3.2, 29.7)) - 138.7356355) < 1e-9);
});

test("uses the household buy rate for net import", () => {
  assert.equal(calculateDailyGridBalanceUAH(10, 0), -43.2);
});

test("returns zero for an exactly balanced day", () => {
  assert.equal(calculateDailyGridBalanceUAH(5, 5), 0);
});

test("requires both buy and sell counters", () => {
  assert.equal(calculateDailyGridBalanceUAH(null, 5), null);
  assert.equal(calculateDailyGridBalanceUAH(5, null), null);
});
