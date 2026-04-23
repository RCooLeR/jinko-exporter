# Jinko Power Flow Card

Large animated Home Assistant Lovelace card for a single Jinko ESS installation.
It is an optional UI layer for the Home Assistant entities published by `jinko-exporter` over MQTT Discovery.

This card is tailored to your topology:

- PV1
- PV2
- JKS-12H-EI inverter
- JKS-B57637-CS battery
- Grid
- On-grid home load
- UPS / backup load

It is inspired by the Sunsynk power flow layout, but wired to the metric names published by `jinko-exporter`.

## What it uses

The card auto-discovers entities by the MQTT-discovered friendly names from this exporter. It expects the names exposed from `metrics.txt`, including:

- `DC Voltage PV1`, `DC Current PV1`, `DC Power PV1`
- `DC Voltage PV2`, `DC Current PV2`, `DC Power PV2`
- `Total Solar Power`
- `Grid Voltage L1/L2/L3`, `Grid Current L1/L2/L3`, `Grid Power L1/L2/L3`
- `Total Grid Power`, `Grid Frequency`, `Daily Energy Buy`, `Daily energy sell`
- `Load Voltage L1/L2/L3`, `Load Power L1/L2/L3`, `Total Consumption Power`, `Daily Consumption`
- `UPS Load Power`
- `Battery Voltage`, `Battery Current`, `Battery Power`, `SoC`
- `Temperature- Battery`, `BMS Temperature`
- `Inverter Output Power L1/L2/L3`, `Total Inverter Output Power`
- `AC Voltage R/U/A`, `AC Voltage S/V/B`, `AC Voltage T/W/C`
- `AC Current R/U/A`, `AC Current S/V/B`, `AC Current T/W/C`
- `AC Output Frequency R`, `Power factor`, `DC Temperature`

Current limitation from the available telemetry:

- UPS branch exposes total power, but not a clean dedicated per-phase UPS voltage/current/power set.
- The card shows UPS total power and keeps per-phase inverter output details in the inverter block.
- Home load current is derived from `P / V` per phase because the exporter sample does not expose direct home-load current sensors.

## Install

1. Copy this whole folder into your Home Assistant `www` directory.

   Example target:

   ```text
   /config/www/jinko-power-flow-card/
   ```

2. Add the JS file as a Lovelace resource:

   ```yaml
   url: /local/jinko-power-flow-card/jinko-power-flow-card.js?v=1
   type: module
   ```

   For the compact dashboard card, add the second resource too:

   ```yaml
   url: /local/jinko-power-flow-card/jinko-power-summary-card.js?v=1
   type: module
   ```

3. Add the card to a dedicated dashboard tab.

## Example

Use a panel view for the large layout:

```yaml
views:
  - title: Power Flow
    path: power-flow
    panel: true
    cards:
      - type: custom:jinko-power-flow-card
        title: Jinko ESS Power Flow
        battery_capacity_kwh: 21.31
        battery_negative_is_charging: true
        show_entity_map: false
```

There is also a ready example in [example-dashboard.yaml](./example-dashboard.yaml).

## Compact dashboard card

Use the summary card on the main dashboard when you only want:

- daily non-zero values
- battery SOC
- live total solar, grid, battery, UPS load, and parallel home load power
- inverter AC voltage

Example:

```yaml
type: custom:jinko-power-summary-card
title: Jinko ESS Overview
battery_capacity_kwh: 21.31
battery_negative_is_charging: true
show_entity_map: false
```

There is also a ready example in [example-summary-card.yaml](./example-summary-card.yaml).

## Optional overrides

Autodiscovery should work if this is the only Jinko inverter in HA.

If you need to force exact entities, override only the ones that do not resolve correctly:

```yaml
type: custom:jinko-power-flow-card
entities:
  grid_total_power: sensor.jinko_inverter_2211117011_total_grid_power
  home_total_power: sensor.jinko_inverter_2211117011_total_consumption_power
  ups_total_power: sensor.jinko_inverter_2211117011_ups_load_power
  battery_soc: sensor.jinko_inverter_2211117011_soc
```

Set `show_entity_map: true` to display what the card resolved at runtime.

## Files

- `jinko-power-flow-card.js`: full-screen card implementation
- `jinko-power-summary-card.js`: compact dashboard card implementation
- `assets/jks-12h-ei.svg`: inverter art
- `assets/jks-b57637-cs.svg`: battery art
- `example-dashboard.yaml`: sample Lovelace config
- `example-summary-card.yaml`: compact card sample

## Reference product data used

- Jinko ESS RESS brochure, published 2026/2024 marketing material on `jinkosolar.eu`
- JKS-6~20H-EI inverter datasheet on `jinkosolar.eu`

Battery capacity is hardcoded to `21.31 kWh` by default for `JKS-B57637-CS`.
