# App look, slice 2: flat full-height rail — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the three floating chrome cards with a flat full-height ink rail, a header row and a page-panel column on the canvas, and give split sign-in pages the same ink panel.

**Architecture:** The chrome lives in one file, `packages/ui/src/hms-shell.tsx`; this plan changes its class strings and one marker, and turns the eight sidebar tokens in `packages/ui/styles.css` ink. Only the shell reads those tokens (checked 2026-11-24), so nothing else repaints. The page panel and header move to canvas tokens. The sign-in ink panel is a restyle of `LoginSurface`'s existing split aside in `packages/ui/src/branding.tsx`.

**Tech Stack:** React 19, Next 16, Tailwind v4, Vitest with `@helivanta/api/testing`, Playwright in GitHub Actions only (AGENTS.md).

**Spec:** `docs/superpowers/specs/2026-11-24-app-chrome-flat-rail-design.md` (issue #1836)

## Global Constraints

- Colour tokens are declared only in `packages/ui/styles.css`, opaque hex with `!important` in the root scope; every pair `contrast.test.ts` lists stays and passes (4.5:1 body, 3:1 UI).
- No tenant accent is ever drawn on ink: the rail and the phone menu use fixed `--sidebar-*` tokens only.
- The stored-chrome widths stay: open rail 208 px (`13rem`), page panel 224 px (`md:w-56`); `navigation.spec.ts` asserts `rail=208px panel=224px` from the first paint.
- The page panel never collapses and has no header (#1286 D4, D5).
- Accessible names do not change: `navigation` "Zones", link "Helivanta home", zone link names, `aria-current="page"`; `smoke.spec.ts` selectors stay valid.
- Task screens on sign-in stay one centred card (#1767 D1, D2).
- Stage files by explicit path; never `git add -A` or `git commit -a` (untracked drafts live under `docs/superpowers/`).
- Commit messages end on their last substantive line; no attribution trailers.

## Decided before planning (rulings carried from the spec review)

- **Platform sign-in stays centred.** The spec's D5 flips `DEFAULT_BRANDING.login_layout` and the Go default to `split`. That contradicts #1767's decision, pinned in `backend/internal/modules/workspace/module_test.go` ("a workspace that never chose a layout gets the single card"). This plan honours #1767: tenants who choose `split` get the ink panel; the platform default and the backend are untouched. Task 5 amends the spec. Cost if wrong: one default in two files and one Go assertion, in a later PR.
- **The current-zone marker on ink is fixed, not tenant-coloured,** in the rail and in the phone menu. Today both paint `bg-(--hms-accent-dim) text-(--hms-accent)`, which the spec's anti-goal forbids on ink. The spec's D6 said the phone menu needs no markup change; it does.
- **The header shows the tenant logo only when one is set.** `BrandLogo` falls back to a heart-pulse tile; with the platform mark already in the rail head, the fallback in the header would show the same icon twice.

## Review Focus

1. **A tenant with a dark or light logo on the header row.** The logo now sits on the canvas, as designed; a logo that fails to load must still fall back to nothing in the header, not to the heart tile. Pinned in Task 3.
2. **Keyboard focus on the ink rail.** The focus ring was `ring-(--hms-accent)`; on ink a tenant accent can be invisible. Pinned in Task 2: rail and phone-menu focus rings use `--sidebar-ring`, asserted by class and by contrast pair.
3. **The collapsed rail's labels.** `text-[11px]` labels at `/70` on ink are the smallest text in the app. Pinned in Task 1 by the existing `/70` blend assertion, which must pass on the new rail.
4. **A zone with one page** has no panel; the content then starts at the rail edge. Pinned in Task 4's e2e: the dashboard's content is inset 24 px from the header's left edge.
5. **A long hospital name with a logo** in the header row wraps rather than pushing the account menu off screen at 768 px. Pinned in Task 3 by a render test with a 100-character name asserting the header row keeps `flex-wrap`.

---

### Task 1: Ink rail tokens

**Files:**

- Modify: `packages/ui/styles.css` (root scope, the eight `--sidebar*` tokens)
- Test: `packages/ui/src/contrast.test.ts`

**Interfaces:**

- Produces: `--sidebar #0b1324`, `--sidebar-rail #0b1324`, `--sidebar-foreground #f6f8fb`, `--sidebar-primary #7aa0ff`, `--sidebar-primary-foreground #0b1324`, `--sidebar-accent #1a2645`, `--sidebar-accent-foreground #ffffff`, `--sidebar-border #1a2645`, `--sidebar-ring #7aa0ff`. Tasks 2 and 3 use them by these names only.

- [ ] **Step 1: Write the failing tests** in `contrast.test.ts`

```ts
it("the rail is ink (#1836)", () => {
  expect(root["--sidebar-rail"]).toBe("#0b1324");
  expect(root["--sidebar"]).toBe("#0b1324");
});
```

Add to `PAIRS`: `["--sidebar-foreground", "--sidebar-rail", BODY]`, `["--sidebar-accent-foreground", "--sidebar-accent", BODY]`, `["--sidebar-primary", "--sidebar-rail", UI]` (the current-zone bar), `["--sidebar-ring", "--sidebar-rail", UI]`, `["--sidebar-foreground", "--sidebar-accent", BODY]` (hover). Add one blend row beside the existing `rail and panel links` row: `rail section labels` at `0.6`.

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm --filter @helivanta/ui exec vitest run src/contrast.test.ts`
Expected: FAIL on "the rail is ink" (received `#ffffff`). Some new pairs may already pass on the old light values; the pin is the RED, and the pairs are what keep the ink values honest afterwards.

- [ ] **Step 3: Set the nine values above in the root scope.** Keep `!important`. Add a one-line comment above them: the rail is ink from #1836; only `hms-shell.tsx` reads these tokens.

- [ ] **Step 4: Run the contrast and token-source tests**

Run: `pnpm --filter @helivanta/ui exec vitest run src/contrast.test.ts src/token-source.test.ts`
Expected: PASS, including the translucent-text blends read from source.

- [ ] **Step 5: Commit** `packages/ui/styles.css packages/ui/src/contrast.test.ts` — `feat(ui): the rail tokens are ink (#1836)`

---

### Task 2: Flat full-height rail and an ink phone menu

**Files:**

- Modify: `packages/ui/src/hms-shell.tsx` (`ZoneLink` at the `active ?` branch; `PhoneMenu`'s `zoneLink` and page links; the rail `<aside className="hms-rail …">`; the rail head `GuardedLink`; the collapse button)
- Test: `packages/ui/src/hms-shell.test.tsx`

**Test pattern (Tasks 2 and 3):** this file renders `renderWithProviders(<HmsShell>content</HmsShell>)`, sets the route with `pathname.current = "/pharmacy"`, and supplies tenant branding by stubbing `fetch` for `/api/v1/workspace/branding` (see the existing test around "Role at Yes Hospital"). In the snippets below, `renderShell({...})` stands for: stub that URL to return `{ ...DEFAULT_BRANDING, ...overrides }`, set `pathname.current` from `path` (default `/dashboard`), grant pharmacy pages with the file's `grantPharmacyPages()` when the path is under `/pharmacy`, then render and `await` the branding to load (`await screen.findByTestId("tenant-name")` showing the name). Write it once as a local helper in the new `describe` block.

**Interfaces:**

- Consumes: Task 1's token names.
- Produces: the class `hms-current` on every current-zone link (rail and phone menu), whose `::before` draws the 3 px bar. Task 4's e2e locates the marker by `[aria-current="page"].hms-current`.

- [ ] **Step 1: Write the failing tests**

```tsx
it("draws the rail flat and full height from md", () => {
  renderShell(); // the file's existing helper for a signed-in shell
  const rail = document.querySelector("aside.hms-rail")!;
  for (const card of [
    "md:m-2",
    "md:rounded-xl",
    "md:shadow-sm",
    "md:self-start",
    "md:border",
  ])
    expect(rail.className).not.toContain(card);
  expect(rail.className).toContain("md:h-full");
});

it("marks the current zone with the fixed rail marker, never the tenant accent", () => {
  renderShell({ theme: "indigo" });
  const current = screen
    .getByRole("navigation", { name: "Zones" })
    .querySelector('[aria-current="page"]')!;
  expect(current.className).toContain("hms-current");
  expect(current.className).not.toMatch(/hms-accent/);
});

it("puts the platform mark alone in the rail head", () => {
  renderShell({ logo_url: "https://example.test/logo.png" });
  const home = screen.getByRole("link", { name: "Helivanta home" });
  expect(within(home).queryByRole("img", { name: /logo/ })).toBeNull();
});
```

and one for the phone menu: open it with the "Open menu" button and assert its current zone link carries `hms-current` and no `hms-accent` class. Use the file's existing render helper and its existing `branding` override shape; if the helper takes a full `Branding`, pass `{ ...DEFAULT_BRANDING, theme: "indigo" }`.

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui exec vitest run src/hms-shell.test.tsx`
Expected: four FAILs: card classes present, `hms-accent` in the current link, the tenant `img` inside the home link.

- [ ] **Step 3: Implement in `hms-shell.tsx`**
  - Rail `<aside>`: drop `md:m-2 md:max-h-[calc(100dvh-1rem)] md:self-start md:rounded-xl md:border md:shadow-sm` from both branches; add `md:h-full`; keep `border-r` below `md` for the drawer; keep `hms-rail`, `bg-(--sidebar-rail)`, the widths.
  - Rail head: replace the `bg-primary` tile and `BrandLogo` with `<HeartPulse className="size-6 text-sidebar-foreground" aria-hidden="true" />` (a `lucide-react` import the file adds); keep the link's `aria-label`, `href`, and the open-state "Helivanta" text. #1835 swaps the icon for the generated mark later.
  - `ZoneLink` and `PhoneMenu`'s `zoneLink`, current state: `hms-current bg-sidebar-accent text-sidebar-accent-foreground`; resting state unchanged (`text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground`).
  - Collapse button: `focus-visible:ring-(--hms-accent)` becomes `focus-visible:ring-(--sidebar-ring)`.
  - `packages/ui/styles.css`, beside `.hms-sidebar-border`: `.hms-current { position: relative; }` and `.hms-current::before { content: ""; position: absolute; left: -0.5rem; top: 0.75rem; bottom: 0.75rem; width: 3px; border-radius: 0 2px 2px 0; background: var(--sidebar-primary); }`. The offset puts the bar on the rail's edge from inside the link's padding; adjust only the `left` value if the rendered bar is not flush, and say so in the commit body.

- [ ] **Step 4: Run the UI suite**

Run: `pnpm --filter @helivanta/ui test`
Expected: PASS; any existing test that pinned `bg-(--hms-accent-dim)` on the current zone is updated to `hms-current` in this commit and named in its body.

- [ ] **Step 5: Commit** `packages/ui/src/hms-shell.tsx packages/ui/src/hms-shell.test.tsx packages/ui/styles.css` — `feat(ui): flat full-height ink rail with a fixed current-zone marker (#1836)`

---

### Task 3: Header row and page-panel column on the canvas

**Files:**

- Modify: `packages/ui/src/hms-shell.tsx` (the column wrapper `<div className="flex min-w-0 flex-1 flex-col md:gap-2 md:py-2 md:pr-2">`, the `<header>`, the page-panel `<aside>` and its links and section headings, `<main>`'s `md:pt-0`)
- Test: `packages/ui/src/hms-shell.test.tsx`

**Interfaces:**

- Consumes: `hms-current` from Task 2.
- Produces: the header row contains `BrandLogo compact` before `data-testid="tenant-name"` only when `branding.logo_url` is non-empty.

- [ ] **Step 1: Write the failing tests**

```tsx
it("draws the header as a row and the page panel as a column", () => {
  renderShell({ path: "/pharmacy" }); // a zone with a page panel
  const header = screen.getByRole("banner");
  expect(header.className).not.toMatch(
    /md:rounded-xl|md:shadow-sm|md:border(\s|$)/,
  );
  expect(header.className).toContain("border-b");
  const panel = document.querySelector(".hms-page-panel")!;
  expect(panel.className).not.toMatch(
    /md:rounded-xl|md:shadow-sm|md:self-start|sidebar/,
  );
  expect(panel.className).toContain("md:w-56");
  const current = within(panel).getByRole("link", { current: "page" });
  expect(current.className).toContain("hms-page-current");
  expect(current.className).not.toMatch(/sidebar/);
});

it("shows the tenant logo in the header only when one is set", () => {
  const { unmount } = renderShell({ logo_url: "" });
  expect(within(screen.getByRole("banner")).queryByRole("img")).toBeNull();
  unmount();
  renderShell({
    logo_url: "https://example.test/logo.png",
    display_name: "YES HOSPITAL",
  });
  const logo = within(screen.getByRole("banner")).getByRole("img", {
    name: "YES HOSPITAL logo",
  });
  fireEvent.error(logo);
  expect(within(screen.getByRole("banner")).queryByRole("img")).toBeNull();
});

it("keeps a long hospital name from pushing the account menu off the row", () => {
  renderShell({
    display_name: "H".repeat(100),
    logo_url: "https://example.test/logo.png",
  });
  expect(screen.getByRole("banner").firstElementChild!.className).toContain(
    "flex-wrap",
  );
});
```

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui exec vitest run src/hms-shell.test.tsx`
Expected: FAIL on the header and panel card classes, on `hms-page-current`, and on the logo in the header.

- [ ] **Step 3: Implement**
  - Column wrapper: drop `md:gap-2 md:py-2 md:pr-2`.
  - `<header>`: `z-30 shrink-0 border-b border-border bg-background`; drop `hms-sidebar-border md:rounded-xl md:border md:bg-(--sidebar-rail) md:shadow-sm`. In its first group, before the tenant-name span, render `{branding.logo_url && <HeaderLogo branding={branding} />}` where `HeaderLogo` is a small function in this file that renders `BrandLogo compact` and returns `null` after the image's `onError` (a `useState` flag, the same pattern `BrandLogo` uses). Do not change `BrandLogo` itself: its heart fallback still serves the sign-in page.
  - Page panel `<aside>`: `hms-page-panel hidden shrink-0 flex-col overflow-hidden border-r border-border md:static md:flex md:h-full md:w-56`; drop the sidebar-border, sidebar-rail background, margin, radius, shadow, self-start and max-h classes. Links: current `hms-page-current text-foreground`, resting `text-muted-foreground hover:bg-accent hover:text-foreground`; section headings `text-muted-foreground`.
  - `packages/ui/styles.css`: `.hms-page-current` and its `::before`, the same geometry as `.hms-current` but `background: var(--hms-accent)` (tenant-themed on purpose, spec D3).
  - `<main>`: drop `md:pt-0` so content keeps its 24 px top padding now that the header has no card gap above the content.

- [ ] **Step 4: Run the UI suite and the shell suite**

Run: `pnpm --filter @helivanta/ui test && pnpm --filter @helivanta/shell exec vitest run`
Expected: PASS.

- [ ] **Step 5: Commit** the two `packages/ui` files — `feat(ui): header is a row and the page panel a column on the canvas (#1836)`

---

### Task 4: Replace the #1286 geometry tests

**Files:**

- Modify: `e2e/tests/navigation.spec.ts` (the tests at "the rail and the page panel are only as tall as their items" and "the header card runs from the rail across the panel, above it")

**Interfaces:**

- Consumes: `aside.hms-rail`, `aside.hms-page-panel`, `role=banner`, `.hms-current` from Tasks 2 and 3.

- [ ] **Step 1: Rewrite the two tests** (same login and PharmaConnect navigation as today, 1280×800):

```ts
// #1836 D1: the rail is a full-height ink column from the window's top-left.
test("the rail spans the window height from the top edge", async ({ page }) => {
  // ...login, open PharmaConnect as before
  const rail = (await page.locator("aside.hms-rail").boundingBox())!;
  expect(rail.x, "rail starts at the left edge").toBeLessThan(1);
  expect(rail.y, "rail starts at the top edge").toBeLessThan(1);
  expect(rail.height, "rail spans the window").toBeGreaterThan(799);
  await expect(page.locator("aside.hms-rail")).toHaveCSS(
    "background-color",
    "rgb(11, 19, 36)",
  );
  await expect(
    page
      .getByRole("navigation", { name: "Zones" })
      .locator('[aria-current="page"].hms-current'),
  ).toHaveCount(1);
});

// #1836 D2, D3: the header row starts at the window top beside the rail; the
// panel is a column below it that reaches the window bottom.
test("the header row and the page panel meet the rail edge to edge", async ({
  page,
}) => {
  // ...login, open PharmaConnect as before
  const rail = (await page.locator("aside.hms-rail").boundingBox())!;
  const header = (await page.getByRole("banner").boundingBox())!;
  const panel = (await page.locator("aside.hms-page-panel").boundingBox())!;
  expect(header.y, "header at the window top").toBeLessThan(1);
  expect(
    Math.abs(header.x - (rail.x + rail.width)),
    "header starts at the rail edge",
  ).toBeLessThan(1);
  expect(
    Math.abs(panel.x - (rail.x + rail.width)),
    "panel starts at the rail edge",
  ).toBeLessThan(1);
  expect(panel.y, "panel below the header").toBeGreaterThanOrEqual(
    header.y + header.height - 1,
  );
  expect(
    panel.y + panel.height,
    "panel reaches the window bottom",
  ).toBeGreaterThan(799);
});
```

Keep "the stored chrome is painted from the first frame" and "content lines up with the header on a page without a panel" unchanged; they must pass as they are.

- [ ] **Step 2: Lint and type-check**

Run: `pnpm --filter e2e lint && pnpm --filter e2e type-check`
Expected: clean.

- [ ] **Step 3: Commit and push the e2e change together with Tasks 1–3** (CI is the only place Playwright runs). Open a draft PR `feat(ui): app look slice 2, flat full-height chrome`, `Closes #1836`.

- [ ] **Step 4: Prove the new tests bite.** If Tasks 1–3 are already on the branch, the RED proof is a commit that reverts only `packages/ui/src/hms-shell.tsx` to `origin/main` and keeps the new tests; push, confirm both new tests fail in the e2e job on rail height/background and header position, then restore the file in the next commit. Ledger the failing run URL.

- [ ] **Step 5: Wait for green** on the restore commit: web, e2e (including `dashboard-phone`, `login-appearance`, `smoke`, the stored-chrome and content-alignment tests), and Go jobs.

---

### Task 5: Sign-in ink panel and the spec amendment

**Files:**

- Modify: `packages/ui/src/branding.tsx` (`LoginSurface`: the `<header>` and the `login-story` aside), `packages/ui/styles.css` (`.login-story`), `docs/superpowers/specs/2026-11-24-app-chrome-flat-rail-design.md` (D5)
- Test: `packages/ui/src/branding.test.tsx`

**Interfaces:**

- Consumes: `--sidebar-rail`, `--sidebar-foreground` from Task 1.

- [ ] **Step 1: Write the failing tests**

```tsx
it("puts the brand in the ink panel on a split entry page, once", () => {
  const { container } = render(
    <LoginSurface
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        welcome_text: "Welcome to our care team.",
        login_layout: "split",
      }}
      entry
    >
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  const panel = container.querySelector("aside.login-story")!;
  expect(
    within(panel as HTMLElement).getByText("YES HOSPITAL"),
  ).toBeInTheDocument();
  expect(
    within(panel as HTMLElement).getByText("Welcome to our care team."),
  ).toBeInTheDocument();
  expect(container.querySelector("header")!.className).toContain("lg:hidden");
  expect(panel.textContent).not.toMatch(/Care teams|Laboratory/); // the illustration card is gone
});

it("keeps the platform's centred entry page without a panel", () => {
  const { container } = render(
    <LoginSurface branding={DEFAULT_BRANDING} entry homeHref="/">
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(container.querySelector("aside.login-story")).toBeNull();
  expect(container.querySelector("header")!.className).not.toContain(
    "lg:hidden",
  );
});
```

The existing tests stay: split welcome content, task screen as the card alone, home link only when given a home, no eyebrow or accent headline.

- [ ] **Step 2: Run to verify they fail**

Run: `pnpm --filter @helivanta/ui exec vitest run src/branding.test.tsx`
Expected: FAIL: the name is in the header not the panel, the header has no `lg:hidden`, the illustration card text is present.

- [ ] **Step 3: Implement**
  - When `story` is true: the `<header>` gets `lg:hidden`; the outer grid becomes `lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]` with no horizontal padding at `lg`, and the aside is full height (`lg:min-h-svh`), no radius.
  - The aside keeps `login-story` and its `hidden … lg:flex` behaviour, and holds, top to bottom: `BrandMark` (the home `Link` around it when `homeHref` is set, as the header does), the existing headline `h2` and `welcome_text` paragraph, and at the foot `display_name` in muted rail text. Remove the illustration card (the bordered `div` with the three icons) and the closing "Built around your people" line.
  - `.login-story` in `styles.css`: `background: var(--sidebar-rail); color: var(--sidebar-foreground);` replacing the gradient; muted text inside uses `color-mix(in srgb, var(--sidebar-foreground) 70%, transparent)` via a `.login-story .text-muted-foreground` rule, so no new token is needed. `BrandLogo`'s heart fallback tile is `bg-primary` (ink) and would vanish on ink; inside `.login-story` the tile gets `background: var(--sidebar-accent)` from the same stylesheet rule.
  - Spec D5: replace the paragraph that changes `DEFAULT_BRANDING.login_layout` and the Go default with: "The platform brand keeps the centred layout (#1767: a workspace that never chose a layout gets the single card, pinned in `module_test.go`). Tenants who choose `split` get the ink panel. Changing the platform default is a separate decision." Remove the matching Verification lines about `workspace.ts`, the Go default and `contractdocs`.

- [ ] **Step 4: Run the UI suite and the shell suite**

Run: `pnpm --filter @helivanta/ui test && pnpm --filter @helivanta/shell exec vitest run`
Expected: PASS, including the branding settings page's live preview, which renders `LoginSurface entry`.

- [ ] **Step 5: Commit and push** `packages/ui/src/branding.tsx packages/ui/src/branding.test.tsx packages/ui/styles.css docs/superpowers/specs/2026-11-24-app-chrome-flat-rail-design.md` — `feat(ui): split sign-in pages get the ink panel (#1836)`. `login-appearance.spec.ts` must stay green unchanged: it pins the ink button and the focus ring, not the panel.

---

### Task 6: Floating-rail spec note, hosted check, merge

**Files:**

- Modify: `docs/superpowers/specs/2026-10-23-floating-rail-design.md` (one note under the title)

- [ ] **Step 1: Add the note** under the title: "Amended 2026-11-24 by `2026-11-24-app-chrome-flat-rail-design.md` (#1836): D1 and D6 reversed; D2 to D5 stand." Prettier-check it.

- [ ] **Step 2: Commit and push** that file alone — `docs: the floating rail is amended by the flat chrome (#1836)`.

- [ ] **Step 3: Full CI green** on the branch head: web, e2e, Go jobs. Read the e2e summary line, not only the badge.

- [ ] **Step 4: Whole-branch review**, then the PR body: what changed, the three rulings above, the RED-proof run URL, and the follow-ups (#1835 swaps the rail icon for the mark; slice 3 is #1256–#1263).
