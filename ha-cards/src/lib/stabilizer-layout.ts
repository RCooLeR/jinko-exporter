// The optional topology redraws only the right-hand branch. Its panels, row
// artwork, text overlays and connectors all use these same source coordinates.
// The original raster assets and default layout are never modified.
export type LayoutBox = [number, number, number, number];

export interface LayoutRow {
  id: string;
  row_box: LayoutBox;
  value_box: LayoutBox;
}

export interface LayoutElement {
  id: string;
  type: string;
  bbox: LayoutBox;
  label?: string;
  value_box?: LayoutBox | null;
  rows?: LayoutRow[];
  soc_value_box?: LayoutBox;
  energy_today_value_box?: LayoutBox;
  temp_value_box?: LayoutBox;
  status_value_box?: LayoutBox;
}

export interface LayoutSpec {
  canvas: { width: number; height: number; background_image: string };
  elements: LayoutElement[];
}

interface StabilizerPanel extends LayoutElement {
  label: string;
  subtitle: string;
  color: string;
  rows: LayoutRow[];
}

export interface StabilizerGeometry {
  mask: LayoutBox;
  masks: LayoutBox[];
  panels: StabilizerPanel[];
  links: string[];
  labelBox: LayoutBox;
}

const labels: Record<string, string> = {
  voltage: "Voltage", current: "Current", power: "Power", energy_today: "Today"
};

export const stabilizerGeometry = (mobile: boolean): StabilizerGeometry => {
  const boxes: LayoutBox[] = mobile
    ? [[665, 752, 253, 250], [665, 1082, 253, 250], [665, 1383, 253, 253]]
    : [[1316, 31, 304, 260], [1316, 379, 304, 260], [1316, 687, 304, 218]];
  const panels = [
    { id: "grid_input", label: "Grid", subtitle: "Before stabilizers · Shelly", color: "#a68cdf", rows: ["voltage", "current", "power", "energy_today"] },
    { id: "grid", label: "Stabilized Grid", subtitle: "After stabilizers · Jinko CT", color: "#bd85df", rows: ["voltage", "current", "power", "energy_today"] },
    { id: "parallel_grid_load", label: "Parallel Grid Load", subtitle: "After stabilizers · Shelly", color: "#d88cde", rows: ["voltage", "current", "power"] }
  ].map((panel, index): StabilizerPanel => {
    const bbox = boxes[index]!;
    const [x, y, width, height] = bbox;
    const header = mobile ? 77 : 80;
    const gap = mobile ? 5 : 4;
    const rowHeight = Math.min(mobile ? 40 : 39, (height - header - 14 - gap * (panel.rows.length - 1)) / panel.rows.length);
    const valueWidth = mobile ? 141 : 170;
    return {
      ...panel, type: "metric_card", bbox,
      rows: panel.rows.map((id, rowIndex) => ({
        id,
        row_box: [x + 12, y + header + rowIndex * (rowHeight + gap), width - 24, rowHeight],
        value_box: [x + width - 21 - valueWidth, y + header + rowIndex * (rowHeight + gap), valueWidth, rowHeight]
      }))
    };
  });
  const center = boxes[0]![0] + boxes[0]![2] / 2;
  const upperBottom = boxes[0]![1] + boxes[0]![3];
  const midTop = boxes[1]![1];
  const midBottom = midTop + boxes[1]![3];
  const bottomTop = boxes[2]![1];
  const inverterX = mobile ? 630 : 1268;
  const connectionY = midTop + boxes[1]![3] / 2;
  return {
    mask: mobile ? [629, 744, 296, 910] : [1267, 23, 377, 893],
    // PV2 reaches x=1291 above y=265. Only widen left below that panel,
    // where the old grid connector needs erasing; never cover the PV artwork.
    masks: mobile ? [[629, 744, 296, 910]] : [[1295, 23, 349, 247], [1267, 270, 377, 646]],
    panels,
    links: [
      `M ${center} ${upperBottom} V ${midTop}`,
      `M ${center} ${midBottom} V ${bottomTop}`,
      `M ${boxes[1]![0]} ${connectionY} H ${inverterX}`
    ],
    labelBox: [center - (mobile ? 70 : 79), (upperBottom + midTop) / 2 - 11, mobile ? 140 : 158, 22]
  };
};

export const withStabilizers = (base: LayoutSpec, mobile: boolean): LayoutSpec => ({
  canvas: { ...base.canvas },
  elements: [
    ...base.elements.filter(({ id }) => id !== "grid" && id !== "parallel_grid_load"),
    ...stabilizerGeometry(mobile).panels
  ]
});

const tower = `<path d="M20 2 9 53h23L20 2ZM8 13h24M5 24h30M14 7l12 6-16 11 17 12-18 16M26 7 14 13l16 11-17 12 19 16M8 13v8M32 13v8M5 24v8M35 24v8"/>`;
const house = `<path d="m2 25 19-20 19 20M8 20v31h26V20M17 51V34h9v17"/>`;

