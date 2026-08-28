import { iconSvg, type IconName } from "./icons";
import { setHiddenIfChanged, setTextContentIfChanged } from "./dom";

export type EnergyCardVariant = "detailed" | "summary";

export interface HtmlRenderState {
  title: string;
  values: Record<string, string>;
  flags: Record<string, boolean>;
  warning?: string;
  entityRows?: Array<[string, string]>;
}

export class HtmlCardRenderer {
  private readonly root: ShadowRoot;
  private renderedVariant?: EnergyCardVariant;
  private styleEl?: HTMLStyleElement;
  private titleEl?: HTMLDivElement;
  private dashboardEl?: HTMLDivElement;
  private warningEl?: HTMLDivElement;
  private entityMapEl?: HTMLDivElement;
  private flowScaleObserver?: ResizeObserver;
  private flowScaleCleanup?: () => void;
  private flowLineObserver?: ResizeObserver;

  constructor(root: ShadowRoot) {
    this.root = root;
  }

  render(variant: EnergyCardVariant, state: HtmlRenderState): void {
    this.ensureStructure(variant);
    this.syncTitle(state.title);
    this.syncFields(state.values);
    this.syncFlags(state.flags);
    this.syncWarning(state.warning);
    this.syncEntityRows(state.entityRows);
    requestAnimationFrame(() => this.positionFlowLines());
  }

  private ensureStructure(variant: EnergyCardVariant): void {
    if (this.renderedVariant === variant && this.styleEl && this.dashboardEl && this.titleEl && this.warningEl && this.entityMapEl) {
      return;
    }

    this.flowScaleObserver?.disconnect();
    this.flowScaleObserver = undefined;
    this.flowScaleCleanup?.();
    this.flowScaleCleanup = undefined;
    this.flowLineObserver?.disconnect();
    this.flowLineObserver = undefined;
    this.root.replaceChildren();
    this.styleEl = document.createElement("style");
    this.styleEl.textContent = styles();

    const card = document.createElement("ha-card");
    const shell = document.createElement("div");
    shell.className = "card-shell";

    this.titleEl = document.createElement("div");
    this.titleEl.className = "card-title";

    this.dashboardEl = document.createElement("div");
    this.dashboardEl.className = `energy-dashboard energy-dashboard--${variant}`;
    this.dashboardEl.innerHTML = variant === "summary" ? summaryMarkup() : detailedMarkup();

    this.warningEl = document.createElement("div");
    this.warningEl.className = "helper helper--warn";
    this.warningEl.hidden = true;

    this.entityMapEl = document.createElement("div");
    this.entityMapEl.className = "entity-map";
    this.entityMapEl.hidden = true;

    shell.append(this.titleEl, this.dashboardEl, this.warningEl, this.entityMapEl);
    card.append(shell);
    this.root.append(this.styleEl, card);
    this.renderedVariant = variant;
    this.setupFlowScaling(variant);
    this.setupFlowLinePositioning();
  }

  private setupFlowScaling(variant: EnergyCardVariant): void {
    if (!this.dashboardEl) return;

    if (variant === "summary") {
      const frame = this.dashboardEl.querySelector<HTMLElement>(".summary-scale-frame");
      const shell = frame?.querySelector<HTMLElement>(".summary-shell");
      if (!frame || !shell) return;

      const desktopViewport = window.matchMedia("(min-width: 1025px)");
      const update = (): void => {
        const frameWidth = frame.clientWidth;
        if (frameWidth <= 0) return;

        if (!desktopViewport.matches) {
          frame.style.removeProperty("--summary-scaled-height");
          shell.style.removeProperty("--summary-scale");
          requestAnimationFrame(() => this.positionFlowLines());
          return;
        }

        const designWidth = Math.max(shell.scrollWidth, shell.offsetWidth);
        const scale = Math.min(1, frameWidth / designWidth);
        shell.style.setProperty("--summary-scale", String(scale));
        if (scale < 1) {
          frame.style.setProperty("--summary-scaled-height", `${shell.scrollHeight * scale}px`);
        } else {
          frame.style.removeProperty("--summary-scaled-height");
        }
        requestAnimationFrame(() => this.positionFlowLines());
      };

      this.flowScaleObserver = new ResizeObserver(() => update());
      this.flowScaleObserver.observe(frame);
      this.flowScaleObserver.observe(shell);
      desktopViewport.addEventListener("change", update);
      window.addEventListener("resize", update);
      this.flowScaleCleanup = (): void => {
        desktopViewport.removeEventListener("change", update);
        window.removeEventListener("resize", update);
      };
      requestAnimationFrame(update);
      return;
    }

    if (variant !== "detailed") return;
    const frame = this.dashboardEl.querySelector<HTMLElement>(".flow-scale-frame");
    const board = frame?.querySelector<HTMLElement>(".flow-board--detailed");
    if (!frame || !board) return;

    const update = (): void => {
      const frameWidth = frame.clientWidth;
      if (frameWidth <= 0) return;
      const scale = Math.min(1, frameWidth / 1024);
      frame.style.setProperty("--flow-scale", String(scale));
      if (scale < 1) {
        frame.style.setProperty("--flow-scaled-height", `${board.scrollHeight * scale}px`);
      } else {
        frame.style.removeProperty("--flow-scaled-height");
      }
    };

    this.flowScaleObserver = new ResizeObserver(() => update());
    this.flowScaleObserver.observe(frame);
    this.flowScaleObserver.observe(board);
    requestAnimationFrame(update);
  }

  private setupFlowLinePositioning(): void {
    if (!this.dashboardEl) return;
    const boards = Array.from(this.dashboardEl.querySelectorAll<HTMLElement>(".flow-board"));
    if (!boards.length) return;

    const update = (): void => this.positionFlowLines();
    this.flowLineObserver = new ResizeObserver(() => update());
    this.flowLineObserver.observe(this.dashboardEl);
    for (const board of boards) {
      this.flowLineObserver.observe(board);
      for (const node of board.querySelectorAll<HTMLElement>("[data-flow-node]")) {
        this.flowLineObserver.observe(node);
      }
    }
    requestAnimationFrame(update);
  }

  private positionFlowLines(): void {
    if (!this.dashboardEl) return;
    for (const line of this.dashboardEl.querySelectorAll<HTMLElement>("[data-flow-line][data-flow-from][data-flow-to]")) {
      const board = line.closest<HTMLElement>(".flow-board");
      if (!board) continue;
      const from = board.querySelector<HTMLElement>(`[data-flow-node="${line.dataset.flowFrom}"]`);
      const to = board.querySelector<HTMLElement>(`[data-flow-node="${line.dataset.flowTo}"]`);
      if (!from || !to) continue;

      const boardRect = board.getBoundingClientRect();
      const fromRect = from.getBoundingClientRect();
      const toRect = to.getBoundingClientRect();
      const scale = board.offsetWidth > 0 ? boardRect.width / board.offsetWidth : 1;
      const safeScale = scale > 0 ? scale : 1;
      const x1 = (fromRect.left + fromRect.width / 2 - boardRect.left) / safeScale;
      const y1 = (fromRect.top + fromRect.height / 2 - boardRect.top) / safeScale;
      const x2 = (toRect.left + toRect.width / 2 - boardRect.left) / safeScale;
      const y2 = (toRect.top + toRect.height / 2 - boardRect.top) / safeScale;
      const dx = x2 - x1;
      const dy = y2 - y1;
      const distance = Math.hypot(dx, dy);
      const angle = Math.atan2(dy, dx) * (180 / Math.PI);

      line.style.left = `${x1}px`;
      line.style.top = `${y1}px`;
      line.style.width = `${distance}px`;
      line.style.transform = `translateY(-50%) rotate(${angle}deg)`;
    }
  }

  private syncTitle(title: string): void {
    if (!this.titleEl) return;
    setTextContentIfChanged(this.titleEl, title);
    setHiddenIfChanged(this.titleEl, title.trim().length === 0);
  }

  private syncFields(values: Record<string, string>): void {
    if (!this.dashboardEl) return;
    for (const node of this.dashboardEl.querySelectorAll<HTMLElement>("[data-field]")) {
      setTextContentIfChanged(node, values[node.dataset.field ?? ""] ?? "");
    }
  }

  private syncFlags(flags: Record<string, boolean>): void {
    if (!this.dashboardEl) return;
    for (const [key, value] of Object.entries(flags)) {
      this.dashboardEl.classList.toggle(`is-${key.replace(/_/g, "-")}`, value);
    }
    this.syncFlowLines(flags);
  }

  private syncFlowLines(flags: Record<string, boolean>): void {
    if (!this.dashboardEl) return;
    for (const node of this.dashboardEl.querySelectorAll<HTMLElement>("[data-flow-line]")) {
      const id = node.dataset.flowLine ?? "";
      const stateKey = id.replace(/-/g, "_");
      const active = flags[`flow_${stateKey}_active`] ?? false;
      const reverse = flags[`flow_${stateKey}_reverse`] ?? false;
      const forward = flags[`flow_${stateKey}_forward`] ?? true;
      node.classList.toggle("active", active);
      node.classList.toggle("reverse", active && reverse);
      node.classList.toggle("forward", active && !reverse && forward);
    }
  }

  private syncWarning(warning: string | undefined): void {
    if (!this.warningEl) return;
    const text = warning ?? "";
    setTextContentIfChanged(this.warningEl, text);
    setHiddenIfChanged(this.warningEl, text.trim().length === 0);
  }

  private syncEntityRows(rows: Array<[string, string]> | undefined): void {
    if (!this.entityMapEl) return;
    if (!rows?.length) {
      setHiddenIfChanged(this.entityMapEl, true);
      if (this.entityMapEl.childElementCount > 0) this.entityMapEl.replaceChildren();
      return;
    }

    const fragment = document.createDocumentFragment();
    for (const [label, value] of rows) {
      const row = document.createElement("div");
      row.className = "entity-row";
      const labelEl = document.createElement("span");
      const valueEl = document.createElement("span");
      labelEl.textContent = label;
      valueEl.textContent = value;
      row.append(labelEl, valueEl);
      fragment.append(row);
    }

    this.entityMapEl.replaceChildren(fragment);
    setHiddenIfChanged(this.entityMapEl, false);
    requestAnimationFrame(() => this.positionFlowLines());
  }
}

