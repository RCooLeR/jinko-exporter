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
| `stabilizers` | `false` | Detailed card only: show the optional three-block grid topology. |
| `stabilizers_test_mirror_grid` | `false` | With `stabilizers: true` only: visibly labelled TEST mode mirrors Stabilized Grid into Grid for layout testing without a new meter. |
| `entities` | `{}` | Manual entity overrides by internal card key. |

### Stabilizer topology

Enable this variant after installing the upstream meter:

```yaml
type: custom:jks-detailed
stabilizers: true
```

The right column becomes **Grid → Stabilized Grid → Parallel Grid Load**, on
both desktop and mobile. Panel positions, labels, live-value boxes, and
connectors share one geometry definition; the normal artwork is unchanged.

| Block | Data source |
| --- | --- |
| Grid (before stabilizers) | New Shelly `grid_input_*` only, including its own phase voltage/current/power. |
| Stabilized Grid | Native Jinko CT current/power with existing fallbacks; **post-stabilizer** Shelly `grid_load_*` voltage, then inverter GRID voltage. |
| Parallel Grid Load | Existing post-stabilizer Shelly `grid_load_*`, with the existing labelled load estimates when readings are absent. |

The new Shelly has independent `SHELLY_GRID_INPUT_*` bridge configuration
([configuration](../../bridge/docs/configuration.md)). It is disabled by default.
Missing upstream readings remain `--`; the card never replaces them with
downstream readings or computes voltage from power/current. The single **Today**
row is today's imported energy minus today's exported energy: `+12.34 kWh`
means net import, `-12.34 kWh` net export, and `0 kWh` balance. It requires both
upstream daily counters (`grid_input_buy_today`, `grid_input_sell_today`);
otherwise it shows `--`. Lifetime counters are never substituted for Today.
The Stabilized Grid Today row and daily summaries remain inverter counters
(after stabilizers).
Selecting this card variant does not enable bridge polling.

The Shelly bridge publishes cumulative energy only. After installation, create
two [Home Assistant Utility Meter helpers](https://www.home-assistant.io/integrations/utility_meter/)
with daily cycles, one for each cumulative source. Example `configuration.yaml`
fragment (replace both `source` placeholders with the actual discovered entities;
merge into an existing `utility_meter:` section if present):

```yaml
utility_meter:
  grid_input_energy_today:
    name: Grid Input Energy Today
    source: sensor.replace_with_upstream_import_total
    cycle: daily
    periodically_resetting: true
    always_available: false
  grid_input_returned_energy_today:
    name: Grid Input Returned Energy Today
    source: sensor.replace_with_upstream_export_total
    cycle: daily
    periodically_resetting: true
    always_available: false
```

The sources must be the upstream `grid_input_energy_total` and
`grid_input_returned_energy_total` readings, respectively. Keep them separate;
subtract the daily helpers in the card, not the lifetime counters in a resetting
meter. The first day is partial; a gap across midnight cannot be split exactly
between days using cumulative counters alone. Match HA's local timezone to the
site. Default helper names resolve automatically; custom names can be mapped:

```yaml
entities:
  grid_input_buy_today: sensor.grid_input_energy_today
  grid_input_sell_today: sensor.grid_input_returned_energy_today
```

To test the layout before installing the new meter, add both options to your
existing card (keep its other settings):

```yaml
stabilizers: true
stabilizers_test_mirror_grid: true
```

The upper Grid temporarily mirrors the displayed Stabilized Grid voltage,
current, power and Today balance, including signed/zero/unavailable values. A persistent
**TEST / MIRROR** banner and the upper panel subtitle identify the substitution.
The mirrored Today is a test substitution, not an upstream measurement.
This does not change HA states, MQTT, Prometheus or
bridge polling; keep `SHELLY_GRID_INPUT_ENABLED=false` until installation.
Remove `stabilizers_test_mirror_grid` (or set it to `false`) to return to real
upstream readings. The flag has no effect unless `stabilizers: true` is also set.
The local preview accepts `?stabilizers=1&mirror=1&width=1672`.

Use `npm run dev -- --host 127.0.0.1` and `?stabilizers=1&width=1672`
(or `width=420`) for a synthetic-data preview. Do not use `static: true` in a
live dashboard.

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
| Grid | CT-first powers/currents; Shelly-first phase voltages with inverter GRID voltage fallback per phase; inverter frequency and import/export counters |
| Grid-side load | Shelly `grid_load_*` power/current/voltage when available; derived load fallback otherwise |
| UPS/load | load phase voltages, estimated currents, phase powers, UPS load power |
| Battery | BMS-first voltage/current/SOC/temperature; inverter fallback plus inverter power/energy counters |
| Inverter | output phase voltages/currents/powers, total output power, frequency, DC temperature |
| Generator | phase voltages, estimated currents, phase powers, total power, daily energy |
| Summary | daily production, generator production, import, export, consumption, estimated cost |

The card displays `--` when a value is missing or effectively zero for fields where zero means offline.

Shelly-backed grid-side load is independent of inverter availability. The detailed card prefers Shelly's own phase voltage, current, and power; the mini card prefers its measured total. Live zero readings remain visible as zero, even while every inverter sensor is unavailable. If a Shelly total is missing, a complete three-phase sum can supply it. Otherwise the load power estimate is total consumption minus dedicated UPS power; both inputs are required and the result is labelled `≈`. Missing phase powers stay missing. Grid voltage can supply a missing Shelly voltage; `P/V` current estimates are also labelled `≈` (not measured RMS current). Shelly never supplies replacement PV, battery, UPS, or inverter totals.

See [source priorities](./entity-resolution.md#source-priorities) for the exact
metric mapping. Grid and grid-side load prefer the same Shelly phase voltages
on the shared bus, falling back to inverter GRID voltage for unavailable phases.
When Shelly recovers, its voltage is selected again automatically. Explicit
entity overrides still win. Inverter and UPS voltages keep their own sources.
The existing `home_l*_power` card keys describe UPS phases;
general-load `LPP_A/B/C` are different measurements and are not valid substitutes.
New CT/BMS entities are picked up when discovery publishes them; a card update
does not start the bridge or enable unvalidated Modbus metrics.

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
