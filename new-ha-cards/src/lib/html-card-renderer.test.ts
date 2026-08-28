import assert from "node:assert/strict";
import test from "node:test";

import { createServer } from "vite";

test("disconnect releases layout observers and reconnect restores them once", async () => {
  const server = await createServer({
    configFile: false,
    server: { middlewareMode: true }
  });

  const originalWindow = globalThis.window;
  const originalResizeObserver = globalThis.ResizeObserver;
  const originalRequestAnimationFrame = globalThis.requestAnimationFrame;

  const mediaListeners = new Set<EventListenerOrEventListenerObject>();
  const windowResizeListeners = new Set<EventListenerOrEventListenerObject>();

  class ResizeObserverMock {
    static instances: ResizeObserverMock[] = [];

    disconnectCalls = 0;
    observed: unknown[] = [];

    constructor() {
      ResizeObserverMock.instances.push(this);
    }

    observe(target: unknown) {
      this.observed.push(target);
    }

    unobserve(): void {}

    disconnect() {
      this.disconnectCalls += 1;
    }
  }

  const mediaQuery = {
    matches: true,
    addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "change");
      mediaListeners.add(listener);
    },
    removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "change");
      assert.equal(mediaListeners.delete(listener), true);
    }
  } as unknown as MediaQueryList;

  globalThis.window = {
    matchMedia() {
      return mediaQuery;
    },
    addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "resize");
      windowResizeListeners.add(listener);
    },
    removeEventListener(type: string, listener: EventListenerOrEventListenerObject) {
      assert.equal(type, "resize");
      assert.equal(windowResizeListeners.delete(listener), true);
    }
  } as unknown as Window & typeof globalThis;
  globalThis.ResizeObserver = ResizeObserverMock as unknown as typeof ResizeObserver;
  globalThis.requestAnimationFrame = () => 1;

  try {
    const { HtmlCardRenderer } = await server.ssrLoadModule("/src/lib/html-card-renderer.ts");
    const frame = { querySelector: (selector: string) => (selector === ".summary-shell" ? shell : null) };
    const shell = {};
    const flowNode = {};
    const flowBoard = { querySelectorAll: () => [flowNode] };
    const dashboard = {
      querySelector: (selector: string) => (selector === ".summary-scale-frame" ? frame : null),
      querySelectorAll: (selector: string) => (selector === ".flow-board" ? [flowBoard] : [])
    };
    const renderer = new HtmlCardRenderer({});
    Object.assign(renderer, { dashboardEl: dashboard, renderedVariant: "summary" });

    renderer.connect();

    assert.equal(ResizeObserverMock.instances.length, 2);
    assert.deepEqual(ResizeObserverMock.instances[0]!.observed, [frame, shell]);
    assert.deepEqual(ResizeObserverMock.instances[1]!.observed, [dashboard, flowBoard, flowNode]);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.connect();
    assert.equal(ResizeObserverMock.instances.length, 2);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1]);
    assert.equal(mediaListeners.size, 0);
    assert.equal(windowResizeListeners.size, 0);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1]);

    renderer.connect();
    assert.equal(ResizeObserverMock.instances.length, 4);
    assert.equal(mediaListeners.size, 1);
    assert.equal(windowResizeListeners.size, 1);

    renderer.disconnect();
    assert.deepEqual(ResizeObserverMock.instances.map((observer) => observer.disconnectCalls), [1, 1, 1, 1]);
    assert.equal(mediaListeners.size, 0);
    assert.equal(windowResizeListeners.size, 0);
  } finally {
    globalThis.window = originalWindow;
    globalThis.ResizeObserver = originalResizeObserver;
    globalThis.requestAnimationFrame = originalRequestAnimationFrame;
    await server.close();
  }
});
