import "./main";

// Local-only visual fixture; production builds use src/main.ts as their entry.
const params = new URLSearchParams(window.location.search);
const width = Number(params.get("width")) || 1672;
const style = document.createElement("style");
style.textContent = `
  body { margin: 0; padding: 12px; background: #02070b; color: #f4f8ff; font-family: "Segoe UI", sans-serif; }
  nav { display: flex; gap: 16px; margin-bottom: 12px; }
  a { color: #6edaff; }
  main { width: min(${Math.max(320, Math.min(2560, width))}px, 100%); margin: auto; }
`;
document.head.append(style);
const nav = document.createElement("nav");
for (const [label, target] of [["Desktop", 1672], ["Narrow desktop", 1024], ["Mobile", 420]] as const) {
  const link = document.createElement("a");
  link.textContent = label;
  link.href = `?width=${target}`;
  nav.append(link);
}
const main = document.createElement("main");
document.body.append(nav, main);
const card = document.createElement("jks-detailed") as HTMLElement & {
  setConfig(config: Record<string, unknown>): void;
};
main.append(card);
card.setConfig({ type: "custom:jks-detailed", title: "", static: true });
