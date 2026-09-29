import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

import { DETAILED_CARD_POSITIONS, type CardElementPositionModel, type PositionBoxModel } from "./position-models.ts";

type Box = [number, number, number, number];

interface LayoutElement {
  id: string;
  type: string;
  bbox: Box;
  value_box?: Box;
  rows?: Array<{ id: string; row_box: Box; value_box: Box }>;
  soc_value_box?: Box;
  temp_value_box?: Box;
}

interface Layout {
  canvas: { width: number; height: number; background_image: string };
  elements: LayoutElement[];
}

interface CardHarness {
  _isMobile: boolean;
  _layout(): Layout;
  _elementPosition(id: string): CardElementPositionModel;
  _applyTextBox(node: HTMLDivElement, layout: Layout, box: Box, text: string, className: string, options: PositionBoxModel, visible: boolean): void;
  _styles(layout: Layout, background: string): string;
}

// These are measured against assets/main/desktop.png, not copied from the spec:
// moving both a row and its value out of the painted panel must fail the test.
const DESKTOP_FIRST_ROW_PANELS: Record<string, Box> = {
  ups_load: [425, 114, 278, 40],
  pv1: [799, 111, 209, 41],
  pv2: [1067, 111, 208, 41],
  grid: [1332, 111, 273, 41],
  battery: [425, 501, 224, 39],
  inverter: [760, 427, 263, 41],
  generator: [858, 734, 319, 38],
  parallel_grid_load: [1363, 663, 242, 41]
};

const DESKTOP_SUMMARY_PANELS: Record<string, Box> = {
  daily_production: [31, 163, 320, 111],
  daily_generator: [31, 286, 320, 111],
  daily_import: [31, 409, 320, 108],
  daily_export: [31, 530, 320, 111],
  daily_consumption: [31, 653, 320, 110],
  daily_costs: [31, 774, 320, 116]
};

