import { ENTITY_KEYS, resolveEntities, valueFor as entityValueFor, type EntityKey, type EntityOverrides, type ResolvedEntityMap } from "../lib/entity-model";
import { buildEnergyViewModel, fakeValueFor, type EnergyMetricKey } from "../lib/energy-view-model";
import { HtmlCardRenderer, type EnergyCardVariant } from "../lib/html-card-renderer";
import type { HomeAssistant, LovelaceCardConfig } from "../types/home-assistant";

export interface EnergyArtCardConfig extends LovelaceCardConfig {
  title?: string;
  static?: boolean;
  show_entity_map?: boolean;
  battery_capacity_kwh?: number;
  battery_negative_is_charging?: boolean;
  entities?: EntityOverrides;
}

export interface EnergyArtCardRegistration {
  tag: string;
  name: string;
  description: string;
}

export abstract class EnergyArtCard extends HTMLElement {
  private readonly renderer: HtmlCardRenderer;
  private readonly rootNode: ShadowRoot;
  private config?: EnergyArtCardConfig;
  private hassValue?: HomeAssistant;
  private resolved: ResolvedEntityMap = {};
  private attemptedEntityKeys = new Set<EntityKey>();
  private hasRendered = false;

  protected abstract readonly variant: EnergyCardVariant;
  protected abstract readonly defaultConfig: EnergyArtCardConfig;
  protected abstract readonly defaultCardSize: number;

  constructor() {
    super();
    this.rootNode = this.attachShadow({ mode: "open" });
    this.renderer = new HtmlCardRenderer(this.rootNode);
  }

  setConfig(config: EnergyArtCardConfig): void {
    if (!config || typeof config !== "object") {
      throw new Error("Card configuration is required");
    }

    this.config = {
      ...this.defaultConfig,
      ...config,
      entities: {
        ...(this.defaultConfig.entities ?? {}),
        ...(config.entities ?? {})
      }
    };
    this.resolved = {};
    this.attemptedEntityKeys.clear();
    this.hasRendered = false;
    this.resolveEntities(true);
    this.render();
  }

  set hass(hass: HomeAssistant) {
    if (this.activeConfig.static && this.hasRendered) {
      this.hassValue = hass;
      return;
    }

    this.hassValue = hass;
    this.resolveEntities();
    this.render();
  }

  connectedCallback(): void {
    this.render();
  }

  disconnectedCallback(): void {}

  getCardSize(): number {
    return this.defaultCardSize;
  }

  static getStubConfig(): EnergyArtCardConfig {
    return { type: "custom:jks-detailed" };
  }

  private get activeConfig(): EnergyArtCardConfig {
    return this.config ?? this.defaultConfig;
  }

  private resolveEntities(force = false): void {
    if (!this.hassValue) return;
    if (force) this.attemptedEntityKeys.clear();

    const keys = force
      ? ENTITY_KEYS
      : ENTITY_KEYS.filter((key) => {
          if (!this.attemptedEntityKeys.has(key)) return true;
          const entityId = this.resolved[key];
          return !entityId || !this.hassValue?.states[entityId];
        });

    if (!keys.length) return;

    this.resolved = {
      ...this.resolved,
      ...resolveEntities(this.hassValue, this.activeConfig.entities, keys)
    };

    for (const key of keys) this.attemptedEntityKeys.add(key);
  }

  private render(): void {
    const config = this.activeConfig;
    const viewModel = buildEnergyViewModel((key) => this.readValue(key), {
      batteryNegativeIsCharging: config.battery_negative_is_charging !== false
    });
    const warning = !config.static && viewModel.missing.length ? `Missing critical sensors: ${viewModel.missing.join(", ")}` : "";

    this.renderer.render(this.variant, {
      title: config.title?.trim() ?? "",
      values: viewModel.values,
      flags: viewModel.flags,
      warning,
      entityRows: config.show_entity_map ? this.entityRows() : undefined
    });

    this.hasRendered = true;
  }

  private readValue(key: EnergyMetricKey): number | null {
    if (this.activeConfig.static) return fakeValueFor(key);
    if (key === "daily_cost") return null;
    return entityValueFor(this.hassValue, this.resolved, key);
  }

  private entityRows(): Array<[string, string]> {
    return Object.entries(this.resolved).map(([key, entityId]) => [key, entityId ?? "--"]);
  }
}

export const registerEnergyArtCard = (registration: EnergyArtCardRegistration): void => {
  window.customCards = window.customCards || [];
  const existing = window.customCards.find((card) => card.type === registration.tag);
  if (existing) {
    existing.name = registration.name;
    existing.description = registration.description;
    return;
  }

  window.customCards.push({
    type: registration.tag,
    name: registration.name,
    description: registration.description
  });
};

declare global {
  interface Window {
    customCards?: Array<{ type: string; name: string; description: string }>;
  }
}
