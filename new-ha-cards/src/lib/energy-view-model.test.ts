import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "vite";
import type { HomeAssistant } from "../types/home-assistant.ts";

test("view model preserves independent readings and labels only calculated fallbacks", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const readings: Record<string, number> = {
      grid_total_power: -600, grid_l1_power: -100, grid_l2_power: 0, grid_l3_power: -500,
      grid_load_total_power: 0, grid_load_l1_power: -10, grid_load_l2_power: 0, grid_load_l3_power: 10,
      grid_load_l1_voltage: 230, grid_load_l2_voltage: 231, grid_load_l3_voltage: 232,
      home_total_power: 1000, ups_total_power: 600, inverter_total_power: 3000,
      home_l1_power: 100, home_l2_power: 200, home_l3_power: 300,
      battery_current: -0.1, battery_voltage: 615, battery_soc: 0,
      pv_total_power: 1200
    };
    const model = () => buildEnergyViewModel((key: string) => readings[key] ?? null, {batteryNegativeIsCharging: true});
    let view = model();
    assert.equal(view.values["grid.phase_power"], "-100 W / 0 W / -500 W");
    assert.equal(view.values["grid_load.power.value"], "0");
    assert.equal(view.flags.grid_load_offline, false);
    assert.equal(view.values["grid_load.phase_power"], "-10 W / 0 W / 10 W");
    assert.equal(view.values["inverter.ac_power.value"], "3");
    assert.equal(view.values["inverter.dc_power.value"], "--"); // PV is not total inverter DC input.
    assert.equal(view.values["battery.current"], "-0.1 A");
    assert.equal(view.flags.battery_offline, false);
    delete readings.grid_load_total_power;
    delete readings.grid_load_l1_power;
    delete readings.grid_load_l2_power;
    delete readings.grid_load_l3_power;
    view = model();
    assert.equal(view.values["grid_load.power.value"], "≈ 400");
    assert.equal(view.values["grid_load.phase_power"], "--"); // No fictional phase split.
    delete readings.inverter_total_power;
    assert.equal(model().values["inverter.ac_power.value"], "--"); // UPS is a different measurement.
    for (const key of Object.keys(readings)) delete readings[key];
    view = model();
    assert.equal(view.flags.online, false);
    assert.equal(view.flags.battery_full, false);
    assert.equal(view.values["system.online"], "Немає даних");
    assert.equal(view.values["grid.status"], "Немає даних");
  } finally { await server.close(); }
});

test("resolved Grid voltage uses Shelly then Jinko per phase and detects primary restoration", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const { ENTITY_DEFINITIONS, resolveEntities, valueFor } = await server.ssrLoadModule("/src/lib/entity-model.ts");
    const { entityInventorySignature } = await server.ssrLoadModule("/src/lib/entity-inventory.ts");
    const hass: HomeAssistant = { states: {} };
    for (const phase of [1, 2, 3]) {
      for (const [suffix, voltage] of [
        [`grid_g_v_l${phase}`, 230 + phase],
        [`grid_load_l${phase}_voltage`, 240 + phase],
        [`electric_av${phase}`, 250 + phase],
        [`consumption_c_v_l${phase}`, 220 + phase]
      ] as const) hass.states[`sensor.device_${suffix}`] = { state: String(voltage), attributes: { unit_of_measurement: "V" } };
    }
    const view = () => {
      const resolved = resolveEntities(hass);
      return buildEnergyViewModel((key: string) => Object.hasOwn(ENTITY_DEFINITIONS, key) ? valueFor(hass, resolved, key) : null, { batteryNegativeIsCharging: true });
    };
    assert.equal(view().values["grid.phases"], "241V / 242V / 243V");
    for (const phase of [1, 2, 3]) {
      const shellyId = `sensor.device_grid_load_l${phase}_voltage`;
      const before = entityInventorySignature(hass);
      hass.states[shellyId]!.state = "unavailable";
      const unavailable = entityInventorySignature(hass);
      assert.notEqual(unavailable, before, "availability must invalidate cached bindings");
      const fallback = [241, 242, 243];
      fallback[phase - 1] = 230 + phase;
      assert.equal(view().values["grid.phases"], fallback.map((voltage) => `${voltage}V`).join(" / "));
      hass.states[shellyId]!.state = String((240 + phase) * 1000);
      hass.states[shellyId]!.attributes.unit_of_measurement = "mV";
      assert.notEqual(entityInventorySignature(hass), unavailable, "restoration/unit change must invalidate cached fallback");
      assert.equal(view().values["grid.phases"], "241V / 242V / 243V");
    }
    assert.equal(view().values["grid_load.phases"], "241V / 242V / 243V");
    assert.equal(view().values["inverter.phases"], "251V / 252V / 253V");
    assert.equal(view().values["ups.phases"], "221V / 222V / 223V");
  } finally { await server.close(); }
});

