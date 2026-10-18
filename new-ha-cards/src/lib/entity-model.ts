import type { HomeAssistant, HomeAssistantState } from "../types/home-assistant";

export type BaseUnit = "W" | "A" | "V" | "kWh" | "Hz" | "%" | "C" | "h" | "";

export interface EntityCandidate {
  // MQTT metric state key, not an assumed HA unique_id state attribute.
  canonicalId: string;
  names: string[];
}

export interface EntityDescriptor {
  // Ordered source preference, not interchangeable names for one reading.
  candidates: EntityCandidate[];
  unit: BaseUnit;
}

const metric = (canonicalId: string, names: string[], unit: BaseUnit): EntityDescriptor => ({
  candidates: [{ canonicalId, names }],
  unit
});

const fallback = (preferred: EntityDescriptor, canonicalId: string, names: string[]): EntityDescriptor => ({
  ...preferred,
  candidates: [...preferred.candidates, { canonicalId, names }]
});

// No canonical measurement exists. Only a deliberate user override can bind it.
const explicitOnly = (unit: BaseUnit): EntityDescriptor => ({ candidates: [], unit });

// Prefer Shelly voltage on the shared grid bus, native external Jinko CTs for
// GRID current/power, and BMS for Battery, with explicit inverter fallbacks.
// Grid daily import/export remain inverter counters, not CT energy measurements.
export const ENTITY_DEFINITIONS = {
  pv_total_power: metric("electric_s_p_t", ["Total Solar Power"], "W"),
  pv_daily_energy: metric("electric_pv_d_p_g", ["PV daily power generation (active)"], "kWh"),
  pv1_voltage: metric("electric_dv1", ["DC Voltage PV1"], "V"),
  pv1_current: metric("electric_dc1", ["DC Current PV1"], "A"),
  pv1_power: metric("electric_dp1", ["DC Power PV1"], "W"),
  pv2_voltage: metric("electric_dv2", ["DC Voltage PV2"], "V"),
  pv2_current: metric("electric_dc2", ["DC Current PV2"], "A"),
  pv2_power: metric("electric_dp2", ["DC Power PV2"], "W"),
  grid_total_power: fallback(fallback(metric("grid_ct_t_e", ["Total External CT Power"], "W"), "grid_pg_pt1", ["Total Grid Power"]), "grid_gs_t", ["Internal Power"]),
  grid_frequency: metric("grid_pg_f1", ["Grid Frequency"], "Hz"),
  grid_buy_today: metric("grid_e_b_d", ["Daily Energy Buy"], "kWh"),
  grid_sell_today: metric("grid_e_s_d", ["Daily energy sell"], "kWh"),
  grid_l1_voltage: fallback(metric("grid_load_l1_voltage", ["Grid Load L1 Voltage"], "V"), "grid_g_v_l1", ["Grid Voltage L1"]),
  grid_l2_voltage: fallback(metric("grid_load_l2_voltage", ["Grid Load L2 Voltage"], "V"), "grid_g_v_l2", ["Grid Voltage L2"]),
  grid_l3_voltage: fallback(metric("grid_load_l3_voltage", ["Grid Load L3 Voltage"], "V"), "grid_g_v_l3", ["Grid Voltage L3"]),
  grid_l1_current: fallback(metric("grid_jinko_modbus_r613_external_ct_l1_current", ["External CT Current L1"], "A"), "grid_g_c_l1", ["Grid Current L1"]),
  grid_l2_current: fallback(metric("grid_jinko_modbus_r614_external_ct_l2_current", ["External CT Current L2"], "A"), "grid_g_c_l2", ["Grid Current L2"]),
  grid_l3_current: fallback(metric("grid_jinko_modbus_r615_external_ct_l3_current", ["External CT Current L3"], "A"), "grid_g_c_l3", ["Grid Current L3"]),
  grid_l1_power: fallback(fallback(metric("grid_ct1_p_e", ["External CT1 Power"], "W"), "grid_g_p_l1", ["Grid Power L1"]), "grid_gs_a", ["Internal L1 Power"]),
  grid_l2_power: fallback(fallback(metric("grid_ct2_p_e", ["External CT2 Power"], "W"), "grid_g_p_l2", ["Grid Power L2"]), "grid_gs_b", ["Internal L2 Power"]),
  grid_l3_power: fallback(fallback(metric("grid_ct3_p_e", ["External CT3 Power"], "W"), "grid_g_p_l3", ["Grid Power L3"]), "grid_gs_c", ["Internal L3 Power"]),
  // The upstream meter is before the stabilizers. Never substitute downstream
  // grid/grid-load readings when it is missing: those belong to another bus.
  grid_input_total_power: metric("grid_input_total_power", ["Grid Input Total Power"], "W"),
  grid_input_total_current: metric("grid_input_total_current", ["Grid Input Total Current"], "A"),
  grid_input_l1_voltage: metric("grid_input_l1_voltage", ["Grid Input L1 Voltage"], "V"),
  grid_input_l2_voltage: metric("grid_input_l2_voltage", ["Grid Input L2 Voltage"], "V"),
  grid_input_l3_voltage: metric("grid_input_l3_voltage", ["Grid Input L3 Voltage"], "V"),
  grid_input_l1_current: metric("grid_input_l1_current", ["Grid Input L1 Current"], "A"),
  grid_input_l2_current: metric("grid_input_l2_current", ["Grid Input L2 Current"], "A"),
  grid_input_l3_current: metric("grid_input_l3_current", ["Grid Input L3 Current"], "A"),
  grid_input_l1_power: metric("grid_input_l1_power", ["Grid Input L1 Power"], "W"),
  grid_input_l2_power: metric("grid_input_l2_power", ["Grid Input L2 Power"], "W"),
  grid_input_l3_power: metric("grid_input_l3_power", ["Grid Input L3 Power"], "W"),
  grid_input_frequency: fallback(fallback(metric("grid_input_l1_frequency", ["Grid Input L1 Frequency"], "Hz"), "grid_input_l2_frequency", ["Grid Input L2 Frequency"]), "grid_input_l3_frequency", ["Grid Input L3 Frequency"]),
  grid_input_energy_total: metric("grid_input_energy_total", ["Grid Input Energy Total"], "kWh"),
  grid_input_returned_energy_total: metric("grid_input_returned_energy_total", ["Grid Input Returned Energy Total"], "kWh"),
  // Daily HA utility meters, not the bridge's cumulative counters.
  grid_input_buy_today: metric("grid_input_energy_today", ["Grid Input Energy Today"], "kWh"),
  grid_input_sell_today: metric("grid_input_returned_energy_today", ["Grid Input Returned Energy Today"], "kWh"),
  grid_load_total_power: metric("grid_load_total_power", ["Grid Load Total Power"], "W"),
  grid_load_total_current: metric("grid_load_total_current", ["Grid Load Total Current"], "A"),
  grid_load_l1_voltage: metric("grid_load_l1_voltage", ["Grid Load L1 Voltage"], "V"),
  grid_load_l2_voltage: metric("grid_load_l2_voltage", ["Grid Load L2 Voltage"], "V"),
  grid_load_l3_voltage: metric("grid_load_l3_voltage", ["Grid Load L3 Voltage"], "V"),
  grid_load_l1_current: metric("grid_load_l1_current", ["Grid Load L1 Current"], "A"),
  grid_load_l2_current: metric("grid_load_l2_current", ["Grid Load L2 Current"], "A"),
  grid_load_l3_current: metric("grid_load_l3_current", ["Grid Load L3 Current"], "A"),
  grid_load_l1_power: metric("grid_load_l1_power", ["Grid Load L1 Power"], "W"),
  grid_load_l2_power: metric("grid_load_l2_power", ["Grid Load L2 Power"], "W"),
  grid_load_l3_power: metric("grid_load_l3_power", ["Grid Load L3 Power"], "W"),
  home_total_power: metric("consumption_e_puse_t1", ["Total Consumption Power"], "W"),
  home_daily_energy: metric("consumption_etdy_use1", ["Daily Consumption"], "kWh"),
  home_frequency: metric("consumption_l_f", ["Load Fequency","Load Frequency"], "Hz"),
  home_l1_voltage: metric("consumption_c_v_l1", ["Load Voltage L1"], "V"),
  home_l2_voltage: metric("consumption_c_v_l2", ["Load Voltage L2"], "V"),
  home_l3_voltage: metric("consumption_c_v_l3", ["Load Voltage L3"], "V"),
  home_l1_power: metric("consumption_c_p_l1", ["Load Power L1"], "W"),
  home_l2_power: metric("consumption_c_p_l2", ["Load Power L2"], "W"),
  home_l3_power: metric("consumption_c_p_l3", ["Load Power L3"], "W"),
  ups_total_power: metric("ups_ups_p", ["UPS Load Power"], "W"),
  generator_daily_energy: metric("generator_gen_p_d", ["Daily Production Generator"], "kWh"),
  generator_total_power: fallback(metric("generator_gen_p_t", ["Total Gen Power"], "W"), "generator_eg_p_ct1", ["Generator Active Power"]),
  generator_l1_power: metric("generator_gen_p_l1", ["Gen Power L1"], "W"),
  generator_l2_power: metric("generator_gen_p_l2", ["Gen Power L2"], "W"),
  generator_l3_power: metric("generator_gen_p_l3", ["Gen Power L3"], "W"),
  generator_l1_voltage: metric("generator_gen_v_l1", ["Gen Voltage L1"], "V"),
  generator_l2_voltage: metric("generator_gen_v_l2", ["Gen Voltage L2"], "V"),
  generator_l3_voltage: metric("generator_gen_v_l3", ["Gen Voltage L3"], "V"),
  generator_daily_runtime: metric("generator_r_t_d", ["Gen Daily Run Time"], "h"),
  battery_voltage: fallback(metric("bms_bms_b_v1", ["BMS Voltage"], "V"), "battery_b_v1", ["Battery Voltage"]),
  battery_current: fallback(metric("bms_bms_b_c1", ["BMS Current"], "A"), "battery_b_c1", ["Battery Current"]),
  battery_power: metric("battery_b_p1", ["Battery Power"], "W"),
  battery_soc: fallback(metric("bms_bms_soc", ["BMS_SOC"], "%"), "battery_b_left_cap1", ["SoC"]),
  battery_temp: fallback(metric("bms_bmst", ["BMS Temperature"], "C"), "temperature_b_t1", ["Temperature- Battery"]),
  battery_charge_today: metric("battery_etdy_cg1", ["Daily Charging Energy"], "kWh"),
  battery_discharge_today: metric("battery_etdy_dcg1", ["Daily Discharging Energy"], "kWh"),
  inverter_total_power: metric("electric_inv_o_p_t", ["Total Inverter Output Power"], "W"),
  inverter_daily_energy: metric("electric_etdy_ge1", ["Daily Production (Active)"], "kWh"),
  inverter_l1_power: metric("electric_inv_o_p_l1", ["Inverter Output Power L1"], "W"),
  inverter_l2_power: metric("electric_inv_o_p_l2", ["Inverter Output Power L2"], "W"),
  inverter_l3_power: metric("electric_inv_o_p_l3", ["Inverter Output Power L3"], "W"),
  inverter_l1_voltage: metric("electric_av1", ["AC Voltage R/U/A"], "V"),
  inverter_l2_voltage: metric("electric_av2", ["AC Voltage S/V/B"], "V"),
  inverter_l3_voltage: metric("electric_av3", ["AC Voltage T/W/C"], "V"),
  inverter_l1_current: metric("electric_ac1", ["AC Current R/U/A"], "A"),
  inverter_l2_current: metric("electric_ac2", ["AC Current S/V/B"], "A"),
  inverter_l3_current: metric("electric_ac3", ["AC Current T/W/C"], "A"),
  inverter_frequency: metric("electric_a_fo1", ["AC Output Frequency R"], "Hz"),
  power_factor: metric("electric_p_f1", ["Power factor"], ""),
  dc_temperature: metric("temperature_t_dc", ["DC Temperature"], "C"),
  inverter_dc_power: explicitOnly("W"),
  ac_temperature: metric("temperature_ac_t", ["AC Temperature"], "C"),
  dc_bus_voltage: metric("basic_bn_v", ["Bus-N Voltage"], "V"),
} satisfies Record<string, EntityDescriptor>;

