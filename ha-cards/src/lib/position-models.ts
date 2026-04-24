export type PositionMode = "desktop" | "mobile";

export interface PositionBoxModel {
  leftPercent?: number;
  topPercent?: number;
  widthPercent?: number;
  heightPercent?: number;
  fontScale?: number;
  fontSizePx?: number;
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
        voltage: { topPercent: 12.7 },
        current: {},
        power: { topPercent: 21.8 },
        energy_today: {}
      }
    },
    pv1: {
      rows: {
        voltage: { leftPercent: 55.4, topPercent: 12.4 },
        current: { leftPercent: 55.4, topPercent: 17 },
        power: { leftPercent: 55.4, topPercent: 21.5 },
        energy_today: { leftPercent: 55.4, topPercent: 26 }
      }
    },
    pv2: {
      rows: {
        voltage: { leftPercent: 71.5, topPercent: 12.4 },
        current: { leftPercent: 71.5, topPercent: 17 },
        power: { leftPercent: 71.5, topPercent: 21.5 },
        energy_today: { leftPercent: 71.5, topPercent: 26 }
      }
    },
    grid: {
      rows: {
        voltage: { leftPercent: 91 },
        current: { leftPercent: 91 },
        power: { leftPercent: 91 },
        energy_today: { leftPercent: 91, topPercent: 27.5 }
      }
    },
    battery: {
      rows: {
        voltage: { topPercent: 54 },
        current: { topPercent: 58.1 },
        power: { topPercent: 62.5 }
      },
      extras: {
        soc: { leftPercent: 29.5, topPercent: 72.4, fontScale: 0.8 },
        energy_today: { topPercent: 84.8 }
      }
    },
    inverter: {
      rows: {
        voltage: { leftPercent: 56.3, topPercent: 46.2 },
        current: { leftPercent: 56.3, topPercent: 51 },
        power: { leftPercent: 56.3, topPercent: 55.5 },
        energy_today: { leftPercent: 56.3, topPercent: 61.5 }
      },
      extras: {
        temp: { leftPercent: 66, topPercent: 52.8, fontScale: 0.8 },
        status: { leftPercent: 65, topPercent: 61.7, widthPercent: 9 }
      }
    },
    generator: {
      rows: {
        voltage: { leftPercent: 63, topPercent: 78.4 },
        current: { leftPercent: 63, topPercent: 82.4 },
        power: { leftPercent: 63 },
        energy_today: { leftPercent: 63, topPercent: 91.4 }
      }
    },
    parallel_grid_load: {
      rows: {
        voltage: { leftPercent: 90.5, topPercent: 71 },
        current: { leftPercent: 90.5, topPercent: 76 },
        power: { leftPercent: 90.5, topPercent: 80.2 },
        energy_today: { leftPercent: 90.5, topPercent: 85.6 }
      }
    }
  },
  mobile: {
    logo: {},
    daily_production: { value: { leftPercent: 16, topPercent: 14 } },
    daily_generator: { value: { leftPercent: 48, topPercent: 14 } },
    daily_import: { value: { leftPercent: 82, topPercent: 14 } },
    daily_export: { value: { leftPercent: 16, topPercent: 22.5 } },
    daily_consumption: { value: { leftPercent: 48, topPercent: 22.5 } },
    daily_costs: { value: { leftPercent: 48, topPercent: 22.5 } },
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
        status: {}
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
      value: { leftPercent: 68.5, topPercent: 16, widthPercent: 9, textAlign: "center", justifyContent: "center" }
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
      value: { leftPercent: 45, topPercent: 52.5, widthPercent: 16, textAlign: "center", justifyContent: "center" }
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
