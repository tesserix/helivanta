// @vitest-environment node
import { globSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

// Colour tokens are declared once, in packages/ui/styles.css, where
// contrast.test.ts asserts them (#542 web slice, spec D4). A page-local palette
// is a copy that drifts and that no contrast check reads, so both ways of
// creating one fail here: declaring colours in another stylesheet, and reading
// a custom property nothing declares.
const root = fileURLToPath(new URL("../../../", import.meta.url));
const read = (path: string) => readFileSync(path, "utf8");
// The shell is the app that imports @tesserix/web and Tailwind into one stylesheet.
const require = createRequire(`${root}apps/shell/package.json`);

const STYLES = "packages/ui/styles.css";
const stylesheets = globSync("{apps,packages}/**/*.css", {
  cwd: root,
  exclude: (path) => /node_modules|\.next|dist/.test(path),
});
const sources = globSync("{apps,packages}/**/*.{ts,tsx}", {
  cwd: root,
  exclude: (path) => /node_modules|\.next|dist|\.test\.|\.d\.ts$/.test(path),
});

const declaredIn = (css: string) => [...css.matchAll(/(--[\w-]+)\s*:/g)].map((m) => m[1]);

const declared = new Set([
  ...stylesheets.flatMap((file) => declaredIn(read(`${root}${file}`))),
  // The design system's own tokens and Tailwind's theme are read from the
  // installed packages, not copied, so an upgrade cannot leave this list stale.
  ...declaredIn(read(require.resolve("@tesserix/web/styles"))),
  ...declaredIn(read(require.resolve("tailwindcss/theme.css"))),
  // Components set local properties such as --story on an element's style.
  ...sources.flatMap((file) =>
    [...read(`${root}${file}`).matchAll(/["'](--[\w-]+)["']\s*:/g)].map((m) => m[1]),
  ),
]);

it("finds the files it checks", () => {
  expect(stylesheets).toContain(STYLES);
  expect(stylesheets).toContain("apps/shell/app/globals.css");
  expect(sources).toContain("apps/shell/app/page.tsx");
  expect(declared).toContain("--background");
  expect(declared).toContain("--radius-lg");
});

it("reads only declared custom properties", () => {
  const undeclared = sources.flatMap((file) =>
    [...read(`${root}${file}`).matchAll(/(?:\(|var\()(--[\w-]+)[),]/g)]
      .map((m) => m[1])
      // Radix sets its --radix-* sizes on the element at runtime.
      .filter((name) => !declared.has(name) && !name.startsWith("--radix-"))
      .map((name) => `${file}: ${name}`),
  );
  expect([...new Set(undeclared)]).toEqual([]);
});

// A whole value that is one colour. Composites such as the --shadow-* scale are
// shape tokens and stay with the app's @theme block (#1255 spec D3).
const COLOUR_DECLARATION =
  /(--[\w-]+)\s*:\s*(?:#[0-9a-f]{3,8}|(?:rgba?|hsla?|oklch|oklab|lab|lch)\([^)]*\))\s*(?:!important\s*)?;/gi;

it(`declares colour tokens only in ${STYLES}`, () => {
  const elsewhere = stylesheets
    .filter((file) => file !== STYLES)
    .flatMap((file) =>
      [...read(`${root}${file}`).matchAll(COLOUR_DECLARATION)].map((m) => `${file}: ${m[1]}`),
    );
  expect(elsewhere).toEqual([]);
});
