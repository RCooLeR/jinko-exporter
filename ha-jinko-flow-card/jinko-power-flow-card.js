const CARD_TAG = "jinko-power-flow-card";
const STAGE_WIDTH = 1920;
const STAGE_HEIGHT = 910;
const STAGE_BOTTOM_TRIM = 0;
const VISIBLE_STAGE_HEIGHT = STAGE_HEIGHT - STAGE_BOTTOM_TRIM;
const DEFAULT_CONFIG = {
  title: "Jinko ESS Power Flow",
  battery_capacity_kwh: 21.31,
  battery_negative_is_charging: true,
  show_entity_map: false,
  entities: {},
};

const ENTITY_DEFINITIONS = {
  pv_total_power: { names: ["Total Solar Power", "Solar"] },
  pv_daily_energy: { names: ["PV daily power generation (active)", "Daily Production (Active)"] },
  pv1_voltage: { names: ["DC Voltage PV1"] },
  pv1_current: { names: ["DC Current PV1"] },
  pv1_power: { names: ["DC Power PV1"] },
  pv2_voltage: { names: ["DC Voltage PV2"] },
  pv2_current: { names: ["DC Current PV2"] },
  pv2_power: { names: ["DC Power PV2"] },
  grid_total_power: { names: ["Total Grid Power", "Internal Power"] },
  grid_frequency: { names: ["Grid Frequency"] },
  grid_buy_today: { names: ["Daily Energy Buy"] },
  grid_sell_today: { names: ["Daily energy sell"] },
  grid_l1_voltage: { names: ["Grid Voltage L1"] },
  grid_l2_voltage: { names: ["Grid Voltage L2"] },
  grid_l3_voltage: { names: ["Grid Voltage L3"] },
  grid_l1_current: { names: ["Grid Current L1"] },
  grid_l2_current: { names: ["Grid Current L2"] },
  grid_l3_current: { names: ["Grid Current L3"] },
  grid_l1_power: { names: ["Grid Power L1"] },
  grid_l2_power: { names: ["Grid Power L2"] },
  grid_l3_power: { names: ["Grid Power L3"] },
  home_total_power: { names: ["Total Consumption Power"] },
  home_daily_energy: { names: ["Daily Consumption"] },
  home_frequency: { names: ["Load Fequency", "Load Frequency"] },
  home_l1_voltage: { names: ["Load Voltage L1"] },
  home_l2_voltage: { names: ["Load Voltage L2"] },
  home_l3_voltage: { names: ["Load Voltage L3"] },
  home_l1_power: { names: ["Load Power L1", "Load phase power A"] },
  home_l2_power: { names: ["Load Power L2", "Load phase power B"] },
  home_l3_power: { names: ["Load Power L3", "Load phase power C"] },
  ups_total_power: { names: ["UPS Load Power"] },
  generator_daily_energy: { names: ["Daily Production Generator"] },
  generator_total_power: { names: ["Total Gen Power", "Generator Active Power"] },
  generator_l1_power: { names: ["Gen Power L1"] },
  generator_l2_power: { names: ["Gen Power L2"] },
  generator_l3_power: { names: ["Gen Power L3"] },
  generator_l1_voltage: { names: ["Gen Voltage L1"] },
  generator_l2_voltage: { names: ["Gen Voltage L2"] },
  generator_l3_voltage: { names: ["Gen Voltage L3"] },
  generator_daily_runtime: { names: ["Gen Daily Run Time"] },
  battery_voltage: { names: ["Battery Voltage"] },
  battery_current: { names: ["Battery Current"] },
  battery_power: { names: ["Battery Power"] },
  battery_soc: { names: ["SoC", "BMS_SOC"] },
  battery_temp: { names: ["Temperature- Battery", "BMS Temperature"] },
  battery_charge_today: { names: ["Daily Charging Energy"] },
  battery_discharge_today: { names: ["Daily Discharging Energy"] },
  inverter_total_power: { names: ["Total Inverter Output Power"] },
  inverter_l1_power: { names: ["Inverter Output Power L1"] },
  inverter_l2_power: { names: ["Inverter Output Power L2"] },
  inverter_l3_power: { names: ["Inverter Output Power L3"] },
  inverter_l1_voltage: { names: ["AC Voltage R/U/A"] },
  inverter_l2_voltage: { names: ["AC Voltage S/V/B"] },
  inverter_l3_voltage: { names: ["AC Voltage T/W/C"] },
  inverter_l1_current: { names: ["AC Current R/U/A"] },
  inverter_l2_current: { names: ["AC Current S/V/B"] },
  inverter_l3_current: { names: ["AC Current T/W/C"] },
  inverter_frequency: { names: ["AC Output Frequency R"] },
  power_factor: { names: ["Power factor"] },
  dc_temperature: { names: ["DC Temperature"] },
};

const CRITICAL_KEYS = ["pv1_power", "pv2_power", "grid_total_power", "home_total_power", "ups_total_power", "battery_power", "battery_soc"];

const normalizeText = (value) =>
  String(value ?? "")
    .toLowerCase()
    .replace(/_/g, " ")
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();

const escapeHtml = (value) =>
  String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");

const toNumber = (stateObj) => {
  if (!stateObj) return null;
  const value = Number.parseFloat(stateObj.state);
  return Number.isFinite(value) ? value : null;
};

const clamp = (value, min, max) => Math.min(Math.max(value, min), max);
const sum = (values) => {
  const filtered = values.filter((value) => Number.isFinite(value));
  return filtered.length ? filtered.reduce((acc, value) => acc + value, 0) : null;
};
const average = (values) => {
  const filtered = values.filter((value) => Number.isFinite(value) && value !== 0);
  return filtered.length ? filtered.reduce((acc, value) => acc + value, 0) / filtered.length : null;
};
const first = (...values) => values.find((value) => value !== null && value !== undefined) ?? null;

const formatNumber = (value, digits = 1) => {
  if (!Number.isFinite(value)) return "--";
  const fixed = value.toFixed(digits);
  return fixed.replace(/\.0+$|(\.\d*[1-9])0+$/, "$1");
};

