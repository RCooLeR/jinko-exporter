# JinkoBridge

[![CI](https://github.com/RCooLeR/jinko-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/RCooLeR/jinko-exporter/actions/workflows/ci.yml)

JinkoBridge is an unofficial solar telemetry bridge for Jinko and compatible Solarman-backed inverter data. It polls one configured upstream source, exposes Prometheus metrics over HTTP, and can publish read-only Home Assistant MQTT Discovery entities.

<div align="center" style="text-align: center">
  <img src="./assets/jinko.png" width="70%" alt="JinkoBridge">
</div>

## Disclaimer

JinkoBridge is an unofficial DIY open-source project for compatibility, monitoring, and local automation. It is not affiliated with, endorsed by, sponsored by, or supported by Jinko Solar, Solarman, Deye, or any related vendor.

This project is read-only and does not send inverter control commands. It still depends on credentials, tokens, and third-party APIs that may change or stop working without notice. Do not use it as the only monitoring path for safety, billing, critical power, medical, or life-support decisions.

## Documentation

- [Bridge documentation](./bridge/docs/index.md)
- [Installation](./bridge/docs/installation.md)
- [Configuration](./bridge/docs/configuration.md)
- [Collected data](./bridge/docs/collected-data.md)
- [Prometheus metrics](./bridge/docs/prometheus.md)
- [Home Assistant MQTT Discovery](./bridge/docs/home-assistant.md)
- [Alerts](./bridge/docs/alerts.md)
- [Home Assistant cards](./ha-cards/README.md)

## Repository Layout

- [`bridge/`](./bridge): Go bridge/exporter module, Dockerfile, tests, and bridge documentation.
- [`ha-cards/`](./ha-cards): optional Home Assistant Lovelace cards for the MQTT entities.
- [`assets/`](./assets): repository images used by documentation.

## License

MIT License. See [LICENSE](./LICENSE). See [NOTICE](./NOTICE) for trademark and affiliation notice.
