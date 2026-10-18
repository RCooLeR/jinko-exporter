import assert from "node:assert/strict";
import test from "node:test";

import { ENTITY_DEFINITIONS, ENTITY_KEYS, normalizeText, resolveEntities, stateFor, toNumber, valueFor, type BaseUnit, type EntityKey } from "./entity-model.ts";
import type { HomeAssistant, HomeAssistantState } from "../types/home-assistant.ts";

const state = (value: string, friendlyName = "", unit?: unknown): HomeAssistantState => ({
  state: value,
  attributes: { friendly_name: friendlyName, ...(unit === undefined ? {} : { unit_of_measurement: unit }) }
});
const hassWithStates = (states: Record<string, HomeAssistantState>): HomeAssistant => ({ states });

test("upstream Shelly is isolated from every downstream source", () => {
  for (const phase of [1, 2, 3]) {
    const onlyUpstreamPF = hassWithStates({
      [`sensor.jinko_grid_input_l${phase}_power_factor`]: state("0.45", `Jinko Grid Input L${phase} Power Factor`)
    });
    assert.equal(valueFor(onlyUpstreamPF, resolveEntities(onlyUpstreamPF), "power_factor"), null,
      "one upstream power factor must not become inverter power factor");
  }
  const hass = hassWithStates({
    "sensor.downstream": state("230", "Grid Load L1 Voltage", "V"),
    "sensor.inverter": state("231", "Grid Voltage L1", "V"),
    "sensor.downstream_power": state("20", "Grid Load Total Power", "W"),
    "sensor.ct": state("-100", "Total External CT Power", "W"),
    "sensor.downstream_energy": state("42", "Grid Load Energy Total", "kWh")
  });
  const upstreamKeys = ENTITY_KEYS.filter((key) => key.startsWith("grid_input_"));
  for (const key of upstreamKeys) {
    assert.equal(valueFor(hass, resolveEntities(hass), key), null, key);
  }
  hass.states["sensor.upstream"] = state("249", "Grid Input L1 Voltage", "V");
  hass.states["sensor.input_power"] = state("0", "Grid Input Total Power", "W");
  hass.states["sensor.input_energy"] = state("0", "Grid Input Energy Total", "kWh");
  const resolved = resolveEntities(hass);
  assert.equal(valueFor(hass, resolved, "grid_input_l1_voltage"), 249);
  assert.equal(valueFor(hass, resolved, "grid_input_total_power"), 0);
  assert.equal(valueFor(hass, resolved, "grid_input_energy_total"), 0);
  assert.equal(valueFor(hass, resolved, "grid_l1_voltage"), 230);
  assert.equal(valueFor(hass, resolved, "grid_load_l1_voltage"), 230);
  assert.equal(valueFor(hass, resolved, "grid_total_power"), -100);
  hass.states["sensor.upstream"]!.state = "unavailable";
  assert.equal(valueFor(hass, resolveEntities(hass), "grid_input_l1_voltage"), null);
  const upstreamOnly = hassWithStates({ "sensor.only": state("249", "Grid Input L1 Voltage", "V") });
  assert.equal(valueFor(upstreamOnly, resolveEntities(upstreamOnly), "grid_l1_voltage"), null);
  assert.equal(valueFor(upstreamOnly, resolveEntities(upstreamOnly), "grid_load_l1_voltage"), null);
});


test("upstream daily utility meters never resolve cumulative or downstream counters", () => {
  const hass = hassWithStates({
    "sensor.device_grid_input_energy_total": state("9999", "Grid Input Energy Total", "kWh"),
    "sensor.device_grid_input_returned_energy_total": state("8888", "Grid Input Returned Energy Total", "kWh"),
    "sensor.device_grid_e_b_d": state("5", "Daily Energy Buy", "kWh"),
    "sensor.device_grid_e_s_d": state("8", "Daily energy sell", "kWh")
  });
  for (const key of ["grid_input_buy_today", "grid_input_sell_today"] as const) {
    assert.equal(valueFor(hass, resolveEntities(hass), key), null);
  }
  hass.states["sensor.import_helper"] = state("0", "Grid Input Energy Today", "kWh");
  hass.states["sensor.export_helper"] = state("2500", "Grid Input Returned Energy Today", "Wh");
  const resolved = resolveEntities(hass);
  assert.equal(valueFor(hass, resolved, "grid_input_buy_today"), 0);
  assert.equal(valueFor(hass, resolved, "grid_input_sell_today"), 2.5);
  assert.equal(valueFor(hass, resolved, "grid_input_energy_total"), 9999);
  assert.equal(valueFor(hass, resolved, "grid_input_returned_energy_total"), 8888);
  assert.equal(valueFor(hass, resolved, "grid_buy_today"), 5);
  assert.equal(valueFor(hass, resolved, "grid_sell_today"), 8);
  hass.states["sensor.import_helper"]!.state = "unavailable";
  assert.equal(valueFor(hass, resolveEntities(hass), "grid_input_buy_today"), null);

  hass.states["sensor.my_daily_import"] = state("3", "My daily upstream import", "kWh");
  const overridden = resolveEntities(hass, { grid_input_buy_today: "sensor.my_daily_import" });
  assert.equal(valueFor(hass, overridden, "grid_input_buy_today"), 3);
});