test("stabilizers keep upstream, CT and parallel-load readings independent", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const readings: Record<string, number> = {
      grid_input_l1_voltage: 190, grid_input_l2_voltage: 205, grid_input_l3_voltage: 215,
      grid_input_l1_current: -1.2, grid_input_l2_current: 0, grid_input_l3_current: 2.3,
      grid_input_l1_power: -200, grid_input_l2_power: 0, grid_input_l3_power: 400,
      grid_input_total_power: -175, grid_input_frequency: 49.9,
      grid_input_energy_total: 1234.56, grid_input_returned_energy_total: 234.56,
      grid_input_buy_today: 5.25, grid_input_sell_today: 2.1,
      grid_l1_voltage: 230, grid_l2_voltage: 231, grid_l3_voltage: 232,
      grid_l1_current: 1, grid_l2_current: 2, grid_l3_current: 3,
      grid_l1_power: 100, grid_l2_power: 200, grid_l3_power: 300, grid_total_power: 620,
      grid_load_l1_voltage: 233, grid_load_l2_voltage: 234, grid_load_l3_voltage: 235,
      grid_load_total_power: 43, grid_buy_today: 4.5, grid_sell_today: 6.7
    };
    const model = () => buildEnergyViewModel((key: string) => readings[key] ?? null, { batteryNegativeIsCharging: true, stabilizers: true });
    let view = model();
    assert.equal(view.values["grid_input.phases"], "190V / 205V / 215V");
    assert.equal(view.values["grid.phases"], "230V / 231V / 232V");
    assert.equal(view.values["grid_load.phases"], "233V / 234V / 235V");
    assert.equal(view.values["grid_input.phase_power"], "-200 W / 0 W / 400 W");
    assert.equal(view.values["grid_input.power.value"], "-175"); // Measured total is independent, not a phase sum.
    assert.equal(view.values["grid.power.value"], "620");
    assert.equal(view.values["grid_load.power.value"], "43");
    assert.equal(view.values["grid_input.energy_today"], "+3.15 kWh");
    assert.equal(view.values["daily.import.value"], "4.5");
    assert.equal(view.values["daily.export.value"], "6.7");
    assert.equal(view.flags.flow_grid_input_stabilized_reverse, true);
    assert.equal(view.flags.flow_inverter_grid_reverse, true); // Different measurement, independently importing.
    delete readings.grid_input_l2_voltage;
    assert.equal(model().values["grid_input.phases"], "190V / -- / 215V");
    for (const key of Object.keys(readings).filter((key) => key.startsWith("grid_input_"))) delete readings[key];
    view = model();
    for (const field of ["phases", "current", "phase_power", "power.value", "frequency", "energy_today"]) {
      assert.equal(view.values[`grid_input.${field}`], "--", field);
    }
    assert.equal(view.flags.grid_input_offline, true);
    assert.equal(view.flags.grid_offline, false);
    assert.equal(view.flags.grid_load_offline, false);
    assert.equal(view.flags.flow_grid_input_stabilized_active, false);
    assert.ok(view.missing.includes("grid_input_total_power"));
  } finally { await server.close(); }
});

test("upstream-only data survives inverter loss only in the optional topology", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const readings: Record<string, number> = { grid_input_l1_voltage: 195, grid_input_total_power: 0, grid_input_total_current: 0 };
    const model = (stabilizers?: boolean) => buildEnergyViewModel((key: string) => readings[key] ?? null, { batteryNegativeIsCharging: true, stabilizers });
    const enabled = model(true);
    assert.equal(enabled.flags.grid_input_offline, false);
    assert.equal(enabled.flags.grid_offline, true);
    assert.equal(enabled.flags.grid_load_offline, true);
    assert.equal(enabled.values["grid_input.power.value"], "0");
    assert.equal(enabled.values["grid_input.current"], "0 A");
    assert.equal(enabled.values["grid.phases"], "--");
    assert.equal(enabled.values["grid_load.phases"], "--");
    assert.equal(enabled.values["daily.import.value"], "--");
    assert.equal(enabled.values["daily.export.value"], "--");
    assert.equal(enabled.values["system.online"], "Часткові дані");
    assert.equal(enabled.flags.flow_grid_input_stabilized_active, false);
    assert.equal(model().flags.online, false);
    assert.equal(model(false).flags.online, false);
    assert.ok(!model().missing.includes("grid_input_total_power"));
  } finally { await server.close(); }
});