const formatPower = (value, { signed = false } = {}) => {
  if (!Number.isFinite(value)) return "--";
  const sign = signed && value > 0 ? "+" : "";
  const abs = Math.abs(value);
  if (abs >= 1000) return `${sign}${value < 0 ? "-" : ""}${formatNumber(abs / 1000, abs >= 10000 ? 1 : 2)} kW`;
  return `${sign}${value < 0 ? "-" : ""}${formatNumber(abs, abs >= 100 ? 0 : 1)} W`;
};

const formatVoltage = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 100 ? 1 : 2)} V` : "--");
const formatCurrent = (value) => {
  if (!Number.isFinite(value)) return "--";
  const abs = Math.abs(value);
  if (abs < 1) return `${formatNumber(abs * 1000, abs < 0.1 ? 0 : 1)} mA`;
  return `${formatNumber(abs, abs >= 10 ? 1 : 2)} A`;
};
const formatEnergy = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 10 ? 1 : 2)} kWh` : "--");
const formatPercent = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 10 ? 0 : 1)}%` : "--");
const formatTemperature = (value) => (Number.isFinite(value) ? `${formatNumber(value, 0)} C` : "--");
const formatFrequency = (value) => (Number.isFinite(value) ? `${formatNumber(value, 2)} Hz` : "--");
const formatFactor = (value) => (Number.isFinite(value) ? formatNumber(value, 2) : "--");

const statusTone = (kind) => {
  switch (kind) {
    case "production":
      return "solar";
    case "export":
      return "mint";
    case "import":
      return "amber";
    case "charge":
      return "sky";
    case "consumption":
      return "slate";
    default:
      return "slate";
  }
};

const renderFlowPath = ({ d, active, direction, power, color, glow }) => `
  <path class="wire" d="${d}"></path>
  <path class="wire-flow ${active ? "wire-flow--active" : ""} ${direction === "reverse" ? "wire-flow--reverse" : ""}" d="${d}" style="--flow-color:${color}; --flow-glow:${glow || color};"></path>
