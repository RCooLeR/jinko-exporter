import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

import { ENTITY_KEYS, type EntityKey, type ResolvedEntityMap } from "./entity-model.ts";
import type { HomeAssistant } from "../types/home-assistant.ts";

interface MetricGroup {
  voltage: number | null;
  current: number | null;
  power: number | null;
  energyToday?: number | null;
  signedEnergyToday?: boolean;
  powerPhases?: Array<number | null>;
}

interface CardHarness {
  _config: { stabilizers?: boolean; stabilizers_test_mirror_grid?: boolean; static?: boolean };
  _hass: HomeAssistant;
  _resolved: ResolvedEntityMap;
  _resolveEntities(force?: boolean): void;
  _buildData(): {
    groups: Record<string, MetricGroup>;
    layers: Record<string, boolean>;
    inverterStatus: string;
    batterySoc: string;
  };
  _buildValues(): Record<string, string>;
  _layerNodes: Map<string, { hidden: boolean }>;
  _syncLayers(): void;
  _formatMetricRowValue(row: string, group: MetricGroup): string;
}

test("cards display independent Shelly readings with the inverter offline", async (t) => {
  const cards = new Map<string, new () => CardHarness>();
  const globals: Record<string, unknown> = {
    HTMLElement: class { attachShadow(): object { return {}; } },
    customElements: {
      get: (name: string) => cards.get(name),
      define: (name: string, value: new () => CardHarness) => cards.set(name, value)
    },
    window: {}
  };
  const previous = new Map(Object.keys(globals).map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  for (const [key, value] of Object.entries(globals)) {
    Object.defineProperty(globalThis, key, { value, configurable: true });
  }
  // Vite loads the production TS/JSON modules without starting a web listener.
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("../..", import.meta.url)),
    server: { middlewareMode: true, hmr: false, ws: false },
    appType: "custom"
  });
  try {
    await server.ssrLoadModule("/src/cards/jks-detailed-card.ts");
    await server.ssrLoadModule("/src/cards/jks-mini-card.ts");
    for (const power of [1533, 0]) {
      await t.test(`live Shelly ${power} W`, () => {
        const detailed = fixtureCard(cards.get("jks-detailed")!, power);
        const data = detailed._buildData();
        const load = data.groups.parallel_grid_load!;
        assert.equal(data.inverterStatus, "Offline");
        assert.equal(data.layers.parallel_offline, false);
        assert.equal(data.layers.ups_offline, true);
        assert.equal(data.groups.inverter!.power, null);
        assert.equal(data.groups.grid!.power, null);
        assert.equal(load.voltage, 231);
        assert.equal(load.power, power);
        assert.equal(load.current, power ? 7 : 0);
        assert.equal(detailed._formatMetricRowValue("voltage", load), "230 / 231 / 232 V");
        assert.equal(detailed._formatMetricRowValue("power", load), power ? "0 / 0.43 / 1.1 kW" : "0 / 0 / 0 W");
        assert.equal(detailed._formatMetricRowValue("current", load), power ? "0 / 2 / 5 A" : "0 / 0 / 0 A");

        const values = fixtureCard(cards.get("jks-mini")!, power)._buildValues();
        assert.equal(values.combined_load, power ? "1.53 kW" : "0 W");
        assert.equal(values.combined_pv, "--");
        assert.equal(values.grid_node, "--");
        assert.equal(values.battery_node, "--");
      });
    }
    await t.test("missing Shelly fields use labelled estimates and inverter voltage fallback", () => {
      const detailed = fixtureCard(cards.get("jks-detailed")!, 1533);
      for (const key of ["grid_load_total_current", "grid_load_l1_current", "grid_load_l2_current", "grid_load_l3_current", "grid_load_l3_voltage"]) {
        detailed._hass.states[`sensor.test_${key}`]!.state = "unavailable";
      }
      detailed._hass.states["sensor.test_grid_l3_voltage"]!.state = "249";
      const load = detailed._buildData().groups.parallel_grid_load!;
      assert.ok(load.current !== null);
      assert.match(detailed._formatMetricRowValue("current", load), /^≈ /);
      assert.equal(detailed._formatMetricRowValue("voltage", load), "230 / 231 / 249 V");
    });
    await t.test("CT and BMS win, fall back when unavailable, and recover without reload", () => {
      const detailed = new (cards.get("jks-detailed")!)();
      detailed._hass = priorityFixture();
      detailed._resolveEntities();
      let data = detailed._buildData();
      assert.equal(data.groups.grid!.power, -600);
      assert.equal(data.groups.battery!.voltage, 615);
      assert.equal(data.groups.battery!.current, -0.1);
      assert.equal(data.batterySoc, "80%");
      assert.equal(data.groups.battery!.power, -61); // No BMS power metric: inverter fallback.
      assert.equal(data.groups.inverter!.power, 3000);
      assert.equal(data.groups.parallel_grid_load!.power, 700);
      assert.deepEqual(data.groups.ups_load!.powerPhases, [10, 20, 30]);
      assert.equal(detailed._formatMetricRowValue("current", data.groups.battery!), "-100 mA");
      detailed._hass.states["sensor.ct_total"]!.state = "unavailable";
      detailed._hass.states["sensor.bms_soc"]!.state = "unavailable";
      detailed._resolveEntities();
      data = detailed._buildData();
      assert.equal(data.groups.grid!.power, 900);
      assert.equal(data.batterySoc, "70%");
      detailed._hass.states["sensor.ct_total"]!.state = "0";
      detailed._hass.states["sensor.bms_soc"]!.state = "0";
      detailed._resolveEntities();
      data = detailed._buildData();
      assert.equal(data.groups.grid!.power, 0);
      assert.equal(data.layers.grid_offline, false);
      assert.equal(data.batterySoc, "0%");
      assert.equal(data.layers.battery_offline, false);
      assert.equal(detailed._formatMetricRowValue("power", data.groups.grid!), "-100 / -200 / -300 W");
      for (const [raw, unit, expected] of [["0x10", "W", 900], ["10", "W", 10], ["1e308", "kW", 900], ["1", "kW", 1000]] as const) {
        detailed._hass.states["sensor.ct_total"]!.state = raw;
        detailed._hass.states["sensor.ct_total"]!.attributes.unit_of_measurement = unit;
        detailed._resolveEntities();
        assert.equal(detailed._buildData().groups.grid!.power, expected, `primary ${raw} ${unit}`);
      }
    });
    await t.test("new upstream meter uses only its own bus and never labels lifetime energy as Today", () => {
      const detailed = new (cards.get("jks-detailed")!)();
      detailed._hass = priorityFixture();
      detailed._resolveEntities();
      let input = detailed._buildData().groups.grid_input!;
      assert.equal(input.voltage, null);
      assert.equal(input.power, null);
      const values: Record<string, [number, string]> = {
        grid_input_l1_voltage: [245, "V"], grid_input_l2_voltage: [236, "V"], grid_input_l3_voltage: [241, "V"],
        grid_input_l1_current: [1, "A"], grid_input_l2_current: [2, "A"], grid_input_l3_current: [0, "A"],
        grid_input_l1_power: [-100, "W"], grid_input_l2_power: [50, "W"], grid_input_l3_power: [0, "W"],
        grid_input_energy_total: [0, "kWh"], grid_input_returned_energy_total: [123.4, "kWh"]
      };
      for (const [id, [value, unit]] of Object.entries(values)) {
        detailed._hass.states[`sensor.meter_${id}`] = { state: String(value), attributes: { unit_of_measurement: unit } };
      }
      detailed._resolveEntities();
      input = detailed._buildData().groups.grid_input!;
      assert.equal(input.power, -50);
      assert.equal(detailed._formatMetricRowValue("power", input), "-100 / 50 / 0 W");
      assert.equal(detailed._formatMetricRowValue("voltage", input), "245 / 236 / 241 V");
      assert.equal(detailed._formatMetricRowValue("energy_today", input), "--");
      detailed._hass.states["sensor.meter_grid_input_energy_total"]!.state = "200";
      detailed._resolveEntities();
      assert.equal(detailed._formatMetricRowValue("energy_today", detailed._buildData().groups.grid_input!), "--");
      detailed._hass.states["sensor.meter_grid_input_returned_energy_total"]!.state = "unavailable";
      detailed._resolveEntities();
      assert.equal(detailed._formatMetricRowValue("energy_today", detailed._buildData().groups.grid_input!), "--");
      assert.equal(detailed._buildData().groups.grid!.power, -600);
      assert.equal(detailed._buildData().groups.parallel_grid_load!.power, 700);
      detailed._hass.states["sensor.meter_grid_input_l2_power"]!.state = "unavailable";
      detailed._resolveEntities();
      input = detailed._buildData().groups.grid_input!;
      assert.equal(input.power, null, "an incomplete phase sum must not become a measured total");
      assert.equal(detailed._formatMetricRowValue("power", input), "-100 / -- / 0 W");
    });
    await t.test("upstream Today requires both own daily counters and preserves units, direction and zero", () => {
      const detailed = new (cards.get("jks-detailed")!)();
      detailed._hass = priorityFixture();
      for (const [key, value] of [
        ["grid_input_energy_total", 9000], ["grid_input_returned_energy_total", 1000],
        ["grid_e_b_d", 30], ["grid_e_s_d", 10]
      ] as const) detailed._hass.states[`sensor.device_${key}`] = { state: String(value), attributes: { unit_of_measurement: "kWh" } };
      const daily = (imported: string | null, exported: string | null, unit = "kWh"): MetricGroup => {
        for (const [key, value] of [["grid_input_energy_today", imported], ["grid_input_returned_energy_today", exported]] as const) {
          if (value === null) delete detailed._hass.states[`sensor.device_${key}`];
          else detailed._hass.states[`sensor.device_${key}`] = { state: value, attributes: { unit_of_measurement: unit } };
        }
        detailed._resolveEntities();
        return detailed._buildData().groups.grid_input!;
      };
      for (const [imported, exported, unit, expected, net] of [
        ["15", "2.66", "kWh", "+12.34 kWh", 12.34],
        ["2.66", "15", "kWh", "-12.34 kWh", -12.34],
        ["15000", "2660", "Wh", "+12.34 kWh", 12.34],
        ["0", "5", "kWh", "-5 kWh", -5], ["5", "0", "kWh", "+5 kWh", 5],
        ["12", "12", "kWh", "0 kWh", 0], ["0", "0", "kWh", "0 kWh", 0],
        [null, "0", "kWh", "--", null], ["0", null, "kWh", "--", null],
        ["unavailable", "0", "kWh", "--", null], ["0", "unknown", "kWh", "--", null],
        ["NaN", "0", "kWh", "--", null], ["0", "Infinity", "kWh", "--", null],
        ["5", "0", "W", "--", null]
      ] as const) {
        const group = daily(imported, exported, unit);
        assert.equal(group.signedEnergyToday, true);
        assert.equal(group.energyToday, net);
        assert.equal(detailed._formatMetricRowValue("energy_today", group), expected, `${imported} - ${exported} ${unit}`);
        assert.equal(detailed._buildData().groups.grid!.energyToday, 20, "upstream must not alter inverter daily counters");
      }
    });
    await t.test("explicit test mirror duplicates Grid daily net without changing HA state", () => {
      const detailed = new (cards.get("jks-detailed")!)();
      detailed._hass = priorityFixture();
      detailed._hass.states["sensor.new_grid_input_total_power"] = { state: "9876", attributes: { unit_of_measurement: "W" } };
      detailed._hass.states["sensor.new_grid_input_energy_total"] = { state: "999", attributes: { unit_of_measurement: "kWh" } };
      detailed._hass.states["sensor.new_grid_input_energy_today"] = { state: "10", attributes: { unit_of_measurement: "kWh" } };
      detailed._hass.states["sensor.new_grid_input_returned_energy_today"] = { state: "1", attributes: { unit_of_measurement: "kWh" } };
      detailed._hass.states["sensor.grid_e_b_d"] = { state: "2", attributes: { unit_of_measurement: "kWh" } };
      detailed._hass.states["sensor.grid_e_s_d"] = { state: "5", attributes: { unit_of_measurement: "kWh" } };
      detailed._resolveEntities();
      const rawBefore = JSON.stringify(detailed._hass);
      const resolvedBefore = JSON.stringify(detailed._resolved);
      detailed._config.stabilizers = true;
      const normal = detailed._buildData();
      assert.equal(normal.groups.grid_input!.power, 9876);
      assert.equal(detailed._formatMetricRowValue("energy_today", normal.groups.grid_input!), "+9 kWh");
      detailed._config.stabilizers_test_mirror_grid = true;
      let data = detailed._buildData();
      for (const row of ["voltage", "current", "power", "energy_today"]) {
        assert.equal(detailed._formatMetricRowValue(row, data.groups.grid_input!), detailed._formatMetricRowValue(row, data.groups.grid!));
      }
      assert.equal(data.groups.grid_input!.power, -600);
      assert.equal(detailed._formatMetricRowValue("energy_today", data.groups.grid_input!), "-3 kWh");
      assert.deepEqual(data.groups.grid_input, data.groups.grid, "the test mirror copies the complete displayed group");
      assert.equal(data.groups.parallel_grid_load!.power, normal.groups.parallel_grid_load!.power);
      assert.equal(JSON.stringify(detailed._hass), rawBefore);
      assert.equal(JSON.stringify(detailed._resolved), resolvedBefore);
      for (const [imported, exported, expected] of [
        ["5", "2", "+3 kWh"], ["2", "5", "-3 kWh"], ["0", "0", "0 kWh"],
        ["unknown", "0", "--"], ["0", "unavailable", "--"]
      ] as const) {
        detailed._hass.states["sensor.grid_e_b_d"]!.state = imported;
        detailed._hass.states["sensor.grid_e_s_d"]!.state = exported;
        detailed._resolveEntities();
        data = detailed._buildData();
        assert.equal(detailed._formatMetricRowValue("energy_today", data.groups.grid_input!), expected);
        assert.deepEqual(data.groups.grid_input, data.groups.grid);
      }
      detailed._hass.states["sensor.ct_total"]!.state = "0";
      detailed._resolveEntities();
      data = detailed._buildData();
      assert.equal(data.groups.grid_input!.power, 0);
      for (const [id, state] of Object.entries(detailed._hass.states)) {
        if (!id.includes("grid_input")) state.state = "unavailable";
      }
      detailed._resolveEntities();
      data = detailed._buildData();
      assert.equal(data.groups.grid_input!.power, null, "test mode follows unavailable Grid, never the real upstream reading");
      assert.equal(detailed._formatMetricRowValue("power", data.groups.grid_input!), "--");
      assert.equal(detailed._formatMetricRowValue("energy_today", data.groups.grid_input!), "--");
      detailed._config.stabilizers_test_mirror_grid = false;
      assert.equal(detailed._buildData().groups.grid_input!.power, 9876);
      assert.equal(detailed._formatMetricRowValue("energy_today", detailed._buildData().groups.grid_input!), "+9 kWh");
      detailed._config.stabilizers_test_mirror_grid = true;
      detailed._config.stabilizers = false;
      assert.equal(detailed._buildData().groups.grid_input!.power, 9876, "test flag is ineffective without stabilizer topology");
      detailed._config.stabilizers = true;
      detailed._config.static = true;
      data = detailed._buildData();
      assert.equal(detailed._formatMetricRowValue("voltage", data.groups.grid_input!), detailed._formatMetricRowValue("voltage", data.groups.grid!));
      assert.equal(detailed._formatMetricRowValue("energy_today", data.groups.grid_input!), detailed._formatMetricRowValue("energy_today", data.groups.grid!));
    });
    await t.test("Grid Shelly voltage recovers per phase through the actual card inventory cache", () => {
      const detailed = new (cards.get("jks-detailed")!)();
      detailed._hass = priorityFixture();
      for (const phase of [1, 2, 3]) {
        for (const [suffix, voltage] of [
          [`grid_g_v_l${phase}`, 230 + phase],
          [`grid_load_l${phase}_voltage`, 240 + phase],
          [`electric_av${phase}`, 250 + phase],
          [`consumption_c_v_l${phase}`, 220 + phase]
        ] as const) {
          detailed._hass.states[`sensor.device_${suffix}`] = { state: String(voltage), attributes: { unit_of_measurement: "V" } };
        }
      }
      const voltageRow = (group: string): string => detailed._formatMetricRowValue("voltage", detailed._buildData().groups[group]!);
      detailed._resolveEntities();
      assert.equal(voltageRow("grid"), "241 / 242 / 243 V");
      assert.equal(voltageRow("parallel_grid_load"), "241 / 242 / 243 V");
      for (const phase of [1, 2, 3]) {
        const shellyId = `sensor.device_grid_load_l${phase}_voltage`;
        const key = `grid_l${phase}_voltage` as EntityKey;
        for (const [raw, unit] of [["unavailable", "V"], ["0x10", "V"], ["245", "W"]]) {
          detailed._hass.states[shellyId] = { state: raw!, attributes: { unit_of_measurement: unit } };
          detailed._resolveEntities();
          assert.equal(detailed._resolved[key], `sensor.device_grid_g_v_l${phase}`);
          detailed._hass.states[shellyId] = { state: String((240 + phase) * 1000), attributes: { unit_of_measurement: "mV" } };
          detailed._resolveEntities();
          assert.equal(detailed._resolved[key], shellyId, "healthy Shelly must reclaim a cached Jinko fallback without reload");
          assert.equal(voltageRow("grid"), "241 / 242 / 243 V");
        }
        delete detailed._hass.states[shellyId];
        detailed._resolveEntities();
        assert.equal(detailed._resolved[key], `sensor.device_grid_g_v_l${phase}`);
        detailed._hass.states[shellyId] = { state: "0", attributes: { unit_of_measurement: "V" } };
        detailed._resolveEntities();
        assert.equal(detailed._resolved[key], shellyId, "zero is a valid restored reading");
        detailed._hass.states[shellyId]!.state = String(240 + phase);
        detailed._resolveEntities();
      }
      assert.equal(voltageRow("grid"), "241 / 242 / 243 V");
      assert.equal(voltageRow("inverter"), "251 / 252 / 253 V");
      assert.equal(voltageRow("ups_load"), "221 / 222 / 223 V");
      assert.equal(detailed._buildData().groups.grid!.power, -600);
    });
    await t.test("mini PV and generator totals use complete phase fallbacks without replacing measured zero", () => {
      for (const scenario of [
        { name: "complete phases", pv: [1000, 2000], generator: [100, 200, 300], pvTotal: null, generatorTotal: null, pvText: "3 kW", generatorText: "600 W", active: true },
        { name: "incomplete phases", pv: [1000, null], generator: [100, 200, null], pvTotal: null, generatorTotal: null, pvText: "--", generatorText: "--", active: false },
        { name: "complete zero phases", pv: [0, 0], generator: [0, 0, 0], pvTotal: null, generatorTotal: null, pvText: "0 W", generatorText: "0 W", active: false },
        { name: "measured zero wins", pv: [1000, 2000], generator: [100, 200, 300], pvTotal: 0, generatorTotal: 0, pvText: "0 W", generatorText: "0 W", active: false },
        { name: "measured totals win", pv: [1000, 2000], generator: [100, 200, 300], pvTotal: 500, generatorTotal: 100, pvText: "500 W", generatorText: "100 W", active: true }
      ]) {
        const card = fixtureCard(cards.get("jks-mini")!, 0);
        const readings: Partial<Record<EntityKey, number | null>> = {
          pv_total_power: scenario.pvTotal,
          pv1_power: scenario.pv[0],
          pv2_power: scenario.pv[1],
          generator_total_power: scenario.generatorTotal,
          generator_l1_power: scenario.generator[0],
          generator_l2_power: scenario.generator[1],
          generator_l3_power: scenario.generator[2]
        };
        for (const [key, value] of Object.entries(readings)) {
          card._hass.states[`sensor.test_${key}`]!.state = value?.toString() ?? "unavailable";
        }
        const values = card._buildValues();
        assert.equal(values.combined_pv, scenario.pvText, scenario.name);
        assert.equal(values.generator_node, scenario.generatorText, scenario.name);
        for (const id of ["pv_offline", "gen_offline"]) card._layerNodes.set(id, { hidden: false });
        card._syncLayers();
        for (const id of ["pv_offline", "gen_offline"]) {
          assert.equal(card._layerNodes.get(id)!.hidden, scenario.active, `${scenario.name}: ${id}`);
        }
      }
    });
    await t.test("UPS never borrows general load phases and estimated load needs both totals", () => {
      for (const tag of ["jks-detailed", "jks-mini"]) {
        const card = new (cards.get(tag)!)();
        card._hass = priorityFixture();
        for (const key of ["shelly_total", "ups_l1", "ups_l2", "ups_l3"]) delete card._hass.states[`sensor.${key}`];
        card._resolveEntities();
        if (tag === "jks-detailed") {
          const data = card._buildData();
          assert.deepEqual(data.groups.ups_load!.powerPhases, [null, null, null]);
          assert.equal(data.groups.ups_load!.power, 60);
          assert.equal(data.groups.parallel_grid_load!.power, 940);
          assert.equal(card._formatMetricRowValue("power", data.groups.parallel_grid_load!), "≈ +940 W");
        } else assert.equal(card._buildValues().combined_load, "≈ 940 W");
        delete card._hass.states["sensor.ups_total"];
        card._resolveEntities();
        if (tag === "jks-detailed") assert.equal(card._buildData().groups.parallel_grid_load!.power, null);
        else assert.equal(card._buildValues().combined_load, "--");
      }
    });
  } finally {
    await server.close();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
});

