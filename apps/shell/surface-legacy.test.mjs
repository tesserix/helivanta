import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { SURFACE_PATTERNS } from "@helivanta/config/eslint";
import { SURFACE_LEGACY } from "./surface-legacy.mjs";

// #1275 spec D4: the legacy list only shrinks. A converted file must leave
// it (and is then held to the surface rule), and only the four zones whose
// tidy-ups own the conversion may be listed.
const ZONES = /^(components\/(pharmacy|supply|emergency)|app\/\(workspace\)\/workforce)\//;
const strings = (source) => source.match(/"[^"\n]*"|'[^'\n]*'|`[^`]*`/g) ?? [];

describe("surface legacy list", () => {
  it.each(SURFACE_LEGACY)("%s still paints its own surface", (file) => {
    expect(file).toMatch(ZONES);
    const source = readFileSync(resolve(import.meta.dirname, file), "utf8");
    expect(
      strings(source).some((s) => SURFACE_PATTERNS.some((re) => re.test(s.slice(1, -1)))),
    ).toBe(true);
  });

  it("is never added to", () => {
    // Lower this as zone tidy-ups remove files; raising it needs a reason in review.
    expect(SURFACE_LEGACY.length).toBeLessThanOrEqual(91);
    expect(new Set(SURFACE_LEGACY).size).toBe(SURFACE_LEGACY.length);
  });
});