`;

const renderIcon = (kind) => {
  switch (kind) {
    case "solar":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="10" y="18" width="44" height="26" rx="4"></rect>
          <path d="M18 18V44M28 18V44M38 18V44M46 18V44M10 26H54M10 36H54"></path>
          <path d="M32 44V54M24 54H40"></path>
        </svg>
      `;
    case "grid":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <path d="M32 8L20 22H26L18 54H46L38 22H44L32 8Z"></path>
          <path d="M25 30H39M23 38H41M21 46H43"></path>
        </svg>
      `;
    case "ups":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="14" y="16" width="36" height="32" rx="6"></rect>
          <path d="M26 24H38M22 32H42M26 40H38"></path>
        </svg>
      `;
    case "home":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <path d="M14 30L32 14L50 30"></path>
          <path d="M20 28V50H44V28"></path>
          <path d="M28 50V38H36V50"></path>
        </svg>
      `;
    case "battery":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="14" y="18" width="34" height="28" rx="5"></rect>
          <rect x="48" y="26" width="4" height="12" rx="2"></rect>
          <path d="M26 32H36M31 27V37"></path>
        </svg>
      `;
    case "generator":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="12" y="18" width="40" height="28" rx="6"></rect>
          <circle cx="24" cy="32" r="6"></circle>
          <path d="M34 26H44M34 32H46M34 38H42"></path>
        </svg>
      `;
    default:
      return "";
  }
};

const CARD_STYLE = `
  :host {
    display: block;
    --bg: radial-gradient(circle at top left, rgba(16, 185, 129, 0.22), transparent 34%),
      radial-gradient(circle at top right, rgba(59, 130, 246, 0.14), transparent 30%),
      linear-gradient(160deg, #06151f 0%, #081b28 34%, #0b1220 100%);
    --panel: linear-gradient(180deg, rgba(9, 18, 32, 0.88), rgba(9, 18, 32, 0.78));
    --panel-border: rgba(148, 163, 184, 0.16);
    --muted: #8ba1ba;
    --text: #ebf2fb;
    --battery-offset: -100px;
    --generator-offset: -80px;
    --ups-offset: -100px;
    --home-offset: -200px;
  }
  ha-card { position: relative; overflow: hidden; border-radius: 28px; color: var(--text); background: var(--bg); box-shadow: 0 24px 52px rgba(2, 6, 23, 0.34); text-shadow: none; }
  .frame { padding: 18px 18px 14px; text-shadow: none; }
  .header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
  .title-wrap { display: grid; gap: 6px; }
  .eyebrow { display: inline-flex; align-items: center; width: fit-content; gap: 6px; padding: 5px 9px; border-radius: 999px; background: rgba(8, 145, 178, 0.12); border: 1px solid rgba(34, 211, 238, 0.18); color: #a5f3fc; font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase; }
  .title { margin: 0; font-size: 24px; line-height: 1.05; font-weight: 800; letter-spacing: -0.04em; }
  .subtitle { color: var(--muted); font-size: 12px; max-width: 640px; }
  .chips { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 8px; max-width: 70%; }
  .chip { min-width: 102px; padding: 8px 10px; border-radius: 14px; border: 1px solid rgba(255, 255, 255, 0.08); background: rgba(15, 23, 42, 0.44); backdrop-filter: blur(10px); display: grid; gap: 3px; }
  .chip__label { font-size: 14px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); }
  .chip__value { font-weight: 600; font-size: 16px; }
  .chip--solar { box-shadow: inset 0 0 0 1px rgba(250, 204, 21, 0.14); background: linear-gradient(180deg, rgba(250, 204, 21, 0.12), rgba(15, 23, 42, 0.42)); }
  .chip--amber { box-shadow: inset 0 0 0 1px rgba(251, 191, 36, 0.14); }
  .chip--mint { box-shadow: inset 0 0 0 1px rgba(34, 197, 94, 0.18); }
  .chip--sky { box-shadow: inset 0 0 0 1px rgba(56, 189, 248, 0.18); }
  .chip--rose { box-shadow: inset 0 0 0 1px rgba(251, 113, 133, 0.18); }
  .chip--slate { box-shadow: inset 0 0 0 1px rgba(148, 163, 184, 0.12); }
  .stage-shell { position: relative; width: 100%; height: var(--scene-height, ${VISIBLE_STAGE_HEIGHT}px); overflow: hidden; border-radius: 26px; border: 1px solid rgba(255, 255, 255, 0.06); box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04); isolation: isolate; contain: layout paint; }
  .stage { position: absolute; left: 50%; top: 0; width: ${STAGE_WIDTH}px; height: ${STAGE_HEIGHT}px; transform: translateX(-50%) translateZ(0) scale(var(--scene-scale, 1)); transform-origin: top center; will-change: transform; backface-visibility: hidden; }
  .stage-surface { position: absolute; inset: 0; border-radius: 26px; background: linear-gradient(180deg, rgba(255, 255, 255, 0.02), rgba(255, 255, 255, 0)), radial-gradient(circle at 18% 12%, rgba(250, 204, 21, 0.06), transparent 18%), radial-gradient(circle at 72% 54%, rgba(16, 185, 129, 0.06), transparent 18%), rgba(7, 14, 24, 0.7); backface-visibility: hidden; }
  .stage-surface::before { content: ""; position: absolute; inset: 0; background-image: linear-gradient(rgba(148, 163, 184, 0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(148, 163, 184, 0.04) 1px, transparent 1px); background-size: 48px 48px; mask-image: radial-gradient(circle at center, black 66%, transparent 100%); }
  .stage-grid { position: absolute; inset: 20px; display: grid; justify-content: center; grid-template-columns: 330px 170px 520px 170px 360px; gap: 32px; height: 968px; }
  .left-rail { display: flex; flex-direction: column; gap: 22px; min-height: 0; }
  .left-rail__spacer { flex: 1 1 auto; min-height: 24px; }
  .right-rail { display: flex; flex-direction: column; gap: 22px; justify-content: space-between; min-height: 0; }
  .right-rail > :nth-child(2) { position: relative; top: var(--ups-offset); }
  .right-rail > :nth-child(3) { position: relative; top: var(--home-offset); }
  .line-rail { position: relative; }
  .line-rail__svg { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; backface-visibility: hidden; transform: translateZ(0); }
  .line-dot { fill: #86efac; filter: drop-shadow(0 0 12px rgba(134, 239, 172, 0.65)); }
  .wire { fill: none; stroke: rgba(100, 116, 139, 0.3); stroke-width: 7; stroke-linecap: round; stroke-linejoin: round; }
  .wire-flow { fill: none; stroke: var(--flow-color); stroke-width: 7; stroke-linecap: round; stroke-linejoin: round; opacity: 0.18; filter: drop-shadow(0 0 8px var(--flow-glow)); }
  .wire-flow--active { opacity: 0.92; }
  .wire-flow--reverse { opacity: 0.92; }
  .node { position: relative; border-radius: 22px; border: 1px solid var(--panel-border); background: var(--panel); backdrop-filter: blur(14px); box-shadow: 0 18px 28px rgba(2, 6, 23, 0.18); text-shadow: none; overflow: hidden; backface-visibility: hidden; transform: translateZ(0); }
  .node[data-entity] { cursor: pointer; }
  .node--solar { background: linear-gradient(180deg, rgba(32, 25, 9, 0.58), rgba(9, 18, 32, 0.82)); }
  .node--battery { background: linear-gradient(180deg, rgba(8, 20, 28, 0.88), rgba(9, 18, 32, 0.84)); }
  .node--inverter { background: linear-gradient(180deg, rgba(10, 16, 28, 0.9), rgba(9, 18, 32, 0.78)); }
  .node--grid { background: linear-gradient(180deg, rgba(12, 19, 33, 0.9), rgba(9, 18, 32, 0.8)); }
  .node--ups { background: linear-gradient(180deg, rgba(10, 18, 34, 0.9), rgba(9, 18, 32, 0.8)); }
  .node--home { background: linear-gradient(180deg, rgba(12, 24, 18, 0.72), rgba(9, 18, 32, 0.8)); }
  .card-pad { padding: 22px 22px 20px; height: auto; min-height: 0; }
  .node__head { display: flex; justify-content: space-between; gap: 14px; align-items: flex-start; margin-bottom: 14px; }
  .node__eyebrow { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; }
  .node__title { margin-top: 5px; font-size: 20px; font-weight: 600; letter-spacing: -0.03em; }
  .node__hero { font-size: 32px; font-weight: 600; line-height: 1; letter-spacing: -0.04em; }
  .node__subhero { margin-top: 6px; color: var(--muted); font-size: 14px; }
  .node--icon .icon-badge { position: absolute; top: 20px; right: 20px; z-index: 3; }
  .node--battery.node--icon .icon-badge { position: absolute; top: 20px; right: 20px; }
  .icon-badge { width: 52px; height: 52px; border-radius: 16px; display: grid; place-items: center; background: rgba(255, 255, 255, 0.04); border: 1px solid rgba(255, 255, 255, 0.08); }
  .icon-badge svg { width: 30px; height: 30px; stroke: currentColor; fill: none; stroke-width: 2.6; stroke-linecap: round; stroke-linejoin: round; }
  .icon-badge--solar { color: #facc15; background: linear-gradient(180deg, rgba(250, 204, 21, 0.18), rgba(250, 204, 21, 0.04)); }
  .icon-badge--grid { color: #34d399; }
  .icon-badge--ups { color: #60a5fa; }
  .icon-badge--home { color: #a3e635; }
  .icon-badge--battery { color: #38bdf8; }
  .icon-badge--generator { color: #f97316; }
  .compact-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .mini { padding: 10px 12px; border-radius: 14px; background: rgba(255, 255, 255, 0.03); border: 1px solid rgba(255, 255, 255, 0.04); }
  .mini__label { display: block; color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 6px; }
  .mini__value { font-weight: 700; font-size: 16px; line-height: 1.28; }
  .mini__value--tight { font-size: 15px; }
  .device-wrap { display: grid; grid-template-columns: 200px 1fr; gap: 20px; align-items: center; }
  .device-art { width: 100%; max-height: 360px; object-fit: contain; filter: drop-shadow(0 12px 22px rgba(2, 6, 23, 0.28)); }
  .device-art--battery { max-height: 180px; }
  .detail-stack { display: grid; gap: 10px; }
  .phase-line { display: flex; justify-content: space-between; gap: 12px; padding: 10px 12px; border-radius: 14px; background: rgba(255, 255, 255, 0.03); border: 1px solid rgba(255, 255, 255, 0.04); }
  .phase-line__label { color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; white-space: nowrap; }
  .phase-line__value { font-size: 15px; font-weight: 700; text-align: right; line-height: 1.3; }
  .node-foot { display: flex; gap: 8px; flex-wrap: wrap; }
  .tag { padding: 6px 9px; border-radius: 999px; font-size: 10px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; background: rgba(255, 255, 255, 0.05); border: 1px solid rgba(255, 255, 255, 0.07); }
  .tag--green { color: #86efac; border-color: rgba(34, 197, 94, 0.2); }
  .tag--blue { color: #7dd3fc; border-color: rgba(56, 189, 248, 0.2); }
  .tag--rose { color: #fda4af; border-color: rgba(251, 113, 133, 0.2); }
  .tag--amber { color: #fde68a; border-color: rgba(250, 204, 21, 0.2); }
  .hero-small { font-size: 32px; font-weight: 600; line-height: 1; letter-spacing: -0.04em; }
  .helper { color: var(--muted); font-size: 12px; line-height: 1.4; }
  .center-rail { position: relative; display: flex; flex-direction: column; justify-content: space-between; padding: 84px 0 22px; min-height: 0; }
  .center-rail__svg { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
  .generator-node { margin-top: 26px; position: relative; top: var(--generator-offset); }
  .left-rail > .node--battery { position: relative; top: var(--battery-offset); }
  .missing { margin-top: 14px; padding: 12px 14px; border-radius: 16px; background: rgba(251, 113, 133, 0.08); border: 1px solid rgba(251, 113, 133, 0.16); color: #fecdd3; font-size: 13px; }
  .entity-map { margin-top: 14px; display: grid; gap: 8px; border-radius: 18px; padding: 14px; background: rgba(15, 23, 42, 0.5); border: 1px solid rgba(148, 163, 184, 0.12); }
  .entity-map__title { font-size: 12px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.08em; margin-bottom: 4px; }
  .map-row { display: flex; justify-content: space-between; gap: 16px; font-size: 12px; padding: 8px 10px; border-radius: 12px; background: rgba(255, 255, 255, 0.02); }
  .map-row__key { color: #cbd5e1; font-weight: 700; }
  .map-row__value { color: var(--muted); text-align: right; word-break: break-all; }
  .loading { padding: 24px; }
  @media (max-width: 1100px) {
    .header { flex-direction: column; }
    .chips { justify-content: flex-start; }
  }
  @media (max-height: 900px) {
    .stage-grid { inset: 16px; gap: 24px; }
    .card-pad { padding: 18px 18px 16px; }
  }
`;

class JinkoPowerFlowCard extends HTMLElement {
  constructor() {
    super();
    this._config = { ...DEFAULT_CONFIG };
    this._resolved = {};
    this._hass = null;
    this._sceneScale = 1;
    this._sceneHeight = VISIBLE_STAGE_HEIGHT;
    this._resizeObserver = null;
    this._resizeFrame = 0;
    this.attachShadow({ mode: "open" });
    this.shadowRoot.addEventListener("click", (event) => {
      const target = event.composedPath().find((node) => node instanceof HTMLElement && node.dataset && node.dataset.entity);
      const entityId = target?.dataset?.entity;
      if (entityId) {
        this._fire("hass-more-info", { entityId });
      }
    });
  }

  connectedCallback() {
    this._ensureResizeObserver();
  }

  disconnectedCallback() {
    if (this._resizeFrame) {
      cancelAnimationFrame(this._resizeFrame);
      this._resizeFrame = 0;
    }
    if (this._resizeObserver) {
      this._resizeObserver.disconnect();
      this._resizeObserver = null;
    }
  }

  setConfig(config) {
    if (!config || typeof config !== "object") {
      throw new Error("Card configuration is required");
    }
    this._config = {
      ...DEFAULT_CONFIG,
      ...config,
      entities: { ...DEFAULT_CONFIG.entities, ...(config.entities || {}) },
    };
    this._resolved = {};
    this._render();
  }

  set hass(hass) {
    this._hass = hass;
    this._resolveEntities();
    this._render();
  }

  getCardSize() {
    return 11;
  }

  static getStubConfig() {
    return { type: `custom:${CARD_TAG}` };
  }

  _ensureResizeObserver() {
    if (this._resizeObserver) return;
    this._resizeObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect?.width || this.clientWidth || STAGE_WIDTH;
      if (this._resizeFrame) {
        cancelAnimationFrame(this._resizeFrame);
      }
      this._resizeFrame = requestAnimationFrame(() => {
        this._resizeFrame = 0;
        this._updateSceneScale(width);
      });
    });
    this._resizeObserver.observe(this);
    this._updateSceneScale(this.clientWidth || STAGE_WIDTH);
  }

  _updateSceneScale(width) {
    const safeWidth = Number.isFinite(width) && width > 0 ? width : STAGE_WIDTH;
    const nextScale = Math.min(safeWidth / STAGE_WIDTH, 1);
    const nextHeight = VISIBLE_STAGE_HEIGHT * nextScale;
    if (Math.abs(nextScale - this._sceneScale) < 0.0001 && Math.abs(nextHeight - this._sceneHeight) < 0.5) {
      return;
    }
    this._sceneScale = nextScale;
    this._sceneHeight = nextHeight;
    const shell = this.shadowRoot?.querySelector(".stage-shell");
    if (shell) {
      shell.style.setProperty("--scene-scale", String(this._sceneScale));
      shell.style.setProperty("--scene-height", `${this._sceneHeight}px`);
    }
  }

  _fire(type, detail) {
    this.dispatchEvent(
      new Event(type, {
        bubbles: true,
        cancelable: false,
        composed: true,
        detail,
      }),
    );
  }

  _resolveEntities() {
    if (!this._hass) return;
    const resolved = {};
    for (const key of Object.keys(ENTITY_DEFINITIONS)) {
      resolved[key] = this._resolveEntity(key);
    }
    this._resolved = resolved;
  }

  _resolveEntity(key) {
    const override = this._config.entities?.[key];
    if (override && this._hass.states[override]) {
      return override;
    }

    const descriptor = ENTITY_DEFINITIONS[key];
    if (!descriptor) return null;

    let best = null;
    for (const [entityId, stateObj] of Object.entries(this._hass.states)) {
      if (!entityId.startsWith("sensor.")) continue;
      const friendly = normalizeText(stateObj.attributes?.friendly_name || "");
      const normalizedEntityId = normalizeText(entityId);
      let score = 0;

      for (const [index, name] of descriptor.names.entries()) {
        const normalizedName = normalizeText(name);
        if (!normalizedName) continue;
        if (friendly === normalizedName) {
          score = Math.max(score, 300 - index);
        } else if (friendly.includes(normalizedName)) {
          score = Math.max(score, 180 - index);
        }
        if (normalizedEntityId.includes(normalizedName)) {
          score = Math.max(score, 120 - index);
        }
      }

      if (score > 0 && (!best || score > best.score)) {
        best = { entityId, score };
      }
    }

    return best?.entityId || null;
  }

  _state(key) {
    const entityId = this._resolved?.[key];
    return entityId ? this._hass?.states?.[entityId] : null;
  }

  _value(key) {
    return toNumber(this._state(key));
  }

  _assetUrl(file) {
    return new URL(`./assets/${file}`, import.meta.url).toString();
  }

  _buildData() {
    const pv1 = {
      title: "PV Field 1",
      power: this._value("pv1_power"),
      voltage: this._value("pv1_voltage"),
      current: this._value("pv1_current"),
      entity: this._resolved.pv1_power || this._resolved.pv1_voltage || this._resolved.pv1_current,
    };
    const pv2 = {
      title: "PV Field 2",
      power: this._value("pv2_power"),
      voltage: this._value("pv2_voltage"),
      current: this._value("pv2_current"),
      entity: this._resolved.pv2_power || this._resolved.pv2_voltage || this._resolved.pv2_current,
    };

    const gridPhases = [1, 2, 3].map((phase) => ({
      label: `L${phase}`,
      voltage: this._value(`grid_l${phase}_voltage`),
      current: this._value(`grid_l${phase}_current`),
      power: this._value(`grid_l${phase}_power`),
      entity: this._resolved[`grid_l${phase}_power`] || this._resolved[`grid_l${phase}_voltage`] || this._resolved[`grid_l${phase}_current`],
    }));

    const backupPhases = [1, 2, 3].map((phase) => {
      const voltage = this._value(`home_l${phase}_voltage`);
      const power = this._value(`home_l${phase}_power`);
      const derivedCurrent = Number.isFinite(power) && Number.isFinite(voltage) && voltage !== 0 ? Math.abs(power) / voltage : null;
      return {
        label: `L${phase}`,
        voltage,
        current: derivedCurrent,
        power,
        entity: this._resolved[`home_l${phase}_power`] || this._resolved[`home_l${phase}_voltage`],
      };
    });

    const inverterPhases = [1, 2, 3].map((phase) => ({
      label: `L${phase}`,
      voltage: this._value(`inverter_l${phase}_voltage`),
      current: this._value(`inverter_l${phase}_current`),
      power: this._value(`inverter_l${phase}_power`),
      entity: this._resolved[`inverter_l${phase}_power`] || this._resolved[`inverter_l${phase}_voltage`] || this._resolved[`inverter_l${phase}_current`],
    }));
    const generatorPhases = [1, 2, 3].map((phase) => ({
      label: `L${phase}`,
      voltage: this._value(`generator_l${phase}_voltage`),
      power: this._value(`generator_l${phase}_power`),
      entity: this._resolved[`generator_l${phase}_power`] || this._resolved[`generator_l${phase}_voltage`],
    }));

    const pvTotalPower = first(this._value("pv_total_power"), sum([pv1.power, pv2.power]));
    const totalConsumptionPower = first(this._value("home_total_power"), sum(backupPhases.map((phase) => phase.power)));
    const backupPhaseTotalPower = sum(backupPhases.map((phase) => phase.power));
    const gridTotalPower = first(this._value("grid_total_power"), sum(gridPhases.map((phase) => phase.power)));
    const inverterTotalPower = first(this._value("inverter_total_power"), sum(inverterPhases.map((phase) => phase.power)));
    const batteryPower = this._value("battery_power");
    const batteryChargeIsNegative = this._config.battery_negative_is_charging !== false;

    let batteryMode = "Idle";
    if (Number.isFinite(batteryPower) && Math.abs(batteryPower) >= 20) {
      const charging = batteryChargeIsNegative ? batteryPower < 0 : batteryPower > 0;
      batteryMode = charging ? "Charging" : "Discharging";
    }

    const batterySoc = this._value("battery_soc");
    const batteryCapacityKwh = Number(this._config.battery_capacity_kwh) || DEFAULT_CONFIG.battery_capacity_kwh;
    const batteryAvailableKwh = Number.isFinite(batterySoc) ? (batteryCapacityKwh * batterySoc) / 100 : null;
    const batteryRuntimeHours =
      batteryMode === "Discharging" && Number.isFinite(batteryAvailableKwh) && Math.abs(batteryPower) > 50
        ? batteryAvailableKwh / (Math.abs(batteryPower) / 1000)
        : null;

    const inverterAverageVoltage = average(inverterPhases.map((phase) => phase.voltage));
    const upsTotalPower = this._value("ups_total_power");
    const upsEstimatedCurrent =
      Number.isFinite(upsTotalPower) && Number.isFinite(inverterAverageVoltage) && inverterAverageVoltage !== 0
        ? Math.abs(upsTotalPower) / inverterAverageVoltage
        : null;

    const gridMode = !Number.isFinite(gridTotalPower) || Math.abs(gridTotalPower) < 20 ? "Balanced" : gridTotalPower > 0 ? "Importing" : "Exporting";
    const actualHomeLoadPower =
      Number.isFinite(totalConsumptionPower) && Number.isFinite(backupPhaseTotalPower)
        ? Math.max(totalConsumptionPower - backupPhaseTotalPower, 0)
        : null;
    const actualHomeVoltage = average(gridPhases.map((phase) => phase.voltage));
    const actualHomeCurrent =
      Number.isFinite(actualHomeLoadPower) && Number.isFinite(actualHomeVoltage) && actualHomeVoltage !== 0
        ? actualHomeLoadPower / actualHomeVoltage
        : null;

    return {
      pv1,
      pv2,
      pvTotalPower,
      pvDailyEnergy: this._value("pv_daily_energy"),
      grid: {
        totalPower: gridTotalPower,
        mode: gridMode,
        frequency: this._value("grid_frequency"),
        buyToday: this._value("grid_buy_today"),
        sellToday: this._value("grid_sell_today"),
        averageVoltage: average(gridPhases.map((phase) => phase.voltage)),
        totalCurrent: sum(gridPhases.map((phase) => Math.abs(phase.current))),
        phases: gridPhases,
        entity: this._resolved.grid_total_power,
      },
      home: {
        totalPower: actualHomeLoadPower,
        totalConsumptionPower,
        backupPhaseTotalPower,
        dailyEnergy: this._value("home_daily_energy"),
        frequency: this._value("home_frequency"),
        averageVoltage: actualHomeVoltage,
        totalCurrent: actualHomeCurrent,
        phases: backupPhases,
        entity: this._resolved.home_total_power,
      },
      ups: {
        totalPower: upsTotalPower,
        estimatedCurrent: upsEstimatedCurrent,
        averageVoltage: average(backupPhases.map((phase) => phase.voltage)),
        phases: backupPhases,
        entity: this._resolved.ups_total_power,
      },
      battery: {
        voltage: this._value("battery_voltage"),
        current: this._value("battery_current"),
        power: batteryPower,
        soc: batterySoc,
        temp: this._value("battery_temp"),
        mode: batteryMode,
        chargeToday: this._value("battery_charge_today"),
        dischargeToday: this._value("battery_discharge_today"),
        capacityKwh: batteryCapacityKwh,
        availableKwh: batteryAvailableKwh,
        runtimeHours: batteryRuntimeHours,
        entity: this._resolved.battery_power || this._resolved.battery_soc,
      },
      inverter: {
        totalPower: inverterTotalPower,
        frequency: this._value("inverter_frequency"),
        powerFactor: this._value("power_factor"),
        dcTemp: this._value("dc_temperature"),
        phases: inverterPhases,
        entity: this._resolved.inverter_total_power,
      },
      generator: {
        totalPower: first(this._value("generator_total_power"), sum(generatorPhases.map((phase) => phase.power))),
        dailyEnergy: this._value("generator_daily_energy"),
        dailyRuntime: this._value("generator_daily_runtime"),
        phases: generatorPhases,
        entity: this._resolved.generator_total_power || this._resolved.generator_daily_energy,
      },
      headerStats: [
        { label: "Daily Buy", value: formatEnergy(this._value("grid_buy_today")), tone: "import" },
        { label: "Daily Sell", value: formatEnergy(this._value("grid_sell_today")), tone: "export" },
        { label: "Daily Production", value: formatEnergy(this._value("pv_daily_energy")), tone: "production" },
        { label: "Daily Consumption", value: formatEnergy(this._value("home_daily_energy")), tone: "consumption" },
        { label: "Daily Charging", value: formatEnergy(this._value("battery_charge_today")), tone: "charge" },
        { label: "Daily Generator", value: formatEnergy(this._value("generator_daily_energy")), tone: "import" },
      ],
      missing: CRITICAL_KEYS.filter((key) => !this._resolved[key]),
    };
  }

  _renderMetricRow(label, values, entityId) {
    return `<div class="metric-row" ${entityId ? `data-entity="${entityId}"` : ""}><span class="metric-row__label">${escapeHtml(label)}</span><span class="metric-row__value">${values.filter(Boolean).join(" | ") || "--"}</span></div>`;
  }

  _renderChip(label, value, tone) {
    return `<div class="chip chip--${statusTone(tone)}"><span class="chip__label">${escapeHtml(label)}</span><span class="chip__value">${escapeHtml(value)}</span></div>`;
  }

  _render() {
    if (!this.shadowRoot) return;
    if (!this._config || !this._hass) {
      this.shadowRoot.innerHTML = `<style>${CARD_STYLE}</style><ha-card><div class="loading">Waiting for Home Assistant state...</div></ha-card>`;
      return;
    }

    const data = this._buildData();
    const solar1Active = Number.isFinite(data.pv1.power) && data.pv1.power > 20;
    const solar2Active = Number.isFinite(data.pv2.power) && data.pv2.power > 20;
    const inverterToBusActive = Number.isFinite(data.inverter.totalPower) && Math.abs(data.inverter.totalPower) > 20;
    const batteryActive = Number.isFinite(data.battery.power) && Math.abs(data.battery.power) > 20;
    const batteryDirection = data.battery.mode === "Charging" ? "reverse" : "forward";
    const generatorActive =
      (Number.isFinite(data.generator.totalPower) && Math.abs(data.generator.totalPower) > 20) ||
      (Number.isFinite(data.generator.dailyEnergy) && data.generator.dailyEnergy > 0);
    const gridDirection = data.grid.mode === "Importing" ? "reverse" : "forward";
    const gridActive = data.grid.mode !== "Balanced";
    const homeActive = Number.isFinite(data.home.totalPower) && Math.abs(data.home.totalPower) > 20;
    const upsActive = Number.isFinite(data.ups.totalPower) && Math.abs(data.ups.totalPower) > 20;

    const chips = data.headerStats.map((stat) => this._renderChip(stat.label, stat.value, stat.tone)).join("");
    const entityMap = Object.entries(this._resolved)
      .filter(([, entityId]) => entityId)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(
        ([key, entityId]) =>
          `<div class="map-row"><span class="map-row__key">${escapeHtml(key)}</span><span class="map-row__value">${escapeHtml(entityId)}</span></div>`,
      )
      .join("");
    const phasePlain = (values, digits = 0) => values.map((value) => (Number.isFinite(value) ? formatNumber(value, digits) : "--")).join(" / ");
    const gridVoltages = `${phasePlain(data.grid.phases.map((phase) => phase.voltage), 1)} V`;
    const gridCurrents = `${phasePlain(data.grid.phases.map((phase) => phase.current), 2)} A`;
    const gridPowers = `${phasePlain(data.grid.phases.map((phase) => phase.power), 0)} W`;
    const inverterVoltages = `${phasePlain(data.inverter.phases.map((phase) => phase.voltage), 0)} V`;
    const inverterCurrents = `${phasePlain(data.inverter.phases.map((phase) => phase.current), 2)} A`;
    const inverterPowers = `${phasePlain(data.inverter.phases.map((phase) => phase.power), 0)} W`;
    const backupVoltages = `${phasePlain(data.ups.phases.map((phase) => phase.voltage), 1)} V`;
    const backupPowers = `${phasePlain(data.ups.phases.map((phase) => phase.power), 0)} W`;
    const generatorVoltages = `${phasePlain(data.generator.phases.map((phase) => phase.voltage), 1)} V`;
    const generatorPowers = `${phasePlain(data.generator.phases.map((phase) => phase.power), 0)} W`;

    this.shadowRoot.innerHTML = `
      <style>${CARD_STYLE}</style>
      <ha-card>
        <div class="frame">
          <div class="header">
            <div class="title-wrap">
              <div class="eyebrow">Jinko ESS / Single-site custom card</div>
              <h1 class="title">${escapeHtml(this._config.title)}</h1>
              <div class="subtitle">Animated hybrid flow view for PV, grid, home load, UPS load, battery, and inverter output using your MQTT-discovered Jinko metrics.</div>
            </div>
            <div class="chips">${chips}</div>
          </div>

          <div class="stage-shell" style="--scene-scale:${this._sceneScale}; --scene-height:${this._sceneHeight}px;">
            <div class="stage">
              <div class="stage-surface"></div>
              <div class="stage-grid">
                <div class="left-rail">
                  <div class="node node--solar node--icon card-pad" ${data.pv1.entity ? `data-entity="${data.pv1.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">PV field 1</div>
                      </div>
                      <div class="icon-badge icon-badge--solar">${renderIcon("solar")}</div>
                    </div>
                    <div class="node__hero">${formatPower(data.pv1.power)}</div>
                    <div class="node__subhero">${formatVoltage(data.pv1.voltage)} | ${formatCurrent(data.pv1.current)}</div>
                    <div class="compact-grid">
                      <div class="mini"><span class="mini__label">Voltage</span><span class="mini__value">${formatVoltage(data.pv1.voltage)}</span></div>
                      <div class="mini"><span class="mini__label">Current</span><span class="mini__value">${formatCurrent(data.pv1.current)}</span></div>
                    </div>
                  </div>

                  <div class="node node--solar node--icon card-pad" ${data.pv2.entity ? `data-entity="${data.pv2.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">PV field 2</div>
                      </div>
                      <div class="icon-badge icon-badge--solar">${renderIcon("solar")}</div>
                    </div>
                    <div class="node__hero">${formatPower(data.pv2.power)}</div>
                    <div class="node__subhero">${formatVoltage(data.pv2.voltage)} | ${formatCurrent(data.pv2.current)}</div>
                    <div class="compact-grid">
                      <div class="mini"><span class="mini__label">Voltage</span><span class="mini__value">${formatVoltage(data.pv2.voltage)}</span></div>
                      <div class="mini"><span class="mini__label">Current</span><span class="mini__value">${formatCurrent(data.pv2.current)}</span></div>
                    </div>
                  </div>

                  <div class="left-rail__spacer"></div>

                  <div class="node node--battery node--icon card-pad" ${data.battery.entity ? `data-entity="${data.battery.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">Battery</div>
                      </div>
                      <div class="icon-badge icon-badge--battery">${renderIcon("battery")}</div>
                    </div>
                    <div class="hero-small">${formatPercent(data.battery.soc)}</div>
                    <div class="detail-stack" style="margin-top:12px;">
                       <div class="phase-line"><span class="phase-line__label">Live</span><span class="phase-line__value">${formatVoltage(data.battery.voltage)} | ${formatCurrent(data.battery.current)} | ${formatPower(data.battery.power)}</span></div>
                       <div class="phase-line"><span class="phase-line__label">Energy</span><span class="phase-line__value">${formatEnergy(data.battery.availableKwh)} | ${formatTemperature(data.battery.temp)}</span></div>
                    </div>
                  </div>
                </div>

                <div class="line-rail">
                  <svg class="line-rail__svg" viewBox="0 0 170 968" preserveAspectRatio="none">
                    ${renderFlowPath({ d: "M0 110C42 110 76 110 116 158C138 183 152 200 170 220", active: solar1Active, direction: "forward", power: data.pv1.power, color: "#FACC15", glow: "#FDE047" })}
                    ${renderFlowPath({ d: "M0 352C56 352 102 352 170 352", active: solar2Active, direction: "forward", power: data.pv2.power, color: "#F59E0B", glow: "#FCD34D" })}
                    ${renderFlowPath({ d: "M0 746H92V426H170", active: batteryActive, direction: batteryDirection, power: data.battery.power, color: data.battery.mode === "Charging" ? "#38BDF8" : "#FB7185", glow: data.battery.mode === "Charging" ? "#7DD3FC" : "#FDA4AF" })}
                  </svg>
                </div>

                <div class="center-rail">
                  <svg class="center-rail__svg" viewBox="0 0 520 968" preserveAspectRatio="none">
                    ${renderFlowPath({ d: "M260 500v70", active: generatorActive, direction: "forward", power: data.generator.totalPower, color: "#F97316", glow: "#FDBA74" })}
                  </svg>
                  <div class="node node--inverter card-pad" ${data.inverter.entity ? `data-entity="${data.inverter.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">Inverter core</div>
                        <div class="node__title">Hybrid Inverter</div>
                      </div>
                      <div>
                        <div class="node__hero">${formatPower(data.inverter.totalPower)}</div>
                        <div class="node__subhero">${formatFrequency(data.inverter.frequency)} | PF ${formatFactor(data.inverter.powerFactor)}</div>
                      </div>
                    </div>
                    <div class="device-wrap">
                      <img class="device-art" src="${this._assetUrl("jks-12h-ei.svg")}" alt="Inverter">
                      <div class="detail-stack">
                        <div class="phase-line"><span class="phase-line__label">PV total</span><span class="phase-line__value">${formatPower(data.pvTotalPower)}</span></div>
                        <div class="phase-line"><span class="phase-line__label">AC voltage</span><span class="phase-line__value">${inverterVoltages}</span></div>
                        <div class="phase-line"><span class="phase-line__label">AC current</span><span class="phase-line__value">${inverterCurrents}</span></div>
                        <div class="phase-line"><span class="phase-line__label">AC power</span><span class="phase-line__value">${inverterPowers}</span></div>
                        <div class="phase-line"><span class="phase-line__label">DC temp</span><span class="phase-line__value">${formatTemperature(data.inverter.dcTemp)}</span></div>
                       
                      </div>
                    </div>
                  </div>

                  <div class="node node--icon card-pad generator-node" ${data.generator.entity ? `data-entity="${data.generator.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">Generator</div>
                        <div class="node__title">Backup Generator</div>
                      </div>
                      <div class="icon-badge icon-badge--generator">${renderIcon("generator")}</div>
                    </div>
                    <div class="hero-small">${formatPower(data.generator.totalPower)}</div>
                    <div class="node__subhero">${formatEnergy(data.generator.dailyEnergy)} today | ${Number.isFinite(data.generator.dailyRuntime) ? `${formatNumber(data.generator.dailyRuntime, 1)} h runtime` : "--"}</div>
                    <div class="detail-stack" style="margin-top:12px;">
                      <div class="phase-line"><span class="phase-line__label">Voltage</span><span class="phase-line__value">${generatorVoltages}</span></div>
                      <div class="phase-line"><span class="phase-line__label">Power</span><span class="phase-line__value">${generatorPowers}</span></div>
                    </div>
                  </div>
                </div>

                <div class="line-rail">
                  <svg class="line-rail__svg" viewBox="0 0 170 968" preserveAspectRatio="none">
                    ${renderFlowPath({ d: "M0 390H82V134H170", active: upsActive, direction: "forward", power: data.ups.totalPower, color: "#60A5FA", glow: "#93C5FD" })}
                    ${renderFlowPath({ d: "M0 390H170", active: gridActive, direction: gridDirection, power: data.grid.totalPower, color: data.grid.mode === "Importing" ? "#FBBF24" : "#34D399", glow: data.grid.mode === "Importing" ? "#FDE68A" : "#6EE7B7" })}
                    ${renderFlowPath({ d: "M0 390H82V700H170", active: homeActive, direction: "forward", power: data.home.totalPower, color: "#A3E635", glow: "#BEF264" })}
                    <circle class="line-dot" cx="82" cy="390" r="8"></circle>
                  </svg>
                </div>

                <div class="right-rail">
                  <div class="node node--grid node--icon card-pad" ${data.grid.entity ? `data-entity="${data.grid.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">Grid</div>
                      </div>
                      <div class="icon-badge icon-badge--grid">${renderIcon("grid")}</div>
                    </div>
                    <div class="hero-small">${formatPower(data.grid.totalPower, { signed: true })}</div>
                    <div class="node__subhero">${escapeHtml(data.grid.mode)} | ${formatFrequency(data.grid.frequency)}</div>
                    <div class="detail-stack" style="margin-top:12px;">
                      <div class="phase-line"><span class="phase-line__label">Voltage</span><span class="phase-line__value">${gridVoltages}</span></div>
                      <div class="phase-line"><span class="phase-line__label">Current</span><span class="phase-line__value">${gridCurrents}</span></div>
                      <div class="phase-line"><span class="phase-line__label">Power</span><span class="phase-line__value">${gridPowers}</span></div>
                    </div>
                  </div>

                  <div class="node node--ups node--icon card-pad" ${data.ups.entity ? `data-entity="${data.ups.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">UPS load</div>
                      </div>
                      <div class="icon-badge icon-badge--ups">${renderIcon("ups")}</div>
                    </div>
                    <div class="hero-small">${formatPower(data.ups.totalPower)}</div>
                    <div class="node__subhero">${formatCurrent(data.ups.estimatedCurrent)} estimated current</div>
                    <div class="detail-stack" style="margin-top:12px;">
                      <div class="phase-line"><span class="phase-line__label">Voltage</span><span class="phase-line__value">${backupVoltages}</span></div>
                      <div class="phase-line"><span class="phase-line__label">Power</span><span class="phase-line__value">${backupPowers}</span></div>
                    </div>
                  </div>

                  <div class="node node--home node--icon card-pad" ${data.home.entity ? `data-entity="${data.home.entity}"` : ""}>
                    <div class="node__head">
                      <div>
                        <div class="node__eyebrow">Parallel home load</div>
                      </div>
                      <div class="icon-badge icon-badge--home">${renderIcon("home")}</div>
                    </div>
                    <div class="hero-small">${formatPower(data.home.totalPower)}</div>
                    <div class="node__subhero">${formatCurrent(data.home.totalCurrent)} | calculated branch</div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          ${data.missing.length ? `<div class="missing">Missing critical sensors for autodiscovery: ${data.missing.map(escapeHtml).join(", ")}. If any values are blank, add explicit overrides under <code>entities:</code>.</div>` : ""}
          ${this._config.show_entity_map ? `<div class="entity-map"><div class="entity-map__title">Resolved entity map</div>${entityMap || '<div class="helper">No entities resolved yet.</div>'}</div>` : ""}
        </div>
      </ha-card>
    `;
    this._ensureResizeObserver();
    this._updateSceneScale(this.clientWidth || STAGE_WIDTH);
  }
}

if (!customElements.get(CARD_TAG)) {
  customElements.define(CARD_TAG, JinkoPowerFlowCard);
}

window.customCards = window.customCards || [];
window.customCards.push({
  type: CARD_TAG,
  name: "Jinko Power Flow Card",
  description: "Large animated Jinko ESS flow card for a single installation.",
});
