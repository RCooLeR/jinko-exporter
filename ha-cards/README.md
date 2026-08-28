# JinkoBridge Home Assistant Cards

This package contains optional TypeScript Lovelace cards for the Home Assistant MQTT entities created by the bridge.

This stable bundle and the redesigned bundle under `new-ha-cards/` register the same custom-element names. Install exactly one of them in Home Assistant; they are release alternatives, not additive resources.

## Cards

- `custom:jks-detailed`
- `custom:jks-mini`

## Documentation

- [Card documentation](./docs/index.md)
- [Installation](./docs/installation.md)
- [Card configuration](./docs/cards.md)
- [Entity resolution](./docs/entity-resolution.md)

## Toolchain

- Node.js `^24.12.0`; the repository pins Node.js 24.20.0 in `.node-version`.
- npm 11.19.0 with reproducible installs from `package-lock.json`.
- TypeScript 7 with strict, erasable-syntax, unchecked-index, and side-effect-import checks.
- Vite 8 producing an ES module for the Baseline Widely Available browser target.

The development server forwards browser console output to the terminal, while production builds use Vite's browser baseline target instead of maintaining a separate browser-version list.

## Development

```shell
npm ci
npm run check
```

`npm run check` runs tests, checks both the browser sources and Vite configuration with TypeScript, then creates the production bundle. For interactive work, start the development server with `npm run dev`.

The production bundle is written to:

```text
dist/jinko-ha-cards.js
```
