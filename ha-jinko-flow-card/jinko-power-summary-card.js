const CARD_TAG = "jinko-power-summary-card";
const DEFAULT_CONFIG = {
  title: "Jinko ESS Overview",
  battery_capacity_kwh: 21.31,
  battery_negative_is_charging: true,
  show_entity_map: false,
  navigation_path: "",
  entities: {},
};

const ENTITY_DEFINITIONS = {
  pv_total_power: { names: ["Total Solar Power", "Solar"] },
  pv_daily_energy: { names: ["PV daily power generation (active)", "Daily Production (Active)"] },
  pv1_power: { names: ["DC Power PV1"] },
  pv2_power: { names: ["DC Power PV2"] },
  grid_total_power: { names: ["Total Grid Power", "Internal Power"] },
  grid_buy_today: { names: ["Daily Energy Buy"] },
  grid_sell_today: { names: ["Daily energy sell"] },
  grid_l1_voltage: { names: ["Grid Voltage L1"] },
  grid_l2_voltage: { names: ["Grid Voltage L2"] },
  grid_l3_voltage: { names: ["Grid Voltage L3"] },
  home_total_power: { names: ["Total Consumption Power"] },
  home_daily_energy: { names: ["Daily Consumption"] },
  home_l1_power: { names: ["Load Power L1", "Load phase power A"] },
  home_l2_power: { names: ["Load Power L2", "Load phase power B"] },
  home_l3_power: { names: ["Load Power L3", "Load phase power C"] },
  ups_total_power: { names: ["UPS Load Power"] },
  generator_daily_energy: { names: ["Daily Production Generator"] },
  battery_power: { names: ["Battery Power"] },
  battery_soc: { names: ["SoC", "BMS_SOC"] },
  battery_charge_today: { names: ["Daily Charging Energy"] },
  inverter_l1_voltage: { names: ["AC Voltage R/U/A"] },
  inverter_l2_voltage: { names: ["AC Voltage S/V/B"] },
  inverter_l3_voltage: { names: ["AC Voltage T/W/C"] },
};

const CRITICAL_KEYS = ["pv_total_power", "grid_total_power", "home_total_power", "ups_total_power", "battery_power", "battery_soc"];

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

const formatEnergy = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 10 ? 1 : 2)} kWh` : "--");
const formatPercent = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 10 ? 0 : 1)}%` : "--");
const formatVoltage = (value) => (Number.isFinite(value) ? `${formatNumber(value, value >= 100 ? 0 : 1)} V` : "--");

const toneClass = (tone) => {
  switch (tone) {
    case "solar":
      return "tone-solar";
    case "grid-in":
      return "tone-amber";
    case "grid-out":
      return "tone-mint";
    case "battery":
      return "tone-sky";
    case "ups":
      return "tone-indigo";
    case "home":
      return "tone-lime";
    default:
      return "tone-slate";
  }
};