test("normalization handles MQTT names and HA entity ids without relying on unique_id", () => {
  assert.equal(normalizeText("Grid\u00a0Voltage L1"), "grid voltage l1");
  assert.equal(normalizeText("sensor.jinko_grid_voltage_l1"), "sensor jinko grid voltage l1");
  const hass = hassWithStates({
    "sensor.jinko_dc_power_pv1": state("1250", "Jinko DC Power PV1"),
    "sensor.garazh_grid_load_total_power": state("-167", "Garazh Grid Load Total Power"),
    "sensor.fake": { state: "1", attributes: { unique_id: "device_electric_dp2" } }
  });
  const resolved = resolveEntities(hass, {}, ["pv1_power", "pv2_power", "grid_load_total_power"]);
  assert.equal(resolved.pv1_power, "sensor.jinko_dc_power_pv1");
  assert.equal(resolved.pv2_power, null);
  assert.equal(resolved.grid_load_total_power, "sensor.garazh_grid_load_total_power");
});

test("every canonical metric candidate resolves from an exact state-key entity suffix", () => {
  for (const key of ENTITY_KEYS) {
    const definition = ENTITY_DEFINITIONS[key];
    for (const candidate of definition.candidates) {
      const entityId = "sensor.renamed_device_" + candidate.canonicalId;
      const hass = hassWithStates({ [entityId]: state("0", "Custom caption", definition.unit) });
      assert.equal(resolveEntities(hass, {}, [key])[key], entityId, key + ": " + candidate.canonicalId);
      assert.equal(valueFor(hass, { [key]: entityId }, key), 0, key);
    }
  }
});

test("native CT power is preferred over selected grid, then independent internal power", () => {
  const hass = hassWithStates({
    "sensor.selected": state("100", "Total Grid Power", "W"),
    "sensor.internal": state("50", "Internal Power", "W"),
    "sensor.external": state("-0.167", "Jinko Total External CT Power", "kW")
  });
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.external");
  assert.equal(valueFor(hass, resolveEntities(hass), "grid_total_power"), -167);
  hass.states["sensor.external"]!.state = "unavailable";
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.selected");
  hass.states["sensor.selected"]!.state = "unknown";
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.internal");
  hass.states["sensor.external"]!.state = "0";
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.external");
  assert.equal(valueFor(hass, resolveEntities(hass), "grid_total_power"), 0);
});

test("all grid phases prefer native CT power/current and only use declared fallback sources", () => {
  for (const phase of [1, 2, 3]) {
    const powerKey = ("grid_l" + phase + "_power") as EntityKey;
    const currentKey = ("grid_l" + phase + "_current") as EntityKey;
    const hass = hassWithStates({
      "sensor.selected": state("20", "Grid Power L" + phase),
      "sensor.internal": state("30", "Internal L" + phase + " Power"),
      "sensor.internal_current": state("1", "Grid Current L" + phase),
      "sensor.ct_power": state("-10", "Jinko External CT" + phase + " Power"),
      "sensor.ct_current": state("-0.2", "Jinko External CT Current L" + phase)
    });
    let resolved = resolveEntities(hass, {}, [powerKey, currentKey]);
    assert.equal(resolved[powerKey], "sensor.ct_power");
    assert.equal(resolved[currentKey], "sensor.ct_current");
    hass.states["sensor.ct_power"]!.state = "unavailable";
    hass.states["sensor.ct_current"]!.state = "unavailable";
    resolved = resolveEntities(hass, {}, [powerKey, currentKey]);
    assert.equal(resolved[powerKey], "sensor.selected");
    assert.equal(resolved[currentKey], "sensor.internal_current");
    hass.states["sensor.selected"]!.state = "unknown";
    assert.equal(resolveEntities(hass, {}, [powerKey])[powerKey], "sensor.internal");
  }
});


