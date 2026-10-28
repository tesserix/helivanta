import { describe, expect, it } from "vitest";
import { globalApiURL } from "./global-api-url";

describe("global API destination", () => {
  it("defaults local clients to the connector and preserves an explicit origin", () => {
    expect(globalApiURL(undefined, false)).toBe("http://localhost:8081");
    expect(globalApiURL("https://global.example/", true)).toBe("https://global.example");
  });
  it("refuses missing production configuration and non-origin destinations", () => {
    for (const url of [
      undefined,
      "",
      "http://u:p@host",
      "http://host/v1",
      "http://host?x=1",
      "file:///tmp/api",
    ]) {
      expect(() => globalApiURL(url, true)).toThrow();
    }
  });
});
