import desktopLayout from "../../assets/overview/desktop_layout_spec.json";
import mobileLayout from "../../assets/overview/mobile_layout_spec.json";
import { setClassNameIfChanged, setHiddenIfChanged, setStyleIfChanged, setTextContentIfChanged } from "../lib/dom";
import { clamp, first, formatEnergy, formatNumber, formatPercent, formatPower, formatTemperature, sum } from "../lib/format";
import { ENTITY_KEYS, resolveEntities, valueFor, type EntityKey, type EntityOverrides, type ResolvedEntityMap } from "../lib/entity-model";
import { MINI_CARD_POSITIONS, type PositionBoxModel, type PositionMode } from "../lib/position-models";
import type { HomeAssistant, LovelaceCardConfig } from "../types/home-assistant";

const CARD_TAG = "jks-mini";
const DEFAULT_CONFIG = {
  title: "JKS Mini",
  show_entity_map: false,
  entities: {} as EntityOverrides
};

interface MiniCardConfig extends LovelaceCardConfig {
  title?: string;
  show_entity_map?: boolean;
  static?: boolean;
  entities?: EntityOverrides;
}

interface LayoutElement {
  id: string;
  type: string;
  bbox: [number, number, number, number];
  value_box?: [number, number, number, number] | null;
  power_value_box?: [number, number, number, number];
  temp_value_box?: [number, number, number, number];
}

interface LayoutSpec {
  image: {
    width: number;
    height: number;
  };
  elements: {
    frame: {
      bbox: [number, number, number, number];
    };
    summary_cards: LayoutElement[];
    diagram_nodes: LayoutElement[];
  };
}

type MiniLayerId = "pv_offline" | "grid_offline" | "battery_offline" | "gen_offline" | "load_offline";

interface MiniLayerDefinition {
  id: MiniLayerId;
  src: string;
  zIndex: number;
}

const DESKTOP_LAYOUT = desktopLayout as LayoutSpec;
const MOBILE_LAYOUT = mobileLayout as LayoutSpec;
const MOBILE_BREAKPOINT = 420;
const MINI_CARD_FONT_REDUCTION = 0.64;
const MINI_CARD_MOBILE_REFERENCE_WIDTH = 360;
const MINI_CARD_DESKTOP_REFERENCE_WIDTH = 520;
const MINI_LAYER_POWER_EPSILON = 1;
const moduleBaseUrl = new URL(/* @vite-ignore */ ".", import.meta.url).toString();
const assetUrl = (file: string): string =>
  import.meta.env.DEV ? `/assets/overview/${file}` : `${moduleBaseUrl}assets/overview/${file}`;
const DESKTOP_LAYERS: MiniLayerDefinition[] = [
  { id: "pv_offline", src: assetUrl("layers/mini-desktop-pv-offline.png"), zIndex: 10 },
  { id: "grid_offline", src: assetUrl("layers/mini-desktop-grid-offline.png"), zIndex: 10 },
  { id: "battery_offline", src: assetUrl("layers/mini-desktop-battery-offline.png"), zIndex: 10 },
  { id: "gen_offline", src: assetUrl("layers/mini-desktop-gen-offline.png"), zIndex: 10 },
  { id: "load_offline", src: assetUrl("layers/mini-desktop-load-offline.png"), zIndex: 10 }
];

class JksMiniCard extends HTMLElement {
  private _hass?: HomeAssistant;
  private _config: MiniCardConfig = { type: `custom:${CARD_TAG}`, ...DEFAULT_CONFIG };
  private _resolved: ResolvedEntityMap = {};
  private _attemptedEntityKeys = new Set<EntityKey>();
  private _root: ShadowRoot;
  private _resizeObserver?: ResizeObserver;
  private _isMobile = false;
  private _hasRendered = false;
  private _renderedMode?: PositionMode;
  private _styleEl?: HTMLStyleElement;
  private _titleEl?: HTMLDivElement;
  private _sceneEl?: HTMLDivElement;
  private _entityMapEl?: HTMLDivElement;
  private _entityMapSignature = "";
  private _layerNodes = new Map<MiniLayerId, HTMLDivElement>();
  private _overlayNodes = new Map<string, HTMLDivElement>();

  constructor() {
    super();
    this._root = this.attachShadow({ mode: "open" });
  }

