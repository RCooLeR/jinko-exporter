import desktopLayout from "../../assets/main/desktop_layout_spec.json";
import mobileLayout from "../../assets/main/mobile_layout_spec.json";
import { setClassNameIfChanged, setHiddenIfChanged, setStyleIfChanged, setTextContentIfChanged } from "../lib/dom";
import { average, clamp, first, formatCurrent, formatEnergy, formatNumber, formatPercent, formatPower, formatTemperature, sum } from "../lib/format";
import { ENTITY_KEYS, resolveEntities, valueFor, type EntityKey, type EntityOverrides, type ResolvedEntityMap } from "../lib/entity-model";
import { DETAILED_CARD_POSITIONS, type CardElementPositionModel, type PositionBoxModel, type PositionMode } from "../lib/position-models";
import type { HomeAssistant, LovelaceCardConfig } from "../types/home-assistant";

const CARD_TAG = "jks-detailed";
const DEFAULT_CONFIG = {
  title: "JKS Detailed",
  battery_capacity_kwh: 21.31,
  battery_negative_is_charging: true,
  show_entity_map: false,
  entities: {} as EntityOverrides
};

interface DetailedCardConfig extends LovelaceCardConfig {
  title?: string;
  battery_capacity_kwh?: number;
  battery_negative_is_charging?: boolean;
  show_entity_map?: boolean;
  static?: boolean;
  entities?: EntityOverrides;
}

interface LayoutRow {
  id: string;
  row_box: [number, number, number, number];
  value_box: [number, number, number, number];
}

interface LayoutElement {
  id: string;
  type: string;
  bbox: [number, number, number, number];
  label?: string;
  value_box?: [number, number, number, number] | null;
  rows?: LayoutRow[];
  soc_value_box?: [number, number, number, number];
  energy_today_value_box?: [number, number, number, number];
  temp_value_box?: [number, number, number, number];
  status_value_box?: [number, number, number, number];
}

interface LayoutSpec {
  canvas: {
    width: number;
    height: number;
    background_image: string;
  };
  elements: LayoutElement[];
}

interface MetricGroup {
  voltage: number | null;
  current: number | null;
  power: number | null;
  energyToday: number | null;
  hideEnergyToday?: boolean;
  voltagePhases?: Array<number | null>;
  currentPhases?: Array<number | null>;
  powerPhases?: Array<number | null>;
}

interface DetailedCardData {
  summary: Record<string, string>;
  groups: Record<string, MetricGroup>;
  batterySoc: string;
  batteryEnergyToday: string;
  inverterTemp: string;
  inverterStatus: string;
  layers: Partial<Record<DetailedLayerId, boolean>>;
  missing: EntityKey[];
}

type DetailedLayerId =
  | "pv1_offline"
  | "pv2_offline"
  | "pv_offline"
  | "grid_offline"
  | "battery_offline"
  | "gen_offline"
  | "ups_offline"
  | "parallel_offline";

interface DetailedLayerDefinition {
  id: DetailedLayerId;
  src: string;
  zIndex: number;
}

const DESKTOP_LAYOUT = desktopLayout as LayoutSpec;
const MOBILE_LAYOUT = mobileLayout as LayoutSpec;
const MOBILE_BREAKPOINT = 960;
const DETAILED_CARD_MOBILE_REFERENCE_WIDTH = 420;
const DETAILED_CARD_DESKTOP_REFERENCE_WIDTH = 1200;
const VOLTAGE_EPSILON = 1;
const CURRENT_EPSILON = 0.01;
const POWER_EPSILON = 1;
const ENERGY_EPSILON = 0.01;
const MISSING_MAIN_KEYS: EntityKey[] = ["pv1_power", "pv2_power", "grid_total_power", "home_total_power", "ups_total_power", "battery_power", "battery_soc"];
const moduleBaseUrl = new URL(/* @vite-ignore */ ".", import.meta.url).toString();
const assetUrl = (file: string): string =>
  import.meta.env.DEV ? `/assets/main/${file}` : `${moduleBaseUrl}assets/main/${file}`;