const L = {
  battery: "&#1040;&#1082;&#1091;&#1084;&#1091;&#1083;&#1103;&#1090;&#1086;&#1088;",
  chargeBattery: "&#1047;&#1072;&#1088;&#1103;&#1076; &#1040;&#1050;&#1041;",
  costDay: "&#1063;&#1080;&#1089;&#1090;&#1080;&#1081; &#1073;&#1072;&#1083;&#1072;&#1085;&#1089; &#1079;&#1072; &#1076;&#1077;&#1085;&#1100;",
  current: "&#1057;&#1090;&#1088;&#1091;&#1084;:",
  dcBus: "DC &#1096;&#1080;&#1085;&#1072;",
  acTemperature: "AC &#1090;&#1077;&#1084;&#1087;&#1077;&#1088;&#1072;&#1090;&#1091;&#1088;&#1072;",
  dcTemperature: "DC &#1090;&#1077;&#1084;&#1087;&#1077;&#1088;&#1072;&#1090;&#1091;&#1088;&#1072;",
  exportDay: "&#1055;&#1088;&#1086;&#1076;&#1072;&#1078; &#1079;&#1072; &#1076;&#1077;&#1085;&#1100;",
  exportGrid: "&#1045;&#1082;&#1089;&#1087;&#1086;&#1088;&#1090; &#1074; &#1084;&#1077;&#1088;&#1077;&#1078;&#1091;",
  frequency: "&#1063;&#1072;&#1089;&#1090;&#1086;&#1090;&#1072; &#1084;&#1077;&#1088;&#1077;&#1078;&#1110;",
  generation: "&#1047;&#1072;&#1075;&#1072;&#1083;&#1100;&#1085;&#1072; &#1075;&#1077;&#1085;&#1077;&#1088;&#1072;&#1094;&#1110;&#1103;",
  generatorDay: "&#1043;&#1077;&#1085;&#1077;&#1088;&#1072;&#1090;&#1086;&#1088; &#1079;&#1072; &#1076;&#1077;&#1085;&#1100;",
  generator: "&#1043;&#1077;&#1085;&#1077;&#1088;&#1072;&#1090;&#1086;&#1088;",
  grid: "&#1052;&#1077;&#1088;&#1077;&#1078;&#1072;",
  gridLoad: "&#1053;&#1072;&#1074;&#1072;&#1085;&#1090;&#1072;&#1078;&#1077;&#1085;&#1085;&#1103; &#1085;&#1072; &#1084;&#1077;&#1088;&#1077;&#1078;&#1091;",
  gridLoadSub: "&#1087;&#1086;&#1090;&#1091;&#1078;&#1085;&#1110; &#1087;&#1088;&#1080;&#1083;&#1072;&#1076;&#1080;",
  gridWork: "&#1056;&#1086;&#1073;&#1086;&#1090;&#1072; &#1074;&#1110;&#1076;<br>&#1084;&#1077;&#1088;&#1077;&#1078;&#1110;",
  importDay: "&#1050;&#1091;&#1087;&#1110;&#1074;&#1083;&#1103; &#1079;&#1072; &#1076;&#1077;&#1085;&#1100;",
  importGrid: "&#1030;&#1084;&#1087;&#1086;&#1088;&#1090; &#1079; &#1084;&#1077;&#1088;&#1077;&#1078;&#1110;",
  inverter: "&#1030;&#1085;&#1074;&#1077;&#1088;&#1090;&#1086;&#1088;",
  inverterOutput: "&#1042;&#1080;&#1093;&#1110;&#1076;&#1085;&#1072; &#1087;&#1086;&#1090;&#1091;&#1078;&#1085;&#1110;&#1089;&#1090;&#1100;",
  lastUpdate: "&#1054;&#1089;&#1090;&#1072;&#1085;&#1085;&#1108; &#1086;&#1085;&#1086;&#1074;&#1083;&#1077;&#1085;&#1085;&#1103;:",
  load: "Load",
  phasePower: "&#1055;&#1086;&#1090;&#1091;&#1078;&#1085;&#1110;&#1089;&#1090;&#1100; &#1087;&#1086; &#1092;&#1072;&#1079;&#1072;&#1093;",
  phases: "&#1060;&#1072;&#1079;&#1080;:",
  power: "&#1055;&#1086;&#1090;&#1091;&#1078;&#1085;&#1110;&#1089;&#1090;&#1100;",
  productionDay: "&#1043;&#1077;&#1085;&#1077;&#1088;&#1072;&#1094;&#1110;&#1103; &#1079;&#1072; &#1076;&#1077;&#1085;&#1100;",
  pvGeneration: "PV &#1075;&#1077;&#1085;&#1077;&#1088;&#1072;&#1094;&#1110;&#1103;",
  pvField1: "PV &#1087;&#1086;&#1083;&#1077; 1",
  pvField2: "PV &#1087;&#1086;&#1083;&#1077; 2",
  selfConsumption: "&#1042;&#1083;&#1072;&#1089;&#1085;&#1077; &#1089;&#1087;&#1086;&#1078;&#1080;&#1074;&#1072;&#1085;&#1085;&#1103;",
  solarWork: "&#1056;&#1086;&#1073;&#1086;&#1090;&#1072; &#1074;&#1110;&#1076;<br>&#1089;&#1086;&#1085;&#1094;&#1103;",
  status: "&#1057;&#1090;&#1072;&#1090;&#1091;&#1089;",
  system: "&#1057;&#1080;&#1089;&#1090;&#1077;&#1084;&#1072;",
  temperature: "&#1058;&#1077;&#1084;&#1087;&#1077;&#1088;&#1072;&#1090;&#1091;&#1088;&#1072;",
  upsLoad: "UPS &#1085;&#1072;&#1074;&#1072;&#1085;&#1090;&#1072;&#1078;&#1077;&#1085;&#1085;&#1103;",
  upsLoadSub: "&#1086;&#1089;&#1085;&#1086;&#1074;&#1085;&#1077;",
  voltage: "&#1053;&#1072;&#1087;&#1088;&#1091;&#1075;&#1072;:"
};

const icon = (name: IconName): string => `<span class="icon icon--${name}">${iconSvg(name)}</span>`;

const batteryIconStack = (): string => `
  <span class="battery-icon-stack" aria-hidden="true">
    ${icon("batteryEmpty")}
    ${icon("batteryLow")}
    ${icon("batteryHalf")}
    ${icon("batteryFull")}
    ${icon("batteryBolt")}
  </span>
`;

const chip = (tone: string, iconName: IconName, label: string, valueField: string, unitField = "", trend: IconName | null = "arrowUpRight"): string => `
  <article class="chip chip--${tone}">
    ${icon(iconName)}
    <div class="chip__content">
      <div class="chip__label">${label}</div>
      <div class="chip__value"><span data-field="${valueField}"></span>${unitField ? `<small data-field="${unitField}"></small>` : ""}</div>
    </div>
    ${trend ? `<span class="chip__trend">${iconSvg(trend)}</span>` : ""}
  </article>
`;

const badge = (tone: string, iconName: IconName, html: string): string => `
  <div class="status-badge status-badge--${tone}">
    ${icon(iconName)}
    <span>${html}</span>
  </div>
`;

const line = (label: string, field: string): string => `
  <div class="metric-line"><span>${label}</span><strong data-field="${field}"></strong></div>
`;

const valueUnit = (label: string, valueField: string, unitField: string, tone = ""): string => `
  <div class="power-readout ${tone ? `power-readout--${tone}` : ""}">
    <span>${label}</span>
    <strong><span data-field="${valueField}"></span><small data-field="${unitField}"></small></strong>
  </div>
`;

const summaryInverterPower = (
  iconName: IconName,
  valueField: string,
  unitField: string,
  temperatureField: string,
  tone: string
): string => `
  <div class="power-readout summary-inverter-readout power-readout--${tone}">
    <div class="summary-readout-main">
      ${icon(iconName)}
      <strong><span data-field="${valueField}"></span><small data-field="${unitField}"></small></strong>
    </div>
    <div class="summary-readout-meta summary-readout-meta--${tone}">
      ${icon("thermometer")}
      <strong data-field="${temperatureField}"></strong>
    </div>
  </div>
`;

const summaryMetricReadout = (label: string, iconName: IconName, field: string, tone: string): string => `
  <div class="power-readout summary-metric-readout power-readout--${tone}">
    ${icon(iconName)}
    <span>${label}</span>
    <strong data-field="${field}"></strong>
  </div>
`;

const nodeTitle = (iconName: IconName, title: string, subtitle = ""): string => `
  <header class="node-title">
    ${icon(iconName)}
    <div>
      <h3>${title}</h3>
      ${subtitle ? `<p>${subtitle}</p>` : ""}
    </div>
  </header>
`;

const flowLine = (id: string, from: string, to: string): string =>
  `<span class="flow-line line-${id}" data-flow-line="${id}" data-flow-from="${from}" data-flow-to="${to}" aria-hidden="true"></span>`;

const detailedFlowLines = (): string => `
  <div class="detail-flow-lines" aria-hidden="true">
    ${flowLine("pv1-inverter", "pv1", "inverter")}
    ${flowLine("pv2-inverter", "pv2", "inverter")}
    ${flowLine("battery-inverter", "battery", "inverter")}
    ${flowLine("inverter-grid", "inverter", "grid")}
    ${flowLine("generator-inverter", "generator", "inverter")}
    ${flowLine("inverter-ups", "inverter", "ups")}
    ${flowLine("grid-load", "grid", "grid-load")}
  </div>
`;

const summaryFlowLines = (): string => `
  <div class="summary-flow-lines" aria-hidden="true">
    ${flowLine("pv-inverter", "pv-total", "inverter")}
    ${flowLine("inverter-grid", "inverter", "grid")}
    ${flowLine("inverter-ups", "inverter", "ups")}
    ${flowLine("battery-inverter", "battery", "inverter")}
    ${flowLine("grid-load", "grid", "grid-load")}
  </div>
`;

const topStrip = (): string => `
  <section class="top-strip">
    <div class="chip-row">
      ${chip("green", "sun", L.productionDay, "daily.production.value", "daily.production.unit")}
      ${chip("green", "arrowUpRight", L.exportDay, "daily.export.value", "daily.export.unit")}
      ${chip("amber", "cart", L.importDay, "daily.import.value", "daily.import.unit")}
      ${chip("purple", "wallet", L.costDay, "daily.cost.value", "daily.cost.unit", null)}
    </div>
    <div class="badge-row">
      ${badge("blue", "grid", L.gridWork)}
      ${badge("green", "sun", L.solarWork)}
      ${badge("green", "battery", L.chargeBattery)}
      ${badge("muted", "generator", `${L.generator}<br>standby`)}
    </div>
  </section>
`;

const summaryChips = (): string => `
  <section class="summary-chips" aria-label="Daily summary">
    ${chip("green", "sun", L.productionDay, "daily.production.value", "daily.production.unit")}
    ${chip("amber", "cart", L.importDay, "daily.import.value", "daily.import.unit")}
    ${chip("green", "arrowUpRight", L.exportDay, "daily.export.value", "daily.export.unit")}
    ${chip("amber", "generator", L.generatorDay, "daily.generator.value", "daily.generator.unit")}
    ${chip("blue", "home", L.selfConsumption, "system.self_consumption", "")}
    ${chip("purple", "wallet", L.costDay, "daily.cost.value", "daily.cost.unit", null)}
  </section>
`;

const detailedMarkup = (): string => `
  <section class="detailed-shell">
    <aside class="side-rail">
      <div class="sidebar-chips">
        ${chip("green", "shield", L.system, "system.online", "")}
        ${chip("green", "sun", L.productionDay, "daily.production.value", "daily.production.unit")}
        ${chip("green", "arrowUpRight", L.exportDay, "daily.export.value", "daily.export.unit")}
        ${chip("amber", "cart", L.importDay, "daily.import.value", "daily.import.unit")}
        ${chip("amber", "generator", L.generatorDay, "daily.generator.value", "daily.generator.unit")}
        ${chip("purple", "wallet", L.costDay, "daily.cost.value", "daily.cost.unit", null)}
        ${chip("blue", "home", L.selfConsumption, "system.self_consumption", "")}
        ${chip("muted", "clock", L.lastUpdate, "system.updated_at", "")}
      </div>
    </aside>
    <div class="flow-scale-frame">
      <section class="flow-board flow-board--detailed" aria-label="Energy flow">
        ${detailedFlowLines()}
        <div class="flow-row flow-row--pv">
          <article class="node node--pv node--pv1" data-flow-node="pv1">
            ${nodeTitle("solarPanel", L.pvField1)}
            ${line(L.voltage, "pv1.voltage")}
            ${line(L.current, "pv1.current")}
            ${valueUnit(L.power, "pv1.power.value", "pv1.power.unit", "green")}
          </article>
          <article class="node node--pv node--pv2" data-flow-node="pv2">
            ${nodeTitle("solarPanel", L.pvField2)}
            ${line(L.voltage, "pv2.voltage")}
            ${line(L.current, "pv2.current")}
            ${valueUnit(L.power, "pv2.power.value", "pv2.power.unit", "green")}
          </article>
        </div>
        <div class="flow-row flow-row--middle">
          <div class="node-slot node-slot--grid">
            <article class="node node--grid" data-flow-node="grid">
              ${nodeTitle("grid", L.grid)}
              ${line(L.phases, "grid.phases")}
              ${line(L.current, "grid.current")}
              ${line(L.frequency, "system.frequency")}
              <div class="phase-row"><span>${L.phasePower}</span><strong data-field="grid.phase_power"></strong></div>
              ${valueUnit(L.power, "grid.power.value", "grid.power.unit", "blue")}
              <div class="node-status node-status--blue">${icon("arrowDown")}<span data-field="grid.status"></span></div>
            </article>
          </div>
          <article class="node node--inverter" data-flow-node="inverter">
            <div class="inverter-layout">
              <div class="inverter-left">
                <div class="inverter-state">
                  ${icon("inverter")}
                  <div>
                    <span>${L.status}</span>
                    <strong data-field="inverter.status"></strong>
                  </div>
                </div>
                <div class="inverter-kpis">
                  ${valueUnit("AC", "inverter.ac_power.value", "inverter.ac_power.unit", "cyan")}
                  ${valueUnit("DC", "inverter.dc_power.value", "inverter.dc_power.unit", "green")}
                </div>
              </div>
              <div class="inverter-right">
                <header class="inverter-heading">
                  <h2>${L.inverter}</h2>
                  <span>AC / DC</span>
                </header>
                <div class="inverter-lines">
                  ${line(`${L.phases}`, "inverter.phases")}
                  ${line(L.current, "inverter.current")}
                  ${line("Hz:", "inverter.frequency")}
                  ${line(L.acTemperature, "inverter.ac_temperature")}
                  ${line(L.dcTemperature, "inverter.dc_temperature")}
                </div>
              </div>
            </div>
          </article>
          <div class="node-slot node-slot--ups">
            <article class="node node--ups" data-flow-node="ups">
              ${nodeTitle("homeDay", L.upsLoad, L.upsLoadSub)}
              ${line(L.phases, "ups.phases")}
              ${line(L.current, "ups.current")}
              <div class="phase-row"><span>${L.phasePower}</span><strong data-field="ups.phase_power"></strong></div>
              ${valueUnit(L.power, "ups.power.value", "ups.power.unit", "cyan")}
            </article>
          </div>
        </div>
        <div class="flow-row flow-row--bottom">
          <article class="node node--grid-load" data-flow-node="grid-load">
            ${nodeTitle("home", L.gridLoad, L.gridLoadSub)}
            ${line(L.phases, "grid_load.phases")}
            ${line(L.current, "grid_load.current")}
            <div class="phase-row"><span>${L.phasePower}</span><strong data-field="grid_load.phase_power"></strong></div>
            ${valueUnit(L.power, "grid_load.power.value", "grid_load.power.unit", "blue")}
          </article>
          <article class="node node--generator" data-flow-node="generator">
            ${nodeTitle("generator", L.generator)}
            ${line(L.phases, "generator.phases")}
            ${line(L.current, "generator.current")}
            <div class="phase-row"><span>${L.phasePower}</span><strong data-field="generator.phase_power"></strong></div>
            ${valueUnit(L.power, "generator.power.value", "generator.power.unit", "amber")}
            <div class="node-status node-status--amber"><span>${L.status}:</span><strong data-field="generator.status"></strong></div>
          </article>
          <article class="node node--battery" data-flow-node="battery">
            <h3 class="battery-heading">${L.battery}</h3>
            <div class="battery-grid">
              <div class="soc-ring">${batteryIconStack()}<strong><span data-field="battery.soc.value"></span><small data-field="battery.soc.unit"></small></strong><span>SOC</span></div>
              <div class="battery-data">
                ${line(L.voltage, "battery.voltage")}
                ${line(L.current, "battery.current")}
                ${valueUnit(L.power, "battery.power.value", "battery.power.unit", "green")}
                <strong class="battery-mode" data-field="battery.mode"></strong>
              </div>
              <div class="battery-temp">${icon("thermometer")}<span>${L.temperature}</span><strong data-field="battery.temperature"></strong></div>
            </div>
          </article>
        </div>
      </section>
    </div>
  </section>
`;

