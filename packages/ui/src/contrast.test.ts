// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// One light sage palette for the public site and the app (#1255, spec D2).
// Contrast is a property of colour pairs, so it is asserted here against the
// tokens themselves (WCAG 2.2 AA) rather than reviewed by eye.
const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");

/** The custom properties declared in the first rule whose selector is exactly `selector`. */
function tokensOf(selector: string): Record<string, string> {
  const at = css.indexOf(`\n${selector} {`);
  expect(at, `no rule ${selector} in styles.css`).toBeGreaterThan(-1);
  const open = css.indexOf("{", at);
  const body = css.slice(open + 1, css.indexOf("}", open));
  return Object.fromEntries(
    [...body.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})/g)].map((m) => [m[1], m[2].toLowerCase()]),
  );
}

const root = tokensOf(':root,\n:root[data-theme="default"],\n[data-theme="default"]');

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

const BODY = 4.5;
const UI = 3;

describe("contrast()", () => {
  it("matches known WCAG ratios", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrast("#777777", "#ffffff")).toBeCloseTo(4.48, 2);
  });
});

it("is sage", () => {
  expect(root["--background"]).toBe("#fbfcfa");
  expect(root["--foreground"]).toBe("#173d3a");
  expect(root["--primary"]).toBe("#23685d");
});

const PAIRS: [fg: string, bg: string, min: number][] = [
  ["--foreground", "--background", BODY],
  ["--foreground", "--card", BODY],
  ["--muted-foreground", "--background", BODY],
  ["--muted-foreground", "--card", BODY],
  ["--muted-foreground", "--muted", BODY],
  ["--secondary-foreground", "--secondary", BODY],
  ["--primary-foreground", "--primary", BODY],
  ["--accent-foreground", "--accent", BODY],
  ["--destructive-foreground", "--destructive", BODY],
  ["--sidebar-foreground", "--sidebar", BODY],
  ["--sidebar-accent-foreground", "--sidebar-accent", BODY],
  ["--hms-good", "--card", BODY],
  ["--hms-warn", "--card", BODY],
  ["--hms-crit", "--card", BODY],
  ["--hms-good", "--muted", BODY],
  ["--hms-warn", "--muted", BODY],
  ["--hms-crit", "--muted", BODY],
  ["--hms-band-ink", "--hms-band", BODY],
  ["--hms-band-muted", "--hms-band", BODY],
  ["--ring", "--background", UI],
];

it.each(PAIRS)("%s on %s passes AA", (fg, bg, min) => {
  expect(root[fg], `${fg} is not a hex token`).toMatch(/^#/);
  expect(root[bg], `${bg} is not a hex token`).toMatch(/^#/);
  expect(contrast(root[fg], root[bg])).toBeGreaterThanOrEqual(min);
});

// A tenant's brand recolours primary actions; every brand must stay legible on
// the shared canvas.
it.each(["teal", "blue", "indigo"])("brand %s passes AA", (brand) => {
  const tokens = { ...root, ...tokensOf(`[data-brand-theme="${brand}"]`) };
  expect(contrast(tokens["--primary-foreground"], tokens["--primary"])).toBeGreaterThanOrEqual(
    BODY,
  );
  expect(contrast(tokens["--hms-accent"], tokens["--background"])).toBeGreaterThanOrEqual(BODY);
  expect(contrast(tokens["--hms-accent"], tokens["--hms-accent-dim"])).toBeGreaterThanOrEqual(BODY);
});
