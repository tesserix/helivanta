# Helivanta frontend standards

Binding rules for the web app (`apps/shell`) and the shared packages
(`packages/ui`, `packages/api`, `packages/config`). "Binding" means: code
that doesn't follow these rules should not pass review, and where a rule
can be machine-enforced it is (ESLint, `tsc`, Vitest, CI). Every section
below points at a real file in this repo — read that file before writing
new code in the same area.

## 1. App anatomy

Helivanta's clinician frontend is **one** Next.js app, `apps/shell`, on
port 4301. MediCore, Pharmacy and Lab ("zones") are route segments inside it —
`app/(workspace)/medicore/`, `app/(workspace)/pharmacy/`,
`app/(workspace)/lab/` — not separate apps (#933,
design spec `docs/superpowers/specs/2026-10-13-one-next-app-design.md`;
ADR-0010). A zone is a product area of one app, not a deployment unit:
entitlement decides what a tenant sees at runtime, not which apps exist.

| Zone     | Routes                                                             | Components             |
| -------- | ------------------------------------------------------------------ | ---------------------- |
| MediCore | `app/(workspace)/medicore/{opd,ipd}`                               | `components/medicore/` |
| Pharmacy | `app/(workspace)/pharmacy`, `app/(workspace)/pharmacy/medications` | `components/pharmacy/` |
| Lab      | `app/(workspace)/lab`                                              | `components/lab/`      |

`apps/shell/next.config.ts` sets `output: "standalone"` (the app builds and
deploys as one self-contained server, `Dockerfile.shell`) and
`transpilePackages: ["@helivanta/ui", "@helivanta/api"]` (these are
TypeScript source packages, not pre-built libraries, so Next compiles them).
Its rewrite sends `/api/:path*` to the Global API for `next dev`;
production ingress must route `/api/v1/*` to the same Global API boundary.
See [Global API rules](../api/global-connector.md) for onboarding and release gates.

Adding a zone is a route segment under
`apps/shell/app/(workspace)/<name>/`, its
components under `apps/shell/components/<name>/`, and an entry in
`packages/ui/src/zones.ts` (section 6). There is no generator: a new zone
is ordinary routes in the existing app.

`apps/shell/app/globals.css` has these three imports plus two `@source`
scans, in this order:

```css
@import "tailwindcss";
@import "@tesserix/web/styles";
@import "@helivanta/ui/styles.css";

/* Tailwind v4 must scan the design system for emitted class names. */
@source "../node_modules/@tesserix/web/dist";

/* Scan the shared UI package for emitted class names. */
@source "../../../packages/ui/src";
```

The order matters: `@helivanta/ui/styles.css` imports last so its `!important`
token overrides (section 7) win over `@tesserix/web`'s defaults. The two
`@source` lines exist because Tailwind v4's CSS-first config only scans
files it's told about — omit either one and classes used only inside
`@tesserix/web` or `packages/ui` get purged from the built CSS even
though the components render fine in dev.

## 2. Server vs client components

Pages and layouts (`app/page.tsx`, `app/layout.tsx`) are React Server
Components by default — no `"use client"` directive, no hooks, no event
handlers. All interactivity — forms, mutations, polling, local state —
lives in a `components/*.tsx` file marked `"use client"` that the server
page renders. The workspace layout, `apps/shell/app/(workspace)/layout.tsx`,
renders `HmsShell`; `apps/shell/app/(workspace)/pharmacy/page.tsx` renders
`<DispenseList />`, and `DispenseList` itself is the client component in
`apps/shell/components/pharmacy/dispense-list.tsx`.

Server components must never call the Go API directly. Authentication in
Helivanta is a browser session cookie (set by `auth-bff`-style login), and that
cookie only flows automatically on same-origin `fetch` calls made from
the browser through the app's `/api/*` route. A server-side `fetch` in a
page or layout runs on the Node server, has no cookie jar, and will hit
the backend unauthenticated. Keep every data fetch client-side, through
`@helivanta/api` (section 3).

## 3. Data fetching

All server data flows through `useApiQuery` / `useApiMutation` from
`@helivanta/api` (`packages/api/src/hooks.ts`), which wrap TanStack Query
around the typed `apiFetch<T>` client (`packages/api/src/client.ts`).
Raw `fetch`, `useState` + manual loading flags, or a hand-rolled
`setInterval` for polling are not used in application components —
`useApiQuery`, `useApiMutation`, and the shared `POLL_INTERVAL_MS`
constant cover every case those patterns used to. The sanctioned
exception is a small, closed set of shell-owned **auth-lifecycle**
routes that sit outside the `/api/v1` envelope's ordinary contract —
each installs or renews the `helivanta_session` cookie itself via the
backend's own `Set-Cookie`, rather than reading or mutating panel data,
so wrapping them in TanStack Query's cache/retry machinery would be
wrong, not just unnecessary. Today that set is exactly two functions,
both in `apps/shell/lib/`, both documented at their own definition:

