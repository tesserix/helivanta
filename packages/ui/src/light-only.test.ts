// @vitest-environment node
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

// The app is light only (#1255, spec D1). Nothing stamps data-theme="dark" any
// more, so a dark token block left behind would be dead code that a future
// stamp could silently revive.
const css = readFileSync(fileURLToPath(new URL("../styles.css", import.meta.url)), "utf8");

it("defines no dark theme", () => {
  expect(css).not.toMatch(/data-theme=["']?dark/);
});
