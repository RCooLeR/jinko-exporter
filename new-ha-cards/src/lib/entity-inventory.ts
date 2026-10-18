import type { HomeAssistant } from "../types/home-assistant";
import { toNumber } from "./entity-model";

export const entityInventorySignature = (hass: HomeAssistant): string => JSON.stringify(
  Object.entries(hass.states)
    .filter(([id]) => id.startsWith("sensor."))
    .map(([id, state]) => [id, state.attributes?.friendly_name ?? "", state.attributes?.unit_of_measurement ?? "", toNumber(state) !== null])
);