const rowIcon = (id: string): string => {
  if (id === "voltage") return `<path d="M1 13h5l3-10 4 22 4-15 3 7h5"/>`;
  if (id === "current") return `<circle cx="13" cy="13" r="10"/><path d="m8 18 5-12 5 12M10 14h6"/>`;
  if (id === "power") return `<path d="m15 1-12 15h9l-2 10 13-17h-9Z" fill="currentColor" stroke="none"/>`;
  return `<path d="M3 23V13h4v10M11 23V7h4v16M19 23V2h4v21"/>`;
};

// All inserted SVG content is static geometry/labels from this module, never
// configuration or HA state. Live values stay in the card's textContent nodes.
export const stabilizerArtwork = (layout: LayoutSpec, mobile: boolean, testMirror = false): string => {
  const geometry = stabilizerGeometry(mobile);
  const [lx, ly, lw, lh] = geometry.labelBox;
  const panels = geometry.panels.map((panel) => {
    const [x, y, width, height] = panel.bbox;
    const titleSize = panel.id === "parallel_grid_load" ? (mobile ? 20 : 23) : (mobile ? 22 : 26);
    const titleX = x + (mobile ? 53 : 66);
    const rows = panel.rows.map((row) => {
      const [rx, ry, rw, rh] = row.row_box;
      return `<rect x="${rx}" y="${ry}" width="${rw}" height="${rh}" rx="7" fill="#061b29" stroke="#28404b" stroke-opacity=".8"/>
        <g transform="translate(${rx + 8} ${ry + rh / 2 - 10}) scale(.77)" color="${panel.color}" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${rowIcon(row.id)}</g>
        <text x="${rx + 33}" y="${ry + rh / 2 + 1}" dominant-baseline="middle" font-size="${mobile ? 12 : 16}" fill="#d8dfe9">${labels[row.id]}</text>`;
    }).join("");
    return `<g data-stabilizer-node="${panel.id}">
      <rect x="${x}" y="${y}" width="${width}" height="${height}" rx="14" fill="url(#st-panel-fill)" stroke="${panel.color}" stroke-opacity=".6"/>
      <rect x="${x + 2}" y="${y + 2}" width="${width - 4}" height="${height - 4}" rx="13" fill="none" stroke="#182c3d"/>
      <g transform="translate(${x + 17} ${y + 13}) scale(${mobile ? .75 : .85})" fill="none" stroke="${panel.color}" stroke-width="1.6" stroke-linejoin="round">${panel.id === "parallel_grid_load" ? house : tower}</g>
      <text x="${titleX}" y="${y + 36}" font-size="${titleSize}" fill="#f2f4fa">${panel.label}</text>
      <text x="${titleX}" y="${y + 55}" font-family="Segoe UI, sans-serif" font-size="${mobile ? 10 : 11}" fill="${testMirror && panel.id === "grid_input" ? "#ffd870" : "#9bacbd"}">${testMirror && panel.id === "grid_input" ? "TEST · Stabilized Grid mirror" : panel.subtitle}</text>
      <path d="M ${x + 25} ${y + 67} H ${x + width - 25}" stroke="url(#st-line)" stroke-width="2"/>
      ${rows}
    </g>`;
  }).join("");
  return `<svg viewBox="0 0 ${layout.canvas.width} ${layout.canvas.height}" xmlns="http://www.w3.org/2000/svg" aria-hidden="true" focusable="false" font-family="Arial Narrow, Segoe UI, sans-serif">
    <defs>
      <linearGradient id="st-panel-fill" x2="0" y2="1"><stop stop-color="#07192b"/><stop offset="1" stop-color="#021421"/></linearGradient>
      <linearGradient id="st-line"><stop stop-color="#a765cd" stop-opacity="0"/><stop offset=".5" stop-color="#dfb6fa"/><stop offset="1" stop-color="#a765cd" stop-opacity="0"/></linearGradient>
      <marker id="st-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0 0 8 4 0 8Z" fill="#bd85df"/></marker>
    </defs>
    ${geometry.masks.map(([x, y, width, height]) => `<rect x="${x}" y="${y}" width="${width}" height="${height}" fill="#031323"/>`).join("")}
    <g fill="none" stroke="#bd85df" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${geometry.links.map((path, index) => `<path d="${path}" marker-end="url(#st-arrow)"${index !== 1 ? ' marker-start="url(#st-arrow)"' : ""}/>`).join("")}</g>
    <rect x="${lx}" y="${ly}" width="${lw}" height="${lh}" rx="11" fill="#0a2032" stroke="#71518d"/>
    <text x="${lx + lw / 2}" y="${ly + lh / 2 + 1}" dominant-baseline="middle" text-anchor="middle" fill="#c6addd" font-size="14">Stabilizers</text>
    ${panels}
  </svg>`;
};