export type EntityKey = keyof typeof ENTITY_DEFINITIONS;
export type ResolvedEntityMap = Partial<Record<EntityKey, string | null>>;
export type EntityOverrides = Partial<Record<EntityKey, string>>;
export const ENTITY_KEYS = Object.keys(ENTITY_DEFINITIONS) as EntityKey[];

export const normalizeText = (value: unknown): string =>
  String(value ?? "")
    .toLowerCase()
    .replace(/_/g, " ")
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();

const UNIT_CONVERSIONS: Record<string, { base: BaseUnit; scale: number }> = {
  W: { base: "W", scale: 1 },
  kW: { base: "W", scale: 1000 },
  A: { base: "A", scale: 1 },
  mA: { base: "A", scale: 0.001 },
  V: { base: "V", scale: 1 },
  mV: { base: "V", scale: 0.001 },
  kWh: { base: "kWh", scale: 1 },
  Wh: { base: "kWh", scale: 0.001 },
  Hz: { base: "Hz", scale: 1 },
  "%": { base: "%", scale: 1 },
  C: { base: "C", scale: 1 },
  "°C": { base: "C", scale: 1 },
  "℃": { base: "C", scale: 1 },
  h: { base: "h", scale: 1 }
};

export const toNumber = (stateObj: HomeAssistantState | null | undefined, expectedUnit?: BaseUnit): number | null => {
  if (!stateObj || typeof stateObj.state !== "string") return null;
  const text = stateObj.state.trim();
  // Number()/parseFloat() alone accept empty, hexadecimal or suffixed text.
  if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(text)) return null;
  const value = Number(text);
  if (!Number.isFinite(value)) return null;

  const rawUnit = stateObj.attributes?.unit_of_measurement;
  // Preserve legacy custom entities without units, interpreted in the key's
  // documented base unit; an explicit unsupported/different unit fails closed.
  if (rawUnit === undefined || rawUnit === null || rawUnit === "") return value;
  if (typeof rawUnit !== "string") return null;
  const unit = rawUnit.trim();
  if (!Object.hasOwn(UNIT_CONVERSIONS, unit)) return null;
  const conversion = UNIT_CONVERSIONS[unit]!;
  if (expectedUnit !== undefined && conversion.base !== expectedUnit) return null;
  const converted = value * conversion.scale;
  return Number.isFinite(converted) ? converted : null;
};