test("detailed card overlays stay aligned to the desktop artwork", async (t) => {
  const cards = new Map<string, new () => CardHarness>();
  const globals: Record<string, unknown> = {
    HTMLElement: class { attachShadow(): object { return {}; } },
    customElements: {
      get: (name: string) => cards.get(name),
      define: (name: string, card: new () => CardHarness) => cards.set(name, card)
    },
    window: {}
  };
  const previous = new Map(Object.keys(globals).map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  for (const [key, value] of Object.entries(globals)) {
    Object.defineProperty(globalThis, key, { value, configurable: true });
  }
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("../..", import.meta.url)),
    server: { middlewareMode: true, hmr: false, ws: false },
    appType: "custom"
  });
  try {
    await server.ssrLoadModule("/src/cards/jks-detailed-card.ts");
    const Card = cards.get("jks-detailed")!;
    const card = new Card();
    const layout = card._layout();

    await t.test("desktop geometry has one source of truth", () => {
      const geometryKeys: Array<keyof PositionBoxModel> = ["leftPercent", "topPercent", "widthPercent", "heightPercent", "xOffsetPx", "yOffsetPx"];
      for (const [id, positions] of Object.entries(DETAILED_CARD_POSITIONS.desktop)) {
        const boxes = [positions.value, ...Object.values(positions.rows ?? {}), ...Object.values(positions.extras ?? {})];
        for (const options of boxes.filter((box) => box !== undefined)) {
          for (const key of geometryKeys) {
            assert.equal(options[key], undefined, `${id}: ${key} duplicates the measured JSON geometry`);
          }
        }
      }
    });

    await t.test("metric values fit inside their painted row panels", () => {
      assert.equal(layout.canvas.width, 1672);
      assert.equal(layout.canvas.height, 941);
      for (const [id, expectedPanel] of Object.entries(DESKTOP_FIRST_ROW_PANELS)) {
        const element = layout.elements.find((entry) => entry.id === id)!;
        const voltage = element.rows!.find((row) => row.id === "voltage")!;
        voltage.row_box.forEach((coordinate, index) => {
          assert.ok(Math.abs(coordinate - expectedPanel[index]!) <= 2, `${id}: row panel coordinate ${index} does not match the artwork`);
        });
        for (const row of element.rows!.filter((entry) => ["voltage", "current", "power"].includes(entry.id))) {
          const node = textNode();
          card._applyTextBox(node, layout, row.value_box, "--", "value value--metric value--primary-metric", card._elementPosition(id).rows?.[row.id] ?? {}, true);
          assertInside(effectiveBox(node, layout), row.row_box, `${id}:${row.id}`);
          assert.equal(node.style.getPropertyValue("text-align"), "right");
          assert.equal(node.style.getPropertyValue("justify-content"), "flex-end");
        }
      }
    });

    await t.test("daily summaries stay below their labels and inside their panels", () => {
      const summaries = layout.elements.filter((element) => element.type === "summary_card");
      assert.equal(summaries.length, Object.keys(DESKTOP_SUMMARY_PANELS).length);
      for (const element of summaries) {
        const panel = DESKTOP_SUMMARY_PANELS[element.id]!;
        assert.deepEqual(element.bbox, panel, `${element.id}: summary panel must match the artwork`);
        for (const text of ["--", "123.45 kWh", "-1234,56₴"]) {
          const node = textNode();
          card._applyTextBox(node, layout, element.value_box!, text, "value value--summary", card._elementPosition(element.id).value ?? {}, true);
          const box = effectiveBox(node, layout);
          assertInside(box, panel, element.id);
          assert.ok(box[0] >= panel[0] + 90, `${element.id}: summary overlaps the painted icon`);
          assert.ok(box[1] >= panel[1] + 48, `${element.id}: summary overlaps the painted label`);
          assert.equal(node.style.getPropertyValue("text-align"), "center");
          assert.equal(node.style.getPropertyValue("justify-content"), "center");
        }
      }
    });

    await t.test("SOC and temperature remain centered in their gauges", () => {
      const gauges: Array<{ id: string; extra: "soc" | "temp"; box: "soc_value_box" | "temp_value_box"; centerX: number; panel: Box }> = [
        { id: "battery", extra: "soc", box: "soc_value_box", centerX: 534, panel: [425, 624, 224, 157] },
        { id: "inverter", extra: "temp", box: "temp_value_box", centerX: 1142, panel: [1038, 427, 208, 135] }
      ];
      for (const gauge of gauges) {
        const element = layout.elements.find((entry) => entry.id === gauge.id)!;
        const node = textNode();
        card._applyTextBox(node, layout, element[gauge.box]!, gauge.extra === "soc" ? "100%" : "25 C", `value value--${gauge.extra}`, card._elementPosition(gauge.id).extras?.[gauge.extra] ?? {}, true);
        const box = effectiveBox(node, layout);
        assertInside(box, gauge.panel, gauge.extra);
        assert.ok(Math.abs(box[0] + box[2] / 2 - gauge.centerX) <= 8, `${gauge.extra}: value box is not centered in the gauge`);
        assert.equal(node.style.getPropertyValue("text-align"), "center", `${gauge.extra}: inline alignment must not override the centered gauge style`);
        assert.equal(node.style.getPropertyValue("justify-content"), "center");
      }
    });

    await t.test("desktop phase fonts scale with the artwork and keep a long-reading fit budget", () => {
      // This is a deterministic width budget, not a replacement for browser
      // visual QA: individual glyph widths still depend on the HA font.
      const phaseReadings = [
        "123 / 123 / 123.4 kW",
        "230.1 / 230.2 / 230.3 V",
        "0.1234 / 0.5678 / 0.9012 A",
        "-123.45 / -678.90 / -123.45 kW"
      ];
      for (const text of phaseReadings) assert.ok(text.length >= 19 && text.length <= 30);
      for (const element of layout.elements.filter((entry) => entry.rows)) {
        for (const row of element.rows!.filter((entry) => ["voltage", "current", "power"].includes(entry.id))) {
          const options = card._elementPosition(element.id).rows?.[row.id] ?? {};
          const shortNode = textNode();
          card._applyTextBox(shortNode, layout, row.value_box, "1 W", "value value--metric value--primary-metric", options, true);
          const shortFont = Number.parseFloat(shortNode.style.getPropertyValue("font-size"));
          for (const text of phaseReadings) {
            const node = textNode();
            card._applyTextBox(node, layout, row.value_box, text, "value value--metric value--primary-metric", options, true);
            const fontCSS = node.style.getPropertyValue("font-size");
            assert.match(fontCSS, /^\d+(?:\.\d+)?cqw$/, `${element.id}:${row.id}: desktop font must scale without a fixed pixel floor`);
            const fontCqw = Number.parseFloat(fontCSS);
            assert.ok(fontCqw > 0 && fontCqw < shortFont, `${element.id}:${row.id}: long phase reading must shrink to fit`);
            const [, , width, height] = effectiveBox(node, layout);
            for (const canvasWidth of [961, 1200, 1672, 2560]) {
              const scale = canvasWidth / layout.canvas.width;
              const fontPixels = fontCqw * canvasWidth / 100;
              assert.ok(fontPixels * text.length * 0.6 <= width * scale * 0.945 + 0.02, `${element.id}:${row.id}: long reading exceeds the width budget at ${canvasWidth}px`);
              assert.ok(fontPixels <= height * scale * 0.9 + 0.01, `${element.id}:${row.id}: font exceeds row height`);
            }
          }
        }
      }
    });

    await t.test("mobile metric fonts retain their separate sizing", () => {
      card._isMobile = true;
      const mobileLayout = card._layout();
      const node = textNode();
      card._applyTextBox(node, mobileLayout, [116, 870, 116, 43], "230/231/232 V", "value value--metric value--primary-metric", {}, true);
      assert.equal(node.style.getPropertyValue("font-size"), "6.5px");
      card._isMobile = false;
    });

    await t.test("hidden values and offline image layers cannot be made visible by card CSS", () => {
      for (const mobile of [false, true]) {
        card._isMobile = mobile;
        const activeLayout = card._layout();
        const css = card._styles(activeLayout, "test.png");
        const hiddenRules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
          .filter((match) => /display\s*:\s*none\s*!important\s*;?/.test(match[2]!))
          .flatMap((match) => match[1]!.split(",").map((selector) => selector.trim()));
        for (const className of ["value", "scene-layer"]) {
          assert.ok(hiddenRules.some((selector) => selector === "[hidden]" || selector === `.${className}[hidden]`), `${mobile ? "mobile" : "desktop"}: ${className}[hidden] needs a display:none !important rule`);
        }
        const node = textNode();
        card._applyTextBox(node, activeLayout, [0, 0, 100, 40], "", "value value--metric", {}, false);
        assert.equal(node.hidden, true);
        card._applyTextBox(node, activeLayout, [0, 0, 100, 40], "1 W", "value value--metric", {}, true);
        assert.equal(node.hidden, false);
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

const textNode = (): HTMLDivElement => {
  const properties = new Map<string, string>();
  return {
    className: "",
    textContent: "",
    hidden: false,
    style: {
      getPropertyValue: (property: string) => properties.get(property) ?? "",
      setProperty: (property: string, value: string) => { properties.set(property, value); }
    }
  } as unknown as HTMLDivElement;
};

const effectiveBox = (node: HTMLDivElement, layout: Layout): Box => [
  Number.parseFloat(node.style.getPropertyValue("left")) * layout.canvas.width / 100,
  Number.parseFloat(node.style.getPropertyValue("top")) * layout.canvas.height / 100,
  Number.parseFloat(node.style.getPropertyValue("width")) * layout.canvas.width / 100,
  Number.parseFloat(node.style.getPropertyValue("height")) * layout.canvas.height / 100
];

const assertInside = ([x, y, width, height]: Box, [outerX, outerY, outerWidth, outerHeight]: Box, label: string): void => {
  const tolerance = 0.01;
  assert.ok(width > 0 && height > 0, `${label}: empty value rectangle`);
  assert.ok(x >= outerX - tolerance && y >= outerY - tolerance, `${label}: value begins outside its panel`);
  assert.ok(x + width <= outerX + outerWidth + tolerance, `${label}: value overflows the panel on the right`);
  assert.ok(y + height <= outerY + outerHeight + tolerance, `${label}: value overflows the panel at the bottom`);
};
