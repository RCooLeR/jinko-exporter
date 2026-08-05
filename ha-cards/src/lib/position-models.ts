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
  desktop: {
    logo: {},
    daily_production: {
      value: { topPercent: 22.1 }
    },
    daily_generator: {
      value: { topPercent: 35.2 }
    },
    daily_import: {
      value: { topPercent: 48.2 }
    },
    daily_export: {
      value: { topPercent: 61.3 }
    },
    daily_consumption: {
      value: { topPercent: 74.3 }
    },
    daily_costs: {
      value: { topPercent: 87.6 }
    },
    ups_load: {
      rows: {
        voltage: { leftPercent: 30.4, topPercent: 12.7, widthPercent: 10.8 },
        current: { leftPercent: 30.4, topPercent: 16.8, widthPercent: 10.8 },
        power: { leftPercent: 30.4, topPercent: 21.7, widthPercent: 10.8 },
        energy_today: { leftPercent: 30.4, widthPercent: 10.8 }
      }
    },
    pv1: {
      rows: {
        voltage: { leftPercent: 58, topPercent: 12.4, widthPercent: 6.5 },
        current: { leftPercent: 58, topPercent: 17, widthPercent: 6.5 },
        power: { leftPercent: 58, topPercent: 21.5, widthPercent: 6.5 },
        energy_today: { leftPercent: 58, topPercent: 26, widthPercent: 6.5 }
      }
    },
    pv2: {
      rows: {
        voltage: { leftPercent: 73.2, topPercent: 12.4, widthPercent: 6.5 },
        current: { leftPercent: 73.2, topPercent: 17, widthPercent: 6.5 },
        power: { leftPercent: 73.2, topPercent: 21.5, widthPercent: 6.5 },
        energy_today: { leftPercent: 73.2, topPercent: 26, widthPercent: 6.5 }
      }
    },
    grid: {
      rows: {
        voltage: { leftPercent: 87.4, topPercent: 12.4, widthPercent: 8.8 },
        current: { leftPercent: 87.4, topPercent: 17, widthPercent: 8.8 },
        power: { leftPercent: 87.4, topPercent: 21.5, widthPercent: 8.8 },
        energy_today: { leftPercent: 87.4, topPercent: 27.5, widthPercent: 8.8 }
      }
    },
    battery: {
      rows: {
        voltage: { leftPercent: 28.2, topPercent: 54, widthPercent: 6.4 },
        current: { leftPercent: 28.2, topPercent: 58.1, widthPercent: 6.4 },
        power: { leftPercent: 28.2, topPercent: 62.5, widthPercent: 6.4 }
      },
      extras: {
        soc: { leftPercent: 28.1, topPercent: 72.2, fontScale: 0.8 },
        energy_today: { leftPercent: 28.2, topPercent: 84.8, widthPercent: 6.4 }
      }
    },
    inverter: {
      rows: {
        voltage: { leftPercent: 53.4, topPercent: 46.2, widthPercent: 7.9 },
        current: { leftPercent: 53.4, topPercent: 51, widthPercent: 7.9 },
        power: { leftPercent: 54.6, topPercent: 55.5, widthPercent: 6.7 },
        energy_today: { leftPercent: 55.8, topPercent: 61.5, widthPercent: 7.2 }
      },
      extras: {
        temp: { leftPercent: 65.1, topPercent: 56.5, fontScale: 0.8 },
        status: { fontScale: 0.5, textAlign: "center", justifyContent: "center" }
      }
    },
    generator: {
      rows: {
        voltage: { leftPercent: 66, topPercent: 78.4, widthPercent: 8.5 },
        current: { leftPercent: 66, topPercent: 82.4, widthPercent: 8.5 },
        power: { leftPercent: 66, widthPercent: 8.5 },
        energy_today: { leftPercent: 66, topPercent: 91.4, widthPercent: 8.5 }
      }
    },
    parallel_grid_load: {
      rows: {
        voltage: { leftPercent: 87.4, topPercent: 71, widthPercent: 8.8 },
        current: { leftPercent: 87.4, topPercent: 76, widthPercent: 8.8 },
        power: { leftPercent: 87.4, topPercent: 80.2, widthPercent: 8.8 },
        energy_today: { leftPercent: 87.4, topPercent: 85.6, widthPercent: 8.8 }
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
