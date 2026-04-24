import type { HomeAssistant, HomeAssistantState } from "../types/home-assistant";

export interface EntityDescriptor {
  names: string[];
}

export const ENTITY_DEFINITIONS = {
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
  dc_temperature: { names: ["DC Temperature"] }
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

export const toNumber = (stateObj: HomeAssistantState | null | undefined): number | null => {
  if (!stateObj) return null;
  const value = Number.parseFloat(stateObj.state);
  return Number.isFinite(value) ? value : null;
};

export const resolveEntities = (
  hass: HomeAssistant | undefined,
  overrides: EntityOverrides = {},
  keys: readonly EntityKey[] = ENTITY_KEYS
): ResolvedEntityMap => {
  const resolved: ResolvedEntityMap = {};
  if (!hass) {
    return resolved;
  }

  for (const key of keys) {
    resolved[key] = resolveEntity(hass, key, overrides[key]);
  }

  return resolved;
};

const resolveEntity = (hass: HomeAssistant, key: EntityKey, override?: string): string | null => {
  if (override && hass.states[override]) {
    return override;
  }

  const descriptor = ENTITY_DEFINITIONS[key];
  let best: { entityId: string; score: number } | null = null;

  for (const [entityId, stateObj] of Object.entries(hass.states)) {
    if (!entityId.startsWith("sensor.")) continue;

    const friendly = normalizeText(stateObj.attributes?.friendly_name);
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

  return best?.entityId ?? null;
};

export const stateFor = (hass: HomeAssistant | undefined, resolved: ResolvedEntityMap, key: EntityKey): HomeAssistantState | null => {
  const entityId = resolved[key];
  return entityId ? (hass?.states[entityId] ?? null) : null;
};

export const valueFor = (hass: HomeAssistant | undefined, resolved: ResolvedEntityMap, key: EntityKey): number | null =>
  toNumber(stateFor(hass, resolved, key));
