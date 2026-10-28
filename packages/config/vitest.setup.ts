import * as matchers from "@testing-library/jest-dom/matchers";

// A peer Vitest copy installs duplicate Chai wrappers; use this runner's global expect.
expect.extend(matchers);

// jsdom omits the pointer-capture and scrolling APIs used by Radix controls.
if (typeof HTMLElement !== "undefined" && typeof window !== "undefined") {
  HTMLElement.prototype.scrollIntoView ??= () => Promise.resolve();
  HTMLElement.prototype.hasPointerCapture ??= () => false;
  HTMLElement.prototype.setPointerCapture ??= () => undefined;
  HTMLElement.prototype.releasePointerCapture ??= () => undefined;
  window.PointerEvent ??= MouseEvent as typeof PointerEvent;
}