test("all Grid voltage phases prefer valid Shelly readings before canonical Jinko fallback", () => {
  for (const phase of [1, 2, 3]) {
    const key = `grid_l${phase}_voltage` as EntityKey;
    const jinkoId = `sensor.jinko_grid_g_v_l${phase}`;
    const shellyId = `sensor.garazh_grid_load_l${phase}_voltage`;
    for (const reversed of [false, true]) {
      const entries: Array<[string, HomeAssistantState]> = [
        [jinkoId, state("233", `Jinko Grid Voltage L${phase}`, "V")],
        [shellyId, state("246", `Garazh Grid Load L${phase} Voltage`, "V")]
      ];
      const hass = hassWithStates(Object.fromEntries(reversed ? entries.reverse() : entries));
      for (const [raw, unit, expected] of [
        ["246", "V", 246], ["246000", "mV", 246], ["0", "V", 0], ["245", undefined, 245]
      ] as const) {
        hass.states[shellyId] = state(raw, `Garazh Grid Load L${phase} Voltage`, unit);
        const resolved = resolveEntities(hass, {}, [key]);
        assert.equal(resolved[key], shellyId, `phase ${phase}: ${raw} ${unit}`);
        assert.equal(valueFor(hass, resolved, key), expected);
      }
      // A known canonical Jinko ID must not outrank an exact Shelly friendly
      // name simply because canonical identifiers score higher within a tier.
      delete hass.states[shellyId];
      hass.states["sensor.renamed_shelly"] = state("247", `Garazh Grid Load L${phase} Voltage`, "V");
      assert.equal(resolveEntities(hass, {}, [key])[key], "sensor.renamed_shelly");
    }
  }
});

test("Grid voltage falls back independently for missing, unavailable, invalid or wrong-unit Shelly phases", () => {
  for (const phase of [1, 2, 3]) {
    const key = `grid_l${phase}_voltage` as EntityKey;
    const jinkoId = `sensor.jinko_grid_g_v_l${phase}`;
    const shellyId = `sensor.garazh_grid_load_l${phase}_voltage`;
    const hass = hassWithStates({ [jinkoId]: state("233", "", "V") });
    assert.equal(resolveEntities(hass, {}, [key])[key], jinkoId, "missing Shelly");
    for (const [raw, unit] of [
      ["unavailable", "V"], ["unknown", "V"], ["", "V"], ["NaN", "V"],
      ["246 V", "V"], ["0xF6", "V"], ["1e309", "V"], ["246", "W"], ["246", "kV"]
    ]) {
      hass.states[shellyId] = state(raw!, "", unit);
      const resolved = resolveEntities(hass, {}, [key]);
      assert.equal(resolved[key], jinkoId, `phase ${phase}: ${raw} ${unit}`);
      assert.equal(valueFor(hass, resolved, key), 233);
    }
    hass.states[jinkoId]!.state = "unavailable";
    assert.equal(resolveEntities(hass, {}, [key])[key], null, "neither source usable");
  }
  const hass = hassWithStates({
    "sensor.jinko_grid_g_v_l1": state("231", "", "V"),
    "sensor.jinko_grid_g_v_l2": state("232", "", "V"),
    "sensor.jinko_grid_g_v_l3": state("233", "", "V"),
    "sensor.shelly_grid_load_l1_voltage": state("241", "", "V"),
    "sensor.shelly_grid_load_l3_voltage": state("unavailable", "", "V")
  });
  const resolved = resolveEntities(hass);
  assert.deepEqual([1, 2, 3].map((phase) => valueFor(hass, resolved, `grid_l${phase}_voltage` as EntityKey)), [241, 232, 233]);
});

