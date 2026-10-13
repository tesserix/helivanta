import "@testing-library/jest-dom/vitest";

// jsdom omits the pointer-capture and scrolling APIs used by Radix controls.
if (typeof HTMLElement !== "undefined" && typeof window !== "undefined") {
  HTMLElement.prototype.scrollIntoView ??= () => undefined;
  HTMLElement.prototype.hasPointerCapture ??= () => false;
  HTMLElement.prototype.setPointerCapture ??= () => undefined;
  HTMLElement.prototype.releasePointerCapture ??= () => undefined;
  window.PointerEvent ??= MouseEvent as typeof PointerEvent;
}
