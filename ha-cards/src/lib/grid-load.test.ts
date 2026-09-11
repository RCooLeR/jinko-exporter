import assert from "node:assert/strict";
import test from "node:test";

import { ENTITY_KEYS, resolveEntities, valueFor, type EntityKey } from "./entity-model.ts";
import { formatMeasuredPhases, formatPower } from "./format.ts";
import { measuredGridLoadFor } from "./grid-load.ts";
import type { HomeAssistant } from "../types/home-assistant.ts";

test("Shelly remains visible while every inverter entity is unavailable", () => {
  const hass = inverterOfflineWithShelly({
    grid_load_total_power: "1533",
    grid_load_total_current: "7",
    grid_load_l1_voltage: "230",
    grid_load_l2_voltage: "231",
    grid_load_l3_voltage: "232",
    grid_load_l1_current: "0",
    grid_load_l2_current: "2",
    grid_load_l3_current: "5",
    grid_load_l1_power: "0",
    grid_load_l2_power: "433",
    grid_load_l3_power: "1100"
  });
  const resolved = resolveEntities(hass, Object.fromEntries(ENTITY_KEYS.map((key) => [key, `sensor.test_${key}`])));
  const read = (key: EntityKey) => valueFor(hass, resolved, key);
  const load = measuredGridLoadFor(read);

  assert.equal(load.available, true);
  assert.equal(load.voltage, 231);
  assert.equal(load.current, 7);
  assert.equal(load.power, 1533);
  assert.equal(formatPower(load.power), "1.53 kW");
  assert.equal(formatMeasuredPhases("voltage", load.voltagePhases, load.voltage), "230 / 231 / 232 V");
  assert.equal(formatMeasuredPhases("current", load.currentPhases, load.current), "0 / 2 / 5 A");
  assert.equal(formatMeasuredPhases("power", load.powerPhases, load.power), "0 / 0.43 / 1.1 kW");
  for (const key of ["inverter_total_power", "grid_total_power", "home_total_power", "pv_total_power"] as const) {
    assert.equal(read(key), null, `${key} must not be synthesized from Shelly`);
  }
});

test("zero measured load remains available and is not replaced by inverter data", () => {
  const readings: Partial<Record<EntityKey, number>> = {
    grid_load_total_power: 0,
    grid_load_total_current: 0,
    grid_load_l1_power: 0,
    grid_load_l2_power: 0,
    grid_load_l3_power: 0,
    grid_load_l1_current: 0,
    grid_load_l2_current: 0,
    grid_load_l3_current: 0,
    grid_load_l1_voltage: 230,
    grid_load_l2_voltage: 231,
    grid_load_l3_voltage: 232,
    home_total_power: 5000,
    grid_l1_voltage: 249
  };
  const load = measuredGridLoadFor((key) => readings[key] ?? null);

  assert.equal(load.available, true);
  assert.equal(load.power, 0);
  assert.equal(load.current, 0);
  assert.equal(load.voltage, 231);
  assert.equal(formatPower(load.power), "0 W");
  assert.equal(formatMeasuredPhases("power", load.powerPhases, load.power), "0 / 0 / 0 W");
  assert.equal(formatMeasuredPhases("current", load.currentPhases, load.current, true), "0/0/0 A");
});

test("missing meter readings stay unknown rather than becoming zero", () => {
  const load = measuredGridLoadFor(() => null);
  assert.equal(load.available, false);
  assert.equal(load.power, null);
  assert.equal(load.current, null);
  assert.equal(load.voltage, null);
  assert.equal(formatMeasuredPhases("power", load.powerPhases, load.power), "--");
  assert.equal(formatMeasuredPhases("current", load.currentPhases, load.current), "--");
});

test("phase fallback preserves zero without inventing missing measurements", () => {
  const readings: Partial<Record<EntityKey, number>> = {
    grid_load_l1_power: 0,
    grid_load_l2_power: 100,
    grid_load_l1_current: 0,
    grid_load_l2_current: -0.5,
    grid_load_l1_voltage: 0,
    grid_load_l2_voltage: 230
  };
  const load = measuredGridLoadFor((key) => readings[key] ?? null);
  assert.equal(load.power, 100);
  assert.equal(load.current, 0.5);
  assert.equal(load.voltage, 115);
  assert.equal(formatMeasuredPhases("power", load.powerPhases, load.power), "0 / 100 / -- W");
  assert.equal(formatMeasuredPhases("voltage", load.voltagePhases, load.voltage), "0 / 230 / -- V");
});

const inverterOfflineWithShelly = (readings: Partial<Record<EntityKey, string>>): HomeAssistant => ({
  states: Object.fromEntries(ENTITY_KEYS.map((key) => [`sensor.test_${key}`, {
    state: readings[key] ?? "unavailable",
    attributes: {}
  }]))
});
