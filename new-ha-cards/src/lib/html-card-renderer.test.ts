import assert from "node:assert/strict";
import test from "node:test";

import { createServer } from "vite";

test("disconnect releases layout observers and reconnect restores them once", async () => {
  const server = await createServer({
    configFile: false,
    server: { middlewareMode: true, hmr: false, ws: false }
  });

  const originalWindow = globalThis.window;
  const originalResizeObserver = globalThis.ResizeObserver;
  const originalRequestAnimationFrame = globalThis.requestAnimationFrame;

  const mediaListeners = new Set<EventListenerOrEventListenerObject>();
  const windowResizeListeners = new Set<EventListenerOrEventListenerObject>();

  class ResizeObserverMock {
    static instances: ResizeObserverMock[] = [];

    disconnectCalls = 0;
    observed: unknown[] = [];

    constructor() {
      ResizeObserverMock.instances.push(this);
    }

    observe(target: unknown) {
      this.observed.push(target);
    }

    unobserve(): void {}

    disconnect() {
      this.disconnectCalls += 1;
    }
  }

  const mediaQuery = {
    matches: true,
    addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "change");
      mediaListeners.add(listener);
    },
    removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "change");
      assert.equal(mediaListeners.delete(listener), true);
    }
  } as unknown as MediaQueryList;

  globalThis.window = {
    matchMedia() {
      return mediaQuery;
    },
    addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "resize");
      windowResizeListeners.add(listener);
    },
    removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "resize");
      assert.equal(windowResizeListeners.delete(listener), true);
    }
  } as unknown as Window & typeof globalThis;
  globalThis.ResizeObserver = ResizeObserverMock as unknown as typeof ResizeObserver;
  globalThis.requestAnimationFrame = () => 1;

  try {
    const { HtmlCardRenderer } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    const frame = { querySelector: (selector: string) => (selector === ".summary-shell" ? shell : null) };
    const shell = {};
    const flowNode = {};
    const flowBoard = { querySelectorAll: () => [flowNode] };
    const dashboard = {
      querySelector: (selector: string) => (selector === ".summary-scale-frame" ? frame : null),
      querySelectorAll: (selector: string) => (selector === ".flow-board" ? [flowBoard] : [])
    };
    const renderer = new HtmlCardRenderer({});
    Object.assign(renderer, { dashboardEl: dashboard, renderedVariant: "summary" });

    renderer.connect();

    assert.equal(ResizeObserverMock.instances.length, 2);
    assert.deepEqual(ResizeObserverMock.instances[0]!.observed, [frame, shell]);
    assert.deepEqual(ResizeObserverMock.instances[1]!.observed, [dashboard, flowBoard, flowNode]);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.connect();
    assert.equal(ResizeObserverMock.instances.length, 2);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1]);
    assert.equal(mediaListeners.size, 0);
    assert.equal(windowResizeListeners.size, 0);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1]);

    renderer.connect();
    assert.equal(ResizeObserverMock.instances.length, 4);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1, 1, 1]);
    assert.equal(mediaListeners.size, 0);
    assert.equal(windowResizeListeners.size, 0);
  } finally {
    globalThis.window = originalWindow;
    globalThis.ResizeObserver = originalResizeObserver;
    globalThis.requestAnimationFrame = originalRequestAnimationFrame;
    await server.close();
  }
});

