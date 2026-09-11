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
}

interface CardHarness {
  _hass: HomeAssistant;
  _resolved: ResolvedEntityMap;
  _buildData(): {
    groups: Record<string, MetricGroup>;
    layers: Record<string, boolean>;
    inverterStatus: string;
  };
  _buildValues(): Record<string, string>;
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
    server: { middlewareMode: true, hmr: false },
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
    await t.test("missing Shelly fields are not replaced by estimates or inverter phases", () => {
      const detailed = fixtureCard(cards.get("jks-detailed")!, 1533);
      for (const key of ["grid_load_total_current", "grid_load_l1_current", "grid_load_l2_current", "grid_load_l3_current", "grid_load_l3_voltage"]) {
        detailed._hass.states[`sensor.test_${key}`]!.state = "unavailable";
      }
      detailed._hass.states["sensor.test_grid_l3_voltage"]!.state = "249";
      const load = detailed._buildData().groups.parallel_grid_load!;
      assert.equal(load.current, null);
      assert.equal(detailed._formatMetricRowValue("current", load), "--");
      assert.equal(detailed._formatMetricRowValue("voltage", load), "230 / 231 / -- V");
    });
  } finally {
    await server.close();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
});

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