test("Grid voltage manual overrides remain authoritative and never silently fall back", () => {
  for (const phase of [1, 2, 3]) {
    const key = `grid_l${phase}_voltage` as EntityKey;
    const jinkoId = `sensor.jinko_grid_g_v_l${phase}`;
    const hass = hassWithStates({
      [jinkoId]: state("233", "", "V"),
      [`sensor.shelly_grid_load_l${phase}_voltage`]: state("246", "", "V"),
      "sensor.custom_voltage": state("248000", "Custom voltage", "mV")
    });
    let resolved = resolveEntities(hass, { [key]: jinkoId }, [key]);
    assert.equal(resolved[key], jinkoId);
    assert.equal(valueFor(hass, resolved, key), 233);
    resolved = resolveEntities(hass, { [key]: "sensor.custom_voltage" }, [key]);
    assert.equal(valueFor(hass, resolved, key), 248);
    for (const missingId of ["sensor.missing", ""]) {
      assert.equal(resolveEntities(hass, { [key]: missingId }, [key])[key], null);
    }
    for (const [raw, unit] of [["unavailable", "V"], ["248", "W"]]) {
      hass.states["sensor.custom_voltage"] = state(raw!, "", unit);
      resolved = resolveEntities(hass, { [key]: "sensor.custom_voltage" }, [key]);
      assert.equal(resolved[key], "sensor.custom_voltage");
      assert.equal(valueFor(hass, resolved, key), null);
    }
  }
});

test("Shelly Grid voltage preference leaves CT current/power and Inverter/UPS voltages independent", () => {
  const hass = hassWithStates({
    "sensor.jinko_grid_ct_t_e": state("-600", "", "W"),
    "sensor.shelly_grid_load_total_power": state("1200", "", "W")
  });
  for (const phase of [1, 2, 3]) {
    const readings: Array<[string, string, string]> = [
      [`grid_g_v_l${phase}`, "233", "V"],
      [`grid_load_l${phase}_voltage`, "246", "V"],
      [`grid_jinko_modbus_r${612 + phase}_external_ct_l${phase}_current`, "-0.2", "A"],
      [`grid_g_c_l${phase}`, "0.3", "A"],
      [`grid_load_l${phase}_current`, "4", "A"],
      [`grid_ct${phase}_p_e`, "-200", "W"],
      [`grid_g_p_l${phase}`, "300", "W"],
      [`grid_load_l${phase}_power`, "400", "W"],
      [`electric_av${phase}`, "234", "V"],
      [`consumption_c_v_l${phase}`, "235", "V"]
    ];
    for (const [id, raw, unit] of readings) hass.states["sensor.device_" + id] = state(raw, "", unit);
  }
  const resolved = resolveEntities(hass);
  assert.equal(valueFor(hass, resolved, "grid_total_power"), -600);
  assert.equal(valueFor(hass, resolved, "grid_load_total_power"), 1200);
  for (const phase of [1, 2, 3]) {
    for (const [key, expected] of [
      [`grid_l${phase}_voltage`, 246], [`grid_load_l${phase}_voltage`, 246],
      [`grid_l${phase}_current`, -0.2], [`grid_load_l${phase}_current`, 4],
      [`grid_l${phase}_power`, -200], [`grid_load_l${phase}_power`, 400],
      [`inverter_l${phase}_voltage`, 234], [`home_l${phase}_voltage`, 235]
    ] as const) assert.equal(valueFor(hass, resolved, key as EntityKey), expected, key);
  }
});

test("BMS voltage/current/SOC/temperature preference is independent of insertion order", () => {
  const cases: Array<[EntityKey, string, string]> = [
    ["battery_voltage", "BMS Voltage", "Battery Voltage"],
    ["battery_current", "BMS Current", "Battery Current"],
    ["battery_soc", "BMS_SOC", "SoC"],
    ["battery_temp", "BMS Temperature", "Temperature- Battery"]
  ];
  for (const [key, primary, secondary] of cases) {
    const hass = hassWithStates({
      "sensor.secondary": state("9", secondary),
      "sensor.primary": state("8", "Jinko Inverter " + primary)
    });
    assert.equal(resolveEntities(hass, {}, [key])[key], "sensor.primary", key);
    hass.states["sensor.primary"]!.state = "unknown";
    assert.equal(resolveEntities(hass, {}, [key])[key], "sensor.secondary", key);
    hass.states["sensor.primary"]!.state = "0";
    assert.equal(resolveEntities(hass, {}, [key])[key], "sensor.primary", key);
  }
});

