import { afterEach, describe, expect, it } from "vitest";
import { CHROME_INIT_SCRIPT, NAVIGATION_KEY } from "./chrome-flags";

function runInitScript() {
  new Function(CHROME_INIT_SCRIPT)();
}

describe("CHROME_INIT_SCRIPT", () => {
  afterEach(() => {
    window.localStorage.clear();
    delete document.documentElement.dataset.hmsPanel;
    delete document.documentElement.dataset.hmsNavigation;
  });

  it("stamps the stored navigation state on <html>", () => {
    window.localStorage.setItem(NAVIGATION_KEY, "1");
    runInitScript();
    expect(document.documentElement.dataset.hmsNavigation).toBe("open");
  });

  it("leaves a flag that was never stored unset, so the default stands", () => {
    runInitScript();
    expect(document.documentElement.dataset.hmsNavigation).toBeUndefined();
  });

  // #1286 D4: the panel cannot be collapsed any more, so a stored collapse
  // from before is ignored rather than painting it shut.
  it("ignores a panel collapse stored before the panel became fixed", () => {
    window.localStorage.setItem("hms.panel.open", "0");
    runInitScript();
    expect(document.documentElement.dataset.hmsPanel).toBeUndefined();
  });

  it("does not throw when storage is unavailable", () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = () => {
      throw new Error("storage unavailable");
    };
    try {
      expect(runInitScript).not.toThrow();
      expect(document.documentElement.dataset.hmsNavigation).toBeUndefined();
    } finally {
      Storage.prototype.getItem = original;
    }
  });
});