const priorityFixture = (): HomeAssistant => ({ states: Object.fromEntries([
  ["ct_total", "Total External CT Power", -600, "W"],
  ["ct_l1", "External CT1 Power", -100, "W"],
  ["ct_l2", "External CT2 Power", -200, "W"],
  ["ct_l3", "External CT3 Power", -300, "W"],
  ["grid_total", "Total Grid Power", 900, "W"],
  ["internal_total", "Internal Power", 500, "W"],
  ["bms_soc", "Jinko Inverter BMS_SOC", 80, "%"],
  ["soc", "Jinko Inverter SoC", 70, "%"],
  ["bms_voltage", "BMS Voltage", 615, "V"],
  ["battery_voltage", "Battery Voltage", 600, "V"],
  ["bms_current", "BMS Current", -0.1, "A"],
  ["battery_current", "Battery Current", 0.2, "A"],
  ["battery_power", "Battery Power", -61, "W"],
  ["inverter_total", "Total Inverter Output Power", 3, "kW"],
  ["shelly_total", "Grid Load Total Power", 700, "W"],
  ["home_total", "Total Consumption Power", 1000, "W"],
  ["ups_total", "UPS Load Power", 60, "W"],
  ["ups_l1", "Load Power L1", 10, "W"],
  ["ups_l2", "Load Power L2", 20, "W"],
  ["ups_l3", "Load Power L3", 30, "W"],
  ["general_l1", "Load phase power A", 300, "W"],
  ["general_l2", "Load phase power B", 400, "W"],
  ["general_l3", "Load phase power C", 500, "W"]
].map(([id, name, value, unit]) => [`sensor.${id}`, {state: String(value), attributes: {friendly_name: name, unit_of_measurement: unit}}])) });

const fixtureCard = (Card: new () => CardHarness, power: number): CardHarness => {
  const readings: Partial<Record<EntityKey, number>> = {
    grid_load_total_power: power,
    grid_load_total_current: power ? 7 : 0,
    grid_load_l1_voltage: 230,
    grid_load_l2_voltage: 231,
    grid_load_l3_voltage: 232,
    grid_load_l1_current: 0,
    grid_load_l2_current: power ? 2 : 0,
    grid_load_l3_current: power ? 5 : 0,
    grid_load_l1_power: 0,
    grid_load_l2_power: power ? 433 : 0,
    grid_load_l3_power: power ? 1100 : 0
  };
  const card = new Card();
  card._hass = { states: Object.fromEntries(ENTITY_KEYS.map((key) => [`sensor.test_${key}`, {
    state: readings[key]?.toString() ?? "unavailable",
    attributes: {}
  }])) };
  card._resolved = Object.fromEntries(ENTITY_KEYS.map((key) => [key, `sensor.test_${key}`]));
  return card;
};