export const resolveEntities = (
  hass: HomeAssistant | undefined,
  overrides: EntityOverrides = {},
  keys: readonly EntityKey[] = ENTITY_KEYS
): ResolvedEntityMap => {
  const resolved: ResolvedEntityMap = {};
  if (!hass) return resolved;

  const candidates = prepareEntities(hass);
  for (const key of keys) {
    resolved[key] = resolveEntity(hass, candidates, key, overrides[key]);
  }
  return resolved;
};

// HA normally prefixes the device name. Match complete metric names at token
// boundaries; a longer known metric (BMS_SOC) must not match a short one (SoC).
const isMetricSuffix = (candidate: string, metricName: string): boolean =>
  candidate === metricName || candidate.endsWith(" " + metricName);

const ALL_CANDIDATES = Object.values(ENTITY_DEFINITIONS).flatMap((definition) => definition.candidates);
const CANONICAL_IDS = [...new Set(ALL_CANDIDATES.map((candidate) => normalizeText(candidate.canonicalId)))]
  .sort((a, b) => b.length - a.length);
const METRIC_NAMES = [...new Set([
  ...ALL_CANDIDATES.flatMap((candidate) => candidate.names),
  // These Shelly readings are not the inverter power factor.
  "Grid Load L1 Power Factor", "Grid Load L2 Power Factor", "Grid Load L3 Power Factor",
  "Grid Input L1 Power Factor", "Grid Input L2 Power Factor", "Grid Input L3 Power Factor"
].map(normalizeText))].sort((a, b) => b.length - a.length);

