# Jinko New Home Assistant Cards

New implementation for the redesigned JinkoBridge Lovelace cards.

This bundle and the stable bundle under `ha-cards/` register the same custom-element names. Install exactly one of them in Home Assistant; they are release alternatives, not additive resources.

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

The renderer also follows the custom element lifecycle: resize observers and window/media-query listeners are released when a card is detached and restored once when it reconnects. This keeps dashboard navigation and editor previews from accumulating background handlers.

## Toolchain

- Node.js `^24.12.0`; the repository pins Node.js 24.20.0 in `.node-version`.
- npm 11.19.0 with reproducible installs from `package-lock.json`.
- TypeScript 7 with strict, erasable-syntax, unchecked-index, and side-effect-import checks.
- Vite 8 producing an ES module for the Baseline Widely Available browser target.

The development server forwards browser console output to the terminal. Production builds target Vite's browser baseline and omit the unused public asset directory.

## Development

```shell
npm ci
npm run check
```

`npm run check` runs tests, checks both browser and tooling TypeScript sources, and creates the production bundle. For interactive work, start the development server with:

```shell
npm run dev
```
