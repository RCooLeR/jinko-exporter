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
stabilizers: false
stabilizers_test_mirror_grid: false
entities:
  pv_total_power: sensor.example_total_solar_power
  grid_load_total_power: sensor.example_grid_load_total_power
```

When the bridge has `SHELLY_GRID_LOAD_ENABLED=true`, the cards automatically prefer `Grid Load ...` sensors for the grid-load node and fall back to the older derived load calculation when those sensors are missing.

Both implementations now use the same [source priorities](../ha-cards/docs/entity-resolution.md#source-priorities):
Grid prefers Shelly voltage per phase (falling back to inverter GRID voltage
on the shared bus) and native CT current/power. Battery prefers BMS measurements, and
inverter fields retain their own source. Declared inverter fallbacks preserve
available data when a preferred source is unavailable and automatically switch
back when it returns. Derived grid load is consumption minus dedicated UPS power
and is labelled `≈`; phase powers are never fabricated from a total. Signed
phase power, zero readings, and compatible unit conversions are preserved.
The DC-bus field uses `BN_V`, not the different full-link `HV_V`; an optional
DC-input power override is not synthesized from PV or UPS power.

Use `static: true` for development/demo rendering from `src/data/fake-energy-data.json`.

### Optional stabilizer topology

Set `stabilizers: true` on either card to show the right-hand column as
**Grid → Stabilized Grid → Parallel Grid Load**. Omission or `false` preserves
the existing layout. On phones these three nodes stay together in the same
top-to-bottom order.

Grid reads only the additional **upstream** Shelly `grid_input_*` entities;
Stabilized Grid retains native Jinko CT current/power and the existing
post-stabilizer Shelly voltage, with inverter GRID voltage as its same-bus
fallback. Parallel Grid Load remains the existing post-stabilizer `grid_load`
meter. In normal operation, missing upstream readings are never copied from downstream, and missing
downstream voltage is never filled from upstream.

The upstream **Today** row shows today's import minus today's export in kWh:
`+` means net import, `-` means net export, and a balanced or rounded-zero result
is `0 kWh`. Both finite **daily** counters are required, including valid zeros;
a missing/invalid counter or overflow displays `--`. Lifetime counters and
downstream daily counters are never automatic fallbacks for upstream Today.
Existing daily import/export chips
still use inverter counters and are marked **після стаб.** in this mode.
Upstream Shelly data can remain visible when the inverter is unavailable.

The bridge publishes only cumulative upstream import/returned counters. Create
two Home Assistant daily Utility Meter helpers from them; the card does not
compute daily boundaries in browser memory. Example `configuration.yaml`
(replace both source IDs with your actual upstream lifetime energy entities):

```yaml
utility_meter:
  grid_input_energy_today:
    name: Grid Input Energy Today
    source: sensor.example_grid_input_energy_total
    cycle: daily
    always_available: false
  grid_input_returned_energy_today:
    name: Grid Input Returned Energy Today
    source: sensor.example_grid_input_returned_energy_total
    cycle: daily
    always_available: false
```

Bind the generated **daily** helper entities explicitly if they have different
names/IDs; never point these two keys at the lifetime counters:

```yaml
type: custom:jks-detailed # Or custom:jks-mini.
stabilizers: true
entities:
  grid_input_buy_today: sensor.grid_input_energy_today
  grid_input_sell_today: sensor.grid_input_returned_energy_today
```

The helpers retain their counts across HA restarts, but the first cycle after
creation is incomplete; a full daily count starts with the next cycle. See
[Home Assistant Utility Meter](https://www.home-assistant.io/integrations/utility_meter/).
The existing `grid_input_energy_total` and `grid_input_returned_energy_total`
entity bindings remain available and unchanged.

#### Temporary display-only mirror test

Until the new upstream Shelly is installed, explicitly enable this test option:

```yaml
type: custom:jks-detailed # Also supported by custom:jks-mini.
stabilizers: true
stabilizers_test_mirror_grid: true
```

A persistent **TEST / MIRROR** banner and label identify the upstream Grid as a
copy of Stabilized Grid, not an actual pre-stabilizer measurement. Voltage,
current, phase/total power, frequency, availability and flow direction use the
downstream Grid display inputs, including zero, negative and unavailable values.
**Today** mirrors downstream `grid_buy_today - grid_sell_today`; if either
daily counter is unavailable it shows `--`. Any real upstream readings,
including daily and lifetime energy, are ignored for that display while the
test is on. The underlying daily inverter counters remain unchanged.

This changes only the card display, never HA entities or bridge publications.
The flag defaults to `false` and has no effect unless `stabilizers: true` is also
set. Remove it or set it to `false` after testing to restore strictly independent
upstream Shelly readings; there is no automatic downstream fallback.

For local synthetic preview, use `?card=detailed&stabilizers=true` or
`?card=summary&stabilizers=true`; omit `stabilizers` to inspect the default layout.

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

- Node.js `^24.22.1`; the repository pins Node.js 24.22.1 in `.node-version`.
- npm 12.2.1 with reproducible installs from `package-lock.json`.
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