test("unit-incompatible or malformed preferred readings use the declared fallback", () => {
  const hass = hassWithStates({
    "sensor.ct": state("8", "Total External CT Power", "VA"),
    "sensor.selected": state("-2", "Total Grid Power", "kW")
  });
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.selected");
  hass.states["sensor.ct"]!.attributes.unit_of_measurement = "W";
  hass.states["sensor.ct"]!.state = "3W";
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.selected");
  hass.states["sensor.ct"]!.state = "3";
  assert.equal(resolveEntities(hass, {}, ["grid_total_power"]).grid_total_power, "sensor.ct");
});

test("canonical identifiers outrank captions within a source tier but cannot cross domains", () => {
  const hass = hassWithStates({
    "sensor.caption": state("9", "BMS Voltage"),
    "sensor.unit_bms_bms_b_v1": state("8", "Renamed"),
    "sensor.unit_battery_b_v1": state("7", "BMS Voltage")
  });
  assert.equal(resolveEntities(hass, {}, ["battery_voltage"]).battery_voltage, "sensor.unit_bms_bms_b_v1");
  delete hass.states["sensor.unit_bms_bms_b_v1"];
  assert.equal(resolveEntities(hass, {}, ["battery_voltage"]).battery_voltage, "sensor.caption");
});

test("ambiguous primary sources fail closed without choosing a secondary or insertion order", () => {
  for (const reversed of [false, true]) {
    const entries: Array<[string, HomeAssistantState]> = [
      ["sensor.first", state("10", "One BMS_SOC")],
      ["sensor.second", state("20", "Two BMS_SOC")],
      ["sensor.inverter", state("30", "SoC")]
    ];
    const hass = hassWithStates(Object.fromEntries(reversed ? entries.reverse() : entries));
    assert.equal(resolveEntities(hass, {}, ["battery_soc"]).battery_soc, null);
  }
  const duplicateIds = hassWithStates({
    "sensor.first_grid_ct_t_e": state("1"),
    "sensor.second_grid_ct_t_e": state("2")
  });
  assert.equal(resolveEntities(duplicateIds, {}, ["grid_total_power"]).grid_total_power, null);
});

test("complete name matching rejects substrings, trailing variants and wrong phase aliases", () => {
  const hass = hassWithStates({
    "sensor.soc_backup": state("81", "BMS_SOC Backup"),
    "sensor.voltage_limit": state("610", "BMS Charge Voltage"),
    "sensor.old_ups": state("900", "Load phase power A"),
    "sensor.solar": state("999", "Solar"),
    "sensor.pv10": state("10", "DC Power PV10"),
    "sensor.shelly_pf": state("0.2", "Garazh Grid Load L1 Power Factor")
  });
  for (const key of ["battery_soc", "battery_voltage", "home_l1_power", "pv_total_power", "pv1_power", "power_factor"] as EntityKey[]) {
    assert.equal(resolveEntities(hass, {}, [key])[key], null, key);
  }
});

test("longer metric names are not treated as a generic SoC fallback", () => {
  const hass = hassWithStates({
    "sensor.bms": state("82", "Jinko BMS_SOC"),
    "sensor.actual_soc": state("81", "Jinko SoC")
  });
  const resolved = resolveEntities(hass, {}, ["battery_soc"]);
  assert.equal(resolved.battery_soc, "sensor.bms");
  hass.states["sensor.bms"]!.state = "unavailable";
  assert.equal(resolveEntities(hass, {}, ["battery_soc"]).battery_soc, "sensor.actual_soc");
});

test("UPS C_P phases, inverter daily energy and PV daily energy remain independent", () => {
  const hass = hassWithStates({
    "sensor.old_load": state("999", "Load phase power A"),
    "sensor.ups": state("10", "Load  Power L1"),
    "sensor.pv_day": state("1", "PV daily power generation (active)"),
    "sensor.inverter_day": state("2", "Daily Production (Active)"),
    "sensor.gen_legacy": state("3", "Generator Active Power"),
    "sensor.gen_total": state("4", "Total Gen Power")
  });
  const resolved = resolveEntities(hass);
  assert.equal(resolved.home_l1_power, "sensor.ups");
  assert.equal(resolved.pv_daily_energy, "sensor.pv_day");
  assert.equal(resolved.inverter_daily_energy, "sensor.inverter_day");
  assert.equal(resolved.generator_total_power, "sensor.gen_total");
  hass.states["sensor.gen_total"]!.state = "unavailable";
  assert.equal(resolveEntities(hass).generator_total_power, "sensor.gen_legacy");
});