const DESKTOP_LAYERS: DetailedLayerDefinition[] = [
  { id: "pv1_offline", src: assetUrl("layers/detailed-desktop-pv1-offline.png"), zIndex: 10 },
  { id: "pv2_offline", src: assetUrl("layers/detailed-desktop-pv2-offline.png"), zIndex: 10 },
  { id: "pv_offline", src: assetUrl("layers/detailed-desktop-pv-offline.png"), zIndex: 11 },
  { id: "grid_offline", src: assetUrl("layers/detailed-desktop-grid-offline.png"), zIndex: 10 },
  { id: "battery_offline", src: assetUrl("layers/detailed-desktop-battery-offline.png"), zIndex: 10 },
  { id: "gen_offline", src: assetUrl("layers/detailed-desktop-gen-offline.png"), zIndex: 10 },
  { id: "ups_offline", src: assetUrl("layers/detailed-desktop-ups-offline.png"), zIndex: 10 },
  { id: "parallel_offline", src: assetUrl("layers/detailed-desktop-paralel-offline.png"), zIndex: 10 }
];

class JksDetailedCard extends HTMLElement {
  private _hass?: HomeAssistant;
  private _config: DetailedCardConfig = { type: `custom:${CARD_TAG}`, ...DEFAULT_CONFIG };
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
  private _warningEl?: HTMLDivElement;
  private _entityMapEl?: HTMLDivElement;
  private _warningText = "";
  private _entityMapSignature = "";
  private _layerNodes = new Map<DetailedLayerId, HTMLDivElement>();
  private _overlayNodes = new Map<string, HTMLDivElement>();

  constructor() {
    super();
    this._root = this.attachShadow({ mode: "open" });
  }

  setConfig(config: DetailedCardConfig): void {
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
    return this._isMobile ? 16 : 10;
  }

  static getStubConfig(): DetailedCardConfig {
    return { type: `custom:${CARD_TAG}` };
  }

  private _ensureResizeObserver(): void {
    if (this._resizeObserver) return;

    this._resizeObserver = new ResizeObserver((entries) => {
      if (this._config.static && this._hasRendered) {
        return;
      }

      const width = entries[0]?.contentRect.width ?? this.clientWidth ?? DESKTOP_LAYOUT.canvas.width;
      const nextMode = width <= MOBILE_BREAKPOINT;
      if (nextMode !== this._isMobile) {
        this._isMobile = nextMode;
        this._render();
      }
    });

    this._resizeObserver.observe(this);
    const width = this.clientWidth || DESKTOP_LAYOUT.canvas.width;
    this._isMobile = width <= MOBILE_BREAKPOINT;
  }

  private _layout(): LayoutSpec {
    return this._isMobile ? MOBILE_LAYOUT : DESKTOP_LAYOUT;
  }

  private _positionMode(): PositionMode {
    return this._isMobile ? "mobile" : "desktop";
  }

