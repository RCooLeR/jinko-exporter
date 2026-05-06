# Home Assistant Cards

The `ha-cards` package provides optional Lovelace cards for the entities published by JinkoBridge MQTT Discovery.

The cards are visual dashboards only. They do not communicate with the inverter and do not send commands. They read existing Home Assistant sensor states.

## Cards

| Card type | Purpose |
| --- | --- |
| `custom:jks-detailed` | Large detailed power-flow dashboard using the `assets/main` artwork. |
| `custom:jks-mini` | Compact overview card using the `assets/overview` artwork. |

## Documentation

- [Installation](./installation.md)
- [Card configuration](./cards.md)
- [Entity resolution](./entity-resolution.md)

## Assets

The cards depend on static image assets:

```text
assets/main/
assets/overview/
```

When installing manually, copy both asset directories next to the built JavaScript bundle.
