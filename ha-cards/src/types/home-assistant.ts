export interface HomeAssistantState {
  entity_id?: string;
  state: string;
  attributes: Record<string, unknown>;
}

export interface HomeAssistant {
  states: Record<string, HomeAssistantState>;
}

export interface LovelaceCardConfig {
  type: string;
  [key: string]: unknown;
}
