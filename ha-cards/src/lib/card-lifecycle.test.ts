import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

import type { HomeAssistant } from "../types/home-assistant.ts";

// Only the DOM operations used by the card are stubbed. Vite loads the actual
// card, layouts and artwork; this test does not duplicate their geometry.
class TestNode {
  children: TestNode[] = [];
  className = "";
  hidden = false;
  textContent = "";
  innerHTML = "";
  dataset: Record<string, string> = {};
  replacements = 0;
  private svgNodes = new Map<string, TestNode>();
  style = {
    opacity: "", backgroundImage: "", zIndex: "",
    values: new Map<string, string>(),
    getPropertyValue(property: string): string { return this.values.get(property) ?? ""; },
    setProperty(property: string, value: string): void { this.values.set(property, value); }
  };
  get childElementCount(): number { return this.children.length; }
  append(...nodes: TestNode[]): void { this.children.push(...nodes); }
  replaceChildren(...nodes: TestNode[]): void { this.replacements++; this.children = nodes; }
  querySelector(selector: string): TestNode | null {
    const attribute = /^\[(data-stabilizer-node="[^"]+")\]$/.exec(selector)?.[1];
    if (!attribute || !this.innerHTML.includes(attribute)) return null;
    if (!this.svgNodes.has(selector)) this.svgNodes.set(selector, new TestNode());
    return this.svgNodes.get(selector)!;
  }
}

interface CardHarness {
  _root: TestNode;
  _sceneEl: TestNode;
  _styleEl: TestNode;
  _stabilizerArtworkEl?: TestNode;
  _layerNodes: Map<string, TestNode>;
  _overlayNodes: Map<string, TestNode>;
  _isMobile: boolean;
  _hass: HomeAssistant;
  _resolveEntities(): void;
  _render(): void;
  setConfig(config: { type: string; stabilizers: boolean; stabilizers_test_mirror_grid?: boolean }): void;
}

test("detailed card rebuilds only for topology/mode changes and isolates offline artwork", async () => {
  const cards = new Map<string, new () => CardHarness>();
  const globals = {
    HTMLElement: class { attachShadow(): TestNode { return new TestNode(); } },
    document: { createElement: () => new TestNode(), createDocumentFragment: () => new TestNode() },
    customElements: {
      get: (name: string) => cards.get(name),
      define: (name: string, card: new () => CardHarness) => cards.set(name, card)
    },
    window: {}
  };
  const previous = new Map(Object.keys(globals).map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  for (const [key, value] of Object.entries(globals)) Object.defineProperty(globalThis, key, { value, configurable: true });
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("../..", import.meta.url)),
    server: { middlewareMode: true, hmr: false, ws: false },
    appType: "custom"
  });
  try {
    await server.ssrLoadModule("/src/cards/jks-detailed-card.ts");
    const card = new (cards.get("jks-detailed")!)();
    const preservedLayers = ["pv1_offline", "pv2_offline", "pv_offline", "battery_offline", "gen_offline", "ups_offline"];
    const allLayers = [...preservedLayers, "grid_offline", "parallel_offline"];
    const assertLayers = (expected: string[]): void => {
      assert.deepEqual([...card._layerNodes.keys()].sort(), [...expected].sort());
      for (const node of card._layerNodes.values()) {
        assert.match(node.style.backgroundImage, /\.png"\)$/);
        assert.ok(card._sceneEl.children.includes(node));
      }
    };
    const renderUnchanged = (): void => {
      const { _sceneEl: scene, _styleEl: style, _stabilizerArtworkEl: artwork } = card;
      const overlays = [...card._overlayNodes];
      const replacements = card._root.replacements;
      card._render();
      assert.equal(card._root.replacements, replacements, "ordinary updates must reuse structure");
      assert.equal(card._sceneEl, scene);
      assert.equal(card._styleEl, style);
      assert.equal(card._stabilizerArtworkEl, artwork);
      for (const [key, node] of overlays) assert.equal(card._overlayNodes.get(key), node);
    };

    card._render();
    assert.equal(card._root.replacements, 1);
    assertLayers(allLayers);
    assert.equal(Boolean(card._stabilizerArtworkEl), false);
    renderUnchanged();
    const defaultScene = card._sceneEl;

    card.setConfig({ type: "custom:jks-detailed", stabilizers: true });
    assert.equal(card._root.replacements, 2, "same desktop mode must rebuild when stabilizers turn on");
    assert.notEqual(card._sceneEl, defaultScene);
    assertLayers(preservedLayers);
    assert.ok(card._stabilizerArtworkEl);
    assert.ok(card._sceneEl.children.includes(card._stabilizerArtworkEl));
    for (const id of ["grid_input", "grid", "parallel_grid_load"]) {
      assert.match(card._stabilizerArtworkEl.innerHTML, new RegExp(`data-stabilizer-node="${id}"`));
    }
    assert.match(card._stabilizerArtworkEl.innerHTML, /Before stabilizers/);
    assert.match(card._stabilizerArtworkEl.innerHTML, /Stabilized Grid/);
    assert.match(card._stabilizerArtworkEl.innerHTML, />Today<\/text>/);
    assert.ok(card._overlayNodes.has("grid_input:row:energy_today"));
    assert.ok(!card._overlayNodes.has("grid_input:row:energy_total"));
    assert.equal(card._overlayNodes.get("grid_input:row:energy_today")?.textContent, "--");
    renderUnchanged();

    const inputNode = card._stabilizerArtworkEl.querySelector('[data-stabilizer-node="grid_input"]')!;
    assert.equal(inputNode.style.opacity, "0.45", "absent upstream readings must look offline");
    card._hass = { states: { "sensor.meter_grid_input_total_power": { state: "0", attributes: { unit_of_measurement: "W" } } } };
    card._resolveEntities();
    renderUnchanged();
    assert.equal(inputNode.style.opacity, "1", "a measured zero is online, not missing");
    assert.equal(card._overlayNodes.get("grid_input:row:power")?.textContent, "0 W");
    card._hass.states["sensor.meter_grid_input_energy_today"] = { state: "0", attributes: { unit_of_measurement: "kWh" } };
    card._hass.states["sensor.meter_grid_input_returned_energy_today"] = { state: "5", attributes: { unit_of_measurement: "kWh" } };
    card._resolveEntities();
    renderUnchanged();
    assert.equal(card._overlayNodes.get("grid_input:row:energy_today")?.textContent, "-5 kWh");
    card._hass.states["sensor.meter_grid_input_total_power"]!.state = "unavailable";
    card._resolveEntities();
    renderUnchanged();
    assert.equal(inputNode.style.opacity, "0.45");

    const desktopArtwork = card._stabilizerArtworkEl;
    card._isMobile = true;
    card._render();
    assert.equal(card._root.replacements, 3, "desktop to mobile must rebuild");
    assertLayers([]); // Existing mobile layout never uses the desktop PNGs.
    assert.ok(card._stabilizerArtworkEl);
    assert.notEqual(card._stabilizerArtworkEl, desktopArtwork);
    assert.match(card._stabilizerArtworkEl.innerHTML, /data-stabilizer-node="grid_input"/);
    renderUnchanged();
    card._isMobile = false;
    card._render();
    assert.equal(card._root.replacements, 4, "mobile to desktop must rebuild");
    assertLayers(preservedLayers);

    card.setConfig({ type: "custom:jks-detailed", stabilizers: false });
    assert.equal(card._root.replacements, 5, "turning stabilizers off must restore original structure");
    assertLayers(allLayers);
    assert.equal(Boolean(card._stabilizerArtworkEl), false);
    assert.ok(!card._overlayNodes.has("grid_input:row:power"));
    assert.ok(!card._sceneEl.children.some((node) => node.className === "stabilizer-artwork"));
    renderUnchanged();
    card.setConfig({ type: "custom:jks-detailed", stabilizers: true, stabilizers_test_mirror_grid: true });
    assert.equal(card._root.replacements, 6, "test mirror requires an explicitly rebuilt, labelled view");
    assert.match(card._stabilizerArtworkEl!.innerHTML, /TEST · Stabilized Grid mirror/);
    const shell = card._root.children[1]!.children[0]!;
    const banner = shell.children.find((node) => node.className.includes("test-mirror-banner"))!;
    assert.equal(banner.hidden, false, "test warning must be visible even without a title or sensor warnings");
    assert.match(banner.textContent, /TEST \/ MIRROR/);
    renderUnchanged();
    card.setConfig({ type: "custom:jks-detailed", stabilizers: true, stabilizers_test_mirror_grid: false });
    assert.equal(card._root.replacements, 7, "removing test mode restores real upstream labels");
    assert.doesNotMatch(card._stabilizerArtworkEl!.innerHTML, /TEST/);
    renderUnchanged();
  } finally {
    await server.close();
    for (const [key, descriptor] of previous) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  }
});
