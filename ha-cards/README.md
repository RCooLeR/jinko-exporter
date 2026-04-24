# JKS Home Assistant Cards

TypeScript Home Assistant Lovelace cards for the MQTT entities published by `jinko-exporter`.

Current cards:

- `custom:jks-detailed`
- `custom:jks-mini`

These cards use the generated background artwork in [`assets/main`](./assets/main) and [`assets/overview`](./assets/overview), then place live values into the layout boxes from the JSON specs.

## Status

This package is in active development.

- `jks-detailed`: first usable pass is implemented
- `jks-mini`: first usable pass is implemented
- typography and value placement still need visual tuning in Home Assistant
- cost data is not wired yet, so cost fields currently show `--`

## Project Layout

- [`src/cards/jks-detailed-card.ts`](./src/cards/jks-detailed-card.ts): large detailed dashboard card
- [`src/cards/jks-mini-card.ts`](./src/cards/jks-mini-card.ts): compact overview card
- [`src/lib/entity-model.ts`](./src/lib/entity-model.ts): entity autodiscovery and override model
- [`assets/main`](./assets/main): detailed card backgrounds and layout specs
- [`assets/overview`](./assets/overview): mini card backgrounds and layout specs

## Development

Install dependencies:

```powershell
npm install
```

Build the bundle:

```powershell
npm run build
```

Local dev server:

```powershell
npm run dev
```

The production bundle is written to:

```text
ha-cards/dist/jinko-ha-cards.js
```

## Home Assistant Install

1. Build the project with `npm run build`.
2. Copy these items into your Home Assistant `www` directory:

   ```text
   dist/jinko-ha-cards.js
   assets/main/
   assets/overview/
   ```

   Example target:

   ```text
   /config/www/jinko-ha-cards/
   ```

3. Add the Lovelace resource:

   ```yaml
   url: /local/jinko-ha-cards/jinko-ha-cards.js?v=1
   type: module
   ```

4. Add one or both custom cards to a dashboard.

## Card Usage

### `jks-detailed`

Large card using `assets/main`.

Example:

```yaml
type: custom:jks-detailed
title: JKS Detailed
battery_capacity_kwh: 21.31
battery_negative_is_charging: true
show_entity_map: false
```

### `jks-mini`

Compact card using `assets/overview`.

Example:

```yaml
type: custom:jks-mini
title: JKS Mini
show_entity_map: false
```

## Entity Resolution

Both cards autodiscover Home Assistant entities by matching sensor friendly names and entity IDs against the exporter metric names.

If autodiscovery misses a sensor, use `entities:` overrides:

```yaml
type: custom:jks-detailed
entities:
  grid_total_power: sensor.jinko_inverter_2211117011_total_grid_power
  home_total_power: sensor.jinko_inverter_2211117011_total_consumption_power
  ups_total_power: sensor.jinko_inverter_2211117011_ups_load_power
  battery_soc: sensor.jinko_inverter_2211117011_soc
```

Set `show_entity_map: true` to render the resolved runtime entity map below the card.

## Main Entity Keys

Commonly used entity keys:

- `pv1_voltage`
- `pv1_current`
- `pv1_power`
- `pv2_voltage`
- `pv2_current`
- `pv2_power`
- `pv_total_power`
- `grid_total_power`
- `grid_buy_today`
- `grid_sell_today`
- `home_total_power`
- `home_daily_energy`
- `ups_total_power`
- `battery_voltage`
- `battery_current`
- `battery_power`
- `battery_soc`
- `battery_charge_today`
- `battery_discharge_today`
- `generator_total_power`
- `generator_daily_energy`
- `inverter_total_power`
- `dc_temperature`

The full list is defined in [`src/lib/entity-model.ts`](./src/lib/entity-model.ts).

## Current Limitations

- The cards currently rely on absolute text placement over raster backgrounds.
- `jks-detailed` still needs in-HA visual alignment adjustments for final polish.
- `jks-mini` uses the overview layout spec structure and shows only the primary values from each node.
- Costs are not currently calculated from exporter metrics.

## Relationship To Old Cards

The old plain JavaScript cards are kept in [`../old-ha-cards`](../old-ha-cards/README.md).

The new `ha-cards` package is the replacement track:

- new names: `jks-detailed`, `jks-mini`
- TypeScript source
- Vite build
- new generated background-based layouts