const renderIcon = (kind) => {
  switch (kind) {
    case "solar":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <circle cx="32" cy="22" r="9"></circle>
          <path d="M32 7V2M32 42v-5M47 22h5M12 22H7M42.6 11.4l3.6-3.6M17.8 36.2l3.6-3.6M46.2 36.2l-3.6-3.6M21.4 11.4l-3.6-3.6"></path>
          <path d="M16 50h32M20 58h24"></path>
        </svg>
      `;
    case "grid":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <path d="M32 8L20 22H26L18 54H46L38 22H44L32 8Z"></path>
          <path d="M24 30h16M22 38h20M20 46h24"></path>
        </svg>
      `;
    case "battery":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="12" y="18" width="38" height="28" rx="5"></rect>
          <rect x="50" y="26" width="4" height="12" rx="2"></rect>
          <path d="M27 32h8M31 28v8"></path>
        </svg>
      `;
    case "ups":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="14" y="14" width="36" height="36" rx="8"></rect>
          <path d="M26 24h12M22 32h20M26 40h12"></path>
        </svg>
      `;
    case "home":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <path d="M14 30L32 14L50 30"></path>
          <path d="M20 28v22h24V28"></path>
          <path d="M28 50V38h8v12"></path>
        </svg>
      `;
    case "inverter":
      return `
        <svg viewBox="0 0 64 64" aria-hidden="true">
          <rect x="14" y="12" width="36" height="40" rx="8"></rect>
          <circle cx="32" cy="26" r="5"></circle>
          <path d="M24 40h16M24 46h16"></path>
        </svg>
      `;
    default:
      return "";
  }
};

const CARD_STYLE = `
  :host {
    display: block;
    --bg:
      radial-gradient(circle at top left, rgba(250, 204, 21, 0.14), transparent 32%),
      radial-gradient(circle at top right, rgba(34, 197, 94, 0.12), transparent 28%),
      linear-gradient(155deg, #071520 0%, #0a1826 45%, #0d1322 100%);
    --panel: rgba(8, 15, 28, 0.74);
    --panel-strong: rgba(10, 18, 32, 0.88);
    --panel-border: rgba(148, 163, 184, 0.14);
    --muted: #8ea3bb;
    --text: #eef5ff;
    --shadow: 0 20px 44px rgba(2, 6, 23, 0.28);
  }
  ha-card {
    overflow: hidden;
    border-radius: 26px;
    background: var(--bg);
    color: var(--text);
    box-shadow: var(--shadow);
  }
  ha-card.is-navigable {
    cursor: pointer;
  }
  .frame {
    position: relative;
    padding: 20px;
  }
  .frame::before {
    content: "";
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(148, 163, 184, 0.04) 1px, transparent 1px),
      linear-gradient(90deg, rgba(148, 163, 184, 0.04) 1px, transparent 1px);
    background-size: 40px 40px;
    mask-image: radial-gradient(circle at center, black 62%, transparent 100%);
    pointer-events: none;
  }
  .stack {
    position: relative;
    display: grid;
    gap: 16px;
    z-index: 1;
  }
  .header {
    display: grid;
    gap: 14px;
  }
  .title-card,
  .soc-card,
  .metrics,
  .entity-map,
  .empty {
    border-radius: 22px;
    border: 1px solid var(--panel-border);
    backdrop-filter: blur(12px);
  }
  .title-card,
  .soc-card {
    background: linear-gradient(180deg, rgba(10, 18, 31, 0.9), rgba(9, 18, 32, 0.72));
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.04);
  }
  .title-card {
    padding: 18px 18px 16px;
    display: grid;
    gap: 14px;
  }
  .eyebrow {
    width: fit-content;
    padding: 6px 10px;
    border-radius: 999px;
    background: rgba(14, 116, 144, 0.16);
    border: 1px solid rgba(34, 211, 238, 0.14);
    color: #b7f3ff;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .title {
    margin: 0;
    font-size: 28px;
    line-height: 1;
    letter-spacing: -0.05em;
    font-weight: 800;
  }
  .subtitle {
    color: var(--muted);
    font-size: 13px;
    line-height: 1.45;
    max-width: 46ch;
  }
  .chip-row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .chip {
    padding: 8px 11px;
    border-radius: 14px;
    background: rgba(15, 23, 42, 0.48);
    border: 1px solid rgba(255, 255, 255, 0.08);
    display: grid;
    gap: 3px;
    min-width: 108px;
  }
  .chip__label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }
  .chip__value {
    font-size: 15px;
    font-weight: 700;
  }
  .soc-card {
    padding: 18px;
    display: grid;
    gap: 16px;
    align-content: space-between;
  }
  .soc-card__label {
    color: var(--muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .soc-card__value {
    font-size: 46px;
    line-height: 0.95;
    font-weight: 800;
    letter-spacing: -0.05em;
  }
  .soc-card__meta {
    color: var(--muted);
    font-size: 13px;
  }
  .soc-bar {
    height: 12px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.08);
    overflow: hidden;
    border: 1px solid rgba(255, 255, 255, 0.06);
  }
  .soc-bar__fill {
    height: 100%;
    width: var(--soc-width, 0%);
    border-radius: inherit;
    background: linear-gradient(90deg, #38bdf8 0%, #34d399 48%, #facc15 100%);
    box-shadow: 0 0 18px rgba(52, 211, 153, 0.22);
  }
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
    gap: 12px;
  }
  .metric {
    padding: 8px 11px;
    border-radius: 14px;
    background: rgba(15, 23, 42, 0.48);
    border: 1px solid rgba(255, 255, 255, 0.08);
    display: grid;
    gap: 3px;
    min-width: 108px;
  }
  .metric::before {
    content: "";
    position: absolute;
    inset: auto 16px 0 auto;
    width: 96px;
    height: 96px;
    border-radius: 24px;
    background: radial-gradient(circle at center, rgba(255, 255, 255, 0.12), transparent 70%);
    opacity: 0.3;
    pointer-events: none;
  }
  .metric__top {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: flex-start;
  }
  .metric__label {
    color: var(--muted);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .metric__icon {
    width: 44px;
    height: 44px;
    border-radius: 14px;
    display: grid;
    place-items: center;
    border: 1px solid rgba(255, 255, 255, 0.08);
    background: rgba(255, 255, 255, 0.04);
  }
  .metric__icon svg {
    width: 25px;
    height: 25px;
    stroke: currentColor;
    fill: none;
    stroke-width: 2.5;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .metric__value {
    font-size: 15px;
    line-height: 1;
    font-weight: 700;
  }
  .metric__sub {
    margin-top: 8px;
    color: var(--muted);
    font-size: 13px;
  }
  .metric__detail {
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.07);
    color: #d8e5f3;
    font-size: 13px;
    line-height: 1.35;
  }
  .metric--span-2 {
    grid-column: span 2;
  }
  .tone-solar .metric__icon,
  .tone-solar.chip {
    color: #facc15;
    box-shadow: inset 0 0 0 1px rgba(250, 204, 21, 0.1);
  }
  .tone-amber .metric__icon,
  .tone-amber.chip {
    color: #fbbf24;
    box-shadow: inset 0 0 0 1px rgba(251, 191, 36, 0.1);
  }
  .tone-mint .metric__icon,
  .tone-mint.chip {
    color: #34d399;
    box-shadow: inset 0 0 0 1px rgba(52, 211, 153, 0.1);
  }
  .tone-sky .metric__icon,
  .tone-sky.chip {
    color: #38bdf8;
    box-shadow: inset 0 0 0 1px rgba(56, 189, 248, 0.1);
  }
  .tone-indigo .metric__icon,
  .tone-indigo.chip {
    color: #818cf8;
    box-shadow: inset 0 0 0 1px rgba(129, 140, 248, 0.1);
  }
  .tone-lime .metric__icon,
  .tone-lime.chip {
    color: #a3e635;
    box-shadow: inset 0 0 0 1px rgba(163, 230, 53, 0.1);
  }
  .tone-slate .metric__icon,
  .tone-slate.chip {
    color: #cbd5e1;
    box-shadow: inset 0 0 0 1px rgba(203, 213, 225, 0.1);
  }
  .entity-map {
    padding: 14px;
    background: rgba(15, 23, 42, 0.56);
    display: grid;
    gap: 8px;
  }
  .entity-map__title {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }
  .map-row {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    padding: 8px 10px;
    border-radius: 12px;
    background: rgba(255, 255, 255, 0.03);
    font-size: 12px;
  }
  .map-row__key {
    font-weight: 700;
    color: #d7e3f3;
  }
  .map-row__value {
    color: var(--muted);
    text-align: right;
    word-break: break-all;
  }
  .loading {
    padding: 24px;
  }
  [data-entity] {
    cursor: pointer;
  }
  @media (max-width: 1400px) {
    .metric--span-2 {
      grid-column: span 1;
    }
  }
  @media (max-width: 980px) {
    .header {
      grid-template-columns: 1fr;
    }
  }
  @media (max-width: 640px) {
    .frame {
      padding: 14px;
    }
    .title {
      font-size: 24px;
    }
    .soc-card__value {
      font-size: 40px;
    }
    .metric {
      min-height: 132px;
    }
  }
`;

class JinkoPowerSummaryCard extends HTMLElement {
  constructor() {
    super();
    this._config = { ...DEFAULT_CONFIG };
    this._resolved = {};
    this._hass = null;
    this.attachShadow({ mode: "open" });
    this.shadowRoot.addEventListener("click", (event) => {
      if (this._config?.navigation_path) {
        this._navigate(this._config.navigation_path);
        return;
      }
      const target = event.composedPath().find((node) => node instanceof HTMLElement && node.dataset && node.dataset.entity);
      const entityId = target?.dataset?.entity;
      if (entityId) {
        this._fire("hass-more-info", { entityId });
      }
    });
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
    return 5;
  }



  static getStubConfig() {
    return { type: `custom:${CARD_TAG}` };
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

  _navigate(path) {
    if (!path) return;
    history.pushState(null, "", path);
    window.dispatchEvent(new CustomEvent("location-changed", { bubbles: true, composed: true }));
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

  _buildData() {
    const pvTotalPower = first(this._value("pv_total_power"), sum([this._value("pv1_power"), this._value("pv2_power")]));
    const backupPhaseTotalPower = sum([this._value("home_l1_power"), this._value("home_l2_power"), this._value("home_l3_power")]);
    const totalConsumptionPower = first(this._value("home_total_power"), backupPhaseTotalPower);
    const parallelHomePower =
      Number.isFinite(totalConsumptionPower) && Number.isFinite(backupPhaseTotalPower)
        ? Math.max(totalConsumptionPower - backupPhaseTotalPower, 0)
        : null;
    const gridTotalPower = this._value("grid_total_power");
    const batteryPower = this._value("battery_power");
    const batterySoc = this._value("battery_soc");
    const batteryChargeIsNegative = this._config.battery_negative_is_charging !== false;
    const batteryMode =
      !Number.isFinite(batteryPower) || Math.abs(batteryPower) < 20
        ? "Idle"
        : (batteryChargeIsNegative ? batteryPower < 0 : batteryPower > 0)
          ? "Charging"
          : "Discharging";
    const inverterVoltages = [1, 2, 3].map((phase) => this._value(`inverter_l${phase}_voltage`));
    const avgGridVoltage = average([this._value("grid_l1_voltage"), this._value("grid_l2_voltage"), this._value("grid_l3_voltage")]);
    const dailyStats = [
      { label: "Daily Buy", value: this._value("grid_buy_today"), tone: "grid-in" },
      { label: "Daily Sell", value: this._value("grid_sell_today"), tone: "grid-out" },
      { label: "Daily Production", value: this._value("pv_daily_energy"), tone: "solar" },
      { label: "Daily Consumption", value: this._value("home_daily_energy"), tone: "home" },
      { label: "Daily Charging", value: this._value("battery_charge_today"), tone: "battery" },
      { label: "Daily Generator", value: this._value("generator_daily_energy"), tone: "grid-in" },
    ].filter((stat) => Number.isFinite(stat.value) && Math.abs(stat.value) > 0.01);

    const gridMode =
      !Number.isFinite(gridTotalPower) || Math.abs(gridTotalPower) < 20 ? "Balanced" : gridTotalPower > 0 ? "Importing" : "Exporting";

    return {
      solar: {
        power: pvTotalPower,
        entity: this._resolved.pv_total_power || this._resolved.pv1_power || this._resolved.pv2_power,
      },
      grid: {
        power: gridTotalPower,
        mode: gridMode,
        entity: this._resolved.grid_total_power,
      },
      battery: {
        power: batteryPower,
        soc: batterySoc,
        mode: batteryMode,
        entity: this._resolved.battery_power || this._resolved.battery_soc,
      },
      ups: {
        power: this._value("ups_total_power"),
        entity: this._resolved.ups_total_power,
      },
      home: {
        power: parallelHomePower,
        entity: this._resolved.home_total_power,
      },
      inverter: {
        voltages: inverterVoltages,
        averageVoltage: avgGridVoltage,
        entity:
          this._resolved.inverter_l1_voltage || this._resolved.inverter_l2_voltage || this._resolved.inverter_l3_voltage || this._resolved.grid_l1_voltage,
      },
      dailyStats,
      missing: CRITICAL_KEYS.filter((key) => !this._resolved[key]),
    };
  }

  _renderChip(stat) {
    return `<div class="chip ${toneClass(stat.tone)}"><span class="chip__label">${escapeHtml(stat.label)}</span><span class="chip__value">${formatEnergy(stat.value)}</span></div>`;
  }

  _renderMetric({ label, icon, value, sub, detail, tone, entity, span = 1 }) {
    return `
      <div class="metric ${toneClass(tone)} ${span > 1 ? `metric--span-${span}` : ""}" ${entity ? `data-entity="${entity}"` : ""}>
        <div class="metric__top">
          <div class="metric__label">${escapeHtml(label)}</div>
          <div class="metric__icon">${renderIcon(icon)}</div>
        </div>
        <div class="metric__value">${escapeHtml(value)}</div>
        ${sub ? `<div class="metric__sub">${escapeHtml(sub)}</div>` : ""}
      </div>
    `;
  }

  _render() {
    if (!this.shadowRoot) return;
    if (!this._config || !this._hass) {
      this.shadowRoot.innerHTML = `<style>${CARD_STYLE}</style><ha-card><div class="loading">Waiting for Home Assistant state...</div></ha-card>`;
      return;
    }

    const data = this._buildData();
    const socWidth = Number.isFinite(data.battery.soc) ? Math.max(0, Math.min(100, data.battery.soc)) : 0;
    const voltageTriplet = data.inverter.voltages.map((value) => (Number.isFinite(value) ? formatNumber(value, 0) : "--")).join(" / ");
    const chips = data.dailyStats.map((stat) => this._renderChip(stat)).join("");
    const entityMap = Object.entries(this._resolved)
      .filter(([, entityId]) => entityId)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(
        ([key, entityId]) =>
          `<div class="map-row"><span class="map-row__key">${escapeHtml(key)}</span><span class="map-row__value">${escapeHtml(entityId)}</span></div>`,
      )
      .join("");

    const metrics = [
      this._renderMetric({
        label: "Total Solar",
        icon: "solar",
        value: formatPower(data.solar.power),
        sub: chips ? "Live PV generation" : "PV generation right now",
        tone: "solar",
        entity: data.solar.entity,
      }),
      this._renderMetric({
        label: "Grid",
        icon: "grid",
        value: formatPower(data.grid.power, { signed: true }),
        sub: data.grid.mode,
        tone: data.grid.mode === "Exporting" ? "grid-out" : "grid-in",
        entity: data.grid.entity,
      }),
      this._renderMetric({
        label: "Battery",
        icon: "battery",
        value: formatPower(data.battery.power, { signed: true }),
        sub: data.battery.mode +` SOC ${formatPercent(data.battery.soc)}`,
        tone: "battery",
        entity: data.battery.entity,
      }),
    ].join("");
    const metrics2 = [
      this._renderMetric({
        label: "Inverter AC",
        icon: "inverter",
        value: `${voltageTriplet} V`,
        tone: "slate",
        entity: data.inverter.entity,
        span: 1,
      }),
      this._renderMetric({
        label: "UPS Load",
        icon: "ups",
        value: formatPower(data.ups.power),
        tone: "ups",
        entity: data.ups.entity,
      }),
      this._renderMetric({
        label: "Parallel Home",
        icon: "home",
        value: formatPower(data.home.power),
        tone: "home",
        entity: data.home.entity,
      }),
    ].join("");

    this.shadowRoot.innerHTML = `
      <style>${CARD_STYLE}</style>
      <ha-card class="${this._config.navigation_path ? "is-navigable" : ""}">
        <div class="frame">
          <div class="stack">
            <div class="header">
                <div class="eyebrow">Jinko ESS / Summary</div>
                ${chips ? `<div class="chip-row">${chips}</div>` : ""}
            </div>
            <div class="metrics">
              ${metrics}
            </div>
            <div class="metrics">
              ${metrics2}
            </div>

            ${this._config.show_entity_map ? `<div class="entity-map"><div class="entity-map__title">Resolved entity map</div>${entityMap || '<div class="map-row"><span class="map-row__key">info</span><span class="map-row__value">No entities resolved yet.</span></div>'}</div>` : ""}
          </div>
        </div>
      </ha-card>
    `;
  }
}

if (!customElements.get(CARD_TAG)) {
  customElements.define(CARD_TAG, JinkoPowerSummaryCard);
}

window.customCards = window.customCards || [];
window.customCards.push({
  type: CARD_TAG,
  name: "Jinko Power Summary Card",
  description: "Compact Jinko ESS dashboard card for daily stats, SOC, live powers, and inverter AC voltage.",
});