- `exchangeIdToken` (`lib/auth-exchange.ts`) — `POST /v1/auth/login`,
  called once, by the login callback
  (`app/api/auth/callback/page.tsx`), to exchange a fresh Zitadel ID
  token for a Helivanta session on first login.
- `renewSession` (`lib/renew.ts`) — `POST /v1/auth/renew`, called on a
  timer by `components/session-renewal.tsx` (#916, design spec D1) to
  re-mint the existing session from its own cookie; no ID token, no
  Zitadel round trip through the browser at all.

The timer itself is the other half of this exception, and it is named
explicitly rather than left implied: `session-renewal.tsx` schedules
`renewSession` with a **self-rescheduling `setTimeout` loop**, which is
the same hand-rolled polling this section bans everywhere else. It is
sanctioned here for the same reason the raw `fetch` is — the cadence comes
from the server's own `renew_at` on each response (`nextRenewalDelayMs`,
`lib/renew.ts`), so it is a variable-interval schedule the API dictates,
not a fixed `POLL_INTERVAL_MS` refetch of cached data, and TanStack
Query's `refetchInterval` cannot express it without fighting the cache.
Nothing else in any app may schedule its own timer this way.
(The tenant switcher, `apps/shell/components/tenant-picker.tsx`, also
re-mints the session via its own `Set-Cookie` response — but does so
through `apiFetch`/`useApiMutation` like an ordinary `/api/v1` mutation,
since `POST /iam/me/tenant` IS inside the standard envelope; it is not
part of this raw-fetch exception.) Anything that is not one of these two
auth-lifecycle exchanges goes through `@helivanta/api`.

`useApiQuery<T>(key, path, opts?)` takes a TanStack query key, the
API path (appended to the fixed `/api/v1` prefix), and an optional
`{ poll: true }` to enable interval refetching at `POLL_INTERVAL_MS`
(3000ms) instead of hand-writing `setInterval`/`clearInterval`.
`useApiMutation(fn, opts)` wraps a mutation function; on success it
fires `opts.successToast` (a sonner toast, section 5) and invalidates
every query key listed in `opts.invalidate`, so a create/update/delete
action refreshes exactly the queries it affects instead of a full page
reload. On error it toasts `error.message` automatically — component
code does not need its own error-toast branch.

Reference implementation:
`apps/shell/components/medicore/visit-panel.tsx`. It queries `["visits"]`,
submits a create mutation that toasts "Visit created" and invalidates
`["visits"]`, and resets the form on success — the shape every
create-and-list panel in Helivanta follows.