test("explicit stabilizer test mirror copies downstream display without using or mutating upstream data", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const readings: Record<string, number> = {
      grid_l1_voltage: 230, grid_l2_voltage: 231, grid_l3_voltage: 232,
      grid_l1_current: -1.2, grid_l2_current: 0, grid_l3_current: 2.3,
      grid_l1_power: -200, grid_l2_power: 0, grid_l3_power: 400,
      grid_total_power: -175, grid_frequency: 49.9,
      grid_input_l1_voltage: 190, grid_input_l2_voltage: 195, grid_input_l3_voltage: 200,
      grid_input_l1_current: 20, grid_input_l2_current: 30, grid_input_l3_current: 40,
      grid_input_l1_power: 1000, grid_input_l2_power: 2000, grid_input_l3_power: 3000,
      grid_input_total_power: 9000, grid_input_total_current: 90, grid_input_frequency: 60,
      grid_input_energy_total: 9999, grid_input_returned_energy_total: 8888,
      grid_input_buy_today: 99, grid_input_sell_today: 88,
      grid_buy_today: 4.5, grid_sell_today: 6.7
    };
    const before = structuredClone(readings);
    const model = (stabilizers = true, stabilizersTestMirrorGrid = true) => buildEnergyViewModel(
      (key: string) => readings[key] ?? null, { batteryNegativeIsCharging: true, stabilizers, stabilizersTestMirrorGrid }
    );
    const view = model();
    for (const field of ["phases", "current", "phase_power", "power.value", "power.unit", "status"]) {
      assert.equal(view.values[`grid_input.${field}`], view.values[`grid.${field}`], field);
    }
    assert.equal(view.values["grid_input.frequency"], view.values["system.frequency"]);
    assert.equal(view.values["grid_input.power.value"], "-175", "copy independent measured total, not phase sum");
    assert.equal(view.values["grid_input.energy_today"], "-2.2 kWh", "test mirror uses downstream daily net, not upstream daily/lifetime counters");
    assert.equal(view.values["daily.import.value"], "4.5");
    assert.equal(view.values["daily.export.value"], "6.7");
    assert.equal(view.flags.test_mirror_grid, true);
    assert.equal(view.flags.grid_input_offline, view.flags.grid_offline);
    assert.equal(view.flags.flow_grid_input_stabilized_active, view.flags.flow_inverter_grid_active);
    assert.equal(view.flags.flow_grid_input_stabilized_reverse, true, "negative downstream power is upstream export");
    assert.equal(view.flags.flow_grid_input_stabilized_forward, false);
    assert.deepEqual(readings, before, "display-only option must not modify source data");

    const real = model(true, false);
    assert.equal(real.flags.test_mirror_grid, false);
    assert.equal(real.values["grid_input.phases"], "190V / 195V / 200V");
    assert.equal(real.values["grid_input.power.value"], "9");
    assert.equal(real.values["grid_input.energy_today"], "+11 kWh");
    const withoutTopology = model(false, true);
    assert.equal(withoutTopology.flags.test_mirror_grid, false);
    assert.equal(withoutTopology.values["grid_input.power.value"], real.values["grid_input.power.value"]);

    for (const key of Object.keys(readings).filter((key) => key.startsWith("grid_input_"))) delete readings[key];
    assert.equal(model(true, false).values["grid_input.power.value"], "--", "normal mode never implicitly mirrors");
    assert.equal(model().values["grid_input.power.value"], "-175");
  } finally { await server.close(); }
});

