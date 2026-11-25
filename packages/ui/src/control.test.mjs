// @vitest-environment node
import { expect, it } from "vitest";
import { SURFACE_PATTERNS } from "@helivanta/config/eslint";
import { controlClass, tableStyles } from "./control";

// #1261 D1: one control style. It must not read as a card to the surface rule
// (a bordered rounded-lg box is one), and it rings in the design system's
// --ring, never the tenant accent.
it("is a control, not a card, and rings like the design system", () => {
  for (const pattern of SURFACE_PATTERNS) expect(controlClass).not.toMatch(pattern);
  const classes = controlClass.split(/\s+/);
  for (const name of ["rounded-md", "min-h-11", "border-input", "focus-visible:ring-ring"])
    expect(classes).toContain(name);
  expect(controlClass).not.toMatch(/hms-accent/);
});

// #1261 D2: comfortable rows, a header that stays put, numbers that line up.
it("gives tables 44px rows, a sticky header and right-aligned numbers", () => {
  expect(tableStyles.row.split(/\s+/)).toContain("h-11");
  expect(tableStyles.head.split(/\s+/)).toEqual(expect.arrayContaining(["sticky", "top-0"]));
  expect(tableStyles.head).not.toMatch(/uppercase/);
  expect(tableStyles.number.split(/\s+/)).toEqual(
    expect.arrayContaining(["text-right", "tabular-nums"]),
  );
});