**Collection endpoints are read with `useApiPagedQuery`, never `useApiQuery`.**
Every collection endpoint returns `{data: [...], page: {next_cursor, has_more}}`
(#816); reading it with `useApiQuery` shows only the first page and silently
hides the rest — exactly the truncation this contract exists to end.
`useApiPagedQuery<T>(key, path, opts?)` (`packages/api/src/paged.ts`) is a
narrow sibling of `useApiQuery` built on `useInfiniteQuery`, not a
replacement: `useApiQuery` is used by every panel and by `permissions.tsx`,
and reshaping it to `useInfiniteQuery` would change the contract for all of
them. It returns `{ items, hasMore, loadMore, isPending, isFetchingMore }`
and accepts the same `{ poll?: boolean }` option `useApiQuery` does, for
queue screens (dispense, lab orders) that need to stay current.

Render `<LoadMore hasMore isLoading onClick />` (`packages/ui/src/load-more.tsx`)
after the list. It renders nothing when `hasMore` is false — that absence is
a positive statement that the client has the whole collection, so its
disappearance is the point, not an oversight to patch with a disabled state.
Never infer "there is more" from a full page (page length equal to the
requested limit): a full final page and a truncated one are indistinguishable
by length alone, which is the defect #816 exists to fix. `hasMore` must come
from the server's explicit `has_more` flag only.

Reference implementations for the paginated shape:
`apps/shell/components/medicore/visit-panel.tsx` and `apps/shell/components/pharmacy/dispense-list.tsx`
(the latter also shows `poll: true` combined with pagination).

## 4. Forms

Forms use `useZodForm` (a `react-hook-form` + `@hookform/resolvers/zod`
wrapper, `packages/ui/src/form.tsx`) paired with the `Field` component
for label + input + inline error. Every `<form>` element sets
`noValidate` — Helivanta disables native browser validation everywhere so the
only validation UI a user sees is the zod-driven inline error, never a
browser's native "Please fill out this field" popup.

```tsx
const form = useZodForm(visitSchema, { patient_name: "" });
// ...
<form
  noValidate
  onSubmit={form.handleSubmit((values) => createVisit.mutate(values))}
>
  <Field
    id="patient_name"
    label="Patient name"
    error={form.formState.errors.patient_name?.message}
  >
    <Input id="patient_name" {...form.register("patient_name")} />
  </Field>
</form>;
```

`Field` renders the error paragraph with `role="alert"` so assistive
technology announces it as soon as it appears (section 10). Reference
implementations: `apps/shell/components/medicore/visit-panel.tsx` for a
data-mutation form, and `apps/shell/app/login/page.tsx` for a form with
non-field-level failure (bad credentials) shown as a separate
`role="alert"` paragraph below the fields rather than attached to one
input.

Selection dropdowns use `SelectControl` from `@helivanta/ui`, backed by the
shared Radix primitives in `@tesserix/web`. Use `FormSelectField` inside
`useZodForm` forms so labels, focus, validation errors and controlled values
stay connected through React Hook Form. Pass options as `{ value, label }`
objects; an explicit empty value supports choices such as "Not recorded".
Do not add visible native `<select>` elements or another selection wrapper.
The shared control handles keyboard navigation, disabled states, bounded
mobile popovers and tenant theme propagation into portalled menus. Action
menus remain separate from value selection.

```tsx
<FormSelectField
  control={form.control}
  name="sex"
  id="registration-sex"
  label="Sex as recorded"
  options={[
    { value: "", label: "Not recorded" },
    { value: "female", label: "Female" },
    { value: "male", label: "Male" },
    { value: "other", label: "Other" },
  ]}
/>
```

Dropdowns are `SelectControl` (plain state) or `FormSelectField` (inside
`useZodForm`) from `@helivanta/ui`, both the `@tesserix/web` Select. A native
`<select>` is a lint error: its popup is drawn by the OS, outside the tokens
(#1255).

## 5. UX vocabulary

Action feedback goes through sonner toasts, mounted once per app via
`<Toaster />` inside `AppProviders`. A success toast's verb matches the
button that triggered it: clicking "Create visit" produces the toast
"Visit created"; clicking "Dispense" produces "Dispensed". This mirrored
phrasing is deliberate — it lets a user glance at the toast and confirm
the action they just took actually happened, without re-reading the
whole sentence.

`ConfirmDialog` (`packages/ui/src/confirm-dialog.tsx`, built on
`@tesserix/web`'s `Dialog`) is the only sanctioned confirmation UI, and
it is reserved for destructive, hard-to-undo actions — deletes and
similar. Anything reversible (dismissing a banner, toggling a filter)
does not need a confirmation step at all.

`alert()`, `confirm()`, and `prompt()` are banned outright — they block
the main thread, can't be styled to match the app, and are unusable with
a screen reader mid-flow. This is not just a style preference: `no-alert`
is a hard ESLint error in `@helivanta/config` (`packages/config/eslint.config.mjs`),
so code that calls any of the three fails `pnpm turbo lint`. Native
browser validation popups are banned for the same reason `noValidate` is
mandatory (section 4) — they're outside the app's control and inconsistent
across browsers.

## 6. Chrome & navigation

Navigation links — the rail, the page panel, the home mark and the
dashboard's department cards — are `next/link`, so moving from
`/medicore/opd` to `/pharmacy` is client-side routing inside one running
app: no page reload, client state and the React Query cache survive
(#933). `e2e/tests/navigation.spec.ts` is the control that keeps it so: it
fails if moving between departments reloads the page. Use a plain `<a>`
only for a destination outside this app, or for a deliberate full reload into
the sign-in flow (`/login`), where starting from fresh state is the point —
e.g. the auth callback's "Try signing in again".

**Sign-out is not a link at all.**
It is a state change, not navigation: it revokes every session for the
subject, so it must be a POST with a same-origin check rather than a GET
any page can trigger with an `<img>` tag (#781). It renders as a form
submission (`packages/ui/src/hms-shell.tsx`, `apps/shell/app/logout/route.ts`).
Do not convert it back to a link.

Zone navigation data — the list of zones, their icons, and their pages —
lives in exactly one place: `packages/ui/src/zones.ts`. Adding a zone
page or a whole new zone means adding an entry to the `ZONES` array
there; nothing else in the shell chrome should hardcode zone labels,
hrefs, or icons.

The chrome is rendered once, by the workspace layout
`apps/shell/app/(workspace)/layout.tsx` (#1091, design spec
`docs/superpowers/specs/2026-10-20-workspace-layout-design.md`). A layout
persists across client-side navigation, so the rail, panel, header and idle
tracker mount once per page load, not once per page;
`e2e/tests/navigation.spec.ts` fails if a move remounts them. Every
authenticated route lives under `app/(workspace)/`; the public routes (`/`,
`/login`, `/signup`, `/logout`, `/ambulance`, `/api/*`) stay outside it. A
route group does not change URLs. **A new authenticated route placed outside
the group gets no chrome and no idle tracking**, so put it inside. Pages
render their content only: importing `HmsShell` anywhere else in
`apps/shell` is a lint error. The layout always passes the tenant switcher,
which renders nothing for a user with fewer than two memberships.

`HmsShell` reads the current path itself with `usePathname()`; there is no
`active` prop. The active zone comes from `activeZone()` and the active page
from `activePage()` (both in `zones.ts`): the panel link whose `href` the
path equals or sits under, the longest one winning, so
`/emergency/bases/b/cases/c` marks `/emergency`. When nothing matches, no
panel link is marked. These set `aria-current="page"` on the matching rail
and panel links (section 10).

The panel and navigation open/closed flags are stored in `localStorage`,
which the server cannot read. `CHROME_INIT_SCRIPT` (root layout, next to the
theme script) stamps them on `<html>` before paint, and `packages/ui/styles.css`
sizes the chrome from those attributes, so a collapsed panel is never painted
open on a hard load.

### List pages: width, the detail pane and unsaved edits (#1303)

- **Width is a zone's declared choice (#1366).** A `Zone` in `packages/ui/src/zones.ts` may
  set `width: "full"`; a `ZonePage` cannot, so a zone's pages never jump in width as you move
  between them. The shell then drops the `max-w-6xl` cap on every page of that zone and stops
  at 1760px. Other zones keep the cap. Inside a full-width zone, lists and panels fill the
  width; only prose (about 70ch) and forms (about 640px) keep a reading limit.
  `e2e/tests/navigation.spec.ts` checks every PharmaConnect page. `main` is a size container
  (`@container/main`), so a page lays out by the space it actually has: expanding the rail
  changes that without changing the viewport.
- **An open record sits beside its list.** Use `ListDetail` from `@helivanta/ui`. It splits
  when main is at least 840px wide, and shows the record alone, with a back link, below
  that. It is one tree open or closed, so neither the list nor a half-typed form is
  remounted. Below 840px, opening a record scrolls the content area (`[data-hms-scroll]`) to
  its top.
- **One mechanism per list page:** `useListDetail` (`apps/shell/lib`) for the open record's
  address (`?open=` and `?new=1` through `useDetail`), Previous / Next over the rows as they
  were when it was opened, the leave guard, Escape and focus; `DetailHeader` from
  `@helivanta/ui` for its title, Previous / Next and Close. Each form in the record reports
  through `formState(key)` and `useReportState`. Do not copy this behaviour into a page.
- **Outlet pages carry their scope.** `pharmacy=` and `outlet=` are in the address of every
  outlet-scoped pharmacy page (#1303 E8), and `useUrlPage` keeps them in every link it
  builds, so a shared link opens the outlet it was copied from. An outlet asked for and not
  loaded is never replaced by another.
- **Unsaved edits are never lost silently.** A detail with forms registers through
  `useListDetail`, or `useLeaveGuard(dirty, name)` directly. Shell links (`GuardedLink`),
  Back and reload then ask "Discard changes to {name}?"; a Cmd, Ctrl, Shift or middle click
  opens another tab and is not held. A page's own moves that are not links (a picker, a
  search) ask through `useConfirmLeave`. Both throw without the shell's
  `NavigationGuardProvider`, so a page rendered outside the shell fails rather than dropping
  edits. Component tests wrap it in that provider.

### Idle session tracking

Interaction tracking for the idle-timeout feature (#848, design spec D2/
D4/D7) lives in `@helivanta/ui` (`packages/ui/src/idle-timer.ts`), mounted once
by `HmsShell` (`packages/ui/src/hms-shell.tsx`) — **not** in `apps/shell`.
The workspace layout renders `HmsShell` once, so every authenticated page
inherits the same tracker automatically; tracking on only some pages would count a clinician
working inside a zone as idle and sign them out mid-consultation. Do not
add a second, page-local copy of this tracking — if a zone needs something
the shared tracker does not do, extend `createIdleTracker` rather than
reimplementing interaction tracking next to it.

`HmsShell`'s activity mutation is deliberately configured with
`suppressErrorToast: true`, and its failure handler only ever acts on a
401 whose `ApiError.code` is `session_idle` — **a failed activity call
must never sign a user out.** The server-side `idle_deadline` is the
backstop, and the next qualifying interaction simply retries the call;
treating a dropped network request, a 5xx, or a rate limit as "sign the
user out" would turn ordinary transient failure into a false idle-timeout
for a clinician who was never idle at all.

## 7. Tokens & the two `@tesserix/web` pitfalls

No hardcoded colors, radii, or shadows in app or component code —
everything comes from the CSS custom properties `@tesserix/web` and
`packages/ui/styles.css` define (`--background`, `--foreground`,
`--sidebar`, `--border`, etc.) via Tailwind's token-backed utility
classes (`bg-background`, `border-sidebar-border`, and so on).

`packages/ui/styles.css` documents two pitfalls in `@tesserix/web`'s CSS
that aren't obvious from the class names alone. Quoted verbatim, because
paraphrasing loses the specific mechanism:

Pitfall 1 — token overrides need `!important`:

> `@tesserix/web`'s own light theme rule includes a
> `:not(.dark):not(.light)` fallback branch, whose two `:not()`
> pseudo-classes each add a class-level specificity point — (0,3,0,0)
> total — which beats a plain `:root[data-theme="default"]` (0,2,0,0)
> override regardless of source order. `!important` sidesteps that (and
> any future `@tesserix/web` selector changes) for this deliberate token
> override.

In practice: if you add or change a token in `packages/ui/styles.css`,
keep the `!important` on it. Dropping it because "it looks unnecessary"
will make the override silently lose to `@tesserix/web`'s own rule in
some theme states.

Pitfall 2 — an unlayered base rule beats Tailwind border utilities:

> `@tesserix/web` ships an UNLAYERED base rule that forces
> `border-color: var(--border)` on every element; unlayered CSS beats
> Tailwind's layered `border-sidebar-border` utility regardless of
> specificity, so sidebar seams rendered light `#e2e8f0`.

The fix already in the repo is the `.hms-sidebar-border` class in
`packages/ui/styles.css`, which is itself unlayered and uses
`!important` to win. If you need a non-default border color anywhere
`@tesserix/web`'s base rule is in play, follow that same pattern — add
an unlayered class with `!important` — rather than reaching for a
`!important` inline style or a one-off Tailwind arbitrary value, which
won't win against the unlayered base rule either.

### One look: light, sage, modern enterprise (#1255)

Design spec: `docs/superpowers/specs/2026-10-22-one-look-design.md`.

- **Light only.** There is no dark theme, theme toggle or `dark:` utility.
  `packages/ui/src/light-only.test.ts` fails on any `data-theme="dark"` rule.
- **One palette.** The public pages and the app read the same sage tokens in
  `packages/ui/styles.css`. `packages/ui/src/contrast.test.ts` holds the
  pairs it lists to WCAG 2.2 AA: text on each surface, status and destructive
  text on cards and tinted tiles, each category tint, each brand theme, and
  the blended chrome pairs (`/70` sidebar text, muted text on a hover fill).
  It covers only what it lists: add a pair when you add a token or render
  one on a new surface.
- **Shapes** come from the `@theme` block in `apps/shell/app/globals.css`
  (radius 6/8/12/16px, faint ink-tinted shadows). `@tesserix/web` follows it,
  so never set a radius or shadow per component.
- **Restraint.** A page opens with `PageHeading` from `@helivanta/ui`: a plain
  title, an optional one-line description and an actions slot. Cards are a
  border plus `shadow-xs`; only floating surfaces cast a real shadow; never
  nest a bordered card in another.
- **Lint-enforced** (`hmsEslint(dir, { design: true })`):
  - raw Tailwind palette colours and hex arbitrary values;
  - marketing type (serif, italic, gradients, wide-tracked or uppercase eyebrows);
  - native `<select>`.

  Only the public pages (`app/page.tsx`, `app/signup/`, `app/ambulance/`,
  `app/login/`) and the login surface opt out of the marketing-type rule, through
  `hmsPublicPages`.

## 8. App Router files

The app ships all three App Router boundary files at `app/` root:
`error.tsx` (a friendly error boundary with a "Try again" action that
calls `reset()`, see `apps/shell/app/error.tsx`), `loading.tsx` (a
`@tesserix/web` `Skeleton`, not a blank screen, see
`apps/shell/app/loading.tsx`), and `not-found.tsx` (see
`apps/shell/app/not-found.tsx`). They cover every zone's routes. Skipping any of them means Next
falls back to its own unstyled default, which looks broken next to the
rest of the app.

The root layout wraps `children` in `AppProviders` from
`@helivanta/api` — this is what mounts the shared `QueryClientProvider` and
the `<Toaster />` that sections 3 and 5 depend on. `apps/shell/app/layout.tsx`
(trimmed):

```tsx
export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const nonce = (await headers()).get("x-nonce");
  if (!nonce) throw new Error("missing CSP nonce: middleware did not run");
  return (
    <html lang="en" data-theme="default" suppressHydrationWarning>
      <body>
        <script
          nonce={nonce}
          dangerouslySetInnerHTML={{ __html: CHROME_INIT_SCRIPT }}
        />
        <AppProviders>
          <StyleNonce nonce={nonce} />
          {children}
        </AppProviders>
      </body>
    </html>
  );
}
```

A layout that renders `children` directly without `AppProviders` will
compile and run, but every `useApiQuery`/`useApiMutation` call beneath
it throws at runtime because there's no `QueryClientProvider` in the
tree.

### Content-Security-Policy

Every shell response carries an enforced, per-request nonce policy (#1113,
`apps/shell/lib/csp.ts`, spec
`docs/superpowers/specs/2026-10-20-nonce-csp-design.md`). `middleware.ts`
mints the nonce; the root layout reads it, which makes every page dynamic.
What that means for code:

- An inline `<script>` needs `nonce={nonce}`. `'strict-dynamic'` trusts
  whatever a nonced script loads, so bundles and `next/script` need nothing.
- A `<style>` element is allowed only by nonce or hash. React-rendered and
  script-created ones are nonced by `StyleNonce`
  (`apps/shell/components/style-nonce.tsx`). A library that inserts a
  stylesheet before hydration (sonner) is allowed by hash, with a test that
  recomputes the hash from the installed package (`lib/csp.test.ts`).
- `style="…"` attributes are allowed (`style-src-attr 'unsafe-inline'`).
- A new origin the browser must call (`connect-src`) or load images from
  goes in `contentSecurityPolicy`, with its reason.
- `e2e/tests/csp.spec.ts` visits every page and fails on any violation.
  Production violations are logged by `app/api/csp-report/route.ts` as
  `csp violation` lines.

## 9. Copy rules

All UI copy — labels, headings, button text, toasts, errors — is
sentence case ("Create visit", not "Create Visit" or "CREATE VISIT").
Buttons are verbs describing the action they perform ("Create visit",
"Dispense", "Sign in"), never generic labels like "Submit" or "OK".
Error messages say what happened and how to recover, not just that
something failed — `apps/shell/app/login/page.tsx`'s sign-in failure
reads "Sign-in failed. Check your email and password.", not just "Error"
or "Failed". No apologies ("Sorry, something went wrong") — state the
problem and, where possible, the fix; `apps/shell/app/error.tsx`'s
boundary reads "Something went wrong" with a "Try again" action, not an
apology.

## 10. A11y baseline

WCAG basics apply to every screen, not just the ones a designer flagged:

- Every form input has an associated `<label htmlFor>` — `Field`
  (`packages/ui/src/form.tsx`) generates this pairing automatically from
  its `id`/`label` props, so use `Field` rather than a bare `<input>`.
- The current nav item carries `aria-current="page"` — see the rail and
  panel links in `packages/ui/src/hms-shell.tsx`, both driven off the
  `active` prop (section 6).
- Focus is always visible; don't suppress the browser's focus ring with
  `outline-none` unless you replace it with an equally visible custom
  ring.
- Every interactive element is reachable and operable by keyboard alone
  — buttons and links, not `<div onClick>`.
- Error text is announced to screen readers via `role="alert"` — `Field`
  sets this on its error paragraph (section 4), and standalone error
  banners (like the login page's sign-in failure) do the same by hand.
- Respect `prefers-reduced-motion`: transitions in `HmsShell` use
  `motion-reduce:transition-none` alongside the animated class so users
  who've asked for reduced motion don't get the rail/panel slide.

## 11. Testing

Every panel component gets at least one Vitest + Testing Library test
covering its primary happy path; add the empty state and a validation
or error case as separate `it` blocks where the component has
meaningful behavior to cover — not every panel needs all three. Tests
render through `renderWithProviders` (`packages/api/src/testing.tsx`),
which wraps the component in a fresh `QueryClientProvider` (retries
disabled) and a `<Toaster />`, and returns a wired-up `user-event`
instance alongside the normal Testing Library queries. See
`apps/shell/components/medicore/visit-panel.test.tsx` for the fullest
example in the repo — mocked `fetch`, an empty-list assertion, a create
flow that asserts the list updates, and a second test asserting the
inline validation error. `apps/shell/components/lab/order-list.test.tsx` is a
narrower, happy-path-only example (save-result flow, list update); no
current test asserts on toast text — treat the toast copy in section 5
as the source of truth for what a mutation shows, not the tests.

Every zone user journey is additionally covered by the Playwright smoke
test at `e2e/tests/smoke.spec.ts`, which drives a real login through all
three zones (create an OPD visit, see it queued for dispense in
Pharmacy, dispense it) across actual hard navigations between apps.

**No two e2e spec files may share a login account.** Signing out writes a
revocation watermark that is per _subject_ and global across every tenant
and device (#781) — so a spec that signs out invalidates every live
session for that account, including sessions other specs are using. Under
parallel workers that is a race; run serially it is a certainty. Each
spec's accounts are therefore derived from its own filename by
`specAdmin()` / `specPharmacist()` in `e2e/tests/support/login.ts`
(`login(page)` defaults to the calling spec's admin), and
`scripts/seed-dev.mjs` seeds a matching `e2e-<spec>-admin@hms.dev` /
`e2e-<spec>-pharmacist@hms.dev` pair for every `*.spec.ts` it finds. Add a
spec file, re-run `make seed`, and the isolation is there — never
hard-code `test@hms.dev`, which is the human account for manual dev, and
never reach for `--workers=1`, which hides the property rather than
holding it.

For the same reason `login()` proves the session against the API
(`GET /api/v1/iam/me/permissions`) rather than against the dashboard
heading: `apps/shell/middleware.ts` admits any request carrying a
`helivanta_session` cookie without inspecting it, so the dashboard renders
perfectly well for a session the API refuses on every call.

Before calling any frontend change done, run:

```bash
pnpm turbo lint type-check test build format:check
```

`format:check` is the fifth task and is easy to leave off — this document
listed four until 2026-08-19, and CI's `web` job failed on `main` because of
exactly that: the four-task command was run, reported green, and a prettier
violation reached `main` unnoticed while Actions were unavailable to catch it.
The `web` job runs all five, so a locally-green four is not the gate.

Until 2026-08-20 this command only reached `apps/*` and `packages/*` — `e2e/`
had no `lint`/`type-check`/`format:check` scripts for turbo to match, and
`scripts/` was not a workspace member at all, so both were invisible to it by
construction rather than by any deliberate exclusion (#920). It now also
covers:

- **`e2e/`**: `lint`, `type-check` and `format:check` all run, same as any
  app or package. It carries no `test` or `build` script — there is nothing
  to unit-test or bundle — so those two tasks skip it, which is expected
  rather than a gap.
- **`scripts/`**: `lint` and `format:check` run. It carries no `type-check`
  because it is plain Node ESM (`.mjs`, no `tsconfig.json`) rather than
  TypeScript — adding types to it is a separate, larger change, not a gate
  fix. It carries no `test`/`build` for the same reason as `e2e/`. Its
  `eslint.config.mjs` turns `no-console` off (these scripts print CLI
  progress and results to a human on purpose) and layers in Node globals
  (`process`, `fetch`, `URL`, ...) that TypeScript packages get for free
  from `tsc` but plain `.mjs` needs from ESLint directly.
- **Shell scripts** (`*.sh`, and any tracked file with a sh/bash shebang,
  anywhere in the repo): not covered by this command, since Prettier and
  ESLint only handle JS/TS. They have their own gate, `make lint-shell`
  (#924), which CI's `scripts` job runs. It is `shellcheck` at every severity,
  pinned by digest and run in Docker, with the configuration in
  `.shellcheckrc`. Files are discovered rather than listed, so a new script
  is checked without anyone adding it anywhere. A finding is fixed, or
  disabled at its own site with the reason written next to the directive.

TypeScript runs side by side. `@typescript/native` (`npm:typescript@^7`)
provides the `tsc` that `type-check` runs. `typescript` is aliased to
`@typescript/typescript6`, because 7.0 ships no compiler API and
typescript-eslint and Next.js import that API by the name `typescript`. Every
package with a `type-check` script lists both. `packages/config` lists only
the 6.0 alias: it owns typescript-eslint, and without it pnpm resolves
typescript-eslint's peer to 7.0 and lint refuses to start. Remove the 6.0
alias once typescript-eslint supports TypeScript 7
([typescript-eslint#10940](https://github.com/typescript-eslint/typescript-eslint/issues/10940)).
There is no blanket exclusion.

Running the suite itself is still a separate step — this command is static
checks only, it does not execute a single Playwright spec:

```bash
cd e2e && npx playwright test
```

All five `turbo` tasks and the Playwright run must be green. That bare
Playwright run deliberately covers **15 of the suite's 17 tests**. Two
projects are filtered out of it, for the same reason:

- **`idle-timeout`** (`idle-timeout.spec.ts`, #848) needs apps/shell's dev
  server swapped for a second instance pointed at a short-`IDLE_TIMEOUT` API.
- **`renewal`** (`session-renewal.spec.ts`, #916) needs one pointed at a
  short-`SESSION_TTL` API.

Next.js 16 refuses a second `next dev` for the same project directory, so
neither fixture can run alongside the main stack the other 15 tests assume is
up — or alongside each other. `e2e/playwright.config.ts`'s `grepInvert`
therefore drops **both** from a project-less run, so `npx playwright test`
stays honest about what it actually covered.

`make e2e`, run from the repo root, is what runs the whole suite, in **three
phases**: "specs"+"bulk" against the main stack, then a swap to the
idle-timeout fixture, then the session-renewal fixture, each against a
production build of the app behind an ingress that routes like production
(see `scripts/e2e.sh`, #976).

**CI runs this suite on every PR and every push to `main`** — the `e2e` job in
`.github/workflows/ci.yml` brings up the real dev stack with the same `make`
targets you use locally and runs `make e2e` (#920). It takes ~8.5 minutes, has
a hard timeout, and does not retry: a flake is a red build, on purpose. On a
failure it uploads the Playwright HTML report and the failing test's trace as
an artifact, and dumps `docker compose ps` plus the Zitadel and stack logs.
`forbidOnly` is set under CI, so a committed `test.only` fails the build
rather than silently shrinking the run to one test.

## 12. Env

Each app validates its required environment variables at module load
via `defineEnv` (`packages/api/src/env.ts`), a thin zod wrapper that
throws one readable error listing every invalid or missing key instead
of failing later with a cryptic `undefined` deep in a fetch call. Every
app ships a checked-in `.env.example` (see `apps/shell/.env.example`)
that's kept current with whatever `defineEnv` actually requires — if you
add a required var, add it to the example file in the same change.

Only prefix a variable `NEXT_PUBLIC_` if its value is safe to ship to
every browser that loads the app — it gets inlined into the client
bundle at build time and is visible to anyone who opens dev tools.
Server-only values (API URLs used for server-side rewrites, secrets)
stay unprefixed.

## 13. Authorization (permission gating)

`Can` and `usePermissions` (`packages/api/src/permissions.tsx`) gate UI
on the caller's resolved permissions, fetched from
`GET /iam/me/permissions`:

```tsx
const { can } = usePermissions();
{
  can("pharmacy.medication.write") && <CreateMedicationButton />;
}

<Can permission="pharmacy.dispense.fulfil">
  <DispenseButton />
</Can>;
```

This is a **convenience layer only** — it decides what to show, not
what's allowed. The Go API is the enforcement point (backend standards
section 11); every guarded action must still work correctly if this
layer were deleted entirely, because a curious user can always call the
API directly. Never add a client-side check in place of, or as a
substitute for, a permission on the route.

`can()` returns `false` while `usePermissions` is loading and on error,
never `true` — so an action a user cannot perform never flashes on
screen before the permission check resolves. Don't special-case the
loading state to show actions optimistically.

Every zone and page in `packages/ui/src/zones.ts` declares a
`permission` string (`authz.Public`'s frontend counterpart is the
literal `"public"`), which `HmsShell` uses to hide rail/panel entries
the caller can't reach — see the `ZONES` array for the pattern of one
permission per zone and per page.