  private _elementPosition(elementId: string): CardElementPositionModel {
    return DETAILED_CARD_POSITIONS[this._positionMode()][elementId] ?? {};
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

  private _buildData(): DetailedCardData {
    if (this._config.static) {
      return this._buildStaticData();
    }

    const gridPhases = [1, 2, 3].map((phase) => ({
      voltage: this._value(`grid_l${phase}_voltage` as EntityKey),
      current: this._value(`grid_l${phase}_current` as EntityKey),
      power: this._value(`grid_l${phase}_power` as EntityKey)
    }));

    const homePhases = [1, 2, 3].map((phase) => {
      const voltage = this._value(`home_l${phase}_voltage` as EntityKey);
      const power = this._value(`home_l${phase}_power` as EntityKey);
      const current = Number.isFinite(power) && Number.isFinite(voltage) && voltage !== 0 ? Math.abs(power) / voltage : null;
      return { voltage, current, power };
    });

    const inverterPhases = [1, 2, 3].map((phase) => ({
      voltage: this._value(`inverter_l${phase}_voltage` as EntityKey),
      current: this._value(`inverter_l${phase}_current` as EntityKey),
      power: this._value(`inverter_l${phase}_power` as EntityKey)
    }));

    const generatorPhases = [1, 2, 3].map((phase) => {
      const voltage = this._value(`generator_l${phase}_voltage` as EntityKey);
      const power = this._value(`generator_l${phase}_power` as EntityKey);
      const current = Number.isFinite(power) && Number.isFinite(voltage) && voltage !== 0 ? Math.abs(power) / voltage : null;
      return { voltage, current, power };
    });

    const pv1Power = this._value("pv1_power");
    const pv2Power = this._value("pv2_power");
    const pv1Voltage = this._value("pv1_voltage");
    const pv1Current = this._value("pv1_current");
    const pv2Voltage = this._value("pv2_voltage");
    const pv2Current = this._value("pv2_current");
    const pvTotalPower = first(this._value("pv_total_power"), sum([pv1Power, pv2Power]));
    const pvDailyEnergy = this._value("pv_daily_energy");
    const gridBuyToday = this._value("grid_buy_today");
    const gridSellToday = this._value("grid_sell_today");
    const gridTotalPower = first(this._value("grid_total_power"), sum(gridPhases.map((phase) => phase.power)));
    const gridAverageVoltage = average(gridPhases.map((phase) => phase.voltage));
    const gridTotalCurrent = sum(gridPhases.map((phase) => Math.abs(phase.current ?? 0)));
    const homeTotalPower = first(this._value("home_total_power"), sum(homePhases.map((phase) => phase.power)));
    const homeBackupPhasePower = sum(homePhases.map((phase) => phase.power));
    const parallelGridLoadPower =
      Number.isFinite(homeTotalPower) && Number.isFinite(homeBackupPhasePower) ? homeTotalPower - homeBackupPhasePower : null;
    const parallelGridLoadCurrent =
      Number.isFinite(parallelGridLoadPower) && Number.isFinite(gridAverageVoltage) && gridAverageVoltage !== 0
        ? Math.abs(parallelGridLoadPower) / gridAverageVoltage
        : null;
    const gridNetEnergyToday =
      Number.isFinite(gridBuyToday) || Number.isFinite(gridSellToday) ? (gridBuyToday ?? 0) - (gridSellToday ?? 0) : null;

    const upsTotalPower = this._value("ups_total_power");
    const inverterAverageVoltage = average(inverterPhases.map((phase) => phase.voltage));
    const upsEstimatedCurrent =
      Number.isFinite(upsTotalPower) && Number.isFinite(inverterAverageVoltage) && inverterAverageVoltage !== 0
        ? Math.abs(upsTotalPower) / inverterAverageVoltage
        : null;

    const generatorTotalPower = first(this._value("generator_total_power"), sum(generatorPhases.map((phase) => phase.power)));
    const generatorAverageVoltage = average(generatorPhases.map((phase) => phase.voltage));
    const generatorTotalCurrent = sum(generatorPhases.map((phase) => Math.abs(phase.current ?? 0)));

    const batteryPower = this._value("battery_power");
    const batteryChargeIsNegative = this._config.battery_negative_is_charging !== false;
    let batteryStatus = "Idle";
    if (Number.isFinite(batteryPower) && Math.abs(batteryPower) >= 20) {
      const charging = batteryChargeIsNegative ? batteryPower < 0 : batteryPower > 0;
      batteryStatus = charging ? "Charging" : "Discharging";
    }

    const inverterTotalPower = first(this._value("inverter_total_power"), sum(inverterPhases.map((phase) => phase.power)));
    const inverterStatus =
      Number.isFinite(inverterTotalPower) && Math.abs(inverterTotalPower) >= 20
        ? "Online"
        : Number.isFinite(this._value("inverter_frequency"))
          ? "Standby"
          : "Offline";

    const batteryChargeToday = this._value("battery_charge_today");
    const batteryDischargeToday = this._value("battery_discharge_today");
    const batteryEnergyToday = this._formatBatteryNetEnergyToday(batteryChargeToday, batteryDischargeToday);
    const batteryVoltage = this._value("battery_voltage");
    const batteryCurrent = this._value("battery_current");
    const batterySoc = this._value("battery_soc");
    const generatorDailyEnergy = this._value("generator_daily_energy");
    const inverterFrequency = this._value("inverter_frequency");
    const gridFrequency = this._value("grid_frequency");

    const pv1Online = this._isMeaningfulValue(pv1Power, POWER_EPSILON) || this._isMeaningfulValue(pv1Voltage, VOLTAGE_EPSILON) || this._isMeaningfulValue(pv1Current, CURRENT_EPSILON);
    const pv2Online = this._isMeaningfulValue(pv2Power, POWER_EPSILON) || this._isMeaningfulValue(pv2Voltage, VOLTAGE_EPSILON) || this._isMeaningfulValue(pv2Current, CURRENT_EPSILON);
    const pvOnline = this._isMeaningfulValue(pvTotalPower, POWER_EPSILON) || pv1Online || pv2Online;
    const gridOnline =
      this._isMeaningfulValue(gridAverageVoltage, VOLTAGE_EPSILON) ||
      this._isMeaningfulValue(gridFrequency, CURRENT_EPSILON) ||
      this._isMeaningfulValue(gridTotalPower, POWER_EPSILON) ||
      this._isMeaningfulValue(gridBuyToday, ENERGY_EPSILON) ||
      this._isMeaningfulValue(gridSellToday, ENERGY_EPSILON);
    const batteryOnline =
      this._isMeaningfulValue(batteryVoltage, VOLTAGE_EPSILON) ||
      this._isMeaningfulValue(batteryCurrent, CURRENT_EPSILON) ||
      this._isMeaningfulValue(batteryPower, POWER_EPSILON) ||
      this._isMeaningfulValue(batterySoc, CURRENT_EPSILON);
    const generatorOnline =
      this._isMeaningfulValue(generatorAverageVoltage, VOLTAGE_EPSILON) ||
      this._isMeaningfulValue(generatorTotalPower, POWER_EPSILON) ||
      this._isMeaningfulValue(generatorDailyEnergy, ENERGY_EPSILON);
    const upsOnline =
      inverterStatus !== "Offline" ||
      this._isMeaningfulValue(inverterAverageVoltage, VOLTAGE_EPSILON) ||
      this._isMeaningfulValue(inverterFrequency, CURRENT_EPSILON) ||
      this._isMeaningfulValue(upsTotalPower, POWER_EPSILON);
    const parallelOnline = Number.isFinite(parallelGridLoadPower) && parallelGridLoadPower > POWER_EPSILON;

    return {
      summary: {
        daily_production: this._formatEnergyDisplay(pvDailyEnergy),
        daily_generator: this._formatEnergyDisplay(generatorDailyEnergy),
        daily_import: this._formatEnergyDisplay(gridBuyToday),
        daily_export: this._formatEnergyDisplay(gridSellToday),
        daily_consumption: this._formatEnergyDisplay(this._value("home_daily_energy")),
        daily_costs: this._formatCosts(gridBuyToday, gridSellToday)
      },
      groups: {
        ups_load: {
          voltage: inverterAverageVoltage,
          current: upsEstimatedCurrent,
          power: upsTotalPower,
          energyToday: null,
          hideEnergyToday: true,
          voltagePhases: homePhases.map((phase) => phase.voltage),
          currentPhases: homePhases.map((phase) => phase.current),
          powerPhases: homePhases.map((phase) => phase.power)
        },
        pv1: {
          voltage: pv1Voltage,
          current: pv1Current,
          power: pv1Power,
          energyToday: null,
          hideEnergyToday: true
        },
        pv2: {
          voltage: pv2Voltage,
          current: pv2Current,
          power: pv2Power,
          energyToday: null,
          hideEnergyToday: true
        },
        grid: {
          voltage: gridAverageVoltage,
          current: gridTotalCurrent,
          power: gridTotalPower,
          energyToday: gridNetEnergyToday,
          voltagePhases: gridPhases.map((phase) => phase.voltage),
          currentPhases: gridPhases.map((phase) => phase.current),
          powerPhases: gridPhases.map((phase) => phase.power)
        },
        battery: {
          voltage: batteryVoltage,
          current: batteryCurrent,
          power: batteryPower,
          energyToday: batteryDischargeToday ?? batteryChargeToday
        },
        inverter: {
          voltage: inverterAverageVoltage,
          current: sum(inverterPhases.map((phase) => Math.abs(phase.current ?? 0))),
          power: inverterTotalPower,
          energyToday: pvDailyEnergy,
          voltagePhases: inverterPhases.map((phase) => phase.voltage),
          currentPhases: inverterPhases.map((phase) => phase.current),
          powerPhases: undefined
        },
        generator: {
          voltage: generatorAverageVoltage,
          current: generatorTotalCurrent,
          power: generatorTotalPower,
          energyToday: generatorDailyEnergy,
          voltagePhases: generatorPhases.map((phase) => phase.voltage),
          currentPhases: generatorPhases.map((phase) => phase.current),
          powerPhases: generatorPhases.map((phase) => phase.power)
        },
        parallel_grid_load: {
          voltage: gridAverageVoltage,
          current: parallelGridLoadCurrent,
          power: parallelGridLoadPower,
          energyToday: this._value("home_daily_energy"),
          hideEnergyToday: true,
          voltagePhases: gridPhases.map((phase) => phase.voltage)
        }
      },
      batterySoc: formatPercent(batterySoc),
      batteryEnergyToday,
      inverterTemp: formatTemperature(this._value("dc_temperature")),
      inverterStatus: `${inverterStatus}${batteryStatus === "Idle" ? "" : ` | ${batteryStatus}`}`,
      layers: {
        pv1_offline: !pv1Online,
        pv2_offline: !pv2Online,
        pv_offline: !pvOnline,
        grid_offline: !gridOnline,
        battery_offline: !batteryOnline,
        gen_offline: !generatorOnline,
        ups_offline: !upsOnline,
        parallel_offline: !parallelOnline
      },
      missing: MISSING_MAIN_KEYS.filter((key) => !this._resolved[key])
    };
  }

  private _buildStaticData(): DetailedCardData {
    return {
      summary: {
        daily_production: "24.8 kWh",
        daily_generator: "6.3 kWh",
        daily_import: "8.1 kWh",
        daily_export: "3.4 kWh",
        daily_consumption: "29.5 kWh",
        daily_costs: this._formatCosts(8.1, 3.4)
      },
      groups: {
        ups_load: {
          voltage: 230.8,
          current: 3.42,
          power: 790,
          energyToday: 12.4,
          hideEnergyToday: true,
          voltagePhases: [231, 230, 231],
          currentPhases: [1.22, 1.08, 1.12],
          powerPhases: [282, 249, 259]
        },
        pv1: {
          voltage: 402.4,
          current: 5.21,
          power: 2100,
          energyToday: null,
          hideEnergyToday: true
        },
        pv2: {
          voltage: 398.7,
          current: 4.76,
          power: 1890,
          energyToday: null,
          hideEnergyToday: true
        },
        grid: {
          voltage: 231.7,
          current: 5.84,
          power: 1320,
          energyToday: 4.7,
          voltagePhases: [231, 232, 232],
          currentPhases: [1.95, 1.88, 2.01],
          powerPhases: [440, 421, 459]
        },
        battery: {
          voltage: 52.3,
          current: -24.5,
          power: -1280,
          energyToday: 5.2
        },
        inverter: {
          voltage: 230.9,
          current: 8.27,
          power: 1910,
          energyToday: 24.8,
          voltagePhases: [231, 231, 231],
          currentPhases: [2.61, 2.74, 2.92],
          powerPhases: undefined
        },
        generator: {
          voltage: 229.4,
          current: 2.16,
          power: 495,
          energyToday: 6.3,
          voltagePhases: [229, 230, 229],
          currentPhases: [0.71, 0.73, 0.72],
          powerPhases: [163, 168, 164]
        },
        parallel_grid_load: {
          voltage: 231.2,
          current: 2.33,
          power: 540,
          energyToday: 17.1,
          hideEnergyToday: true,
          voltagePhases: [231, 232, 232]
        }
      },
      batterySoc: "68%",
      batteryEnergyToday: "+5.2 kWh",
      inverterTemp: "46 C",
      inverterStatus: "Online | Charging",
      layers: {},
      missing: []
    };
  }

  private _formatBatteryNetEnergyToday(chargeToday: number | null, dischargeToday: number | null): string {
    if (Number.isFinite(dischargeToday) || Number.isFinite(chargeToday)) {
      const net = (dischargeToday ?? 0) - (chargeToday ?? 0);
      if (this._isNearZero(net, ENERGY_EPSILON)) {
        return "--";
      }
      return `${net > 0 ? "+" : net < 0 ? "-" : ""}${formatEnergy(Math.abs(net))}`;
    }
    return "--";
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

  private _value(key: EntityKey): number | null {
    return valueFor(this._hass, this._resolved, key);
  }

  private _render(): void {
    const layout = this._layout();
    const mode = this._positionMode();
    const background = this._isMobile ? assetUrl("mobile.png") : assetUrl("desktop.png");
    const data = this._buildData();

    this._ensureStructure(layout, background, mode);
    this._syncTitle();
    this._syncWarnings(data);
    this._syncEntityMap();
    this._syncLayers(data);
    this._syncOverlays(layout, data);

    this._hasRendered = true;
  }

  private _ensureStructure(layout: LayoutSpec, background: string, mode: PositionMode): void {
    if (this._renderedMode === mode && this._styleEl && this._titleEl && this._sceneEl && this._warningEl && this._entityMapEl) {
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

    this._warningEl = document.createElement("div");
    this._warningEl.className = "helper helper--warn";
    this._warningEl.hidden = true;

    this._entityMapEl = document.createElement("div");
    this._entityMapEl.className = "entity-map";
    this._entityMapEl.hidden = true;

    shell.append(this._titleEl, this._sceneEl, this._warningEl, this._entityMapEl);
    card.append(shell);
    this._root.append(this._styleEl, card);
    this._renderedMode = mode;
  }

  private _syncTitle(): void {
    if (!this._titleEl) return;
    const title = this._config.title?.trim() ?? "";
    setTextContentIfChanged(this._titleEl, title);
    setHiddenIfChanged(this._titleEl, !title);
  }

  private _syncWarnings(data: DetailedCardData): void {
    if (!this._warningEl) return;
    if (!data.missing.length) {
      this._warningText = "";
      setHiddenIfChanged(this._warningEl, true);
      setTextContentIfChanged(this._warningEl, "");
      return;
    }

    const warningText = `Missing critical sensors: ${data.missing.join(", ")}`;
    this._warningText = warningText;
    setHiddenIfChanged(this._warningEl, false);
    setTextContentIfChanged(this._warningEl, warningText);
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

  private _syncLayers(data: DetailedCardData): void {
    for (const layer of DESKTOP_LAYERS) {
      const node = this._layerNodes.get(layer.id);
      if (!node) {
        continue;
      }
      setHiddenIfChanged(node, !data.layers[layer.id]);
    }
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

  private _syncOverlays(layout: LayoutSpec, data: DetailedCardData): void {
    const activeKeys = new Set<string>();

    for (const element of layout.elements) {
      this._syncElement(layout, element, data, activeKeys);
    }

    for (const [key, node] of this._overlayNodes) {
      setHiddenIfChanged(node, !activeKeys.has(key));
    }
  }

  private _syncElement(layout: LayoutSpec, element: LayoutElement, data: DetailedCardData, activeKeys: Set<string>): void {
    if (element.type === "brand_card") {
      return;
    }

    const position = this._elementPosition(element.id);

    if (element.type === "summary_card" && element.value_box) {
      this._updateOverlayNode(activeKeys, `summary:${element.id}`, layout, element.value_box, data.summary[element.id] ?? "--", "value value--summary", position.value);
      return;
    }

    if (element.type !== "metric_card" && element.type !== "metric_card_with_soc" && element.type !== "inverter_card") {
      return;
    }

    if (!element.rows) {
      return;
    }

    const group = data.groups[element.id];
    if (!group) {
      return;
    }

    for (const row of element.rows) {
      const value = this._formatMetricRowValue(row.id, group);
      const shouldShow = value.trim().length > 0;
      this._updateOverlayNode(
        activeKeys,
        `${element.id}:row:${row.id}`,
        layout,
        row.value_box,
        value,
        "value value--metric",
        position.rows?.[row.id],
        shouldShow
      );
    }

    if (element.soc_value_box) {
      this._updateOverlayNode(activeKeys, `${element.id}:extra:soc`, layout, element.soc_value_box, data.batterySoc, "value value--soc", position.extras?.soc);
    }

    if (element.energy_today_value_box) {
      const energyValue = element.id === "battery" ? data.batteryEnergyToday : this._formatEnergyToday(group.energyToday);
      const shouldShow = !group.hideEnergyToday && energyValue.trim().length > 0;
      this._updateOverlayNode(
        activeKeys,
        `${element.id}:extra:energy_today`,
        layout,
        element.energy_today_value_box,
        energyValue,
        "value value--tiny",
        position.extras?.energy_today,
        shouldShow
      );
    }

    if (element.temp_value_box) {
      this._updateOverlayNode(activeKeys, `${element.id}:extra:temp`, layout, element.temp_value_box, data.inverterTemp, "value value--temp", position.extras?.temp);
    }

    if (element.status_value_box) {
      this._updateOverlayNode(activeKeys, `${element.id}:extra:status`, layout, element.status_value_box, data.inverterStatus, "value value--status", position.extras?.status);
    }
  }

  private _updateOverlayNode(
    activeKeys: Set<string>,
    key: string,
    layout: LayoutSpec,
    box: [number, number, number, number],
    text: string,
    className: string,
    options: PositionBoxModel = {},
    visible = true
  ): void {
    const node = this._ensureOverlayNode(key, className);
    activeKeys.add(key);
    this._applyTextBox(node, layout, box, text, className, options, visible);
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

  private _applyTextBox(
    node: HTMLDivElement,
    layout: LayoutSpec,
    box: [number, number, number, number],
    text: string,
    className: string,
    options: PositionBoxModel = {},
    visible: boolean
  ): void {
    const [x, y, width, height] = box;
    const adjustedX = x + (options.xOffsetPx ?? 0);
    const adjustedY = y + (options.yOffsetPx ?? 0);
    const left = options.leftPercent ?? (adjustedX / layout.canvas.width) * 100;
    const top = options.topPercent ?? (adjustedY / layout.canvas.height) * 100;
    const widthPct = options.widthPercent ?? (width / layout.canvas.width) * 100;
    const heightPct = (height / layout.canvas.height) * 100;
    const baseFontSize = height * 0.62;
    const lengthScale = text.includes("/") ? 0.78 : text.length > 16 ? 0.84 : 1;
    const targetFontSize = clamp(baseFontSize * lengthScale * (options.fontScale ?? 1), 12, 44);
    const referenceWidth = this._isMobile ? DETAILED_CARD_MOBILE_REFERENCE_WIDTH : DETAILED_CARD_DESKTOP_REFERENCE_WIDTH;
    const responsiveFont = `clamp(${Math.max(12, Math.round(targetFontSize * 0.84 * 10) / 10)}px, ${(
      (targetFontSize / referenceWidth) *
      100
    ).toFixed(3)}cqw, ${Math.round(targetFontSize * 1.08 * 10) / 10}px)`;

    setClassNameIfChanged(node, className);
    setHiddenIfChanged(node, !visible);
    setTextContentIfChanged(node, text);
    setStyleIfChanged(node, "left", `${left}%`);
    setStyleIfChanged(node, "top", `${top}%`);
    setStyleIfChanged(node, "width", `${widthPct}%`);
    setStyleIfChanged(node, "height", `${heightPct}%`);
    setStyleIfChanged(node, "font-size", responsiveFont);
  }

  private _formatMetricRowValue(rowId: string, group: MetricGroup): string {
    switch (rowId) {
      case "voltage":
        return this._formatPhaseVoltage(group.voltagePhases, group.voltage);
      case "current":
        return this._formatPhaseCurrent(group.currentPhases, group.current);
      case "power":
        return this._formatPhasePower(group.powerPhases, group.power);
      case "energy_today":
        if (group.hideEnergyToday) return "";
        return this._formatEnergyToday(group.energyToday);
      default:
        return "--";
    }
  }

  private _formatEnergyToday(value: number | null): string {
    return this._formatEnergyDisplay(value);
  }

  private _formatPhaseVoltage(phases: Array<number | null> | undefined, fallback: number | null): string {
    if (phases?.some((value) => this._isMeaningfulValue(value, VOLTAGE_EPSILON))) {
      return this._formatPhaseSeries(phases, (value) => formatNumber(value, 0), "V", VOLTAGE_EPSILON);
    }
    return this._isMeaningfulValue(fallback, VOLTAGE_EPSILON) ? `${formatNumber(fallback as number, 0)} V` : "--";
  }

  private _formatPhaseCurrent(phases: Array<number | null> | undefined, fallback: number | null): string {
    if (phases?.some((value) => this._isMeaningfulValue(value, CURRENT_EPSILON))) {
      return this._formatPhaseSeries(
        phases,
        (value) => formatNumber(value, Math.abs(value) >= 10 ? 1 : 2),
        "A",
        CURRENT_EPSILON
      );
    }
    return this._isMeaningfulValue(fallback, CURRENT_EPSILON) ? formatCurrent(fallback) : "--";
  }

  private _formatPhasePower(phases: Array<number | null> | undefined, fallback: number | null): string {
    if (phases?.some((value) => this._isMeaningfulValue(value, POWER_EPSILON))) {
      const finite = phases.filter((value): value is number => this._isMeaningfulValue(value, POWER_EPSILON));
      const useKw = finite.some((value) => Math.abs(value) >= 1000);
      return this._formatPhaseSeries(
        phases,
        (value) => {
          if (useKw) return formatNumber(value / 1000, Math.abs(value) >= 10000 ? 1 : 2);
          return formatNumber(value, Math.abs(value) >= 100 ? 0 : 1);
        },
        useKw ? "kW" : "W",
        POWER_EPSILON
      );
    }
    return this._isMeaningfulValue(fallback, POWER_EPSILON) ? formatPower(fallback, true) : "--";
  }

  private _formatPhaseSeries(
    phases: Array<number | null>,
    formatter: (value: number) => string,
    unit: string,
    epsilon: number
  ): string {
    const meaningful = phases.filter((value): value is number => this._isMeaningfulValue(value, epsilon));
    if (!meaningful.length) {
      return "--";
    }
    const formatted = phases.map((value) => (this._isMeaningfulValue(value, epsilon) ? formatter(value as number) : "--"));
    return `${formatted.join(" / ")} ${unit}`;
  }

  private _formatEnergyDisplay(value: number | null | undefined): string {
    return this._isMeaningfulValue(value, ENERGY_EPSILON) ? formatEnergy(value) : "--";
  }

  private _isMeaningfulValue(value: number | null | undefined, epsilon: number): value is number {
    return Number.isFinite(value) && !this._isNearZero(value, epsilon);
  }

  private _isNearZero(value: number | null | undefined, epsilon: number): boolean {
    return !Number.isFinite(value) || Math.abs(value) < epsilon;
  }

  private _styles(layout: LayoutSpec, background: string): string {
    const ratio = `${layout.canvas.width} / ${layout.canvas.height}`;

    return `
      :host {
        display: block;
        container-type: inline-size;
      }

      ha-card {
        overflow: hidden;
        border-radius: 28px;
        background: linear-gradient(180deg, #031224 0%, #071c30 100%);
        box-shadow: 0 18px 48px rgba(2, 11, 28, 0.38);
      }

      .card-shell {
        display: grid;
        gap: 12px;
        padding: 14px;
        color: #f4f8ff;
        font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      }

      .card-title {
        font-size: 14px;
        font-size: clamp(11px, 1.45cqw, 14px);
        font-weight: 600;
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
        border-radius: 22px;
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

      .value--summary {
        font-weight: 600;
      }

      .value--metric {
        font-weight: 600;
      }

      .value--soc {
        justify-content: center;
        text-align: center;
        font-size: clamp(28px, 3vw, 56px);
        font-weight: 600;
      }

      .value--temp {
        justify-content: center;
        text-align: center;
        font-weight: 600;
      }

      .value--status {
        justify-content: center;
        text-align: center;
        font-size: 14px;
        font-weight: 600;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        color: #8fd7ff;
      }

      .value--tiny {
        font-size: 12px;
        font-weight: 600;
      }

      .helper,
      .entity-map {
        border-radius: 18px;
        background: rgba(7, 24, 40, 0.88);
        border: 1px solid rgba(17, 50, 74, 0.92);
        padding: 12px 14px;
        font-size: 12px;
        font-size: clamp(10px, 1.25cqw, 12px);
        line-height: 1.45;
      }

      .helper--warn {
        color: #ffd870;
      }

      .entity-map {
        display: grid;
        gap: 8px;
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
}

if (!customElements.get(CARD_TAG)) {
  customElements.define(CARD_TAG, JksDetailedCard);
}

declare global {
  interface Window {
    customCards?: Array<{ type: string; name: string; description: string }>;
  }
}

window.customCards = window.customCards || [];
window.customCards.push({
  type: CARD_TAG,
  name: "JKS Detailed",
  description: "Background-based Jinko dashboard card with sensor autodiscovery."
});
