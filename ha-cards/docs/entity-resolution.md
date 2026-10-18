# Entity Resolution

## Optional upstream meter

`grid_input_*` belongs exclusively to the new Shelly **before** the stabilizers.
It never falls back to `grid_load_*`, native Jinko CT, or inverter GRID values.
The separately labelled `stabilizers_test_mirror_grid` display-only test mode
does not change these entity bindings; it copies the finished Grid display into
the upstream panel only when explicitly enabled with `stabilizers: true`.
Conversely, existing grid/load/inverter keys never adopt upstream voltage,
current, power, energy, or power factor. Missing input readings remain unknown.
The optional detailed-card `stabilizers: true` layout shows the new input block;
`grid_input_energy_total` and `grid_input_returned_energy_total` are cumulative,
not daily counters. They remain available as independent raw entity bindings,
but never supply the Today row. That row uses `grid_input_buy_today` minus
`grid_input_sell_today`, resolving daily HA utility meters with canonical IDs
`grid_input_energy_today` and `grid_input_returned_energy_today` (friendly names
`Grid Input Energy Today` and `Grid Input Returned Energy Today`). Both must be
available; zero is valid. See [configuration](./cards.md#stabilizer-topology).
Existing daily energy/cost summaries stay on the inverter's
post-stabilizer counters.

The cards do not talk to MQTT directly. They read Home Assistant entity states. Entity IDs are resolved automatically by matching Home Assistant sensor friendly names and entity IDs against expected bridge metric names.

## Resolution Order

For each internal card key:

1. An explicit `entities:` override wins. A missing/unavailable override stays missing; it never silently selects another entity.
2. Try the documented source candidates in order, requiring a valid numeric state and compatible unit.
3. Within a source, prefer an exact canonical metric-key suffix in the entity ID, then a complete friendly-name match (including a device prefix), then a complete name suffix in the entity ID.
4. Reject equal-ranked ambiguity. Arbitrary substring matching is not used: `BMS_SOC` and `SoC` cannot accidentally compete as the same metric.
5. If a preferred source is unavailable, use its next declared candidate. Re-evaluate on discovery, name, unit, and availability changes so the preferred source returns without reloading.

W/kW, A/mA, V/mV and kWh/Wh are normalized before display; zero and negative
values are retained. Invalid numeric text and incompatible units are unavailable,
not zero. Legacy entities without a unit use the documented base unit.

## Source Priorities

| Panel/quantity | Preferred source | Fallback |
| --- | --- | --- |
| Grid power | External CT `CT1_P_E..CT3_P_E`, `CT_T_E` | Selected grid `G_P_L1..3`, `PG_Pt1`, then internal `GS_A/B/C/T` |
| Grid current | Native CT currents R613-615 | Inverter `G_C_L1..3` |
| Grid voltage | Shelly `grid_load_l1..3_voltage`, independently per phase | Inverter GRID `G_V_L1..3` for each unavailable phase |
| Grid frequency | Inverter `PG_F1` | CTs do not measure frequency separately |
| Parallel Grid Load | Shelly `grid_load_*` | Complete Shelly phase sums, then total consumption minus dedicated UPS total for power; grid voltage and labelled current estimates where needed |
| Inverter | Own `INV_O_P_*`, `AV1..3`, `AC1..3`, `A_Fo1`, `T_DC` | Complete own phase-power sum for a missing output total; never UPS power |
| Battery voltage/current/SOC/temperature | `BMS_B_V1`, `BMS_B_C1`, `BMS_SOC`, `BMST` | `B_V1`, `B_C1`, `B_left_cap1`, `B_T1` |
| Battery power/energy | Inverter `B_P1`, `Etdy_cg1`, `Etdy_dcg1` | No independent BMS power/energy sensor exists |
| UPS | `C_P_L1..3`, `UPS_P`, `C_V_L1..3` | Complete UPS phase sum for its missing total; never general-load `LPP_A/B/C` |

Priorities are per quantity: a panel can intentionally mix CT current/power
with Shelly voltage, or BMS SOC with inverter battery power. The Shelly-first
grid-voltage preference assumes both instruments measure the same bus; revisit
it if stabilizers separate their voltage inputs. This is a display preference,
not a change to either instrument's published readings or power calculations.
Daily import/export remain the inverter counters, not separate CT energy readings. `show_entity_map`
shows the actual chosen entities. Calculated values use `≈`; missing phases
are never fabricated by dividing a total between phases.

Enable the runtime map while configuring:

```yaml
type: custom:jks-detailed
show_entity_map: true
```

## Manual Overrides

Use overrides when Home Assistant generated unexpected entity IDs or when multiple inverters expose similar names.

```yaml
type: custom:jks-detailed
entities:
  pv_total_power: sensor.garage_inverter_total_solar_power
  grid_total_power: sensor.garage_inverter_total_grid_power
  battery_soc: sensor.garage_inverter_soc
```

## Entity Naming Pattern From The Bridge

The bridge publishes metric sensor names from the metric `name` field.

Example metric:

```json
{
  "group": "grid",
  "key": "PG_Pt1",
  "name": "Total Grid Power",
  "unit": "W",
  "value": 1234
}
```

Likely Home Assistant entity:

```text
sensor.jinko_inverter_synthetic_inv_001_total_grid_power
```

Home Assistant owns the final entity ID, so it may append suffixes such as `_2` if an entity already exists.

## Internal Entity Keys

| Card key | Expected names |
| --- | --- |
| `pv_total_power` | Total Solar Power |
| `pv_daily_energy` | PV daily power generation (active) |
| `pv1_voltage` | DC Voltage PV1 |
| `pv1_current` | DC Current PV1 |
| `pv1_power` | DC Power PV1 |
| `pv2_voltage` | DC Voltage PV2 |
| `pv2_current` | DC Current PV2 |
| `pv2_power` | DC Power PV2 |
| `grid_total_power` | Total External CT Power → Total Grid Power → Internal Power |
| `grid_frequency` | Grid Frequency |
| `grid_buy_today` | Daily Energy Buy |
| `grid_sell_today` | Daily energy sell |
| `grid_l1_voltage` | Grid Load L1 Voltage → Grid Voltage L1 |
| `grid_l2_voltage` | Grid Load L2 Voltage → Grid Voltage L2 |
| `grid_l3_voltage` | Grid Load L3 Voltage → Grid Voltage L3 |
| `grid_l1_current` | External CT Current L1 → Grid Current L1 |
| `grid_l2_current` | External CT Current L2 → Grid Current L2 |
| `grid_l3_current` | External CT Current L3 → Grid Current L3 |
| `grid_l1_power` | External CT1 Power → Grid Power L1 → Internal L1 Power |
| `grid_l2_power` | External CT2 Power → Grid Power L2 → Internal L2 Power |
| `grid_l3_power` | External CT3 Power → Grid Power L3 → Internal L3 Power |
| `home_total_power` | Total Consumption Power |
| `home_daily_energy` | Daily Consumption |
| `home_frequency` | Load Fequency, Load Frequency |
| `home_l1_voltage` | Load Voltage L1 |
| `home_l2_voltage` | Load Voltage L2 |
| `home_l3_voltage` | Load Voltage L3 |
| `home_l1_power` | Load Power L1 (UPS; legacy card-key name) |
| `home_l2_power` | Load Power L2 (UPS) |
| `home_l3_power` | Load Power L3 (UPS) |
| `ups_total_power` | UPS Load Power |
| `generator_daily_energy` | Daily Production Generator |
| `generator_total_power` | Total Gen Power, Generator Active Power |
| `generator_l1_power` | Gen Power L1 |
| `generator_l2_power` | Gen Power L2 |
| `generator_l3_power` | Gen Power L3 |
| `generator_l1_voltage` | Gen Voltage L1 |
| `generator_l2_voltage` | Gen Voltage L2 |
| `generator_l3_voltage` | Gen Voltage L3 |
| `generator_daily_runtime` | Gen Daily Run Time |
| `battery_voltage` | BMS Voltage → Battery Voltage |
| `battery_current` | BMS Current → Battery Current |
| `battery_power` | Battery Power |
| `battery_soc` | BMS_SOC → SoC |
| `battery_temp` | BMS Temperature → Temperature- Battery |
| `battery_charge_today` | Daily Charging Energy |
| `battery_discharge_today` | Daily Discharging Energy |
| `inverter_total_power` | Total Inverter Output Power |
| `inverter_daily_energy` | Daily Production (Active) |
| `inverter_l1_power` | Inverter Output Power L1 |
| `inverter_l2_power` | Inverter Output Power L2 |
| `inverter_l3_power` | Inverter Output Power L3 |
| `inverter_l1_voltage` | AC Voltage R/U/A |
| `inverter_l2_voltage` | AC Voltage S/V/B |
| `inverter_l3_voltage` | AC Voltage T/W/C |
| `inverter_l1_current` | AC Current R/U/A |
| `inverter_l2_current` | AC Current S/V/B |
| `inverter_l3_current` | AC Current T/W/C |
| `inverter_frequency` | AC Output Frequency R |
| `power_factor` | Power factor |
| `dc_temperature` | DC Temperature |

## Debug Checklist

1. Confirm MQTT Discovery created the bridge device in Home Assistant.
2. Open the device and confirm sensors exist and have numeric states.
3. Add `show_entity_map: true` to the card.
4. For unresolved keys, copy the correct entity ID from Home Assistant.
5. Add that entity ID under `entities:`.
6. Remove `show_entity_map` after the card resolves correctly.
