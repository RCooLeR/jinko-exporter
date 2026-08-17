import "./main";

const style = document.createElement("style");
style.textContent = `
  *,
  *::before,
  *::after {
    box-sizing: border-box;
  }

  html,
  body {
    min-height: 100%;
  }

  body {
    margin: 0;
    background: #02070b;
    color: #eff8ff;
    font-family: Inter, "Segoe UI", system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
  }

  main {
    display: grid;
    gap: 28px;
    width: min(1672px, 100vw);
    margin: 0 auto;
    padding: 16px;
  }
`;
document.head.append(style);

const main = document.createElement("main");
const params = new URLSearchParams(window.location.search);
const cardMode = params.get("card") ?? "detailed";
const previewWidth = Number(params.get("width"));

if (Number.isFinite(previewWidth) && previewWidth > 0) {
  main.style.width = `min(${previewWidth}px, 100vw)`;
}

if (cardMode === "summary" || cardMode === "all") {
  const summary = document.createElement("jks-mini") as HTMLElement & {
    setConfig(config: Record<string, unknown>): void;
  };
  summary.setConfig({ type: "custom:jks-mini", static: true });
  main.append(summary);
}

if (cardMode === "detailed" || cardMode === "all") {
  const detailed = document.createElement("jks-detailed") as HTMLElement & {
    setConfig(config: Record<string, unknown>): void;
  };
  detailed.setConfig({ type: "custom:jks-detailed", static: true });
  main.append(detailed);
}

document.body.append(main);
