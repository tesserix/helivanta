# App look, slice 1: tokens and typeface — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the all-grey token set with a cool-white canvas, blue-black ink, ink buttons and one ultramarine accent, add IBM Plex Sans as the app face, and ban the one non-token colour escape hatch, so every page changes at once with no component edits.

**Architecture:** All colour lives in `packages/ui/styles.css` behind `!important` overrides of `@tesserix/web`'s theme, with `contrast.test.ts` as the gate; this plan changes values there and nowhere else. The typeface is committed OFL files loaded with `next/font/local` in the root layout, exposed as a CSS variable that Tailwind's `font-sans` reads, with the home page scoping its own Anek to headings. The lint rule is one more `no-restricted-syntax` entry beside the existing design rules.

**Tech Stack:** Next 16 app router, Tailwind v4 `@theme`, Vitest, ESLint flat config, Playwright (run in GitHub Actions only, per AGENTS.md).

**Spec:** `docs/superpowers/specs/2026-11-24-app-look-tokens-design.md` (issue #1834)

## Global Constraints

- Colour tokens are declared only in `packages/ui/styles.css`; every value is opaque six-digit hex with `!important` in the root scope (`token-source.test.ts`, styles.css header comment).
- Every pair in `contrast.test.ts` stays and passes at 4.5:1 body, 3:1 UI; no pair is removed.
- `--hms-good #0f7a4f`, `--hms-warn #a85503`, `--hms-crit #c93b3b`, `--destructive #c93b3b` and their tints do not change.
- The tenant theme enum `teal | blue | indigo` in `packages/api/src/workspace.ts` does not change.
- No build-time font fetch: font files are committed under `apps/shell/app/fonts/` with their OFL text (repo precedent: Anek).
- No new CSP origin; `apps/shell/lib/csp.ts` is not edited.
- Validation runs in GitHub Actions on the branch, never a local build (AGENTS.md). Commit and push after each task; the CI web job runs lint, type-check, test, build and format:check; Playwright runs in the `ci.yml` e2e job.
- Commit messages end on their last substantive line, no attribution trailers (user's global rule).

## Review Focus

1. **A tenant theme on sign-in.** After D2 the submit button is ink under every theme, so a tenant whose accent is dark could lose every visible trace of their brand. Pinned in Task 5: the focused field's ring carries the theme's accent. The story panel's tint is deliberately not asserted, because #1836 turns that panel ink.
2. **Autofilled inputs.** The autofill override paints `var(--background)` behind autofilled text; with the canvas now `#f6f8fb` and cards white, an autofilled field on a white card shows a faint tinted box. Pinned in Task 2: `--background` on `--card` is asserted below 1.1:1 so the difference is invisible, and the comment in styles.css records it.
3. **Chart series after the hue move.** The dashboard's OPD series reads `--hms-accent` and IPD reads `--hms-violet`; a reader with deuteranopia must still tell plum from ultramarine. Pinned in Task 2: `--hms-violet` against `--hms-accent` is asserted at 2:1 or more in lightness-independent terms (the pair's contrast ratio), the one chart pair that shares a hue family.
4. **Font fallback before hydration and on a slow network.** `next/font` with `display: "swap"` shows the system face first; a layout shift on dense tables is the visible cost. Pinned in Task 1: the font is loaded with `display: "swap"` and `adjustFontFallback` left on (the default), and the layout test asserts the variable class is on `<body>`.
5. **Translucent text over the new surfaces.** `contrast.test.ts` already reads every `text-…/NN` utility from source and blends it over the surfaces; the new muted and accent values must still pass there. Covered by Task 2 running the whole file, not a subset.

---

### Task 1: IBM Plex Sans in the root layout, Anek scoped to the home page's headings

**Files:**
- Create: `apps/shell/app/fonts/plex/ibm-plex-sans-latin-{400,500,600,700}-normal.woff2`, `apps/shell/app/fonts/plex/ibm-plex-sans-latin-ext-{400,500,600,700}-normal.woff2`, `apps/shell/app/fonts/plex/OFL.txt`
- Modify: `apps/shell/app/layout.tsx` (the `<body>` element), `apps/shell/app/globals.css` (the `@theme` block and one new rule), `apps/shell/app/page.tsx:19-23` (the `localFont` call) and `:155` (the wrapper class)
- Test: `apps/shell/app/layout.test.tsx`, `apps/shell/app/landing.test.tsx`

**Interfaces:**
- Produces: CSS variable `--font-plex` on `<body>`; Tailwind `font-sans` resolves to it through `@theme { --font-sans: var(--font-plex), ui-sans-serif, system-ui, sans-serif; }`. CSS variable `--font-anek` on the home page wrapper, used only by the rule `.home-slate :is(h1, h2, h3) { font-family: var(--font-anek), var(--font-sans); }`.

- [ ] **Step 1: Fetch the font files and licence**

Run from the repo root:

```bash
mkdir -p apps/shell/app/fonts/plex
for s in latin latin-ext; do for w in 400 500 600 700; do
  curl -fsSL "https://cdn.jsdelivr.net/npm/@fontsource/ibm-plex-sans@5.2.5/files/ibm-plex-sans-${s}-${w}-normal.woff2" \
    -o "apps/shell/app/fonts/plex/ibm-plex-sans-${s}-${w}-normal.woff2"; done; done
curl -fsSL "https://cdn.jsdelivr.net/npm/@fontsource/ibm-plex-sans@5.2.5/LICENSE" -o apps/shell/app/fonts/plex/OFL.txt
ls -la apps/shell/app/fonts/plex
```

Expected: eight `.woff2` files between 10 and 40 KB each and an `OFL.txt` beginning with the IBM Plex copyright line and "SIL Open Font License, Version 1.1".

- [ ] **Step 2: Write the failing layout test**

Add to `apps/shell/app/layout.test.tsx`, after the existing mocks:

```tsx
vi.mock("next/font/local", () => ({
  default: () => ({ className: "font-plex-class", variable: "font-plex-var" }),
}));

it("puts the UI face on the body as a variable", async () => {
  requestHeaders.current = new Headers({ "x-nonce": "n1" });
  const html = renderToStaticMarkup(await RootLayout({ children: null }));
  expect(html).toMatch(/<body[^>]*class="[^"]*font-plex-var[^"]*"/);
});
```

- [ ] **Step 3: Run it to verify it fails**

Run: `pnpm --filter @helivanta/shell exec vitest run app/layout.test.tsx`
Expected: FAIL, the body has no class attribute.

- [ ] **Step 4: Load the font in `apps/shell/app/layout.tsx`**

Add at module level:

```ts
import localFont from "next/font/local";
const plex = localFont({
  src: [
    { path: "./fonts/plex/ibm-plex-sans-latin-400-normal.woff2", weight: "400" },
    { path: "./fonts/plex/ibm-plex-sans-latin-ext-400-normal.woff2", weight: "400" },
    { path: "./fonts/plex/ibm-plex-sans-latin-500-normal.woff2", weight: "500" },
    { path: "./fonts/plex/ibm-plex-sans-latin-ext-500-normal.woff2", weight: "500" },
    { path: "./fonts/plex/ibm-plex-sans-latin-600-normal.woff2", weight: "600" },
    { path: "./fonts/plex/ibm-plex-sans-latin-ext-600-normal.woff2", weight: "600" },
    { path: "./fonts/plex/ibm-plex-sans-latin-700-normal.woff2", weight: "700" },
    { path: "./fonts/plex/ibm-plex-sans-latin-ext-700-normal.woff2", weight: "700" },
  ],
  variable: "--font-plex",
  display: "swap",
});
```

and render `<body className={plex.variable}>`. In `globals.css` add to the existing `@theme` block: `--font-sans: var(--font-plex), ui-sans-serif, system-ui, sans-serif;`. Nothing sets `font-family` on `body` by hand: `@tesserix/web`'s base styles use `font-sans`, which now resolves to Plex.

- [ ] **Step 5: Run the layout test to verify it passes**

Run: `pnpm --filter @helivanta/shell exec vitest run app/layout.test.tsx`
Expected: PASS, four tests.

- [ ] **Step 6: Write the failing home page test**

In `apps/shell/app/landing.test.tsx` change the mock to return both fields and add one assertion to the existing render test (or a new `it`):

```tsx
vi.mock("next/font/local", () => ({
  default: () => ({ className: "font-site", variable: "font-site-var" }),
}));

it("scopes Anek to the home page by variable, not by class", () => {
  const { container } = render(<LandingPage />);
  const wrapper = container.firstElementChild as HTMLElement;
  expect(wrapper.className).toContain("font-site-var");
  expect(wrapper.className).not.toContain("font-site ");
});
```

Make the same mock change in `apps/shell/app/landing-content.test.tsx`.

- [ ] **Step 7: Run it to verify it fails**

Run: `pnpm --filter @helivanta/shell exec vitest run app/landing.test.tsx`
Expected: FAIL, the wrapper class contains `font-site` and not `font-site-var`.

- [ ] **Step 8: Scope Anek in `apps/shell/app/page.tsx` and `globals.css`**

In the `localFont` call add `variable: "--font-anek"`; on the wrapper replace `${anek.className}` with `${anek.variable}`. In `globals.css`, beside the `.home-slate` rule, add:

```css
/* The public page keeps its display face for headlines only; body text is the
   shared UI face from the root layout (#1834 spec D4). */
.home-slate :is(h1, h2, h3) {
  font-family: var(--font-anek), var(--font-sans);
}
```

- [ ] **Step 9: Run the shell tests and lint**

Run: `pnpm --filter @helivanta/shell exec vitest run && pnpm --filter @helivanta/shell lint`
Expected: PASS; no lint errors (the new rule is a plain `font-family`, which `token-source.test.ts` does not police).

- [ ] **Step 10: Commit and push**

```bash
git add apps/shell/app/fonts/plex apps/shell/app/layout.tsx apps/shell/app/layout.test.tsx apps/shell/app/globals.css apps/shell/app/page.tsx apps/shell/app/landing.test.tsx apps/shell/app/landing-content.test.tsx
git commit -m "feat(shell): IBM Plex Sans as the UI face, Anek scoped to home page headings (#1834)"
git push -u origin feat/1834-app-look-tokens
```

Then open the CI run for the push and confirm the web job's build step is green (the `next/font/local` paths are resolved at build time; a typo fails only there).

---

### Task 2: The token values

**Files:**
- Modify: `packages/ui/styles.css` root scope (lines 16–75), the three `[data-brand-theme]` blocks in the app scope (lines 151–171) and under `.login-page` (lines 250–264), `--auth-button` in `.login-content` (line 182)
- Modify: `apps/shell/app/globals.css` `@theme` shadows (the five `rgb(15 23 42 / …)` values)
- Test: `packages/ui/src/contrast.test.ts`

**Interfaces:**
- Produces: the token values in spec D1 and D3, verbatim. Later tasks and the e2e rewrite depend on `--primary #0b1324`, `--hms-accent #2b4ee6`, and the tenant accents `teal #2b4ee6` (default, Task 2 ruling), `blue #1d5da8`, `indigo #5352a6` with dims `#d9eeea`, `#e7effa`, `#eeedf9`.

- [ ] **Step 1: Write the failing tests**

In `packages/ui/src/contrast.test.ts` replace the `is slate` test and add three pairs and one pin:

```ts
it("is cool white, blue-black ink and ultramarine (#1834)", () => {
  expect(root["--background"]).toBe("#f6f8fb");
  expect(root["--foreground"]).toBe("#0b1324");
  expect(root["--primary"]).toBe("#0b1324");
  expect(root["--hms-accent"]).toBe("#2b4ee6");
});

// Primary actions are ink under every tenant theme (#1834 spec D2).
it.each(["teal", "blue", "indigo"])("brand %s leaves primary as ink", (brand) => {
  expect(tokensOf(`[data-brand-theme="${brand}"]`)["--primary"]).toBeUndefined();
});

// Autofill paints --background behind text on a --card field; the two must be
// close enough that the box is invisible (Review Focus 2).
it("canvas and card are near-identical for autofill", () => {
  expect(contrast(root["--background"], root["--card"])).toBeLessThan(1.1);
});

// The one chart pair in the same hue family must still separate (Review Focus 3).
it("plum separates from the accent", () => {
  expect(contrast(root["--hms-violet"], root["--hms-accent"])).toBeGreaterThanOrEqual(1.6);
});
```

Add `["--muted-foreground", "--secondary", BODY]` and `["--hms-accent", "--hms-accent-dim", BODY]` to `PAIRS`.

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui exec vitest run src/contrast.test.ts`
Expected: FAIL on the pin test (`#f8fafc` is not `#f6f8fb`) and on the `brand %s leaves primary as ink` tests (each brand block sets `--primary` today).

- [ ] **Step 3: Set the values in `packages/ui/styles.css`**

Root scope, every value from spec D1 with `!important` kept: `--background #f6f8fb`, `--foreground #0b1324`, `--card-foreground #0b1324`, `--popover-foreground #0b1324`, `--primary #0b1324`, `--secondary #f0f4f8`, `--secondary-foreground #0b1324`, `--muted #f0f4f8`, `--muted-foreground #4e5a6e`, `--accent #e4eaff`, `--accent-foreground #1f3ab8`, `--border #dfe4ec`, `--input #c7cfdc`, `--ring #2b4ee6`, `--sidebar #ffffff`, `--sidebar-foreground #0b1324`, `--sidebar-primary #0b1324`, `--sidebar-accent #e4eaff`, `--sidebar-accent-foreground #1f3ab8`, `--sidebar-border #dfe4ec`, `--sidebar-ring #2b4ee6`, `--sidebar-rail #ffffff`, `--hms-accent #2b4ee6`, `--hms-accent-strong #1f3ab8`, `--hms-accent-dim #e4eaff`, `--hms-violet #6b1a6e`, `--hms-violet-tint #f7e9f9`, `--hms-sky #0e7490`, `--hms-sky-tint #e3f3f8`, `--hms-track #e3e8ef`, `--hms-band #f0f4f8`, `--hms-band-ink #0b1324`, `--hms-band-muted #4e5a6e`, `--hms-band-line #4e5a6e`, `--hms-shadow #0b1324`. Unchanged: `--card`, `--popover`, `--primary-foreground`, `--destructive*`, `--hms-good`, `--hms-warn`, `--hms-crit`, `--hms-rose*`, `--hms-amber*`.

Tenant blocks, both scopes: delete the `--primary` and `--primary-foreground` lines; set `teal` (the default) to the platform accent `#2b4ee6 / #1f3ab8 / #e4eaff`, keep `blue` `#1d5da8 / #17497f / #e7effa` and `indigo` `#5352a6 / #403f83 / #eeedf9`.

`.login-content`: `--auth-button: var(--primary);` and `--auth-button-shadow: 0 2px 4px color-mix(in srgb, var(--primary) 20%, transparent);`.

Update the header comment's first line to "Helivanta color tokens — cool white, blue-black ink, one ultramarine accent (#1834); light only".

In `apps/shell/app/globals.css` change the five shadow colours from `rgb(15 23 42 / …)` to `rgb(11 19 36 / …)`.

- [ ] **Step 4: Run the whole contrast file and the token-source test**

Run: `pnpm --filter @helivanta/ui exec vitest run src/contrast.test.ts src/token-source.test.ts`
Expected: PASS, every pair including the translucent-text blends read from source.

- [ ] **Step 5: Run the UI and shell suites**

Run: `pnpm --filter @helivanta/ui test && pnpm --filter @helivanta/shell exec vitest run`
Expected: PASS. If a snapshot or a test pins an old hex (search `#334155`, `#f8fafc`, `#0f172a` under `apps/ packages/ --include=*.test.*`), update it to the new value in the same commit and name the file in the commit body.

- [ ] **Step 6: Commit and push**

```bash
git add packages/ui/styles.css packages/ui/src/contrast.test.ts apps/shell/app/globals.css
git commit -m "feat(ui): cool-white canvas, ink primary, ultramarine accent; tenant themes recolour the accent only (#1834)"
git push
```

---

### Task 3: Lint ban on `Input`'s `valid` prop

**Files:**
- Modify: `packages/config/eslint.config.mjs` (the `DESIGN_SYNTAX` array, lines 50–61)
- Test: `packages/config/eslint.config.test.mjs` (inside `describe("design rules")`)

**Interfaces:**
- Produces: a design-rule message `"Input's valid prop paints a non-token green beside the clinical good colour; show success with text or a status badge (#1834 spec D5)."`

- [ ] **Step 1: Write the failing test**

```js
it("rejects Input's valid prop", async () => {
  expect(await messages(`export const A = () => <Input valid />;`)).toEqual([
    expect.stringMatching(/valid prop paints a non-token green/),
  ]);
  expect(await messages(`export const A = () => <Input valid={ok} />;`)).toEqual([
    expect.stringMatching(/valid prop paints a non-token green/),
  ]);
  expect(await messages(`export const A = () => <Input aria-invalid />;`)).toEqual([]);
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm --filter @helivanta/config test`
Expected: FAIL, received `[]` for the first two snippets.

- [ ] **Step 3: Add the rule to `DESIGN_SYNTAX`**

```js
{
  selector: "JSXOpeningElement[name.name='Input'] > JSXAttribute[name.name='valid']",
  message:
    "Input's valid prop paints a non-token green beside the clinical good colour; show success with text or a status badge (#1834 spec D5).",
},
```

- [ ] **Step 4: Run the config tests and the repo lint**

Run: `pnpm --filter @helivanta/config test && pnpm turbo lint`
Expected: PASS; no existing file uses the prop (checked 2026-11-24 with `grep -rnE "<Input\b[^>]*\bvalid"`), so lint stays green.

- [ ] **Step 5: Commit and push**

```bash
git add packages/config/eslint.config.mjs packages/config/eslint.config.test.mjs
git commit -m "chore(lint): ban Input's valid prop, the one non-token colour in @tesserix/web (#1834)"
git push
```

---

### Task 4: Docs that name the palette

**Files:**
- Modify: `CLAUDE.md:84`, `docs/standards/frontend.md:474-490`, `docs/superpowers/specs/2026-10-22-one-look-design.md` (the superseded note at the top), `docs/superpowers/specs/2026-10-28-shared-web-token-roles.md` (header note)

- [ ] **Step 1: Update the rule text**

`CLAUDE.md:84`: "light only, one slate palette" becomes "light only, one palette (cool white, blue-black ink, one ultramarine accent, #1834)". `frontend.md` §7 heading "One look: light, slate, modern enterprise (#1255)" becomes "One look: light, one palette, modern enterprise (#1255, #1834)" and the "One palette" bullet names the three values. Add one line under the one-look spec's existing superseded note: "Palette superseded again on 2026-11-24 by `2026-11-24-app-look-tokens-design.md` (#1834)." Same one-line note at the top of the shared-web-token-roles spec.

- [ ] **Step 2: Format check**

Run: `pnpm turbo format:check --filter=@helivanta/shell` (docs are covered by the root prettier through the workspace that owns them; if the check does not include `docs/`, run `pnpm exec prettier --check CLAUDE.md docs/standards/frontend.md`).
Expected: clean.

- [ ] **Step 3: Commit and push**

```bash
git add CLAUDE.md docs/standards/frontend.md docs/superpowers/specs/2026-10-22-one-look-design.md docs/superpowers/specs/2026-10-28-shared-web-token-roles.md
git commit -m "docs: the one look is cool white with an ultramarine accent (#1834)"
git push
```

---

### Task 5: Sign-in appearance e2e for an ink button and a visible tenant accent

**Files:**
- Modify: `e2e/tests/login-appearance.spec.ts`

**Interfaces:**
- Consumes: Task 2's values. Button `rgb(11, 19, 36)` under every theme. Accents: teal `rgb(43, 78, 230)` (the default carries the platform accent, Task 2 ruling), blue `rgb(29, 93, 168)`, indigo `rgb(83, 82, 166)`.

- [ ] **Step 1: Rewrite the palette table and the assertions**

Replace the `palettes` constant with:

```ts
const INK = "rgb(11, 19, 36)";
const palettes = [
  { theme: "teal", accent: "rgb(43, 78, 230)" },
  { theme: "blue", accent: "rgb(29, 93, 168)" },
  { theme: "indigo", accent: "rgb(83, 82, 166)" },
] as const;
```

Rename the test to `` `${theme} ${layout} login has an ink primary action and shows the tenant accent` ``. Replace both `toHaveCSS("background-color", background)` assertions (idle and busy) with `INK`. After the existing `action.focus()` outline check add:

```ts
// The tenant brand is in the focus ring, not the button (#1834 spec D2).
await loginName.focus();
const ring = await loginName.evaluate((el) => {
  const s = getComputedStyle(el);
  return `${s.outlineColor} ${s.boxShadow} ${s.borderColor}`;
});
expect(ring, "focused field carries the tenant accent").toContain(accent);
```

The story panel's tint is not asserted: #1836 turns that panel ink, and this spec must survive that change unedited.

- [ ] **Step 2: Lint and type-check the e2e workspace**

Run: `pnpm --filter e2e lint && pnpm --filter e2e type-check`
Expected: clean.

- [ ] **Step 3: Prove the assertions bite, then that they pass**

Commit and push; open the CI run. If Task 2 is already on the branch the e2e job must be green on this spec. To prove the new assertions can fail, run the e2e job once on a temporary commit that sets `INK` to the old `rgb(51, 65, 85)`, confirm the six cases fail on the first `toHaveCSS`, then drop that commit (`git reset --hard HEAD~1`) before continuing. Record the failing run's URL in the PR body.

- [ ] **Step 4: Commit and push**

```bash
git add e2e/tests/login-appearance.spec.ts
git commit -m "test(e2e): sign-in button is ink under every tenant theme; the accent shows in the focus ring and story panel (#1834)"
git push
```

---

### Task 6: Hosted verification and the pull request

**Files:**
- None in the repo beyond the PR body.

- [ ] **Step 1: Full CI green**

Confirm on the branch's latest run: web job (lint, type-check, test, build, format:check) and the e2e job including `csp.spec.ts`, `smoke.spec.ts`, `navigation.spec.ts`, `login-appearance.spec.ts`. Inspect the logs, not only the badge (AGENTS.md).

- [ ] **Step 2: Look at it**

On the hosted preview or the dev deployment of the branch, capture and attach to the PR: the dashboard Overview and Patient activity tabs (plum IPD series beside ultramarine OPD), the pharmacy counter sale and stock pages, sign-in under the platform brand and under `teal`, `blue` and `indigo` (use the branding route stub from the e2e spec if no tenant has those themes), and the public home page's landing boards with the moved chart hues. Note anything that reads wrong; a wrong read is a spec amendment, not a silent fix.

- [ ] **Step 3: Open the PR**

Title: `feat(ui): app look slice 1, one palette and a UI typeface`. Body: `Closes #1834`, the spec path, the contracts consulted (none: no routes, events or schemas change), the screenshots, the failing-run URL from Task 5, and the two follow-ups it unlocks (#1836 chrome, #1835 mark). No attribution lines. Hand the merge to the user once checks are green.
