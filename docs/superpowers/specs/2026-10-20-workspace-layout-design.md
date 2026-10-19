# One workspace layout owns the chrome

Resolves #1091 and #1092. Follow-on from #933 / ADR-0010
(`docs/superpowers/specs/2026-10-13-one-next-app-design.md`, "What this slice
does not cover").

## Why

Every authenticated page in `apps/shell` renders its own `<HmsShell>` (33
render sites today, across `dashboard`, `emergency`, `lab`, `medicore`,
`patients`, `pharmacy`, `settings`, `supply`, `workforce`). Moving between them
is client-side navigation since #933, but each move unmounts the whole chrome
(rail, page panel, header) and mounts a new one. That costs:

- **A session-activity request per navigation (#1092).** The idle tracker is
  created in `HmsShell`'s mount effect, which calls `markActivity()` and
  `reportActivity()`. A new tracker per page bypasses the 60 s debounce
  (`ACTIVITY_DEBOUNCE_MS`), and `POST /v1/auth/session/activity` is rate
  limited at 10/min with a burst of 3. Reproduced on 2026-10-13: three sidebar
  hops two seconds apart returned 429 from the fourth request; e2e run
  38184807077 logged 10 `rate limited` warnings on that route.
- **Chrome state resets.** The idle-warning countdown, the activity mutation's
  in-flight state and the panel's scroll position restart per page.
- **The header changes shape.** `tenantPicker` is a per-page prop; 20 pages
  pass it and 13 do not, so the tenant switcher appears and disappears as a
  clinician moves between departments.

## Goals

1. The chrome mounts once per full page load and survives every client-side
   navigation between authenticated pages.
2. Navigation alone never sends `POST /v1/auth/session/activity`; only the
   tracker's debounced interaction signals, its one mount-time seed, and
   "Stay signed in" do.
3. The tenant switcher is shown on **every** authenticated page (decided with
   the product owner on 2026-10-20): a clinician can always see and change
   which hospital they are acting in, and the header is the same shape
   everywhere.
4. A page cannot opt back into rendering its own chrome: that is a lint error,
   and passing an `active` path is a type error.

## Non-goals

- Visual redesign of the chrome.
- Changing the activity rate-limit values. They are correct for real activity.
- Tenant names in the switcher (#1211, assigned separately).
- Any URL change. Route groups do not affect URLs, so the #933 URL contract,
  `navigation.spec.ts` and `smoke.spec.ts` selectors stay as they are.

## Anti-goals (failure even if it "works")

- An authenticated page that renders with no chrome, or two chromes.
- An authenticated page that does not run the idle tracker. That would count a
  clinician as idle and sign them out mid-consultation
  (frontend standards §6, "Idle session tracking").
- A public route (`/`, `/login`, `/signup`, `/logout`, `/ambulance`,
  `/api/*`) that starts rendering the chrome or calling authenticated APIs.

## Design

### D1. Route group

Create `apps/shell/app/(workspace)/` and move into it, unchanged in path,
every authenticated segment: `dashboard`, `emergency`, `lab`, `medicore`,
`patients`, `pharmacy`, `settings`, `supply`, `workforce`. Outside it, as
today: `page.tsx` (landing), `login`, `signup`, `logout`, `ambulance`, `api`,
and the root `layout.tsx`, `error.tsx`, `loading.tsx`, `not-found.tsx`.

`middleware.ts` keys on paths, not files, so it does not change.

### D2. The layout renders `HmsShell` once

`app/(workspace)/layout.tsx` is a server component that renders
`<HmsShell tenantPicker={<TenantPicker />}>{children}</HmsShell>`. `HmsShell`
and `TenantPicker` are already client components; a server layout may pass a
client element as a prop. Every page in the group drops its `HmsShell`
wrapper and its `TenantPicker` import and renders its content only. Pages
whose wrapper sits inside a client `*-workspace.tsx` component (the workforce
pages) drop it there.

`(workspace)/loading.tsx` and `(workspace)/error.tsx` are added so a
suspending or failing page shows its fallback inside the chrome, not in place
of it. The root boundary files stay for the public routes. The chrome-identity
e2e test (D6) is the check that a navigation does not drop the chrome.

### D3. `HmsShell` reads the path itself

`HmsShell` drops the `active` prop and reads `usePathname()` from
`next/navigation`. A leftover `active=` anywhere is then a compile error.

- Active zone: `activeZone(pathname)`, unchanged.
- Active page: a new `activePage(zone, pathname)` in `packages/ui/src/zones.ts`
  returns the zone page whose `href` is the longest prefix of `pathname` that
  ends at a segment boundary (`pathname === href` or
  `pathname.startsWith(href + "/")`). `/emergency/bases/b/cases/c` therefore
  marks `/emergency` current, as it is hand-coded today, and
  `/workforce/setup` marks `/workforce/setup`, not `/workforce`. No match
  marks no page current; it does not fall back to the first page, because
  `aria-current` on the wrong link is a false statement to a screen reader.

The rail and panel `aria-current` checks use `activePage`'s result instead of
comparing `active` by equality.

### D4. Tenant switcher everywhere

The layout always passes `<TenantPicker />`. `TenantPicker` already renders
nothing for a user with fewer than two memberships, so single-tenant users see
no change. Rewrite the `tenantPicker` doc comment in `hms-shell.tsx`: its
"zone apps pass nothing and show no switcher" sentence describes the
multi-app era and is wrong after this change.

### D5. Structural enforcement

- **Type:** `active` is removed from `HmsShell`'s props (D3).
- **Lint:** in `apps/shell`, importing `HmsShell` from `@helivanta/ui` is a
  `no-restricted-imports` error everywhere except
  `app/(workspace)/layout.tsx`, with a message pointing at that file. That
  stops a page from rendering a second chrome. The rule lives in
  `apps/shell/eslint.config.mjs` (adding a config object after
  `hmsEslint(...)`) unless the shared factory already has a hook for it.
- **Placement:** a new authenticated route has to be added under
  `(workspace)/` to get the chrome and the idle tracker. Tests can't catch a
  route created outside the group, so frontend standards §6 states the rule,
  and the e2e test in D6 covers every zone the rail lists.

### D6. Tests (each shown failing on `main` before the change)

1. **Chrome survives navigation** (`e2e/tests/navigation.spec.ts`). After the
   first authenticated page loads, set a property on the rail's `<aside>` DOM
   node through `page.evaluate`, then visit every zone the rail lists, then
   come back to the start. After each move, assert the node still has the
   property, which means it is the same element and was not remounted. On
   `main` this fails at the first move.
2. **Navigation sends no activity requests** (#1092, same spec or
   `idle-timeout.spec.ts`, whichever project's config fits). Wait for the
   mount-time activity request to finish, start counting
   `POST …/auth/session/activity` requests, make at least four rail moves in
   well under `ACTIVITY_DEBOUNCE_MS`, then assert the count is 0. On `main`
   it is one or more per move.
3. **Tenant switcher on department pages.** Signed in as `specAdmin` (two
   tenants, per `scripts/seed-dev.mjs` and `tenant-switch.spec.ts`), assert the `Tenant` combobox is
   visible on `/medicore/opd`, `/pharmacy` and `/lab`. On `main` it is
   missing on those pages.
4. **`activePage` unit tests** (`packages/ui/src/zones.test.ts`): exact
   match, a nested path under a page, longest prefix winning over a shorter
   one (`/workforce/setup` against `/workforce`), a segment-boundary near-miss
   (`/pharmacyx` matches nothing), and no match.
5. **`hms-shell.test.tsx`** mocks `next/navigation`'s `usePathname` in place of
   the 26 `active=` props, and keeps every existing assertion.

PR evidence: the e2e API log from the PR's CI run shows no `rate limited`
line for `POST /v1/auth/session/activity`.

### D7. Panel flash on full page load

`usePersistedFlag` already reads `localStorage` synchronously through
`useSyncExternalStore`, so a client-side mount does not flash. Hydration can
still flash, because the server snapshot is the fallback: on a hard load, a
collapsed panel or an expanded rail may render in its default state for one
frame. After D2, hydration is the only mount left.

The builder first checks whether the flash is visible in a production build
(a hard load of `/pharmacy` with the panel collapsed). If it is, fix it the
same way the theme is fixed: a static pre-hydration script, exported from
`@helivanta/ui` next to `THEME_INIT_SCRIPT` and run in the root layout,
stamps `data-hms-panel` and `data-hms-navigation` on `<html>` from
`localStorage`. CSS in `packages/ui/styles.css` uses those attributes to size
the panel and rail until React takes over, and since the stored values match,
hydration does not change anything visible. Add an e2e assertion that the
collapsed panel is never painted open after a hard load. If it is not
visible, record the measurement in the PR and make no change.

### D8. Docs corrected in the same change

- `docs/standards/frontend.md` §2: the example becomes "the workspace layout
  renders `HmsShell`; `app/(workspace)/pharmacy/page.tsx` renders
  `<DispenseList />`".
- §6: replace the `active` prop paragraph with D3's rule, and replace "Every
  page renders `HmsShell`" in "Idle session tracking" with "the workspace
  layout renders `HmsShell` once". State the placement rule from D5.
- `docs/superpowers/specs/2026-10-13-one-next-app-design.md`, "What this slice
  does not cover": add a pointer to this spec.

## Left to the builder

- Whether test 2 sits in `navigation.spec.ts` or `idle-timeout.spec.ts`
  (they run under different Playwright projects).
- How the `no-restricted-imports` exception is expressed in the flat config.
- The order the pages move in, as long as one PR moves them all. Doing half
  would leave some pages with two chromes.

## What this slice does not cover

- #1211 (tenant names in the switcher).
- `activeZone`'s match without a segment boundary
  (`path.startsWith("/" + z.key)`). No route is affected today; tidying it is
  separate.
