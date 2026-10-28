import { afterEach, expect, it, vi } from "vitest";
import { notifySessionChanged, SESSION_CHANGE_KEY } from "./session-change";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  window.localStorage.clear();
});

it("publishes a fresh data-free marker for every session change", () => {
  notifySessionChanged();
  const first = window.localStorage.getItem(SESSION_CHANGE_KEY);
  expect(first).toMatch(/^[a-f0-9-]{36}$/);
  notifySessionChanged();
  expect(window.localStorage.getItem(SESSION_CHANGE_KEY)).not.toBe(first);
});

it("broadcasts the change even when local storage cannot be written", () => {
  const postMessage = vi.fn();
  const close = vi.fn();
  class Channel {
    postMessage = postMessage;
    close = close;
  }
  vi.stubGlobal("BroadcastChannel", Channel);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("storage disabled");
  });
  expect(() => notifySessionChanged()).not.toThrow();
  expect(postMessage).toHaveBeenCalledExactlyOnceWith({
    type: "changed",
    source: expect.stringMatching(/^[a-f0-9-]{36}$/),
  });
  expect(close).toHaveBeenCalledOnce();
});