const summaryMarkup = (): string => `
  <div class="summary-scale-frame">
    <section class="summary-shell">
      ${summaryChips()}
      <section class="flow-board flow-board--summary" aria-label="Energy summary flow">
        ${summaryFlowLines()}
        <article class="node node--pv-total summary-node" data-flow-node="pv-total">
          ${nodeTitle("solarPanel", L.pvGeneration)}
          ${valueUnit("DC", "pv.total.power.value", "pv.total.power.unit", "green")}
        </article>
        <article class="node node--grid summary-node" data-flow-node="grid">
          ${nodeTitle("grid", L.grid)}
          ${valueUnit(L.power, "grid.power.value", "grid.power.unit", "blue")}
          <div class="node-status node-status--blue">${icon("arrowDown")}<span data-field="grid.status"></span></div>
        </article>
        <article class="node node--grid-load summary-node" data-flow-node="grid-load">
          ${nodeTitle("home", L.gridLoad)}
          ${valueUnit(L.power, "grid_load.power.value", "grid_load.power.unit", "blue")}
        </article>
        <article class="node node--inverter summary-inverter" data-flow-node="inverter">
          <header class="summary-inverter__head">
            ${icon("inverter")}
            <div>
              <h2>${L.inverter}</h2>
              <span data-field="inverter.status"></span>
            </div>
          </header>
          <div class="summary-inverter__power">
            ${summaryInverterPower("wave", "inverter.ac_power.value", "inverter.ac_power.unit", "inverter.ac_temperature_compact", "cyan")}
            ${summaryInverterPower("solarPanel", "inverter.dc_power.value", "inverter.dc_power.unit", "inverter.dc_temperature_compact", "green")}
          </div>
        </article>
        <article class="node node--ups summary-node" data-flow-node="ups">
          ${nodeTitle("homeDay", L.upsLoad)}
          ${valueUnit(L.power, "ups.power.value", "ups.power.unit", "cyan")}
        </article>
        <article class="node node--battery summary-battery-node" data-flow-node="battery">
          <div class="summary-battery">
            <div class="soc-ring">${batteryIconStack()}<strong><span data-field="battery.soc.value"></span><small data-field="battery.soc.unit"></small></strong><span>SOC</span></div>
            <div class="summary-battery__data">
              ${valueUnit(L.power, "battery.power.value", "battery.power.unit", "green")}
              <div class="summary-battery__stats">
                ${summaryMetricReadout("Напруга", "battery", "battery.voltage", "green")}
                ${summaryMetricReadout("Температура", "thermometer", "battery.temperature", "green")}
              </div>
            </div>
          </div>
        </article>
      </section>
    </section>
  </div>
`;

const systemPanel = (variant: EnergyCardVariant): string => `
  <section class="system-panel system-panel--${variant}">
    <header>
      ${icon("shield")}
      <h3>${L.system}</h3>
      <span class="online"><span data-field="system.online"></span><i></i></span>
    </header>
    <div class="system-grid">
      <div>${icon("wave")}<span>${L.frequency}</span><strong data-field="system.frequency"></strong></div>
      <div>${icon("inverter")}<span>${L.dcBus}</span><strong data-field="system.dc_bus"></strong></div>
      <div>${icon("sun")}<span>${L.generation}</span><strong data-field="system.generation"></strong></div>
      <div>${icon("home")}<span>${L.selfConsumption}</span><strong data-field="system.self_consumption"></strong></div>
      <div>${icon("arrowUpRight")}<span>${L.exportGrid}</span><strong data-field="system.export"></strong></div>
      <div>${icon("arrowDown")}<span>${L.importGrid}</span><strong data-field="system.import"></strong></div>
    </div>
    <footer>${icon("thermometer")}<span>${L.lastUpdate}</span><strong data-field="system.updated_at"></strong></footer>
  </section>
`;

const flowSvg = (variant: EnergyCardVariant): string => `
  <svg class="flow-lines flow-lines--${variant}" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
    <defs>
      <marker id="${variant}-green" markerWidth="1.65" markerHeight="1.65" refX="7" refY="4" orient="auto" viewBox="0 0 8 8"><path d="M0,0 L8,4 L0,8 Z" fill="#38ef77"/></marker>
      <marker id="${variant}-blue" markerWidth="1.65" markerHeight="1.65" refX="7" refY="4" orient="auto" viewBox="0 0 8 8"><path d="M0,0 L8,4 L0,8 Z" fill="#4aa3ff"/></marker>
      <marker id="${variant}-cyan" markerWidth="1.65" markerHeight="1.65" refX="7" refY="4" orient="auto" viewBox="0 0 8 8"><path d="M0,0 L8,4 L0,8 Z" fill="#00d8ff"/></marker>
      <marker id="${variant}-amber" markerWidth="1.65" markerHeight="1.65" refX="7" refY="4" orient="auto" viewBox="0 0 8 8"><path d="M0,0 L8,4 L0,8 Z" fill="#ffbf2f"/></marker>
    </defs>
    ${
      variant === "summary"
        ? `<path class="flow flow--green" marker-end="url(#${variant}-green)" d="M50 18V31"/>
           <path class="flow flow--blue flow--duplex" marker-start="url(#${variant}-blue)" marker-end="url(#${variant}-blue)" d="M24 49H39"/>
           <path class="flow flow--cyan" marker-end="url(#${variant}-cyan)" d="M61 49H77"/>
           <path class="flow flow--green flow--duplex" marker-start="url(#${variant}-green)" marker-end="url(#${variant}-green)" d="M50 64V80"/>
           <path class="flow flow--amber" marker-end="url(#${variant}-amber)" d="M38 84C38 72 41 66 45 61"/>`
        : `<path class="flow flow--green" marker-end="url(#${variant}-green)" d="M43 12V23"/>
           <path class="flow flow--green" marker-end="url(#${variant}-green)" d="M57 12V23"/>
           <path class="flow flow--blue flow--duplex" marker-start="url(#${variant}-blue)" marker-end="url(#${variant}-blue)" d="M17 38H38"/>
           <path class="flow flow--cyan" marker-end="url(#${variant}-cyan)" d="M62 38H83"/>
           <path class="flow flow--blue" marker-end="url(#${variant}-blue)" d="M13 51V66"/>
           <path class="flow flow--green flow--duplex" marker-start="url(#${variant}-green)" marker-end="url(#${variant}-green)" d="M50 60V78"/>
           <path class="flow flow--amber" marker-end="url(#${variant}-amber)" d="M36 82C36 70 40 63 44 57"/>`
    }
  </svg>
`;

