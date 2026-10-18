import type { HomeAssistant } from "../types/home-assistant";
import { toNumber } from "./entity-model";

// Re-resolve on discovery or availability/unit changes, not for every reading.
// A cached match must not hide a newly introduced ambiguity or canonical entity.
export const entityInventorySignature = (hass: HomeAssistant): string => JSON.stringify(
  Object.entries(hass.states)
    .filter(([id]) => id.startsWith("sensor."))
    .map(([id, state]) => [id, state.attributes?.friendly_name ?? "", state.attributes?.unit_of_measurement ?? "", toNumber(state) !== null])
);