const completeMetricName = (text: string): string | undefined =>
  METRIC_NAMES.find((name) => isMetricSuffix(text, name));

interface PreparedEntity {
  entityId: string;
  state: HomeAssistantState;
  friendly: string;
  canonicalId?: string;
  friendlyMetric?: string;
  entityMetric?: string;
}

// Normalize the HA inventory once per resolution, not once per field/candidate.
const prepareEntities = (hass: HomeAssistant): PreparedEntity[] =>
  Object.entries(hass.states).filter(([entityId]) => entityId.startsWith("sensor.")).map(([entityId, state]) => {
    const friendly = normalizeText(state.attributes?.friendly_name);
    const normalizedEntityId = normalizeText(entityId.slice("sensor.".length));
    return {
      entityId,
      state,
      friendly,
      canonicalId: CANONICAL_IDS.find((id) => isMetricSuffix(normalizedEntityId, id)),
      friendlyMetric: completeMetricName(friendly),
      entityMetric: completeMetricName(normalizedEntityId)
    };
  });

const resolveEntity = (hass: HomeAssistant, entities: PreparedEntity[], key: EntityKey, override?: string): string | null => {
  // A missing/unavailable configured entity must not switch to another source.
  if (override !== undefined) return override && Object.hasOwn(hass.states, override) ? override : null;

  const descriptor = ENTITY_DEFINITIONS[key];
  for (const candidate of descriptor.candidates) {
    let bestId: string | null = null;
    let bestScore = 0;
    let tied = false;
    const canonicalId = normalizeText(candidate.canonicalId);

    for (const { entityId, state, friendly, canonicalId: knownCanonical, friendlyMetric, entityMetric } of entities) {
      if (toNumber(state, descriptor.unit) === null) continue;
      if (knownCanonical && knownCanonical !== canonicalId) continue;
      let score = knownCanonical === canonicalId ? 400 : 0;

      for (const name of candidate.names) {
        const normalizedName = normalizeText(name);
        if (friendlyMetric === normalizedName) score = Math.max(score, friendly === normalizedName ? 300 : 200);
        if (entityMetric === normalizedName) score = Math.max(score, 100);
      }

      if (score > bestScore) {
        bestId = entityId;
        bestScore = score;
        tied = false;
      } else if (score > 0 && score === bestScore) {
        tied = true;
      }
    }
    // Ambiguous primary sources require an override, not a silent lower-tier choice.
    if (tied) return null;
    if (bestId) return bestId;
  }
  return null;
};

export const stateFor = (hass: HomeAssistant | undefined, resolved: ResolvedEntityMap, key: EntityKey): HomeAssistantState | null => {
  const entityId = resolved[key];
  return entityId ? (hass?.states[entityId] ?? null) : null;
};

export const valueFor = (hass: HomeAssistant | undefined, resolved: ResolvedEntityMap, key: EntityKey): number | null =>
  toNumber(stateFor(hass, resolved, key), ENTITY_DEFINITIONS[key].unit);
