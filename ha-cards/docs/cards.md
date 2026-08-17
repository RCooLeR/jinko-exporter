# Card Configuration

## `custom:jks-detailed`

Large dashboard card with detailed PV, grid, load, battery, generator, UPS, inverter, and summary values.

Basic example:

```yaml
type: custom:jks-detailed
title: JKS Detailed
```

Full example:

```yaml
type: custom:jks-detailed
title: JKS Detailed
battery_capacity_kwh: 21.31
battery_negative_is_charging: true
show_entity_map: false
entities:
  grid_total_power: sensor.jinko_inverter_synthetic_inv_001_total_grid_power
  grid_load_total_power: sensor.jinko_inverter_synthetic_inv_001_grid_load_total_power
  home_total_power: sensor.jinko_inverter_synthetic_inv_001_total_consumption_power
  ups_total_power: sensor.jinko_inverter_synthetic_inv_001_ups_load_power
  battery_soc: sensor.jinko_inverter_synthetic_inv_001_soc
```

Options:

| Option | Default | Description |
| --- | --- | --- |
| `title` | `JKS Detailed` | Header text. Empty string hides the title. |
| `battery_capacity_kwh` | `21.31` | Reserved for battery display calculations. |
| `battery_negative_is_charging` | `true` | Treat negative battery power as charging. Set `false` if your source reports the opposite sign. |
| `show_entity_map` | `false` | Show resolved entity IDs under the card for debugging. |
| `static` | `false` | Render demo values and stop live updates after first render. |
| `entities` | `{}` | Manual entity overrides by internal card key. |

## `custom:jks-mini`

Compact overview card with summary tiles and primary flow values.

Basic example:

```yaml
type: custom:jks-mini
title: JKS Mini
```

Full example:

```yaml
type: custom:jks-mini
title: JKS Mini
show_entity_map: false
entities:
  pv_total_power: sensor.jinko_inverter_synthetic_inv_001_total_solar_power
  grid_total_power: sensor.jinko_inverter_synthetic_inv_001_total_grid_power
  grid_load_total_power: sensor.jinko_inverter_synthetic_inv_001_grid_load_total_power
  home_total_power: sensor.jinko_inverter_synthetic_inv_001_total_consumption_power
  battery_soc: sensor.jinko_inverter_synthetic_inv_001_soc
```

Options:

| Option | Default | Description |
| --- | --- | --- |
| `title` | `JKS Mini` | Header text. Empty string hides the title. |
| `show_entity_map` | `false` | Show resolved entity IDs under the card for debugging. |
| `static` | `false` | Render demo values and stop live updates after first render. |
| `entities` | `{}` | Manual entity overrides by internal card key. |

## Values Used By The Detailed Card

The detailed card uses these internal groups:

| Area | Main values |
| --- | --- |
| PV1/PV2 | voltage, current, power |
| Grid | phase voltages, phase currents, phase powers, total grid power, frequency, import/export energy |
| Grid-side load | Shelly `grid_load_*` power/current/voltage when available; derived load fallback otherwise |
| UPS/load | load phase voltages, estimated currents, phase powers, UPS load power |
| Battery | voltage, current, power, SOC, charge/discharge energy |
| Inverter | output phase voltages/currents/powers, total output power, frequency, DC temperature |
| Generator | phase voltages, estimated currents, phase powers, total power, daily energy |
| Summary | daily production, generator production, import, export, consumption, estimated cost |

The card displays `--` when a value is missing or effectively zero for fields where zero means offline.

## Values Used By The Mini Card

The mini card uses:

- daily production
- grid import today
- grid export today
- daily consumption
- estimated cost
- battery SOC
- combined PV power
- total grid power
- total load power
- battery power
- generator power
- inverter temperature

## Offline Layers

Desktop card artwork includes offline overlay layers. A layer appears when the related source area has no meaningful power, voltage, current, energy, or status value.

The detailed card can show offline overlays for:

- PV1
- PV2
- combined PV
- grid
- battery
- generator
- UPS
- parallel grid load

The mini card can show offline overlays for:

- PV
- grid
- battery
- generator
- load

## Cost Display

The cards currently estimate cost from daily import and export using hard-coded rates in the card code. Treat the cost fields as visual placeholders unless you have reviewed and adjusted the implementation for your tariff.