test("stabilizer markup keeps three separate grid nodes and correctly routed connectors", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false } });
  try {
    const { energyCardMarkup, energyCardStyles } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    for (const variant of ["detailed", "summary"]) {
      const legacy = energyCardMarkup(variant);
      assert.equal(legacy, energyCardMarkup(variant, false));
      assert.doesNotMatch(legacy, /data-flow-node="grid-input"|Stabilized Grid/);
      const markup = energyCardMarkup(variant, true);
      for (const node of ["grid-input", "grid", "grid-load"]) {
        assert.equal(markup.match(new RegExp(`data-flow-node="${node}"`, "g"))?.length, 1);
      }
      assert.match(markup, /data-flow-line="grid-input-stabilized" data-flow-from="grid-input" data-flow-to="grid"/);
      assert.match(markup, /data-flow-line="inverter-grid" data-flow-from="inverter" data-flow-to="grid"/);
      assert.match(markup, /data-flow-line="grid-load" data-flow-from="grid" data-flow-to="grid-load"/);
      assert.doesNotMatch(markup, /data-flow-from="grid-input" data-flow-to="(?:inverter|grid-load)"/);
      const upstream = markup.match(/<article[^>]+data-flow-node="grid-input">([\s\S]*?)<\/article>/)?.[1] ?? "";
      assert.match(upstream, /grid_input\.phases/);
      assert.equal(upstream.match(/data-field="grid_input\.energy_today"/g)?.length, 1);
      assert.match(upstream, /<span>Today<\/span>/);
      assert.doesNotMatch(upstream, /grid_input\.(?:energy_balance|energy_total|returned_energy_total)|<span>Total<\/span>|Імпорт · всього|Експорт · всього/);
      assert.doesNotMatch(upstream, /data-field="(?:daily\.|system\.import|system\.export)/);
      assert.match(markup, /Stabilized Grid/);
      assert.match(markup, /Parallel Grid Load/);
      assert.match(markup, /після стаб\./);
    }
    const css = energyCardStyles();
    assert.match(css, /"ups pv1 pv2 grid-input"\s+"battery inverter inverter grid"\s+"battery generator generator grid-load"/);
    assert.match(css, /"grid-input" "grid" "grid-load" "pv1"/);
  } finally { await server.close(); }
});

test("stabilizer side cards give long phase readings a full wrapping row without changing default rows", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false } });
  try {
    const { energyCardStyles, HtmlCardRenderer } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    const css = energyCardStyles();
    const scopedSelector = ".energy-dashboard--stabilizers .flow-board--detailed :is(.node--ups, .node--grid-input, .node--grid, .node--grid-load) :is(.metric-line, .phase-row)";
    const rule = (selector: string): string => {
      const start = css.indexOf(`${selector} {`);
      assert.notEqual(start, -1, selector);
      return css.slice(start, css.indexOf("}", start) + 1);
    };
    assert.match(rule(scopedSelector), /grid-template-columns: minmax\(0, 1fr\);/);
    assert.match(rule(scopedSelector), /padding-block: 6px;/);
    const valueRule = rule(`${scopedSelector} strong`);
    assert.match(valueRule, /white-space: normal;/);
    assert.match(valueRule, /overflow-wrap: anywhere;/);
    assert.match(valueRule, /overflow: visible;/);
    assert.match(valueRule, /text-overflow: clip;/);
    assert.doesNotMatch(valueRule, /ellipsis|nowrap/);
    assert.match(css, /\.metric-line,\s+\.phase-row\s*\{[^}]*grid-template-columns: auto minmax\(0, 1fr\);/);

    // Keep the complete text for CSS wrapping; the renderer must not replace
    // a long three-phase value with an ellipsis before it reaches the DOM.
    const phaseReading = "1.86 kW / 1.88 kW / 1.88 kW";
    const valueNode = { dataset: { field: "ups.phase_power" }, textContent: "" };
    const renderer = new HtmlCardRenderer({});
    Object.assign(renderer, { dashboardEl: { querySelectorAll: () => [valueNode] } });
    renderer.syncFields({ "ups.phase_power": phaseReading });
    assert.equal(valueNode.textContent, phaseReading);
  } finally { await server.close(); }
});

test("test mirror has a persistent banner and upstream label only in explicit stabilizer mode", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false } });
  try {
    const { energyCardMarkup, energyCardStyles } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    for (const variant of ["detailed", "summary"]) {
      assert.equal(energyCardMarkup(variant, false, true), energyCardMarkup(variant, false, false));
      const real = energyCardMarkup(variant, true);
      assert.doesNotMatch(real, /TEST \/ MIRROR|test-mirror-banner/);
      const mirrored = energyCardMarkup(variant, true, true);
      assert.match(mirrored, /^<div class="test-mirror-banner" role="note">/);
      assert.match(mirrored, /<strong>TEST \/ MIRROR<\/strong>/);
      assert.match(mirrored, /Це не вимірювання до стабілізаторів/);
      const banner = mirrored.slice(0, mirrored.indexOf("</div>") + 6);
      assert.doesNotMatch(banner, /hidden|helper--warn/, "banner must not depend on missing-sensor warnings");
      const upstream = mirrored.match(/<article[^>]+data-flow-node="grid-input">([\s\S]*?)<\/article>/)?.[1] ?? "";
      assert.match(upstream, /Grid · TEST \/ MIRROR/);
      assert.match(upstream, /Копія Stabilized Grid · не вимірювання Shelly/);
      assert.doesNotMatch(upstream, /До стабілізаторів · Shelly/);
    }
    assert.match(energyCardStyles(), /\.energy-dashboard--stabilizers \.test-mirror-banner\s*\{[^}]*border: 2px solid #ffbf2f;/);
  } finally { await server.close(); }
});

