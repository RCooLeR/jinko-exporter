import assert from "node:assert/strict";
import test from "node:test";

import { normalizeText, resolveEntities, stateFor, toNumber, valueFor, type EntityKey } from "./entity-model.ts";
import type { HomeAssistant, HomeAssistantState } from "../types/home-assistant.ts";

test("normalizeText makes names comparable to Home Assistant entity IDs", () => {
  assert.equal(normalizeText("Grid\u00a0Voltage L1"), "grid voltage l1");
  assert.equal(normalizeText("sensor.jinko_grid_voltage_l1"), "sensor jinko grid voltage l1");
});

test("resolveEntities matches friendly names and entity ids", () => {
  const hass = hassWithStates({
    "sensor.jinko_dc_power_pv1": state("1250", "Jinko DC Power PV1"),
    "sensor.jinko_bms_soc": state("82", "Jinko BMS_SOC"),
    "sensor.other": state("1", "Other"),
  });

  const resolved = resolveEntities(hass, {}, ["pv1_power", "battery_soc"]);

  assert.equal(resolved.pv1_power, "sensor.jinko_dc_power_pv1");
  assert.equal(resolved.battery_soc, "sensor.jinko_bms_soc");
});

test("resolveEntities matches Shelly-backed grid load sensors", () => {
  const hass = hassWithStates({
    "sensor.jinko_grid_load_total_power": state("1533", "Jinko Grid Load Total Power"),
    "sensor.jinko_grid_load_l1_current": state("1.1", "Jinko Grid Load L1 Current"),
  });

  const resolved = resolveEntities(hass, {}, ["grid_load_total_power", "grid_load_l1_current"]);

  assert.equal(resolved.grid_load_total_power, "sensor.jinko_grid_load_total_power");
  assert.equal(resolved.grid_load_l1_current, "sensor.jinko_grid_load_l1_current");
});

test("explicit overrides win when the entity exists", () => {
  const hass = hassWithStates({
    "sensor.custom_pv": state("777", "My custom PV"),
    "sensor.jinko_dc_power_pv1": state("1250", "Jinko DC Power PV1"),
  });

  const resolved = resolveEntities(hass, { pv1_power: "sensor.custom_pv" }, ["pv1_power"]);

  assert.equal(resolved.pv1_power, "sensor.custom_pv");
});

test("missing overrides fall back to automatic resolution", () => {
  const hass = hassWithStates({
    "sensor.jinko_dc_power_pv1": state("1250", "Jinko DC Power PV1"),
  });

  const resolved = resolveEntities(hass, { pv1_power: "sensor.missing" }, ["pv1_power"]);

  assert.equal(resolved.pv1_power, "sensor.jinko_dc_power_pv1");
});

test("state and value helpers distinguish unknown from zero", () => {
  const hass = hassWithStates({
    "sensor.grid_power": state("0", "Total Grid Power"),
    "sensor.battery_soc": state("unknown", "SoC"),
  });
  const resolved = {
    grid_total_power: "sensor.grid_power",
    battery_soc: "sensor.battery_soc",
    pv1_power: null,
  } satisfies Partial<Record<EntityKey, string | null>>;

  assert.equal(stateFor(hass, resolved, "grid_total_power")?.state, "0");
  assert.equal(valueFor(hass, resolved, "grid_total_power"), 0);
  assert.equal(valueFor(hass, resolved, "battery_soc"), null);
  assert.equal(valueFor(hass, resolved, "pv1_power"), null);
  assert.equal(toNumber(undefined), null);
});

const hassWithStates = (states: Record<string, HomeAssistantState>): HomeAssistant => ({ states });

const state = (value: string, friendlyName: string): HomeAssistantState => ({
  state: value,
  attributes: {
    friendly_name: friendlyName,
  },
});
