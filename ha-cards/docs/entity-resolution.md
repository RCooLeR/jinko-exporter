# Entity Resolution

The cards do not talk to MQTT directly. They read Home Assistant entity states. Entity IDs are resolved automatically by matching Home Assistant sensor friendly names and entity IDs against expected bridge metric names.

## Resolution Order

For each internal card key:

1. If an `entities:` override is configured and exists in Home Assistant, use it.
2. Search `sensor.*` entities.
3. Score exact friendly-name matches highest.
4. Score friendly-name contains matches next.
5. Score entity-ID contains matches last.
6. Use the best match or leave the key unresolved.

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
| `pv_total_power` | Total Solar Power, Solar |
| `pv_daily_energy` | PV daily power generation (active), Daily Production (Active) |
| `pv1_voltage` | DC Voltage PV1 |
| `pv1_current` | DC Current PV1 |
| `pv1_power` | DC Power PV1 |
| `pv2_voltage` | DC Voltage PV2 |
| `pv2_current` | DC Current PV2 |
| `pv2_power` | DC Power PV2 |
| `grid_total_power` | Total Grid Power, Internal Power |
| `grid_frequency` | Grid Frequency |
| `grid_buy_today` | Daily Energy Buy |
| `grid_sell_today` | Daily energy sell |
| `grid_l1_voltage` | Grid Voltage L1 |
| `grid_l2_voltage` | Grid Voltage L2 |
| `grid_l3_voltage` | Grid Voltage L3 |
| `grid_l1_current` | Grid Current L1 |
| `grid_l2_current` | Grid Current L2 |
| `grid_l3_current` | Grid Current L3 |
| `grid_l1_power` | Grid Power L1 |
| `grid_l2_power` | Grid Power L2 |
| `grid_l3_power` | Grid Power L3 |
| `home_total_power` | Total Consumption Power |
| `home_daily_energy` | Daily Consumption |
| `home_frequency` | Load Fequency, Load Frequency |
| `home_l1_voltage` | Load Voltage L1 |
| `home_l2_voltage` | Load Voltage L2 |
| `home_l3_voltage` | Load Voltage L3 |
| `home_l1_power` | Load Power L1, Load phase power A |
| `home_l2_power` | Load Power L2, Load phase power B |
| `home_l3_power` | Load Power L3, Load phase power C |
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
| `battery_voltage` | Battery Voltage |
| `battery_current` | Battery Current |
| `battery_power` | Battery Power |
| `battery_soc` | SoC, BMS_SOC |
| `battery_temp` | Temperature- Battery, BMS Temperature |
| `battery_charge_today` | Daily Charging Energy |
| `battery_discharge_today` | Daily Discharging Energy |
| `inverter_total_power` | Total Inverter Output Power |
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
