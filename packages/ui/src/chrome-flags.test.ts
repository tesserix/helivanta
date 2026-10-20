import { afterEach, describe, expect, it } from "vitest";
import { CHROME_INIT_SCRIPT, NAVIGATION_KEY, PANEL_KEY } from "./chrome-flags";

function runInitScript() {
  new Function(CHROME_INIT_SCRIPT)();
}

describe("CHROME_INIT_SCRIPT", () => {
  afterEach(() => {
    window.localStorage.clear();
    delete document.documentElement.dataset.hmsPanel;
    delete document.documentElement.dataset.hmsNavigation;
  });

  it("stamps the stored panel and navigation state on <html>", () => {
    window.localStorage.setItem(PANEL_KEY, "0");
    window.localStorage.setItem(NAVIGATION_KEY, "1");
    runInitScript();
    expect(document.documentElement.dataset.hmsPanel).toBe("closed");
    expect(document.documentElement.dataset.hmsNavigation).toBe("open");
  });

  it("leaves a flag that was never stored unset, so the default stands", () => {
    window.localStorage.setItem(PANEL_KEY, "0");
    runInitScript();
    expect(document.documentElement.dataset.hmsPanel).toBe("closed");
    expect(document.documentElement.dataset.hmsNavigation).toBeUndefined();
  });

  it("does not throw when storage is unavailable", () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = () => {
      throw new Error("storage unavailable");
    };
    try {
      expect(runInitScript).not.toThrow();
      expect(document.documentElement.dataset.hmsPanel).toBeUndefined();
    } finally {
      Storage.prototype.getItem = original;
    }
  });
});