  setConfig(config: MiniCardConfig): void {
    if (!config || typeof config !== "object") {
      throw new Error("Card configuration is required");
    }

    this._config = {
      ...DEFAULT_CONFIG,
      ...config,
      entities: { ...DEFAULT_CONFIG.entities, ...(config.entities ?? {}) }
    };

    this._resolved = {};
    this._attemptedEntityKeys.clear();
    this._hasRendered = false;
    this._resolveEntities(true);
    this._render();
  }

  set hass(hass: HomeAssistant) {
    if (this._config.static && this._hasRendered) {
      this._hass = hass;
      return;
    }

    this._hass = hass;
    this._resolveEntities();
    this._ensureResizeObserver();
    this._render();
  }

  connectedCallback(): void {
    this._ensureResizeObserver();
    this._render();
  }

  disconnectedCallback(): void {
    this._resizeObserver?.disconnect();
    this._resizeObserver = undefined;
  }

  getCardSize(): number {
    return this._isMobile ? 7 : 5;
  }

  static getStubConfig(): MiniCardConfig {
    return { type: `custom:${CARD_TAG}` };
  }

  private _ensureResizeObserver(): void {
    if (this._resizeObserver) return;

    this._resizeObserver = new ResizeObserver((entries) => {
      if (this._config.static && this._hasRendered) {
        return;
      }

      const width = entries[0]?.contentRect.width ?? this.clientWidth ?? DESKTOP_LAYOUT.image.width;
      const nextMode = width <= MOBILE_BREAKPOINT;
      if (nextMode !== this._isMobile) {
        this._isMobile = nextMode;
        this._render();
      }
    });

    this._resizeObserver.observe(this);
    const width = this.clientWidth || DESKTOP_LAYOUT.image.width;
    this._isMobile = width <= MOBILE_BREAKPOINT;
  }

  private _layout(): LayoutSpec {
    return this._isMobile ? MOBILE_LAYOUT : DESKTOP_LAYOUT;
  }

  private _positionMode(): PositionMode {
    return this._isMobile ? "mobile" : "desktop";
  }

  private _resolveEntities(force = false): void {
    if (!this._hass) return;
    if (force) {
      this._attemptedEntityKeys.clear();
    }

    const keys = force ? ENTITY_KEYS : ENTITY_KEYS.filter((key) => !this._attemptedEntityKeys.has(key));
    if (!keys.length) return;

    this._resolved = {
      ...this._resolved,
      ...resolveEntities(this._hass, this._config.entities, keys)
    };

    for (const key of keys) {
      this._attemptedEntityKeys.add(key);
    }
  }

  private _value(key: EntityKey): number | null {
    return valueFor(this._hass, this._resolved, key);
  }

  private _render(): void {
    const layout = this._layout();
    const mode = this._positionMode();
    const background = this._isMobile ? assetUrl("mobile.png") : assetUrl("desktop.png");
    const values = this._buildValues();

    this._ensureStructure(layout, background, mode);
    this._syncTitle();
    this._syncEntityMap();
    this._syncLayers();
    this._syncOverlays(layout, values);

    this._hasRendered = true;
  }

  private _ensureStructure(layout: LayoutSpec, background: string, mode: PositionMode): void {
    if (this._renderedMode === mode && this._styleEl && this._titleEl && this._sceneEl && this._entityMapEl) {
      return;
    }

    this._root.replaceChildren();
    this._layerNodes.clear();
    this._overlayNodes.clear();
    this._entityMapSignature = "";

    this._styleEl = document.createElement("style");
    this._styleEl.textContent = this._styles(layout, background);

    const card = document.createElement("ha-card");
    const shell = document.createElement("div");
    shell.className = "card-shell";

    this._titleEl = document.createElement("div");
    this._titleEl.className = "card-title";

    this._sceneEl = document.createElement("div");
    this._sceneEl.className = "scene";
    this._createLayerNodes(mode);

    this._entityMapEl = document.createElement("div");
    this._entityMapEl.className = "entity-map";
    this._entityMapEl.hidden = true;

    shell.append(this._titleEl, this._sceneEl, this._entityMapEl);
    card.append(shell);
    this._root.append(this._styleEl, card);
    this._renderedMode = mode;
  }

