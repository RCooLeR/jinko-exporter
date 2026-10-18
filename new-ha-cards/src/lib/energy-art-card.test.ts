import assert from "node:assert/strict";
import test from "node:test";
import { createServer } from "vite";
import type { HtmlRenderState } from "./html-card-renderer.ts";

test("mirror configuration requires a boolean and forwards display-only mode only with stabilizers", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false }, appType: "custom" });
  const originalHTMLElement = globalThis.HTMLElement;
  globalThis.HTMLElement = class {
    attachShadow() { return {}; }
  } as unknown as typeof HTMLElement;
  try {
    const { EnergyArtCard } = await server.ssrLoadModule("/src/cards/energy-art-card.ts");
    const snapshots: HtmlRenderState[] = [];
    const card = new EnergyArtCard();
    Object.assign(card, {
      variant: "detailed",
      defaultConfig: { type: "custom:jks-detailed", stabilizers: false, stabilizers_test_mirror_grid: false },
      renderer: { render(_variant: string, state: HtmlRenderState) { snapshots.push(state); } }
    });
    assert.throws(() => card.setConfig({ type: "custom:jks-detailed", stabilizers_test_mirror_grid: "true" }), /must be a boolean/);
    card.setConfig({ type: "custom:jks-detailed", stabilizers: true });
    assert.equal(snapshots.at(-1)?.stabilizersTestMirrorGrid, false);
    assert.equal(snapshots.at(-1)?.flags.test_mirror_grid, false);

    const hass = Object.freeze({ states: Object.freeze({
      "sensor.device_grid_ct_t_e": Object.freeze({ state: "-175", attributes: Object.freeze({ unit_of_measurement: "W" }) }),
      "sensor.device_grid_input_total_power": Object.freeze({ state: "9000", attributes: Object.freeze({ unit_of_measurement: "W" }) })
    }) });
    card.hass = hass;
    card.setConfig({ type: "custom:jks-detailed", stabilizers: true, stabilizers_test_mirror_grid: true });
    assert.equal(snapshots.at(-1)?.stabilizersTestMirrorGrid, true);
    assert.equal(snapshots.at(-1)?.flags.test_mirror_grid, true);
    assert.equal(snapshots.at(-1)?.values["grid_input.power.value"], "-175");
    assert.equal(hass.states["sensor.device_grid_input_total_power"].state, "9000");
    assert.equal(card.resolved.grid_input_total_power, "sensor.device_grid_input_total_power", "do not rewrite entity bindings");

    card.setConfig({ type: "custom:jks-detailed", stabilizers_test_mirror_grid: true });
    assert.equal(snapshots.at(-1)?.stabilizersTestMirrorGrid, false);
    assert.equal(snapshots.at(-1)?.flags.test_mirror_grid, false);
    assert.equal(snapshots.at(-1)?.values["grid_input.power.value"], "9");
    card.setConfig({ type: "custom:jks-detailed", stabilizers: true, stabilizers_test_mirror_grid: false });
    assert.equal(snapshots.at(-1)?.values["grid_input.power.value"], "9");
  } finally {
    globalThis.HTMLElement = originalHTMLElement;
    await server.close();
  }
});
