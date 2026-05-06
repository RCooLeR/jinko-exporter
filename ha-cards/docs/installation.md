# Card Installation

The cards are built with Vite and installed as a Lovelace JavaScript module.

## Build

From `ha-cards/`:

```shell
npm install
npm run build
```

Build output:

```text
ha-cards/dist/jinko-ha-cards.js
```

## Copy To Home Assistant

Create a directory under Home Assistant `www`:

```text
/config/www/jinko-ha-cards/
```

Copy:

```text
ha-cards/dist/jinko-ha-cards.js
ha-cards/assets/main/
ha-cards/assets/overview/
```

Expected target layout:

```text
/config/www/jinko-ha-cards/jinko-ha-cards.js
/config/www/jinko-ha-cards/assets/main/desktop.png
/config/www/jinko-ha-cards/assets/overview/desktop.png
```

## Add Lovelace Resource

In Home Assistant, go to:

```text
Settings -> Dashboards -> Resources
```

Add:

```yaml
url: /local/jinko-ha-cards/jinko-ha-cards.js?v=1
type: module
```

Increase the `v=` value after replacing the bundle to bypass browser caching.

## Add A Card

Detailed card:

```yaml
type: custom:jks-detailed
title: JKS Detailed
```

Mini card:

```yaml
type: custom:jks-mini
title: JKS Mini
```

## Static Preview

Both cards support `static: true` for layout preview without live entities:

```yaml
type: custom:jks-detailed
title: Preview
static: true
```

## Troubleshooting

- Blank card: confirm the Lovelace resource URL is correct and browser cache is refreshed.
- Missing background: confirm `assets/main` and `assets/overview` were copied next to the bundle.
- Values show `--`: check [entity resolution](./entity-resolution.md) and enable `show_entity_map`.
- Card not listed in the UI picker: reload the browser tab after adding the resource.