  private _createLayerNodes(mode: PositionMode): void {
    if (!this._sceneEl || mode !== "desktop") {
      return;
    }

    for (const layer of DESKTOP_LAYERS) {
      const node = document.createElement("div");
      node.className = "scene-layer";
      node.hidden = true;
      node.style.backgroundImage = `url("${layer.src}")`;
      node.style.zIndex = String(layer.zIndex);
      this._layerNodes.set(layer.id, node);
      this._sceneEl.append(node);
    }
  }

  private _syncTitle(): void {
    if (!this._titleEl) return;
    const title = this._config.title?.trim() ?? "";
    setTextContentIfChanged(this._titleEl, title);
    setHiddenIfChanged(this._titleEl, !title);
  }

  private _syncEntityMap(): void {
    if (!this._entityMapEl) return;
    if (!this._config.show_entity_map) {
      this._entityMapSignature = "";
      setHiddenIfChanged(this._entityMapEl, true);
      if (this._entityMapEl.childElementCount > 0) {
        this._entityMapEl.replaceChildren();
      }
      return;
    }

    const rows = Object.entries(this._resolved);
    const signature =
      rows.length === 0 ? "info:No entities resolved yet." : rows.map(([key, entityId]) => `${key}:${entityId ?? "--"}`).join("|");
    if (signature === this._entityMapSignature) {
      setHiddenIfChanged(this._entityMapEl, false);
      return;
    }

    this._entityMapSignature = signature;
    const fragment = document.createDocumentFragment();

    if (!rows.length) {
      fragment.append(this._createEntityMapRow("info", "No entities resolved yet."));
    } else {
      for (const [key, entityId] of rows) {
        fragment.append(this._createEntityMapRow(key, entityId ?? "--"));
      }
    }

    setHiddenIfChanged(this._entityMapEl, false);
    this._entityMapEl.replaceChildren(fragment);
  }

  private _createEntityMapRow(label: string, value: string): HTMLDivElement {
    const row = document.createElement("div");
    row.className = "map-row";

    const keyEl = document.createElement("span");
    keyEl.textContent = label;

    const valueEl = document.createElement("span");
    valueEl.textContent = value;

    row.append(keyEl, valueEl);
    return row;
  }

  private _syncLayers(): void {
    const pvTotalPower = first(this._value("pv_total_power"), sum([this._value("pv1_power"), this._value("pv2_power")]));
    const gridTotalPower = this._value("grid_total_power");
    const homeTotalPower = this._value("home_total_power");
    const batteryPower = this._value("battery_power");
    const generatorTotalPower = first(
      this._value("generator_total_power"),
      sum([this._value("generator_l1_power"), this._value("generator_l2_power"), this._value("generator_l3_power")])
    );

    const visibility: Record<MiniLayerId, boolean> = {
      pv_offline: !this._isMeaningfulPower(pvTotalPower),
      grid_offline: !this._isMeaningfulPower(gridTotalPower),
      battery_offline: !this._isMeaningfulPower(batteryPower),
      gen_offline: !this._isMeaningfulPower(generatorTotalPower),
      load_offline: !this._isMeaningfulPower(homeTotalPower)
    };

    for (const layer of DESKTOP_LAYERS) {
      const node = this._layerNodes.get(layer.id);
      if (!node) {
        continue;
      }
      setHiddenIfChanged(node, !visibility[layer.id]);
    }
  }

  private _syncOverlays(layout: LayoutSpec, values: Record<string, string>): void {
    const activeKeys = new Set<string>();
    const positions = MINI_CARD_POSITIONS[this._positionMode()];

    for (const element of layout.elements.summary_cards) {
      if (!element.value_box) continue;
      this._updateOverlayNode(
        activeKeys,
        `summary:${element.id}`,
        layout,
        element.value_box,
        values[element.id] ?? "--",
        "value",
        positions[element.id]?.value
      );
    }

    for (const element of layout.elements.diagram_nodes) {
      const primaryBox = element.power_value_box ?? element.value_box;
      const primaryPosition = positions[element.id]?.value;
      if (primaryBox && primaryPosition) {
        this._updateOverlayNode(
          activeKeys,
          `node:${element.id}:primary`,
          layout,
          primaryBox,
          values[element.id] ?? "--",
          "value",
          primaryPosition
        );
      }

      if (element.temp_value_box && element.id === "inverter_node") {
        const tempPosition = positions[element.id]?.extras?.temp;
        if (tempPosition) {
          this._updateOverlayNode(
            activeKeys,
            "node:inverter_node:temp",
            layout,
            element.temp_value_box,
            values.inverter_temp ?? "--",
            "value value--secondary",
            tempPosition
          );
        }
      }
    }

    for (const [key, node] of this._overlayNodes) {
      setHiddenIfChanged(node, !activeKeys.has(key));
    }
  }

