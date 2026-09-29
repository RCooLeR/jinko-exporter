export type PositionMode = "desktop" | "mobile";

export interface PositionBoxModel {
  leftPercent?: number;
  topPercent?: number;
  widthPercent?: number;
  heightPercent?: number;
  fontScale?: number;
  fontSizePx?: number;
  minFontSizePx?: number;
  xOffsetPx?: number;
  yOffsetPx?: number;
  textAlign?: "left" | "center" | "right";
  justifyContent?: "flex-start" | "center" | "flex-end";
}

export interface CardElementPositionModel {
  value?: PositionBoxModel;
  rows?: Record<string, PositionBoxModel>;
  extras?: Record<string, PositionBoxModel>;
}

export type CardPositionModel = Record<string, CardElementPositionModel>;

export interface ResponsiveCardPositionModels {
  desktop: CardPositionModel;
  mobile: CardPositionModel;
}

export const DETAILED_CARD_POSITIONS: ResponsiveCardPositionModels = {
  // Desktop geometry lives in assets/main/desktop_layout_spec.json, measured
  // against desktop.png. Keep only typography here so the two cannot drift.
  desktop: {
    daily_production: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    daily_generator: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    daily_import: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    daily_export: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    daily_consumption: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    daily_costs: { value: { textAlign: "center", justifyContent: "center", fontSizePx: 30 } },
    battery: {
      extras: {
        soc: { fontSizePx: 38 },
        energy_today: { fontSizePx: 20 }
      }
    },
    inverter: {
      extras: {
        temp: { fontSizePx: 28 },
        status: { fontSizePx: 12, textAlign: "center", justifyContent: "center" }
      }
    }
  },
  mobile: {
    logo: {},
    daily_production: { value: {} },
    daily_generator: { value: {} },
    daily_import: { value: {} },
    daily_export: { value: {} },
    daily_consumption: { value: {} },
    daily_costs: { value: {} },
    ups_load: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    },
    pv1: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    },
    pv2: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    },
    grid: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    },
    battery: {
      rows: {
        voltage: {},
        current: {},
        power: {}
      },
      extras: {
        soc: {},
        energy_today: {}
      }
    },
    inverter: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      },
      extras: {
        temp: {},
        status: { yOffsetPx: 12, minFontSizePx: 6, textAlign: "center", justifyContent: "center" }
      }
    },
    generator: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    },
    parallel_grid_load: {
      rows: {
        voltage: {},
        current: {},
        power: {},
        energy_today: {}
      }
    }
  }
};

export const MINI_CARD_POSITIONS: ResponsiveCardPositionModels = {
  desktop: {
    production_card: { value: { leftPercent: 9, topPercent: 18.5, widthPercent: 11, fontScale: 0.9 } },
    import_card: { value: { leftPercent: 30, topPercent: 18.5, widthPercent: 11, fontScale: 0.9 } },
    export_card: { value: { leftPercent: 9, topPercent: 48.5, widthPercent: 11, fontScale: 0.9 } },
    consumption_card: { value: { leftPercent: 30, topPercent: 48.5, widthPercent: 11, fontScale: 0.9 } },
    costs_card: { value: { leftPercent: 9, topPercent: 79.5, widthPercent: 11, fontScale: 0.9 } },
    battery_soc_card: {
      value: { leftPercent: 32.5, topPercent: 80.5, widthPercent: 5, fontScale: 0.81, textAlign: "center", justifyContent: "center" }
    },
    combined_pv: {
      value: { leftPercent: 68.5, topPercent: 16, widthPercent: 9, fontScale: 0.72, textAlign: "center", justifyContent: "center" }
    },
    grid_node: {
      value: { leftPercent: 47, topPercent: 44, widthPercent: 10, textAlign: "center", justifyContent: "center" }
    },
    inverter_node: {
      extras: {
        temp: { leftPercent: 67.7, topPercent: 51.2, widthPercent: 6, textAlign: "center", justifyContent: "center" }
      }
    },
    combined_load: {
      value: { leftPercent: 89, topPercent: 42, widthPercent: 7, textAlign: "center", justifyContent: "center" }
    },
    battery_node: {
      value: { leftPercent: 49, topPercent: 77, widthPercent: 7, fontScale: 0.9, textAlign: "center", justifyContent: "center" }
    },
    generator_node: {
      value: { leftPercent: 69, topPercent: 83, widthPercent: 7, textAlign: "center", justifyContent: "center" }
    }
  },
  mobile: {
    production_card: {
      value: { leftPercent: 19.6, topPercent: 10, widthPercent: 26, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    import_card: {
      value: { leftPercent: 64.6, topPercent: 10, widthPercent: 26, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    export_card: {
      value: { leftPercent: 19.5, topPercent: 25.5, widthPercent: 26, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    consumption_card: {
      value: { leftPercent: 64, topPercent: 26, widthPercent: 26, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    costs_card: {
      value: { leftPercent: 19.5, topPercent: 40, widthPercent: 26, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    battery_soc_card: {
      value: { leftPercent: 64.5, topPercent: 40, widthPercent: 26, fontSizePx: 22.68, textAlign: "center", justifyContent: "center" }
    },
    combined_pv: {
      value: { leftPercent: 45, topPercent: 52.5, widthPercent: 16, fontScale: 0.68, textAlign: "center", justifyContent: "center" }
    },
    grid_node: {
      value: { leftPercent: 11.5, topPercent: 66.5, widthPercent: 11, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    inverter_node: {
      extras: {
        temp: { leftPercent: 45.5, topPercent: 71, widthPercent: 9, textAlign: "center", justifyContent: "center" }
      }
    },
    combined_load: {
      value: { leftPercent: 77, topPercent: 66, widthPercent: 14, fontSizePx: 25.2, textAlign: "center", justifyContent: "center" }
    },
    battery_node: {
      value: { leftPercent: 48, topPercent: 82.8, widthPercent: 10, fontScale: 0.9, textAlign: "center", justifyContent: "center" }
    },
    generator_node: {
      value: { leftPercent: 49, topPercent: 94, widthPercent: 10, textAlign: "center", justifyContent: "center" }
    }
  }
};