test("test mirror propagates zero, missing phases and downstream loss despite live upstream data", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const readings: Record<string, number> = {
      grid_l1_voltage: 0, grid_l1_current: 0, grid_l1_power: 0, grid_total_power: 0, grid_frequency: 0,
      grid_input_l1_voltage: 230, grid_input_total_current: 3, grid_input_total_power: 500,
      grid_input_frequency: 50, grid_input_energy_total: 10, grid_input_returned_energy_total: 20
    };
    const model = () => buildEnergyViewModel((key: string) => readings[key] ?? null,
      { batteryNegativeIsCharging: true, stabilizers: true, stabilizersTestMirrorGrid: true });
    let view = model();
    assert.equal(view.values["grid_input.phases"], "0V / -- / --");
    assert.equal(view.values["grid_input.current"], "0 A / -- / --");
    assert.equal(view.values["grid_input.phase_power"], "0 W / -- / --");
    assert.equal(view.values["grid_input.power.value"], "0");
    assert.equal(view.values["grid_input.frequency"], "0 Hz");
    assert.equal(view.flags.grid_input_offline, false);
    assert.equal(view.flags.flow_grid_input_stabilized_active, false);
    assert.equal(view.flags.flow_grid_input_stabilized_forward, false);
    assert.equal(view.flags.flow_grid_input_stabilized_reverse, false);

    readings.grid_total_power = 25;
    view = model();
    assert.equal(view.flags.flow_grid_input_stabilized_active, true);
    assert.equal(view.flags.flow_grid_input_stabilized_forward, true);
    assert.equal(view.flags.flow_grid_input_stabilized_reverse, false);
    for (const key of Object.keys(readings).filter((key) => !key.startsWith("grid_input_"))) delete readings[key];
    view = model();
    for (const field of ["phases", "current", "phase_power", "power.value", "frequency", "energy_today"]) {
      assert.equal(view.values[`grid_input.${field}`], "--", field);
    }
    assert.equal(view.flags.grid_input_offline, true);
    assert.equal(view.flags.grid_offline, true);
    assert.equal(view.flags.online, false, "real upstream data is ignored while its card is a test mirror");
    assert.equal(view.flags.flow_grid_input_stabilized_active, false);
    assert.equal(view.flags.test_mirror_grid, true, "test indication remains visible during source loss");
    assert.ok(view.missing.includes("grid_total_power"));
    assert.ok(!view.missing.includes("grid_input_total_power"), "do not request a real upstream sensor in explicit test mode");
  } finally { await server.close(); }
});

test("upstream Today is one signed daily balance requiring both finite daily counters", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  try {
    const { buildEnergyViewModel } = await server.ssrLoadModule("/src/lib/energy-view-model.ts");
    const cases: Array<[number | null, number | null, string]> = [
      [1234.56, 234.56, "+1000 kWh"],
      [234.56, 1234.56, "-1000 kWh"],
      [10.12, 3, "+7.12 kWh"],
      [3, 10.12, "-7.12 kWh"],
      [5, 5, "0 kWh"],
      [0, 0, "0 kWh"],
      [-0, 0, "0 kWh"],
      [0, 3.5, "-3.5 kWh"],
      [3.5, 0, "+3.5 kWh"],
      [0.004, 0, "0 kWh"],
      [0, 0.004, "0 kWh"],
      [0.006, 0, "+0.01 kWh"],
      [0, 0.006, "-0.01 kWh"],
      [null, 0, "--"],
      [0, null, "--"],
      [null, null, "--"],
      [NaN, 0, "--"],
      [0, NaN, "--"],
      [Infinity, 0, "--"],
      [0, -Infinity, "--"],
      [Number.MAX_VALUE, -Number.MAX_VALUE, "--"],
      [-Number.MAX_VALUE, Number.MAX_VALUE, "--"]
    ];
    for (const [imported, returned, expected] of cases) {
      const readings = Object.freeze({
        grid_input_buy_today: imported,
        grid_input_sell_today: returned,
        grid_input_energy_total: 9999,
        grid_input_returned_energy_total: 1000,
        grid_buy_today: 50,
        grid_sell_today: 20
      });
      const model = (stabilizersTestMirrorGrid: boolean) => buildEnergyViewModel(
        (key: string) => readings[key as keyof typeof readings] ?? null,
        { batteryNegativeIsCharging: true, stabilizers: true, stabilizersTestMirrorGrid }
      );
      const view = model(false);
      assert.equal(view.values["grid_input.energy_today"], expected, `${imported} - ${returned}`);
      assert.equal(view.values["grid_input.energy_balance"], undefined, "cumulative balance is not presented as Today");
      assert.equal(view.values["grid_input.energy_total"], undefined, "superseded display field is removed");
      assert.equal(view.values["grid_input.returned_energy_total"], undefined, "raw binding is not a second display field");
      assert.equal(view.values["daily.import.value"], "50");
      assert.equal(view.values["daily.export.value"], "20");
      assert.equal(model(true).values["grid_input.energy_today"], "+30 kWh", "explicit test mirror uses downstream daily balance");

      const mirroredReadings = {
        ...readings, grid_input_buy_today: 5, grid_input_sell_today: 1,
        grid_buy_today: imported, grid_sell_today: returned
      };
      const mirrored = buildEnergyViewModel(
        (key: string) => mirroredReadings[key as keyof typeof mirroredReadings] ?? null,
        { batteryNegativeIsCharging: true, stabilizers: true, stabilizersTestMirrorGrid: true }
      );
      assert.equal(mirrored.values["grid_input.energy_today"], expected, `mirror ${imported} - ${returned}`);
    }
  } finally { await server.close(); }
});