  private _updateOverlayNode(
    activeKeys: Set<string>,
    key: string,
    layout: LayoutSpec,
    box: [number, number, number, number],
    text: string,
    className = "value",
    options: PositionBoxModel = {}
  ): void {
    const node = this._ensureOverlayNode(key, className);
    activeKeys.add(key);
    this._applyTextBox(node, layout, box, text, className, options);
  }

  private _ensureOverlayNode(key: string, className: string): HTMLDivElement {
    const existing = this._overlayNodes.get(key);
    if (existing) {
      setClassNameIfChanged(existing, className);
      return existing;
    }

    const node = document.createElement("div");
    node.className = className;
    this._overlayNodes.set(key, node);
    this._sceneEl?.append(node);
    return node;
  }

  private _buildValues(): Record<string, string> {
    if (this._config.static) {
      return {
        production_card: "24.8 kWh",
        import_card: "8.1 kWh",
        export_card: "3.4 kWh",
        consumption_card: "29.5 kWh",
        costs_card: this._formatCosts(8.1, 3.4),
        battery_soc_card: "68%",
        combined_pv: "4.0 kW",
        grid_node: "+1.3 kW",
        inverter_node: "1.9 kW",
        inverter_temp: "46 C",
        combined_load: "2.9 kW",
        battery_node: "-1.3 kW",
        generator_node: "495 W"
      };
    }

    const gridTotalPower = this._value("grid_total_power");
    const pvTotalPower = first(this._value("pv_total_power"), sum([this._value("pv1_power"), this._value("pv2_power")]));
    const homeTotalPower = this._value("home_total_power");
    const pvDailyEnergy = this._value("pv_daily_energy");
    const gridBuyToday = this._value("grid_buy_today");
    const gridSellToday = this._value("grid_sell_today");
    const homeDailyEnergy = this._value("home_daily_energy");
    const generatorTotalPower = first(
      this._value("generator_total_power"),
      sum([this._value("generator_l1_power"), this._value("generator_l2_power"), this._value("generator_l3_power")])
    );

    return {
      production_card: formatEnergy(pvDailyEnergy),
      import_card: formatEnergy(gridBuyToday),
      export_card: formatEnergy(gridSellToday),
      consumption_card: formatEnergy(homeDailyEnergy),
      costs_card: this._formatCosts(gridBuyToday, gridSellToday),
      battery_soc_card: formatPercent(this._value("battery_soc")),
      combined_pv: formatPower(pvTotalPower),
      grid_node: formatPower(gridTotalPower, true),
      inverter_node: "--",
      inverter_temp: formatTemperature(this._value("dc_temperature")),
      combined_load: formatPower(homeTotalPower),
      battery_node: formatPower(this._value("battery_power"), true),
      generator_node: formatPower(generatorTotalPower)
    };
  }

  private _formatCosts(buyToday: number | null, sellToday: number | null): string {
    if (!Number.isFinite(buyToday) && !Number.isFinite(sellToday)) {
      return "--";
    }

    const buy = buyToday ?? 0;
    const sell = sellToday ?? 0;

    if (sell > buy) {
      return `${formatNumber((sell - buy) * 6.515, 1).replace(".", ",")}\u20B4`;
    }

    return `-${formatNumber((buy - sell) * 4.32, 1).replace(".", ",")}\u20B4`;
  }