test("changing stabilizers mode rebuilds structure but ordinary updates reuse it", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false } });
  const originalDocument = globalThis.document;
  const originalRAF = globalThis.requestAnimationFrame;
  class ElementMock {
    className = "";
    textContent = "";
    innerHTML = "";
    hidden = false;
    childElementCount = 0;
    classList = { toggle() {} };
    append() {}
    replaceChildren() {}
    querySelectorAll() { return []; }
  }
  globalThis.document = { createElement: () => new ElementMock() } as unknown as Document;
  globalThis.requestAnimationFrame = () => 1;
  try {
    const { HtmlCardRenderer } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    let rebuilds = 0;
    const renderer = new HtmlCardRenderer({ replaceChildren: () => { rebuilds += 1; }, append() {} });
    const state = { title: "", values: {}, flags: {} };
    renderer.render("detailed", state);
    renderer.render("detailed", { ...state, stabilizers: false });
    assert.equal(rebuilds, 1);
    renderer.render("detailed", { ...state, stabilizers: true });
    assert.equal(rebuilds, 2);
    assert.match(renderer.dashboardEl.innerHTML, /data-flow-node="grid-input"/);
    renderer.render("detailed", { ...state, stabilizers: true });
    assert.equal(rebuilds, 2);
    renderer.render("detailed", { ...state, stabilizers: true, stabilizersTestMirrorGrid: true });
    assert.equal(rebuilds, 3);
    assert.match(renderer.dashboardEl.innerHTML, /test-mirror-banner/);
    renderer.render("detailed", { ...state, stabilizers: true, stabilizersTestMirrorGrid: true });
    assert.equal(rebuilds, 3, "ordinary mirror updates retain their persistent banner");
    renderer.render("detailed", { ...state, stabilizers: true, stabilizersTestMirrorGrid: false });
    assert.equal(rebuilds, 4);
    assert.doesNotMatch(renderer.dashboardEl.innerHTML, /test-mirror-banner/);
    renderer.render("detailed", state);
    assert.equal(rebuilds, 5);
    assert.doesNotMatch(renderer.dashboardEl.innerHTML, /data-flow-node="grid-input"/);
    renderer.render("detailed", { ...state, stabilizersTestMirrorGrid: true });
    assert.equal(rebuilds, 5, "mirror option alone does not activate a new layout or test mode");
  } finally {
    globalThis.document = originalDocument;
    globalThis.requestAnimationFrame = originalRAF;
    await server.close();
  }
});

test("stabilizer connector follows measured node positions after scaling and resize", async () => {
  const server = await createServer({ configFile: false, server: { middlewareMode: true, hmr: false, ws: false } });
  try {
    const { HtmlCardRenderer } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    let lowerTop = 230;
    const upper = { getBoundingClientRect: () => ({ left: 410, top: 80, width: 100, height: 60 }) };
    const lower = { getBoundingClientRect: () => ({ left: 410, top: lowerTop, width: 100, height: 60 }) };
    const board = {
      offsetWidth: 1000,
      getBoundingClientRect: () => ({ left: 10, top: 20, width: 500 }),
      querySelector: (selector: string) => selector === '[data-flow-node="grid-input"]' ? upper : lower
    };
    const line = { dataset: { flowFrom: "grid-input", flowTo: "grid" }, closest: () => board, style: {} as Record<string, string> };
    const renderer = new HtmlCardRenderer({});
    renderer.dashboardEl = { querySelectorAll: () => [line] };
    renderer.positionFlowLines();
    assert.equal(line.style.left, "900px");
    assert.equal(line.style.top, "180px");
    assert.equal(line.style.width, "300px");
    assert.equal(line.style.transform, "translateY(-50%) rotate(90deg)");
    lowerTop = 280;
    renderer.positionFlowLines();
    assert.equal(line.style.width, "400px");
  } finally { await server.close(); }
});
