# Jinko New Home Assistant Cards

New implementation for the redesigned JinkoBridge Lovelace cards.

## Cards

- `custom:jks-detailed` renders the full desktop/mobile topology card.
- `custom:jks-mini` renders the new summary card, keeping the old public card name for compatibility.

## Home Assistant dist

Production build output:

- `dist/jinko-ha-cards.js`

Use it as the Lovelace resource that previously pointed at the old card bundle:

```yaml
url: /local/jinko-ha-cards.js
type: module
```

Both cards support the existing config style:

```yaml
type: custom:jks-detailed
static: false
show_entity_map: false
battery_negative_is_charging: true
entities:
  pv_total_power: sensor.example_total_solar_power
  grid_load_total_power: sensor.example_grid_load_total_power
```

When the bridge has `SHELLY_GRID_LOAD_ENABLED=true`, the cards automatically prefer `Grid Load ...` sensors for the grid-load node and fall back to the older derived load calculation when those sensors are missing.

Use `static: true` for development/demo rendering from `src/data/fake-energy-data.json`.

## Design Direction

The images in `designs/` are visual references only. The production cards do not use background images; every chip, node, value, icon, and flow arrow is rendered as HTML/CSS/SVG.

Icons are centralized in:

- `src/lib/icons.ts`

That file currently uses inline SVG icons so there are no licensing or network dependencies. If we choose Flaticon or another icon set later, replace the SVGs in that registry and the cards will pick them up everywhere.

## Tuning

Final visual tuning lives in one place:

- `src/lib/html-card-renderer.ts`

The renderer contains the card markup and CSS grid rules. Data formatting stays separate in `src/lib/energy-view-model.ts`, so visual tuning does not disturb Home Assistant entity logic.

## Development

```shell
npm install
npm run dev
npm run build
```

If local npm is unavailable, this repo can still be checked with the parent package toolchain:

```shell
node ..\ha-cards\node_modules\typescript\bin\tsc --noEmit -p tsconfig.json
node ..\ha-cards\node_modules\vite\bin\vite.js build
```