  private _applyTextBox(
    node: HTMLDivElement,
    layout: LayoutSpec,
    box: [number, number, number, number],
    text: string,
    className = "value",
    options: PositionBoxModel = {}
  ): void {
    const [x, y, width, height] = box;
    const adjustedX = x + (options.xOffsetPx ?? 0);
    const adjustedY = y + (options.yOffsetPx ?? 0);
    const left = options.leftPercent ?? (adjustedX / layout.image.width) * 100;
    const top = options.topPercent ?? (adjustedY / layout.image.height) * 100;
    const widthPct = options.widthPercent ?? (width / layout.image.width) * 100;
    const heightPct = options.heightPercent ?? (height / layout.image.height) * 100;
    const baseFontSize = options.fontSizePx ?? clamp(height * 0.56 * (options.fontScale ?? 1), 11, 30);
    const targetFontSize = Math.round(baseFontSize * MINI_CARD_FONT_REDUCTION * 10) / 10;
    const referenceWidth = this._isMobile ? MINI_CARD_MOBILE_REFERENCE_WIDTH : MINI_CARD_DESKTOP_REFERENCE_WIDTH;
    const responsiveFont = `clamp(${Math.max(10, Math.round(targetFontSize * 0.82 * 10) / 10)}px, ${(
      (targetFontSize / referenceWidth) *
      100
    ).toFixed(3)}cqw, ${Math.round(targetFontSize * 1.08 * 10) / 10}px)`;
    const textAlign = options.textAlign ?? "right";
    const justifyContent = options.justifyContent ?? "flex-end";

    setClassNameIfChanged(node, className);
    setHiddenIfChanged(node, false);
    setTextContentIfChanged(node, text);
    setStyleIfChanged(node, "left", `${left}%`);
    setStyleIfChanged(node, "top", `${top}%`);
    setStyleIfChanged(node, "width", `${widthPct}%`);
    setStyleIfChanged(node, "height", `${heightPct}%`);
    setStyleIfChanged(node, "font-size", responsiveFont);
    setStyleIfChanged(node, "text-align", textAlign);
    setStyleIfChanged(node, "justify-content", justifyContent);
  }

  private _styles(layout: LayoutSpec, background: string): string {
    const ratio = `${layout.image.width} / ${layout.image.height}`;

    return `
      :host {
        display: block;
        container-type: inline-size;
      }

      ha-card {
        overflow: hidden;
        border-radius: 24px;
        background: linear-gradient(180deg, #041221 0%, #0a1c2f 100%);
      }

      .card-shell {
        display: grid;
        gap: 10px;
        padding: 12px;
        color: #f4f8ff;
        font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      }

      .card-title {
        font-size: 13px;
        font-size: clamp(10px, 3.4cqw, 13px);
        font-weight: 500;
        letter-spacing: 0.08em;
        text-transform: uppercase;
        color: rgba(244, 248, 255, 0.76);
        padding-inline: 4px;
      }

      .scene {
        position: relative;
        width: 100%;
        aspect-ratio: ${ratio};
        background-image: url("${background}");
        background-size: cover;
        background-position: center;
        background-repeat: no-repeat;
        border-radius: 18px;
        container-type: inline-size;
        overflow: hidden;
      }

      .value {
        position: absolute;
        display: flex;
        align-items: center;
        justify-content: flex-end;
        text-align: right;
        white-space: nowrap;
        color: #f4f8ff;
        text-shadow: 0 0 14px rgba(2, 11, 28, 0.92);
        line-height: 1;
        font-weight: 500;
        font-variant-numeric: tabular-nums;
        z-index: 20;
      }

      .scene-layer {
        position: absolute;
        inset: 0;
        background-position: center;
        background-repeat: no-repeat;
        background-size: cover;
        pointer-events: none;
      }

      .value--secondary {
        justify-content: center;
        text-align: center;
      }

      .entity-map {
        display: grid;
        gap: 8px;
        border-radius: 18px;
        background: rgba(7, 24, 40, 0.88);
        border: 1px solid rgba(17, 50, 74, 0.92);
        padding: 12px 14px;
        font-size: 12px;
        font-size: clamp(10px, 3.1cqw, 12px);
        line-height: 1.45;
      }

      .map-row {
        display: grid;
        grid-template-columns: minmax(0, 180px) minmax(0, 1fr);
        gap: 12px;
        font-family: Consolas, "Courier New", monospace;
      }

      .map-row span:last-child {
        overflow-wrap: anywhere;
        color: rgba(244, 248, 255, 0.82);
      }
    `;
  }

  private _isMeaningfulPower(value: number | null | undefined): value is number {
    return Number.isFinite(value) && Math.abs(value) > MINI_LAYER_POWER_EPSILON;
  }
}

if (!customElements.get(CARD_TAG)) {
  customElements.define(CARD_TAG, JksMiniCard);
}

declare global {
  interface Window {
    customCards?: Array<{ type: string; name: string; description: string }>;
  }
}

window.customCards = window.customCards || [];
window.customCards.push({
  type: CARD_TAG,
  name: "JKS Mini",
  description: "Compact Jinko overview card."
});
