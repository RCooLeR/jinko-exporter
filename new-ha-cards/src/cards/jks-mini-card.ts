import { EnergyArtCard, registerEnergyArtCard, type EnergyArtCardConfig } from "./energy-art-card";

const CARD_TAG = "jks-mini";

class JksMiniCard extends EnergyArtCard {
  protected readonly variant = "summary" as const;
  protected readonly defaultCardSize = 5;
  protected readonly defaultConfig: EnergyArtCardConfig = {
    type: `custom:${CARD_TAG}`,
    title: "",
    static: false,
    show_entity_map: false,
    battery_capacity_kwh: 21.31,
    battery_negative_is_charging: true,
    entities: {}
  };

  static getStubConfig(): EnergyArtCardConfig {
    return { type: `custom:${CARD_TAG}` };
  }
}

if (!customElements.get(CARD_TAG)) {
  customElements.define(CARD_TAG, JksMiniCard);
}

registerEnergyArtCard({
  tag: CARD_TAG,
  name: "JKS Mini",
  description: "Redesigned summary Jinko energy card."
});