test("explicit overrides never silently fall back when missing or unavailable", () => {
  const hass = hassWithStates({
    "sensor.auto": state("1250", "DC Power PV1"),
    "sensor.custom": state("777", "My custom PV")
  });
  assert.equal(resolveEntities(hass, { pv1_power: "sensor.custom" }).pv1_power, "sensor.custom");
  assert.equal(resolveEntities(hass, { pv1_power: "sensor.missing" }).pv1_power, null);
  assert.equal(resolveEntities(hass, { pv1_power: "" }).pv1_power, null);
  hass.states["sensor.custom"]!.state = "unavailable";
  const resolved = resolveEntities(hass, { pv1_power: "sensor.custom" });
  assert.equal(resolved.pv1_power, "sensor.custom");
  assert.equal(valueFor(hass, resolved, "pv1_power"), null);
  assert.deepEqual(resolveEntities(undefined), {});
});

test("strict units convert only dimension-compatible readings to the card base units", () => {
  const cases: Array<[string, unknown, BaseUnit, number]> = [
    ["-123", "W", "W", -123], ["-1.5", "kW", "W", -1500],
    ["-0.16", "A", "A", -0.16], ["-160", "mA", "A", -0.16],
    ["230", "V", "V", 230], ["230000", "mV", "V", 230],
    ["1.2", "kWh", "kWh", 1.2], ["1200", "Wh", "kWh", 1.2],
    ["50", "Hz", "Hz", 50], ["99", "%", "%", 99],
    ["26", "C", "C", 26], ["26", "°C", "C", 26], ["26", "℃", "C", 26],
    ["1.5", "h", "h", 1.5], ["0.95", "", "", 0.95],
    ["0", "kW", "W", 0], ["-2.5e2", undefined, "W", -250]
  ];
  for (const [text, unit, expectedUnit, expected] of cases) {
    assert.equal(toNumber(state(text, "", unit), expectedUnit), expected, text + " " + String(unit));
  }
  const hass = hassWithStates({ "sensor.power": state("-1.5", "", "kW") });
  assert.equal(valueFor(hass, { grid_total_power: "sensor.power" }, "grid_total_power"), -1500);
});

test("missing units keep legacy base units; explicit unsupported units fail closed", () => {
  for (const unit of [undefined, null, ""]) assert.equal(toNumber(state("-7", "", unit), "W"), -7);
  for (const unit of ["VA", "kVA", "Var", "foo", "toString", "kw", " ", 1, true]) {
    assert.equal(toNumber(state("12", "", unit), "W"), null, String(unit));
  }
  assert.equal(toNumber(state("12", "", "A"), "W"), null);
  assert.equal(toNumber(state("12", "", "%"), ""), null);
  assert.equal(toNumber(state("1e308", "", "kW"), "W"), null);
});

test("strict numeric parsing rejects invalid states rather than parsing a numeric prefix", () => {
  for (const text of ["", " ", "unknown", "unavailable", "None", "NaN", "Infinity", "1 W", "1junk", "1.2.3", "0x10", "1,25", "+", "1_000", "1e309"]) {
    assert.equal(toNumber(state(text)), null, text);
  }
  assert.equal(toNumber(state("  +.25e2  ")), 25);
  assert.equal(toNumber(undefined), null);
  assert.equal(toNumber(null), null);
  const hass = hassWithStates({ "sensor.zero": state("0") });
  assert.equal(stateFor(hass, { pv1_power: "sensor.zero" }, "pv1_power")?.state, "0");
  assert.equal(valueFor(hass, { pv1_power: "sensor.zero" }, "pv1_power"), 0);
  assert.equal(valueFor(hass, {}, "pv1_power"), null);
});

test("DC bus is BN_V only and unimplemented DC power never guesses a PV sensor", () => {
  const hass = hassWithStates({
    "sensor.hv": state("600", "HV Voltage"),
    "sensor.pv": state("1000", "Total Solar Power"),
    "sensor.dc": state("2000", "DC Input Power")
  });
  assert.equal(resolveEntities(hass).dc_bus_voltage, null);
  assert.equal(resolveEntities(hass).inverter_dc_power, null);
  hass.states["sensor.bus"] = state("500", "Jinko Bus-N Voltage");
  assert.equal(resolveEntities(hass).dc_bus_voltage, "sensor.bus");
  assert.equal(resolveEntities(hass, { inverter_dc_power: "sensor.dc" }).inverter_dc_power, "sensor.dc");
});
