import { EnergyArtCard, registerEnergyArtCard, type EnergyArtCardConfig } from "./energy-art-card";

const CARD_TAG = "jks-detailed";

class JksDetailedCard extends EnergyArtCard {
  protected readonly variant = "detailed" as const;
  protected readonly defaultCardSize = 10;
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
  customElements.define(CARD_TAG, JksDetailedCard);
}

registerEnergyArtCard({
  tag: CARD_TAG,
  name: "JKS Detailed",
  description: "Redesigned full Jinko energy topology card."
});