const styles = (): string => `
  :host {
    display: block;
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    max-width: 100%;
    max-inline-size: 100%;
    overflow: hidden;
    container-type: inline-size;
  }

  *,
  *::before,
  *::after {
    box-sizing: border-box;
  }

  [hidden] {
    display: none !important;
  }

  ha-card {
    display: block;
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    max-width: 100%;
    max-inline-size: 100%;
    overflow: hidden;
    border: 1px solid rgba(160, 218, 255, 0.16);
    border-radius: 10px;
    background:
      radial-gradient(circle at 50% 40%, rgba(0, 216, 255, 0.08), transparent 42%),
      linear-gradient(180deg, #061018 0%, #02070b 100%);
    color: #eff8ff;
    box-shadow: 0 20px 46px rgba(0, 0, 0, 0.34);
  }

  .card-shell {
    min-width: 0;
    min-inline-size: 0;
    display: grid;
    gap: 14px;
    padding: 16px;
    font-family: Inter, "Segoe UI", system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
  }

  .card-title {
    color: rgba(239, 248, 255, 0.68);
    font-size: clamp(11px, 1cqw, 14px);
    font-weight: 600;
  }

  .energy-dashboard {
    --green: #38ef77;
    --cyan: #00d8ff;
    --blue: #4aa3ff;
    --amber: #ffbf2f;
    --purple: #9a72ff;
    --red: #ff5a65;
    --panel: rgba(8, 21, 30, 0.9);
    --panel2: rgba(12, 29, 39, 0.9);
    --line: rgba(255, 255, 255, 0.14);
    --muted: rgba(239, 248, 255, 0.7);
    display: grid;
    gap: 14px;
    min-width: 0;
    min-inline-size: 0;
  }

  .energy-dashboard--detailed {
    --detail-card-gap: 24px;
    --detail-row-gap: 30px;
    --detail-node-pad: 11px;
    --detail-icon-size: 36px;
    --detail-title-size: clamp(13px, 0.95cqw, 18px);
    --detail-copy-size: clamp(10px, 0.72cqw, 13px);
    --detail-value-size: clamp(18px, 1.34cqw, 25px);
  }

  .top-strip {
    display: grid;
    gap: 12px;
  }

  .detailed-shell {
    display: grid;
    grid-template-columns: minmax(210px, 242px) minmax(0, 1fr);
    gap: var(--detail-card-gap);
    align-items: stretch;
    min-width: 0;
    min-inline-size: 0;
  }

  .side-rail {
    display: grid;
    min-height: 0;
    align-content: stretch;
  }

  .sidebar-chips {
    min-height: 0;
    display: grid;
    grid-template-rows: repeat(8, minmax(0, 1fr));
    gap: 8px;
    align-self: stretch;
  }

  .chip-row,
  .badge-row {
    display: grid;
    gap: 12px;
  }

  .chip-row {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .badge-row {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .chip,
  .status-badge,
  .node,
  .system-panel {
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 8px;
    background:
      linear-gradient(140deg, #122834, #050e15);
    box-shadow: inset 0 0 38px rgba(255, 255, 255, 0.025);
  }

  .chip {
    min-width: 0;
    min-height: 82px;
    display: grid;
    grid-template-columns: 58px minmax(0, 1fr) 28px;
    gap: 12px;
    align-items: center;
    padding: 12px 16px;
  }

  .side-rail .chip {
    min-height: 0;
    grid-template-columns: 45px minmax(0, 1fr);
    gap: 8px;
    padding: 7px 10px;
    background: rgba(255, 255, 255, 0.045);
    box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018);
  }

  .side-rail .chip > .icon {
    width: 40px;
    height: 40px;
    font-size: 24px;
    padding: 0;
    border-radius: 0;
    background: transparent;
  }

  .side-rail .chip__content {
    min-width: 0;
    display: grid;
    gap: 8px;
    align-items: baseline;
  }

  .side-rail .chip__label {
    overflow: hidden;
    color: rgba(239, 248, 255, 0.78);
    font-size: clamp(10px, 1cqw, 20px);
    line-height: 1.15;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .side-rail .chip__value {
    gap: 4px;
    margin: 0;
    font-size: clamp(13px, 1.4cqw, 20px);
    font-weight: 750;
  }

  .side-rail .chip__value small {
    font-size: 0.68em;
  }

  .side-rail .chip__trend {
    display: none;
  }

  .chip--green { border-color: rgba(56, 239, 119, 0.24); box-shadow: inset 0 0 46px rgba(56, 239, 119, 0.07); }
  .chip--blue { border-color: rgba(74, 163, 255, 0.26); box-shadow: inset 0 0 46px rgba(74, 163, 255, 0.07); }
  .chip--amber { border-color: rgba(255, 191, 47, 0.26); box-shadow: inset 0 0 46px rgba(255, 191, 47, 0.08); }
  .chip--purple { border-color: rgba(154, 114, 255, 0.28); box-shadow: inset 0 0 46px rgba(154, 114, 255, 0.09); }
  .chip--muted { border-color: rgba(239, 248, 255, 0.16); box-shadow: inset 0 0 46px rgba(255, 255, 255, 0.035); }

  .side-rail .chip--green { border-color: rgba(56, 239, 119, 0.26); background: rgba(23, 132, 59, 0.11); box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018); }
  .side-rail .chip--blue { border-color: rgba(74, 163, 255, 0.26); background: rgba(31, 92, 153, 0.12); box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018); }
  .side-rail .chip--amber { border-color: rgba(255, 191, 47, 0.26); background: rgba(255, 191, 47, 0.08); box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018); }
  .side-rail .chip--purple { border-color: rgba(154, 114, 255, 0.28); background: rgba(154, 114, 255, 0.08); box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018); }
  .side-rail .chip--muted { border-color: rgba(239, 248, 255, 0.14); background: rgba(255, 255, 255, 0.045); box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018); }

  .chip > .icon {
    width: 54px;
    height: 54px;
    display: grid;
    place-items: center;
    padding: 10px;
    border-radius: 50%;
    color: var(--green);
    background: rgba(56, 239, 119, 0.12);
  }

  .chip--amber > .icon { color: var(--amber); background: rgba(255, 191, 47, 0.12); }
  .chip--blue > .icon { color: var(--blue); background: rgba(74, 163, 255, 0.12); }
  .chip--purple > .icon { color: var(--purple); background: rgba(154, 114, 255, 0.12); }
  .chip--muted > .icon { color: rgba(239, 248, 255, 0.58); background: rgba(255, 255, 255, 0.08); }

  .chip__label {
    min-width: 0;
    color: var(--muted);
    font-size: clamp(12px, 1cqw, 16px);
    line-height: 1.15;
  }

  .chip__value {
    display: flex;
    align-items: baseline;
    gap: 7px;
    margin-top: 7px;
    color: var(--green);
    font-size: clamp(22px, 2cqw, 34px);
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
  }

  .chip--amber .chip__value { color: var(--amber); }
  .chip--blue .chip__value { color: var(--blue); }
  .chip--purple .chip__value { color: var(--purple); }
  .chip--muted .chip__value { color: #f4f8ff; }
  .chip__value small { font-size: 0.58em; font-weight: 500; }
  .chip__trend { color: var(--green); }
  .chip--blue .chip__trend { color: var(--blue); }
  .chip--amber .chip__trend { color: var(--amber); }
  .chip--purple .chip__trend { color: var(--purple); }

  .status-badge {
    min-height: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 8px 12px;
    color: var(--muted);
    font-size: clamp(12px, 1cqw, 16px);
    font-weight: 600;
    line-height: 1.18;
    text-align: left;
  }

  .side-rail .status-badge {
    min-height: 40px;
    display: grid;
    grid-template-columns: 22px minmax(0, 1fr);
    gap: 8px;
    justify-content: stretch;
    padding: 7px 10px;
    background: rgba(255, 255, 255, 0.045);
    font-size: clamp(10px, 0.82cqw, 12px);
    box-shadow: inset 0 0 26px rgba(255, 255, 255, 0.018);
  }

  .side-rail .status-badge .icon {
    width: 21px;
    height: 21px;
    font-size: 21px;
  }

  .side-rail .status-badge span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .status-badge--blue { color: var(--blue); border-color: rgba(74, 163, 255, 0.32); background: rgba(31, 92, 153, 0.18); }
  .status-badge--green { color: var(--green); border-color: rgba(56, 239, 119, 0.3); background: rgba(23, 132, 59, 0.2); }
  .status-badge--muted { color: rgba(239, 248, 255, 0.78); background: rgba(255, 255, 255, 0.06); }

  .sidebar-chips > .chip:first-child {
    border-color: rgba(56, 239, 119, 0.34);
    background: rgba(23, 132, 59, 0.16);
  }

  .icon,
  .chip__trend {
    width: 28px;
    height: 28px;
    display: inline-grid;
    flex: 0 0 auto;
    place-items: center;
    color: currentColor;
    font-size: 28px;
  }

  .icon svg,
  .chip__trend svg,
  .sync-icon svg {
    width: 100%;
    height: 100%;
    display: block;
    filter: drop-shadow(0 0 7px currentColor);
  }

  .flow-board {
    position: relative;
    display: grid;
    gap: 16px;
    isolation: isolate;
    overflow: hidden;
  }

  .flow-scale-frame {
    display: block;
    min-width: 0;
    min-inline-size: 0;
    max-width: 100%;
    max-inline-size: 100%;
    contain: inline-size layout paint;
  }

  .flow-board--detailed {
    grid-template-columns: 1fr;
    min-height: 0;
    overflow: visible;
    gap: var(--detail-row-gap);
  }

  .flow-row {
    min-width: 0;
    display: grid;
    gap: var(--detail-card-gap);
    align-items: stretch;
  }

  .node-slot {
    min-width: 0;
    display: grid;
    align-content: center;
  }

  .node-slot > .node {
    min-height: 0;
  }

  .flow-row--pv {
    grid-template-columns: repeat(2, minmax(198px, 258px));
    gap: clamp(54px, 8cqw, 126px);
    justify-content: center;
  }

  .flow-row--middle {
    grid-template-columns: minmax(198px, 0.78fr) minmax(430px, 1.46fr) minmax(218px, 0.86fr);
    gap: clamp(28px, 4cqw, 58px);
  }

  .flow-row--bottom {
    grid-template-columns: minmax(218px, 0.9fr) minmax(218px, 0.9fr) minmax(350px, 1.28fr);
    gap: clamp(24px, 3.4cqw, 48px);
  }

  .flow-board--summary {
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    max-width: 100%;
    max-inline-size: 100%;
    grid-template-columns: minmax(188px, 0.9fr) minmax(300px, 1.22fr) minmax(188px, 0.9fr);
    grid-template-rows: minmax(70px, 0.74fr) minmax(132px, 1.18fr) minmax(76px, 0.8fr);
    grid-template-areas:
      ". pv ."
      "grid inverter load"
      "grid-load battery .";
    gap: clamp(12px, 1.8cqw, 24px) clamp(18px, 3.2cqw, 44px);
    align-items: center;
    height: 100%;
    min-height: 0;
    padding: clamp(4px, 0.8cqw, 10px) clamp(2px, 0.6cqw, 8px);
    overflow: visible;
  }

  .energy-dashboard--detailed .flow-lines {
    display: none;
  }

  .detail-flow-lines {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
    overflow: visible;
  }

  .summary-flow-lines {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
    overflow: visible;
  }

  .flow-board--detailed .flow-line {
    position: absolute;
    z-index: 0;
    height: 4px;
    transform-origin: left center;
    border-radius: 999px;
    background: rgba(180, 205, 218, 0.18);
    overflow: hidden;
    opacity: 0;
    --flow-rgb: 126, 197, 255;
    --flow-speed: 1.25s;
    --flow-texture-speed: 0.55s;
  }

  .flow-board--summary .flow-line {
    position: absolute;
    z-index: 0;
    height: 4px;
    transform-origin: left center;
    border-radius: 999px;
    background: rgba(180, 205, 218, 0.16);
    overflow: hidden;
    opacity: 0;
    --flow-rgb: 126, 197, 255;
    --flow-speed: 1.2s;
    --flow-texture-speed: 0.62s;
  }

  .flow-board--detailed .flow-line::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 34%;
    border-radius: inherit;
    background: linear-gradient(
      90deg,
      transparent 0%,
      rgba(var(--flow-rgb), 0.15) 18%,
      rgba(var(--flow-rgb), 1) 50%,
      rgba(var(--flow-rgb), 0.15) 82%,
      transparent 100%
    );
    opacity: 0;
    transform: translateX(-120%);
    box-shadow: 0 0 12px rgba(var(--flow-rgb), 0.65);
    will-change: transform, opacity;
  }

  .flow-board--detailed .flow-line::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: repeating-linear-gradient(
      90deg,
      transparent 0,
      transparent 9px,
      rgba(var(--flow-rgb), 0.4) 9px,
      rgba(var(--flow-rgb), 0.4) 16px,
      transparent 16px,
      transparent 22px
    );
    background-size: 44px 100%;
    opacity: 0;
    will-change: background-position, opacity;
  }

  .flow-board--summary .flow-line::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 34%;
    border-radius: inherit;
    background: linear-gradient(
      90deg,
      transparent 0%,
      rgba(var(--flow-rgb), 0.15) 18%,
      rgba(var(--flow-rgb), 1) 50%,
      rgba(var(--flow-rgb), 0.15) 82%,
      transparent 100%
    );
    opacity: 0;
    transform: translateX(-120%);
    box-shadow: 0 0 12px rgba(var(--flow-rgb), 0.65);
    will-change: transform, opacity;
  }

  .flow-board--summary .flow-line::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: repeating-linear-gradient(
      90deg,
      transparent 0,
      transparent 9px,
      rgba(var(--flow-rgb), 0.4) 9px,
      rgba(var(--flow-rgb), 0.4) 16px,
      transparent 16px,
      transparent 22px
    );
    background-size: 44px 100%;
    opacity: 0;
    will-change: background-position, opacity;
  }

  .flow-board--detailed .flow-line.active {
    opacity: 1;
    background:
      linear-gradient(
        90deg,
        transparent 0%,
        rgba(var(--flow-rgb), 0.18) 8%,
        rgba(var(--flow-rgb), 0.78) 18%,
        rgba(var(--flow-rgb), 0.18) 30%,
        transparent 42%
      ),
      linear-gradient(
        90deg,
        rgba(var(--flow-rgb), 0.12),
        rgba(var(--flow-rgb), 0.82),
        rgba(var(--flow-rgb), 0.12)
      );
    background-size: 72px 100%, 100% 100%;
    box-shadow:
      0 0 8px rgba(var(--flow-rgb), 0.55),
      0 0 18px rgba(var(--flow-rgb), 0.32),
      0 0 34px rgba(var(--flow-rgb), 0.16);
    animation: jks-power-flow-base calc(var(--flow-speed) * 1.35) linear infinite;
  }

  .flow-board--summary .flow-line.active {
    opacity: 1;
    background:
      linear-gradient(
        90deg,
        transparent 0%,
        rgba(var(--flow-rgb), 0.18) 8%,
        rgba(var(--flow-rgb), 0.78) 18%,
        rgba(var(--flow-rgb), 0.18) 30%,
        transparent 42%
      ),
      linear-gradient(
        90deg,
        rgba(var(--flow-rgb), 0.12),
        rgba(var(--flow-rgb), 0.82),
        rgba(var(--flow-rgb), 0.12)
      );
    background-size: 72px 100%, 100% 100%;
    box-shadow:
      0 0 8px rgba(var(--flow-rgb), 0.55),
      0 0 18px rgba(var(--flow-rgb), 0.32),
      0 0 34px rgba(var(--flow-rgb), 0.16);
    animation: jks-power-flow-base calc(var(--flow-speed) * 1.35) linear infinite;
  }

  .flow-board--detailed .flow-line.active::before {
    opacity: 1;
    animation: jks-power-flow-pulse var(--flow-speed) linear infinite;
  }

  .flow-board--summary .flow-line.active::before {
    opacity: 1;
    animation: jks-power-flow-pulse var(--flow-speed) linear infinite;
  }

  .flow-board--detailed .flow-line.active::after {
    opacity: 0.72;
    animation: jks-power-flow-texture var(--flow-texture-speed) linear infinite;
  }

  .flow-board--summary .flow-line.active::after {
    opacity: 0.72;
    animation: jks-power-flow-texture var(--flow-texture-speed) linear infinite;
  }

  .flow-board--detailed .flow-line.reverse.active::before {
    animation-name: jks-power-flow-pulse-reverse;
  }

  .flow-board--summary .flow-line.reverse.active::before {
    animation-name: jks-power-flow-pulse-reverse;
  }

  .flow-board--detailed .flow-line.reverse.active::after,
  .flow-board--detailed .flow-line.reverse.active {
    animation-direction: reverse;
  }

  .flow-board--summary .flow-line.reverse.active::after,
  .flow-board--summary .flow-line.reverse.active {
    animation-direction: reverse;
  }

  .flow-board--detailed .flow-line.forward.active::before {
    animation-name: jks-power-flow-pulse;
  }

  .flow-board--summary .flow-line.forward.active::before {
    animation-name: jks-power-flow-pulse;
  }

  .flow-board--detailed .flow-line.warning.active {
    --flow-rgb: 255, 196, 92;
  }

  .flow-board--detailed .flow-line.alert.active {
    --flow-rgb: 255, 106, 106;
  }

  .flow-board--detailed .line-pv1-inverter,
  .flow-board--detailed .line-pv2-inverter {
    --flow-rgb: 56, 239, 119;
    --flow-speed: 1.05s;
  }

  .flow-board--detailed .line-battery-inverter {
    --flow-rgb: 110, 224, 142;
    --flow-speed: 1.55s;
  }

  .flow-board--detailed .line-inverter-grid {
    --flow-rgb: 74, 163, 255;
    --flow-speed: 1.15s;
  }

  .flow-board--detailed .line-generator-inverter {
    --flow-rgb: 255, 191, 47;
    --flow-speed: 1.1s;
  }

  .flow-board--detailed .line-inverter-ups {
    --flow-rgb: 0, 216, 255;
    --flow-speed: 1.35s;
  }

  .flow-board--detailed .line-grid-load {
    --flow-rgb: 158, 184, 200;
    --flow-speed: 1.45s;
  }

  .flow-board--summary .line-pv-inverter {
    --flow-rgb: 56, 239, 119;
    --flow-speed: 1.1s;
    left: 50%;
    top: 28%;
    width: 15%;
    transform: rotate(90deg);
  }

  .flow-board--summary .line-inverter-grid {
    --flow-rgb: 74, 163, 255;
    --flow-speed: 1.18s;
    left: 26%;
    top: 51%;
    width: 22%;
  }

  .flow-board--summary .line-inverter-ups {
    --flow-rgb: 0, 216, 255;
    --flow-speed: 1.26s;
    left: 53%;
    top: 51%;
    width: 21%;
  }

  .flow-board--summary .line-battery-inverter {
    --flow-rgb: 110, 224, 142;
    --flow-speed: 1.5s;
    left: 50%;
    top: 65%;
    width: 15%;
    transform: rotate(90deg);
  }

  .flow-board--summary .line-grid-load {
    --flow-rgb: 74, 163, 255;
    --flow-speed: 1.38s;
    left: 17%;
    top: 63%;
    width: 11%;
    transform: rotate(90deg);
  }

  .flow-board--detailed .line-pv1-inverter {
    left: 34%;
    top: 24%;
    width: 21%;
    transform: rotate(38deg);
  }

  .flow-board--detailed .line-pv2-inverter {
    left: 66%;
    top: 24%;
    width: 20%;
    transform: rotate(141deg);
  }

  .flow-board--detailed .line-battery-inverter {
    left: 84%;
    top: 72%;
    width: 36%;
    transform: rotate(-150deg);
  }

  .flow-board--detailed .line-inverter-grid {
    left: 45%;
    top: 48%;
    width: 24%;
    transform: rotate(180deg);
  }

  .flow-board--detailed .line-generator-inverter {
    left: 50%;
    top: 72%;
    width: 24%;
    transform: rotate(-78deg);
  }

  .flow-board--detailed .line-inverter-ups {
    left: 65%;
    top: 48%;
    width: 25%;
  }

  .flow-board--detailed .line-grid-load {
    left: 14%;
    top: 52%;
    width: 23%;
    transform: rotate(90deg);
  }

  @keyframes jks-power-flow-pulse {
    0% { transform: translateX(-115%) scaleX(0.55); opacity: 0; }
    4% { transform: translateX(-100%) scaleX(0.62); opacity: 0.22; }
    8% { transform: translateX(-82%) scaleX(0.7); opacity: 0.55; }
    12% { transform: translateX(-66%) scaleX(0.76); opacity: 0.76; }
    18% { transform: translateX(-44%) scaleX(0.85); opacity: 1; }
    26% { transform: translateX(-12%) scaleX(0.94); opacity: 1; }
    34% { transform: translateX(18%) scaleX(1); opacity: 1; }
    42% { transform: translateX(50%) scaleX(1); opacity: 1; }
    50% { transform: translateX(80%) scaleX(1); opacity: 1; }
    58% { transform: translateX(112%) scaleX(1); opacity: 1; }
    66% { transform: translateX(142%) scaleX(1); opacity: 1; }
    74% { transform: translateX(176%) scaleX(0.98); opacity: 1; }
    82% { transform: translateX(204%) scaleX(0.95); opacity: 1; }
    88% { transform: translateX(236%) scaleX(0.86); opacity: 0.82; }
    92% { transform: translateX(260%) scaleX(0.78); opacity: 0.6; }
    96% { transform: translateX(292%) scaleX(0.66); opacity: 0.28; }
    100% { transform: translateX(320%) scaleX(0.55); opacity: 0; }
  }

  @keyframes jks-power-flow-pulse-reverse {
    0% { transform: translateX(320%) scaleX(0.55); opacity: 0; }
    4% { transform: translateX(292%) scaleX(0.66); opacity: 0.28; }
    8% { transform: translateX(260%) scaleX(0.78); opacity: 0.6; }
    12% { transform: translateX(236%) scaleX(0.86); opacity: 0.82; }
    18% { transform: translateX(204%) scaleX(0.95); opacity: 1; }
    26% { transform: translateX(176%) scaleX(0.98); opacity: 1; }
    34% { transform: translateX(142%) scaleX(1); opacity: 1; }
    42% { transform: translateX(112%) scaleX(1); opacity: 1; }
    50% { transform: translateX(80%) scaleX(1); opacity: 1; }
    58% { transform: translateX(50%) scaleX(1); opacity: 1; }
    66% { transform: translateX(18%) scaleX(1); opacity: 1; }
    74% { transform: translateX(-12%) scaleX(0.94); opacity: 1; }
    82% { transform: translateX(-44%) scaleX(0.85); opacity: 1; }
    88% { transform: translateX(-66%) scaleX(0.76); opacity: 0.76; }
    92% { transform: translateX(-82%) scaleX(0.7); opacity: 0.55; }
    96% { transform: translateX(-100%) scaleX(0.62); opacity: 0.22; }
    100% { transform: translateX(-115%) scaleX(0.55); opacity: 0; }
  }

  @keyframes jks-power-flow-base {
    from { background-position: -72px 0, 0 0; }
    to { background-position: 72px 0, 0 0; }
  }

  @keyframes jks-power-flow-texture {
    from { background-position: 0 0; }
    to { background-position: 44px 0; }
  }

  @media (prefers-reduced-motion: reduce) {
    .flow-board--detailed .flow-line.active::before,
    .flow-board--detailed .flow-line.active::after,
    .flow-board--summary .flow-line.active::before,
    .flow-board--summary .flow-line.active::after {
      animation: none;
    }

    .flow-board--detailed .flow-line.active::before,
    .flow-board--summary .flow-line.active::before {
      opacity: 0.75;
      transform: translateX(80%);
    }
  }

  .flow-lines {
    position: absolute;
    inset: 0;
    z-index: 0;
    pointer-events: none;
  }

  .flow {
    fill: none;
    stroke-width: 0.3;
    stroke-linecap: round;
    stroke-linejoin: round;
    filter: drop-shadow(0 0 1.2px currentColor);
  }

  .flow--green { color: var(--green); stroke: var(--green); }
  .flow--blue { color: var(--blue); stroke: var(--blue); }
  .flow--cyan { color: var(--cyan); stroke: var(--cyan); }
  .flow--amber { color: var(--amber); stroke: var(--amber); }

  .node {
    position: relative;
    z-index: 1;
    min-width: 0;
    overflow: hidden;
    padding: 14px;
  }

  .flow-board--detailed .node {
    padding: var(--detail-node-pad);
  }

  .node::before {
    content: "";
    position: absolute;
    inset: 0;
    pointer-events: none;
    background: radial-gradient(circle at 50% 0%, var(--node-glow, transparent), transparent 60%);
    opacity: 0.75;
  }

  .node > * {
    position: relative;
    z-index: 1;
  }

  .node--pv,
  .node--pv-total {
    --node-glow: rgba(56, 239, 119, 0.16);
    border-color: rgba(56, 239, 119, 0.58);
  }

  .node--grid,
  .node--grid-load {
    --node-glow: rgba(74, 163, 255, 0.16);
    border-color: rgba(74, 163, 255, 0.58);
  }

  .node--inverter,
  .node--ups {
    --node-glow: rgba(0, 216, 255, 0.17);
    border-color: rgba(0, 216, 255, 0.58);
  }

  .node--generator {
    --node-glow: rgba(255, 191, 47, 0.16);
    border-color: rgba(255, 191, 47, 0.58);
  }

  .node--battery {
    --node-glow: rgba(56, 239, 119, 0.18);
    border-color: rgba(56, 239, 119, 0.58);
  }

  .energy-dashboard--detailed.is-pv1-offline .node--pv1,
  .energy-dashboard--detailed.is-pv2-offline .node--pv2,
  .energy-dashboard--detailed.is-grid-offline .node--grid,
  .energy-dashboard--detailed.is-grid-load-offline .node--grid-load,
  .energy-dashboard--detailed.is-ups-offline .node--ups,
  .energy-dashboard--detailed.is-generator-offline .node--generator,
  .energy-dashboard--detailed.is-battery-offline .node--battery {
    --node-glow: transparent;
    border-color: rgba(170, 185, 196, 0.26);
    background:
      linear-gradient(140deg, rgba(31, 42, 50, 0.72), rgba(5, 13, 18, 0.94));
    box-shadow: inset 0 0 30px rgba(255, 255, 255, 0.015);
  }

  .energy-dashboard--detailed.is-pv1-offline .node--pv1 .node-title,
  .energy-dashboard--detailed.is-pv2-offline .node--pv2 .node-title,
  .energy-dashboard--detailed.is-grid-offline .node--grid .node-title,
  .energy-dashboard--detailed.is-grid-load-offline .node--grid-load .node-title,
  .energy-dashboard--detailed.is-ups-offline .node--ups .node-title,
  .energy-dashboard--detailed.is-generator-offline .node--generator .node-title,
  .energy-dashboard--detailed.is-battery-offline .node--battery .battery-heading,
  .energy-dashboard--detailed.is-battery-offline .node--battery .battery-temp .icon {
    color: rgba(178, 191, 202, 0.72);
  }

  .energy-dashboard--detailed.is-pv1-offline .node--pv1 .power-readout strong,
  .energy-dashboard--detailed.is-pv2-offline .node--pv2 .power-readout strong,
  .energy-dashboard--detailed.is-grid-offline .node--grid .power-readout strong,
  .energy-dashboard--detailed.is-grid-load-offline .node--grid-load .power-readout strong,
  .energy-dashboard--detailed.is-ups-offline .node--ups .power-readout strong,
  .energy-dashboard--detailed.is-generator-offline .node--generator .power-readout strong,
  .energy-dashboard--detailed.is-generator-offline .node--generator .node-status,
  .energy-dashboard--detailed.is-battery-offline .node--battery .power-readout strong,
  .energy-dashboard--detailed.is-battery-offline .node--battery .battery-mode,
  .energy-dashboard--detailed.is-battery-offline .node--battery .battery-temp strong {
    color: rgba(178, 191, 202, 0.78);
  }

  .energy-dashboard--summary.is-pv-offline .node--pv-total,
  .energy-dashboard--summary.is-grid-offline .node--grid,
  .energy-dashboard--summary.is-grid-load-offline .node--grid-load,
  .energy-dashboard--summary.is-ups-offline .node--ups,
  .energy-dashboard--summary.is-battery-offline .node--battery {
    --node-glow: transparent;
    border-color: rgba(170, 185, 196, 0.26);
    background:
      linear-gradient(140deg, rgba(31, 42, 50, 0.72), rgba(5, 13, 18, 0.94));
    box-shadow: inset 0 0 30px rgba(255, 255, 255, 0.015);
  }

  .energy-dashboard--summary.is-pv-offline .node--pv-total .node-title,
  .energy-dashboard--summary.is-grid-offline .node--grid .node-title,
  .energy-dashboard--summary.is-grid-load-offline .node--grid-load .node-title,
  .energy-dashboard--summary.is-ups-offline .node--ups .node-title,
  .energy-dashboard--summary.is-battery-offline .node--battery .soc-ring,
  .energy-dashboard--summary.is-battery-offline .node--battery .battery-mode {
    color: rgba(178, 191, 202, 0.72);
  }

  .energy-dashboard--summary.is-pv-offline .node--pv-total .power-readout strong,
  .energy-dashboard--summary.is-grid-offline .node--grid .power-readout strong,
  .energy-dashboard--summary.is-grid-load-offline .node--grid-load .power-readout strong,
  .energy-dashboard--summary.is-ups-offline .node--ups .power-readout strong,
  .energy-dashboard--summary.is-battery-offline .node--battery .power-readout strong {
    color: rgba(178, 191, 202, 0.78);
  }

  .flow-board--summary .node--pv-total { grid-area: pv; }
  .flow-board--summary .node--grid { grid-area: grid; }
  .flow-board--summary .node--grid-load { grid-area: grid-load; }
  .flow-board--summary .summary-inverter { grid-area: inverter; }
  .flow-board--summary .node--ups { grid-area: load; }
  .flow-board--summary .summary-battery-node { grid-area: battery; }

  .energy-dashboard--summary {
    gap: 0;
    overflow: hidden;
  }

  .summary-scale-frame {
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    height: var(--summary-scaled-height, auto);
    overflow: hidden;
  }

  .summary-shell {
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    display: grid;
    grid-template-columns: minmax(164px, 214px) minmax(0, 1fr);
    gap: clamp(8px, 1cqw, 12px);
    align-items: stretch;
    height: clamp(380px, 34cqw, 380px);
  }

  .summary-chips {
    width: 100%;
    min-width: 0;
    min-inline-size: 0;
    max-width: 100%;
    max-inline-size: 100%;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: repeat(6, minmax(0, 1fr));
    gap: 6px;
    overflow: hidden;
  }

  .energy-dashboard--summary .chip {
    min-height: 0;
    grid-template-columns: 34px minmax(0, 1fr);
    gap: 8px;
    padding: 7px 9px;
  }

  .energy-dashboard--summary .chip > .icon {
    width: 34px;
    height: 34px;
    padding: 7px;
  }

  .energy-dashboard--summary .chip__trend {
    display: none;
  }

  .energy-dashboard--summary .chip__label {
    font-size: clamp(9px, 0.72cqw, 11px);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .energy-dashboard--summary .chip__value {
    font-size: clamp(15px, 1.25cqw, 21px);
  }

  .flow-board--summary .node {
    min-height: 82px;
    padding: 10px;
  }

  .flow-board--summary .summary-node {
    display: grid;
    align-content: center;
  }

  .flow-board--summary .node-title {
    grid-template-columns: 34px minmax(0, 1fr);
    gap: 8px;
    margin-bottom: 7px;
  }

  .flow-board--summary .node-title .icon {
    width: 32px;
    height: 32px;
    font-size: 32px;
  }

  .flow-board--summary .node h3 {
    font-size: clamp(14px, 1.12cqw, 18px);
  }

  .flow-board--summary .power-readout {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 7px;
    margin-top: 0;
    padding-top: 7px;
  }

  .flow-board--summary .power-readout > span {
    color: var(--muted);
    white-space: nowrap;
  }

  .flow-board--summary .power-readout strong {
    justify-content: flex-end;
    font-size: clamp(19px, 1.75cqw, 27px);
  }

  .flow-board--summary .node-status {
    margin-top: 6px;
    padding-top: 6px;
    font-size: clamp(10px, 0.82cqw, 12px);
  }

  .summary-inverter {
    min-height: 158px;
    display: grid;
    gap: 0;
    align-content: center;
    padding: clamp(11px, 1.2cqw, 15px);
  }

  .summary-inverter__head {
    display: grid;
    grid-template-columns: 46px minmax(0, 1fr);
    gap: 10px;
    align-items: center;
  }

  .summary-inverter__head .icon {
    width: 42px;
    height: 42px;
    padding: 8px;
    border-radius: 8px;
    color: var(--cyan);
    background: rgba(0, 216, 255, 0.1);
    box-shadow: 0 0 18px rgba(0, 216, 255, 0.18);
  }

  .summary-inverter__head h2 {
    font-size: clamp(18px, 1.5cqw, 24px);
    line-height: 1;
  }

  .summary-inverter__head span {
    display: block;
    margin-top: 4px;
    color: var(--green);
    font-weight: 700;
  }

  .summary-inverter__power {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 7px;
  }

  .summary-inverter__power .power-readout {
    min-width: 0;
    padding: 8px 10px;
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.04);
    overflow: hidden;
  }

  .summary-inverter-readout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-content: center;
    margin-top: 0;
    padding-top: 0;
    gap: 5px;
  }

  .flow-board--summary .summary-inverter-readout {
    grid-template-columns: minmax(0, 1fr);
  }

  .summary-readout-main {
    min-width: 0;
    display: grid;
    grid-template-columns: 16px minmax(0, 1fr);
    align-items: center;
    gap: 5px;
  }

  .summary-readout-main .icon {
    width: 16px;
    height: 16px;
  }

  .summary-readout-main strong {
    min-width: 0;
    justify-content: flex-start;
    gap: 3px;
    overflow: visible;
    font-size: clamp(13px, 1.18cqw, 17px);
  }

  .summary-readout-main strong span,
  .summary-readout-main strong small {
    flex: 0 0 auto;
  }

  .summary-readout-meta {
    min-width: 0;
    min-height: 18px;
    display: grid;
    grid-template-columns: 16px minmax(0, 1fr);
    align-items: center;
    justify-content: flex-start;
    gap: 5px;
    padding-top: 5px;
    border-top: 1px solid var(--line);
    color: var(--green);
  }

  .summary-readout-meta .icon {
    width: 16px;
    height: 16px;
  }

  .summary-readout-meta--cyan .icon {
    color: var(--cyan);
  }

  .summary-readout-meta--green .icon {
    color: var(--green);
  }

  .summary-readout-meta strong {
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    font-size: clamp(11px, 0.92cqw, 14px);
  }

  .flow-board--summary .summary-battery {
    gap: 10px;
  }

  .flow-board--summary .summary-battery .soc-ring {
    width: clamp(74px, 6cqw, 80px);
  }

  .summary-battery .battery-icon-stack {
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    margin-bottom: 0;
  }

  .summary-battery__data {
    min-width: 0;
    display: grid;
    gap: 0;
  }

  .flow-board--summary .summary-battery__data .power-readout {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding-inline: 0 10px;
  }

  .summary-battery__stats {
    display: grid;
    gap: 4px;
  }

  .flow-board--summary .summary-battery__stats .power-readout {
    min-height: 24px;
    display: grid;
    grid-template-columns: 17px minmax(0, 1fr) auto;
    gap: 6px;
    align-items: center;
    margin-top: 0;
    padding: 5px 10px 0 0;
    border-top: 1px solid var(--line);
    color: var(--muted);
  }

  .summary-battery__stats .icon {
    width: 16px;
    height: 16px;
    color: var(--green);
  }

  .summary-battery__stats .power-readout > span {
    font-size: clamp(10px, 0.82cqw, 12px);
  }

  .summary-battery__stats .power-readout strong {
    justify-content: flex-end;
    color: var(--green);
    font-size: clamp(11px, 0.92cqw, 14px);
  }

  @media (min-width: 1025px) {
    .energy-dashboard--summary .summary-shell {
      transform: scale(var(--summary-scale, 1));
      transform-origin: top left;
    }
  }

  h2,
  h3,
  p {
    margin: 0;
  }

  .node-title {
    display: grid;
    grid-template-columns: 46px minmax(0, 1fr);
    gap: 12px;
    align-items: center;
    margin-bottom: 11px;
  }

  .node-title .icon {
    width: 44px;
    height: 44px;
    font-size: 44px;
  }

  .flow-board--detailed .node-title {
    grid-template-columns: var(--detail-icon-size) minmax(0, 1fr);
    gap: 9px;
    margin-bottom: 8px;
  }

  .flow-board--detailed .node-title .icon {
    width: var(--detail-icon-size);
    height: var(--detail-icon-size);
    font-size: var(--detail-icon-size);
  }

  .node--pv .node-title,
  .node--pv-total .node-title,
  .node--battery .node-title { color: var(--green); }
  .node--grid .node-title,
  .node--grid-load .node-title { color: var(--blue); }
  .node--inverter .node-title,
  .node--ups .node-title { color: var(--cyan); }
  .node--generator .node-title { color: var(--amber); }

  .node h3,
  .system-panel h3 {
    color: #f4f8ff;
    font-size: clamp(15px, 1.2cqw, 22px);
    font-weight: 700;
    line-height: 1.05;
  }

  .flow-board--detailed .node h3 {
    font-size: var(--detail-title-size);
  }

  .node p {
    margin-top: 3px;
    color: var(--muted);
    font-size: clamp(10px, 0.82cqw, 13px);
  }

  .flow-board--detailed .node p {
    font-size: var(--detail-copy-size);
  }

  .metric-line,
  .phase-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 10px;
    align-items: baseline;
    color: var(--muted);
    font-size: clamp(11px, 0.9cqw, 15px);
    line-height: 1.5;
  }

  .flow-board--detailed .metric-line,
  .flow-board--detailed .phase-row {
    align-items: center;
    gap: 8px;
    font-size: var(--detail-copy-size);
    line-height: 1.38;
  }

  .flow-board--detailed .metric-line,
  .flow-board--detailed .phase-row,
  .flow-board--detailed .power-readout,
  .flow-board--detailed .node-status {
    min-height: 31px;
    border-top: 1px solid var(--line);
  }

  .flow-board--detailed .phase-row {
    padding-block: 0;
  }

  .metric-line strong,
  .phase-row strong {
    min-width: 0;
    color: rgba(244, 248, 255, 0.9);
    font-weight: 500;
    text-align: right;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .flow-board--detailed .metric-line strong,
  .flow-board--detailed .phase-row strong {
    overflow: visible;
    text-overflow: clip;
  }

  .phase-row {
    margin-top: 9px;
  }

  .flow-board--detailed .phase-row {
    margin-top: 0;
  }

  .power-readout {
    display: grid;
    gap: 5px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--line);
    color: var(--muted);
    font-size: clamp(11px, 0.9cqw, 15px);
  }

  .flow-board--detailed .power-readout {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px;
    align-items: center;
    margin-top: 0;
    padding-top: 0;
    font-size: var(--detail-copy-size);
  }

  .flow-board--detailed .power-readout > span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .power-readout strong {
    display: flex;
    align-items: baseline;
    gap: 6px;
    color: #f4f8ff;
    font-size: clamp(21px, 1.8cqw, 32px);
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
  }

  .flow-board--detailed .power-readout strong {
    align-items: center;
    gap: 5px;
    justify-content: flex-end;
    font-size: var(--detail-value-size);
  }

  .power-readout small {
    font-size: 0.62em;
    font-weight: 500;
  }

  .power-readout--green strong,
  .battery-mode { color: var(--green); }
  .power-readout--blue strong { color: var(--blue); }
  .power-readout--cyan strong { color: var(--cyan); }
  .power-readout--amber strong { color: var(--amber); }

  .flow-board--detailed .battery-mode {
    min-height: 30px;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    border-top: 1px solid var(--line);
    font-size: var(--detail-copy-size);
    font-weight: 700;
    text-align: right;
  }

  .node-status {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--line);
    font-size: clamp(12px, 0.95cqw, 16px);
    font-weight: 600;
  }

  .flow-board--detailed .node-status {
    gap: 7px;
    margin-top: 0;
    padding-top: 0;
    font-size: clamp(10px, 0.82cqw, 13px);
  }

  .node-status .icon {
    width: 22px;
    height: 22px;
  }

  .node-status--blue { color: var(--blue); }
  .node-status--amber { color: var(--amber); }

  .inverter-heading {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 18px;
    margin-bottom: 14px;
  }

  .inverter-heading .icon {
    width: 58px;
    height: 58px;
    font-size: 58px;
    padding: 10px;
    border: 1px solid rgba(0, 216, 255, 0.5);
    border-radius: 8px;
    color: var(--cyan);
    background: rgba(0, 148, 255, 0.12);
    box-shadow: 0 0 20px rgba(0, 216, 255, 0.22);
  }

  .inverter-heading h2 {
    font-size: clamp(22px, 1.8cqw, 30px);
    font-weight: 700;
  }

  .inverter-kpis {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 44px minmax(0, 1fr);
    gap: 12px;
    align-items: center;
  }

  .inverter-kpis .power-readout {
    margin: 0;
    padding: 10px 12px;
    border: 1px solid rgba(255, 255, 255, 0.065);
    border-radius: 8px;
    background: rgba(255, 255, 255, 0.035);
    text-align: center;
  }

  .inverter-kpis .power-readout strong {
    justify-content: center;
  }

  .sync-icon {
    width: 38px;
    height: 38px;
    color: var(--cyan);
    justify-self: center;
  }

  .inverter-lines {
    display: grid;
    max-width: 430px;
    margin: 14px auto 0;
  }

  .inverter-footer {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
    margin-top: 14px;
    padding-top: 14px;
    border-top: 1px solid var(--line);
  }

  .inverter-footer > div {
    display: grid;
    grid-template-columns: 30px minmax(0, 1fr);
    grid-template-areas: "icon label" "icon value";
    column-gap: 10px;
    align-items: center;
  }

  .inverter-footer .icon { grid-area: icon; width: 28px; height: 28px; color: var(--cyan); }
  .inverter-footer span { grid-area: label; color: var(--muted); font-size: clamp(10px, 0.8cqw, 14px); }
  .inverter-footer strong { grid-area: value; color: var(--cyan); font-size: clamp(13px, 0.95cqw, 18px); }
  .inverter-footer div:last-child .icon,
  .inverter-footer div:last-child strong { color: var(--green); }

  .flow-board--detailed .node--inverter {
    padding: 0;
    min-height: 222px;
  }

  .flow-board--detailed .inverter-layout {
    min-height: 100%;
    display: grid;
    grid-template-columns: minmax(172px, 0.86fr) minmax(246px, 1.14fr);
  }

  .flow-board--detailed .inverter-left,
  .flow-board--detailed .inverter-right {
    min-width: 0;
    padding: 12px;
  }

  .flow-board--detailed .inverter-left {
    display: grid;
    gap: 9px;
    align-content: center;
    border-right: 1px solid var(--line);
    background: rgba(0, 216, 255, 0.045);
  }

  .flow-board--detailed .inverter-right {
    display: grid;
    align-content: center;
  }

  .flow-board--detailed .inverter-state {
    min-width: 0;
    display: grid;
    grid-template-columns: 70px minmax(0, 1fr);
    gap: 14px;
    align-items: center;
    padding: 11px 13px;
    border: 1px solid rgba(56, 239, 119, 0.22);
    border-radius: 8px;
    background: rgba(56, 239, 119, 0.07);
  }

  .flow-board--detailed .inverter-state .icon {
    width: 50px;
    height: 50px;
    font-size: 30px;
    padding: 10px;
    border-radius: 8px;
    color: var(--green);
    background: rgba(56, 239, 119, 0.11);
    box-shadow: 0 0 16px rgba(56, 239, 119, 0.18);
  }

  .flow-board--detailed .inverter-state > div {
    min-width: 0;
  }

  .flow-board--detailed .inverter-state span {
    display: block;
    color: var(--muted);
    font-size: var(--detail-copy-size);
    line-height: 1.25;
  }

  .flow-board--detailed .inverter-state strong {
    display: block;
    margin-top: 4px;
    color: var(--green);
    font-size: clamp(15px, 1cqw, 19px);
    font-weight: 700;
    line-height: 1.1;
    white-space: nowrap;
  }

  .flow-board--detailed .inverter-kpis {
    grid-template-columns: 1fr;
    gap: 8px;
    align-items: stretch;
  }

  .flow-board--detailed .inverter-kpis .power-readout {
    min-height: 46px;
    gap: 10px;
    text-align: left;
  }

  .flow-board--detailed .inverter-heading {
    display: block;
    margin: 0 0 11px;
    text-align: left;
  }

  .flow-board--detailed .inverter-heading h2 {
    font-size: clamp(18px, 1.26cqw, 24px);
    line-height: 1;
  }

  .flow-board--detailed .inverter-heading span {
    display: block;
    margin-top: 5px;
    color: var(--muted);
    font-size: var(--detail-copy-size);
    font-weight: 600;
  }

  .flow-board--detailed .inverter-lines {
    max-width: none;
    margin: 0;
    gap: 0;
  }

  .flow-board--detailed .inverter-lines .metric-line {
    min-height: 29px;
    align-items: center;
    border-top: 1px solid var(--line);
  }

  .flow-board--detailed .inverter-lines .metric-line:first-child {
    border-top-color: rgba(255, 255, 255, 0.2);
  }

  .battery-heading {
    text-align: center;
    margin-bottom: 10px;
  }

  .battery-grid {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 12px;
    align-items: center;
  }

  .soc-ring {
    --soc-color: var(--green);
    --soc-track: rgba(239, 248, 255, 0.13);
    width: clamp(92px, 7.6cqw, 120px);
    aspect-ratio: 1;
    display: grid;
    place-items: center;
    border: 6px solid var(--soc-track);
    border-radius: 50%;
    color: #f4f8ff;
    box-shadow: 0 0 18px color-mix(in srgb, var(--soc-color) 24%, transparent);
  }

  .is-battery-empty .soc-ring { --soc-color: var(--red); }
  .is-battery-low .soc-ring { --soc-color: var(--amber); }
  .is-battery-half .soc-ring { --soc-color: #b8ef38; }
  .is-battery-full .soc-ring,
  .is-battery-charging .soc-ring { --soc-color: var(--green); }

  .is-battery-q1 .soc-ring { border-top-color: var(--soc-color); }
  .is-battery-q2 .soc-ring { border-right-color: var(--soc-color); }
  .is-battery-q3 .soc-ring { border-bottom-color: var(--soc-color); }
  .is-battery-q4 .soc-ring { border-left-color: var(--soc-color); }

  .battery-icon-stack {
    width: 36px;
    height: 30px;
    display: grid;
    place-items: center;
    margin-bottom: -14px;
  }

  .battery-icon-stack .icon {
    grid-area: 1 / 1;
    width: 31px;
    height: 31px;
    font-size: 31px;
    color: var(--soc-color);
    display: none;
  }

  .is-battery-charging .battery-icon-stack .icon--batteryBolt,
  .is-battery-empty:not(.is-battery-charging) .battery-icon-stack .icon--batteryEmpty,
  .is-battery-low:not(.is-battery-charging) .battery-icon-stack .icon--batteryLow,
  .is-battery-half:not(.is-battery-charging) .battery-icon-stack .icon--batteryHalf,
  .is-battery-full:not(.is-battery-charging) .battery-icon-stack .icon--batteryFull {
    display: inline-grid;
  }

  .soc-ring strong {
    display: flex;
    align-items: baseline;
    font-size: clamp(24px, 2cqw, 34px);
    line-height: 0.8;
  }

  .soc-ring small { font-size: 0.55em; }
  .soc-ring > span:last-child { font-size: 12px; font-weight: 650; }

  .battery-temp {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    gap: 7px;
    padding-top: 8px;
    border-top: 1px solid var(--line);
    color: var(--muted);
  }

  .battery-temp .icon,
  .summary-temp .icon { width: 24px; height: 24px; color: var(--green); }
  .battery-temp strong,
  .summary-temp strong { color: var(--green); }

  .summary-battery {
    height: 100%;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: 18px;
    align-items: center;
  }

  .summary-battery .power-readout {
    margin: 0;
    padding: 0 18px;
    border-top: 0;
    border-left: 1px solid var(--line);
    border-right: 1px solid var(--line);
  }

  .summary-temp {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .system-panel {
    display: grid;
    gap: 12px;
    padding: 14px 16px;
  }

  .energy-dashboard--detailed > .system-panel {
    order: -1;
    gap: 8px;
    padding: 10px 12px;
  }

  .system-panel header {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .system-panel header .icon {
    width: 28px;
    height: 28px;
    color: rgba(239, 248, 255, 0.74);
  }

  .energy-dashboard--detailed .system-panel header .icon {
    width: 22px;
    height: 22px;
  }

  .system-panel header h3 {
    flex: 1;
  }

  .online {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--green);
    font-weight: 600;
  }

  .energy-dashboard--detailed .online {
    font-size: var(--detail-copy-size);
  }

  .online i {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--green);
    box-shadow: 0 0 12px var(--green);
  }

  .system-grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 0 18px;
  }

  .system-grid > div {
    min-width: 0;
    min-height: 38px;
    display: grid;
    grid-template-columns: 26px minmax(0, 1fr) auto;
    gap: 10px;
    align-items: center;
    border-top: 1px solid var(--line);
    color: var(--muted);
    font-size: clamp(11px, 0.86cqw, 15px);
  }

  .energy-dashboard--detailed .system-grid {
    gap: 0 14px;
  }

  .energy-dashboard--detailed .system-grid > div {
    min-height: 30px;
    grid-template-columns: 22px minmax(0, 1fr) auto;
    gap: 8px;
    font-size: var(--detail-copy-size);
  }

  .system-grid .icon {
    width: 22px;
    height: 22px;
    color: var(--cyan);
  }

  .energy-dashboard--detailed .system-grid .icon {
    width: 18px;
    height: 18px;
  }

  .system-grid strong {
    color: rgba(244, 248, 255, 0.92);
    font-weight: 500;
    white-space: nowrap;
  }

  .system-panel footer {
    display: flex;
    align-items: center;
    gap: 8px;
    color: rgba(239, 248, 255, 0.46);
    font-size: clamp(12px, 0.9cqw, 15px);
  }

  .energy-dashboard--detailed .system-panel footer {
    font-size: var(--detail-copy-size);
  }

  .system-panel footer .icon {
    width: 20px;
    height: 20px;
  }

  .helper,
  .entity-map {
    border-radius: 8px;
    background: rgba(7, 24, 40, 0.88);
    border: 1px solid rgba(162, 221, 255, 0.14);
    padding: 10px 12px;
    font-size: clamp(10px, 1.25cqw, 12px);
    line-height: 1.45;
  }

  .helper--warn { color: #ffcf66; }

  .entity-map {
    display: grid;
    gap: 7px;
  }

  .entity-row {
    display: grid;
    grid-template-columns: minmax(0, 180px) minmax(0, 1fr);
    gap: 12px;
    font-family: Consolas, "Courier New", monospace;
  }

  .entity-row span:last-child {
    overflow-wrap: anywhere;
    color: rgba(239, 248, 255, 0.8);
  }

  @container (max-width: 1024px) {
    .card-shell {
      padding: 10px;
    }

    .energy-dashboard--detailed {
      --detail-card-gap: 14px;
      --detail-row-gap: 14px;
      --detail-node-pad: 11px;
      --detail-icon-size: 36px;
      --detail-title-size: 18px;
      --detail-copy-size: 13px;
      --detail-value-size: 24px;
    }

    .detailed-shell {
      grid-template-columns: 1fr;
      gap: 14px;
    }

    .sidebar-chips {
      grid-template-columns: repeat(auto-fit, minmax(min(100%, 146px), 1fr));
      grid-template-rows: none;
      gap: 8px;
    }

    .chip-row,
    .badge-row {
      grid-template-columns: repeat(auto-fit, minmax(166px, 1fr));
    }

    .flow-lines {
      display: none;
    }

    .flow-scale-frame {
      position: relative;
      width: 100%;
      height: var(--flow-scaled-height, auto);
      overflow: hidden;
    }

    .detail-flow-lines {
      display: block;
    }

    .flow-board--detailed {
      position: absolute;
      top: 0;
      left: 0;
      display: grid;
      width: 1024px;
      max-width: none;
      grid-template-columns: minmax(0, 1.04fr) minmax(0, 0.96fr);
      grid-template-areas:
        "pv1 pv2"
        "inverter ups"
        "inverter generator"
        "grid battery"
        "grid-load battery";
      grid-auto-rows: minmax(0, auto);
      min-height: 0;
      gap: 16px;
      overflow: visible;
      transform: scale(var(--flow-scale, 1));
      transform-origin: top left;
    }

    .flow-row,
    .flow-row--middle,
    .flow-row--bottom {
      display: contents;
    }

    .flow-board--detailed .node--pv1 { grid-area: pv1; }
    .flow-board--detailed .node--pv2 { grid-area: pv2; }
    .flow-board--detailed .node--inverter { grid-area: inverter; }
    .flow-board--detailed .node-slot--ups { grid-area: ups; }
    .flow-board--detailed .node-slot--grid { grid-area: grid; }
    .flow-board--detailed .node--generator { grid-area: generator; }
    .flow-board--detailed .node--battery { grid-area: battery; }
    .flow-board--detailed .node--grid-load { grid-area: grid-load; }

    .flow-board--detailed .node,
    .flow-board--detailed .node-slot {
      min-height: 0;
    }

    .flow-board--detailed .node-slot {
      display: grid;
      align-content: stretch;
    }

    .flow-board--detailed .node-slot > .node {
      min-height: 100%;
    }

    .flow-board--detailed .inverter-layout {
      grid-template-columns: 1fr;
    }

    .flow-board--detailed .inverter-left {
      border-right: 0;
      border-bottom: 1px solid var(--line);
    }

    .flow-board--detailed .inverter-kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .flow-board--detailed .inverter-state {
      grid-template-columns: 56px minmax(0, 1fr);
    }

    .flow-board--detailed .inverter-state .icon {
      width: 48px;
      height: 48px;
    }

    .flow-board--detailed .metric-line,
    .flow-board--detailed .phase-row {
      grid-template-columns: minmax(0, 0.86fr) minmax(0, 1.14fr);
    }

    .flow-board--detailed .metric-line strong,
    .flow-board--detailed .phase-row strong {
      white-space: normal;
      overflow-wrap: anywhere;
      line-height: 1.24;
    }

    .flow-board--detailed .power-readout {
      grid-template-columns: minmax(0, 0.86fr) minmax(0, 1.14fr);
    }

    .flow-board--detailed .power-readout strong {
      min-width: 0;
    }

    .flow-board--detailed .node--battery {
      display: grid;
      grid-template-rows: auto minmax(0, 1fr);
    }

    .flow-board--detailed .battery-grid {
      min-height: 100%;
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto auto minmax(196px, 1fr);
      grid-template-areas:
        "battery-data"
        "battery-temp"
        "battery-soc";
      align-items: start;
    }

    .flow-board--detailed .battery-data {
      grid-area: battery-data;
    }

    .flow-board--detailed .battery-temp {
      grid-area: battery-temp;
    }

    .flow-board--detailed .soc-ring {
      grid-area: battery-soc;
      align-self: end;
      justify-self: center;
      width: 190px;
      border-width: 8px;
      margin-top: 24px;
      margin-bottom: 24px;
    }

    .flow-board--detailed .soc-ring strong {
      font-size: 46px;
    }

    .flow-board--detailed .soc-ring > span:last-child {
      font-size: 15px;
    }

    .flow-board--detailed .battery-icon-stack {
      width: 50px;
      height: 42px;
      margin-bottom: -18px;
    }

    .flow-board--detailed .battery-icon-stack .icon {
      width: 42px;
      height: 42px;
      font-size: 42px;
    }

    .flow-board--detailed .line-pv1-inverter {
      left: 23%;
      top: 17%;
      width: 23%;
      transform: rotate(55deg);
    }

    .flow-board--detailed .line-pv2-inverter {
      left: 74%;
      top: 17%;
      width: 34%;
      transform: rotate(153deg);
    }

    .flow-board--detailed .line-inverter-ups {
      left: 46%;
      top: 37%;
      width: 10%;
    }

    .flow-board--detailed .line-generator-inverter {
      left: 57%;
      top: 52%;
      width: 13%;
      transform: rotate(180deg);
    }

    .flow-board--detailed .line-battery-inverter {
      left: 73%;
      top: 68%;
      width: 39%;
      transform: rotate(-160deg);
    }

    .flow-board--detailed .line-inverter-grid {
      left: 25%;
      top: 57%;
      width: 12%;
      transform: rotate(90deg);
    }

    .flow-board--detailed .line-grid-load {
      left: 25%;
      top: 79%;
      width: 12%;
      transform: rotate(90deg);
    }

    .system-grid {
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    }

    .chip {
      min-height: 72px;
      grid-template-columns: 44px minmax(0, 1fr) 24px;
      padding: 10px;
    }

    .chip > .icon {
      width: 42px;
      height: 42px;
      padding: 8px;
    }

    .sync-icon {
      display: none;
    }

    .summary-battery .power-readout {
      padding: 12px 0;
      border-left: 0;
      border-right: 0;
      border-top: 1px solid var(--line);
    }
  }

  @media (max-width: 767px) {
    .energy-dashboard--detailed {
      --detail-card-gap: 12px;
      --detail-row-gap: 12px;
      --detail-node-pad: 12px;
      --detail-icon-size: 38px;
      --detail-title-size: 18px;
      --detail-copy-size: 13px;
      --detail-value-size: 24px;
    }

    .energy-dashboard--detailed .sidebar-chips {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }

    .energy-dashboard--detailed .side-rail,
    .energy-dashboard--detailed .sidebar-chips,
    .energy-dashboard--detailed .side-rail .chip {
      max-width: 100%;
      min-width: 0;
    }

    .energy-dashboard--detailed .side-rail .chip {
      width: 100%;
      min-height: 56px;
      grid-template-columns: 32px minmax(0, 1fr);
      align-items: center;
      padding: 8px 12px;
    }

    .energy-dashboard--detailed .side-rail .chip__content {
      max-width: 100%;
      min-width: 0;
      grid-template-columns: minmax(0, 1fr);
      gap: 4px;
      overflow: visible;
    }

    .energy-dashboard--detailed .side-rail .chip__value {
      min-width: 0;
      margin-top: 0;
      justify-content: flex-start;
      overflow: visible;
    }

    .energy-dashboard--detailed .flow-scale-frame {
      position: relative;
      height: auto;
      overflow: visible;
    }

    .energy-dashboard--detailed .detail-flow-lines {
      display: none;
    }

    .energy-dashboard--detailed .flow-board--detailed {
      position: relative;
      top: auto;
      left: auto;
      width: 100%;
      max-width: 100%;
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        "pv1"
        "pv2"
        "grid"
        "generator"
        "battery"
        "inverter"
        "ups"
        "grid-load";
      gap: 12px;
      transform: none;
      overflow: visible;
    }

    .energy-dashboard--detailed .flow-board--detailed .node-slot,
    .energy-dashboard--detailed .flow-board--detailed .node-slot > .node {
      min-height: 0;
    }

    .energy-dashboard--detailed .flow-board--detailed .inverter-kpis .power-readout {
      gap: 5px;
    }

    .energy-dashboard--detailed .flow-board--detailed .power-readout strong {
      min-width: 0;
      overflow-wrap: anywhere;
      white-space: normal;
    }
  }

  @media (max-width: 1024px) {
    .energy-dashboard--summary .summary-shell {
      grid-template-columns: 1fr;
      gap: 8px;
      height: auto;
      min-height: 0;
    }

    .energy-dashboard--summary .summary-chips {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      grid-template-rows: none;
      gap: 8px;
    }

    .energy-dashboard--summary .chip {
      grid-template-columns: 38px minmax(0, 1fr);
      gap: 8px;
      padding: 8px;
      overflow: hidden;
    }

    .energy-dashboard--summary .chip > .icon {
      width: 36px;
      height: 36px;
      padding: 7px;
    }

    .energy-dashboard--summary .chip__trend {
      width: 18px;
      height: 18px;
    }

    .energy-dashboard--summary .chip__value {
      gap: 4px;
      font-size: clamp(16px, 4.8cqw, 22px);
      overflow: hidden;
      text-overflow: clip;
    }

    .flow-board--summary {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      grid-template-rows: none;
      grid-template-areas:
        "pv inverter"
        "grid inverter"
        "grid-load load"
        "battery battery";
      height: auto;
      min-height: auto;
      gap: 8px;
      padding: 0;
      overflow: hidden;
    }

    .flow-board--summary .summary-flow-lines {
      display: none;
    }

    .flow-board--summary .summary-inverter {
      min-height: 100%;
    }

    .flow-board--summary .node,
    .flow-board--summary .summary-inverter,
    .flow-board--summary .summary-inverter__head,
    .flow-board--summary .summary-battery,
    .flow-board--summary .summary-battery__data {
      min-width: 0;
    }

    .flow-board--summary .node {
      padding: 10px;
    }

    .flow-board--summary .node-title {
      grid-template-columns: 34px minmax(0, 1fr);
      gap: 8px;
    }

    .flow-board--summary .node-title .icon {
      width: 32px;
      height: 32px;
      font-size: 32px;
    }

    .flow-board--summary .summary-inverter__head {
      grid-template-columns: 46px minmax(0, 1fr);
      gap: 10px;
    }

    .flow-board--summary .summary-inverter__head .icon {
      width: 42px;
      height: 42px;
      padding: 8px;
    }

    .flow-board--summary .summary-inverter__head h2 {
      font-size: clamp(19px, 5.8cqw, 26px);
    }

    .flow-board--summary .summary-inverter__power {
      grid-template-columns: 1fr;
    }

    .flow-board--summary .summary-battery {
      grid-template-columns: auto minmax(0, 1fr);
      gap: 8px;
    }

    .flow-board--summary .summary-battery .soc-ring {
      width: 78px;
      border-width: 5px;
    }

    .flow-board--summary .summary-battery .battery-icon-stack {
      width: 30px;
      height: 30px;
      display: grid;
      place-items: center;
      margin-bottom: 0;
    }

    .flow-board--summary .summary-battery .battery-icon-stack .icon {
      width: 24px;
      height: 24px;
      font-size: 24px;
    }

    .flow-board--summary .summary-battery .soc-ring strong {
      font-size: 23px;
    }

    .flow-board--summary .summary-battery .soc-ring > span:last-child {
      font-size: 10px;
    }

    .flow-board--summary .summary-battery__data .power-readout {
      grid-template-columns: 1fr;
      gap: 5px;
      padding: 0;
      border-left: 0;
      border-right: 0;
    }

    .flow-board--summary .summary-battery__data .power-readout strong {
      font-size: clamp(18px, 5.4cqw, 24px);
    }

    .flow-board--summary .summary-battery__stats .power-readout {
      grid-template-columns: 17px minmax(0, 1fr) auto;
      gap: 6px;
      padding: 5px 0 0;
    }

    .flow-board--summary .summary-battery__stats .power-readout strong {
      font-size: clamp(12px, 3.4cqw, 15px);
    }

    .flow-board--summary .battery-mode {
      padding-right: 0;
    }
  }
`;
