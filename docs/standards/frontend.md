# HMS frontend standards

Binding rules for every zone app (`apps/*`) and the shared packages
(`packages/ui`, `packages/api`, `packages/config`). "Binding" means: code
that doesn't follow these rules should not pass review, and where a rule
can be machine-enforced it is (ESLint, `tsc`, Vitest, CI). Every section
below points at a real file in this repo — read that file before writing
new code in the same area.

## 1. Zone-app anatomy

HMS is a set of independent Next.js apps ("zones") stitched together by
the shell at the HTTP layer, not by a monorepo import graph. Each zone
owns a port, a `basePath`, and its own build.

| App             | Port  | basePath        |
| --------------- | ----- | --------------- |
| `apps/shell`    | 4301  | none (owns `/`) |
| `apps/medicore` | 4302  | `/medicore`     |
| `apps/pharmacy` | 4303  | `/pharmacy`     |
| `apps/lab`      | 4304  | `/lab`          |
| next zone       | 4305+ | `/<name>`       |

Every app's `next.config.ts` sets `output: "standalone"` (each zone
builds and deploys as its own self-contained server) and
`transpilePackages: ["@hms/ui", "@hms/api"]` (these are TypeScript
source packages, not pre-built libraries, so Next has to compile them
per-consumer). A zone app also declares a direct-hit `/api/:path*`
rewrite so it works standalone when hit on its own port during local
dev, with `basePath: false` on that one rewrite — without it Next
silently prefixes the rewrite source with the app's `basePath` and the
route never matches. `apps/pharmacy/next.config.ts`:

```ts
async rewrites() {
  return [
    {
      source: "/api/:path*",
      destination: `${API_URL}/:path*`,
      basePath: false,
    },
  ];
},
```

The shell has no `basePath` of its own; instead it owns `/` and forwards
every other zone's paths to that zone's port, plus the same `/api/*`
rewrite to the Go backend. `apps/shell/next.config.ts` carries one
rewrite pair per zone (bare path + `:path*` catch-all) so both
`/pharmacy` and `/pharmacy/medications` resolve:

```ts
{ source: "/pharmacy", destination: `${PHARMACY_URL}/pharmacy` },
{ source: "/pharmacy/:path*", destination: `${PHARMACY_URL}/pharmacy/:path*` },
// ...
{ source: "/api/:path*", destination: `${API_URL}/:path*` },
```

Adding a zone means adding its pair here — this is one of the manual
follow-ups the generator (below) prints, because rewrites live in a
config object the generator doesn't own.

Every zone's `app/globals.css` has the exact same three imports plus two
`@source` scans, in this order. This is the full, current content of
`apps/pharmacy/app/globals.css` — copy it verbatim into any new zone,
adjusting only the relative `@source` paths if the app's nesting depth
changes:

```css
@import "tailwindcss";
@import "@tesserix/web/styles";
@import "@hms/ui/styles.css";

/* Tailwind v4 must scan the design system for emitted class names. */
@source "../node_modules/@tesserix/web/dist";

/* Scan the shared UI package for emitted class names. */
@source "../../../packages/ui/src";
```

The order matters: `@hms/ui/styles.css` imports last so its `!important`
token overrides (section 7) win over `@tesserix/web`'s defaults. The two
`@source` lines exist because Tailwind v4's CSS-first config only scans
files it's told about — omit either one and classes used only inside
`@tesserix/web` or `packages/ui` get purged from the built CSS even
though the components render fine in dev.

Never hand-copy an existing app to start a new zone. Run
`pnpm new-zone <name>` — it scaffolds `apps/<name>` with the correct
port, `basePath`, `globals.css`, App Router boundary files, and a
starter panel with its test, then prints the manual follow-ups (shell
rewrite pair, `zones.ts` entry, README/Makefile port mention).

## 2. Server vs client components

Pages and layouts (`app/page.tsx`, `app/layout.tsx`) are React Server
Components by default — no `"use client"` directive, no hooks, no event
handlers. All interactivity — forms, mutations, polling, local state —
lives in a `components/*.tsx` file marked `"use client"` that the server
page renders. `apps/pharmacy/app/page.tsx` renders `HmsShell` around
`<DispenseList />`, and `DispenseList` itself is the client component in
`apps/pharmacy/components/dispense-list.tsx`.

Server components must never call the Go API directly. Authentication in
HMS is a browser session cookie (set by `auth-bff`-style login), and that
cookie only flows automatically on same-origin `fetch` calls made from
the browser through a zone's `/api/*` rewrite. A server-side `fetch` in a
page or layout runs on the Node server, has no cookie jar, and will hit
the backend unauthenticated. Keep every data fetch client-side, through
`@hms/api` (section 3).

## 3. Data fetching

All server data flows through `useApiQuery` / `useApiMutation` from
`@hms/api` (`packages/api/src/hooks.ts`), which wrap TanStack Query
around the typed `apiFetch<T>` client (`packages/api/src/client.ts`).
Raw `fetch`, `useState` + manual loading flags, or a hand-rolled
`setInterval` for polling are not used in application components —
`useApiQuery`, `useApiMutation`, and the shared `POLL_INTERVAL_MS`
constant cover every case those patterns used to. The sole sanctioned
exception is the shell's session route, `POST /api/session` — it's an
auth route outside the `/api/v1` envelope, so it uses raw `fetch`
directly. Two callers post to it, both shell-owned and both for the same
reason (install a freshly minted ID token as the `hms_session` cookie):
login (`apps/shell/app/login/page.tsx`) and the tenant switcher
(`apps/shell/components/tenant-picker.tsx`), which re-mints the session
after the backend authorizes a switch. Anything that is not "exchange an
ID token for the session cookie" goes through `@hms/api`.

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
`apps/medicore/components/visit-panel.tsx`. It queries `["visits"]`,
submits a create mutation that toasts "Visit created" and invalidates
`["visits"]`, and resets the form on success — the shape every
create-and-list panel in HMS follows.

## 4. Forms

Forms use `useZodForm` (a `react-hook-form` + `@hookform/resolvers/zod`
wrapper, `packages/ui/src/form.tsx`) paired with the `Field` component
for label + input + inline error. Every `<form>` element sets
`noValidate` — HMS disables native browser validation everywhere so the
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
implementations: `apps/medicore/components/visit-panel.tsx` for a
data-mutation form, and `apps/shell/app/login/page.tsx` for a form with
non-field-level failure (bad credentials) shown as a separate
`role="alert"` paragraph below the fields rather than attached to one
input.

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
is a hard ESLint error in `@hms/config` (`packages/config/eslint.config.mjs`),
so code that calls any of the three fails `pnpm turbo lint`. Native
browser validation popups are banned for the same reason `noValidate` is
mandatory (section 4) — they're outside the app's control and inconsistent
across browsers.

## 6. Chrome & navigation

Every cross-zone link and every sidebar link is a plain `<a href="...">`
— never `next/link`. This is a hard navigation by design: each zone is a
separately-deployed Next.js app, so moving from `/medicore/opd` to
`/pharmacy` is a real page load across app boundaries, not client-side
routing within one app. Using `next/link` there would silently fail or
produce a broken client-side transition. `packages/ui/src/hms-shell.tsx`
uses `<a>` for every rail and panel link, including "Sign out".

Zone navigation data — the list of zones, their icons, and their pages —
lives in exactly one place: `packages/ui/src/zones.ts`. Adding a zone
page or a whole new zone means adding an entry to the `ZONES` array
there; nothing else in the shell chrome should hardcode zone labels,
hrefs, or icons.

`HmsShell`'s `active` prop is the current page's absolute path (e.g.
`"/pharmacy/medications"`), not a zone key or a boolean. `HmsShell`
derives the active zone and active page from that path (via
`activeZone()` in `zones.ts`) and uses it to set `aria-current="page"`
on the matching rail and panel links (section 10). Every zone's
`app/page.tsx` renders `<HmsShell active="/pharmacy">...</HmsShell>`
with its own literal path.

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

## 8. App Router files

Every zone ships all three App Router boundary files at `app/` root:
`error.tsx` (a friendly error boundary with a "Try again" action that
calls `reset()`, see `apps/pharmacy/app/error.tsx`), `loading.tsx` (a
`@tesserix/web` `Skeleton`, not a blank screen, see
`apps/pharmacy/app/loading.tsx`), and `not-found.tsx` (see
`apps/pharmacy/app/not-found.tsx`). Skipping any of them means Next
falls back to its own unstyled default, which looks broken next to the
rest of the app.

Every zone's root layout wraps `children` in `AppProviders` from
`@hms/api` — this is what mounts the shared `QueryClientProvider` and
the `<Toaster />` that sections 3 and 5 depend on. `apps/pharmacy/app/layout.tsx`:

```tsx
export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" data-theme="default">
      <body>
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
```

A layout that renders `children` directly without `AppProviders` will
compile and run, but every `useApiQuery`/`useApiMutation` call beneath
it throws at runtime because there's no `QueryClientProvider` in the
tree.

## 9. Copy rules

All UI copy — labels, headings, button text, toasts, errors — is
sentence case ("Create visit", not "Create Visit" or "CREATE VISIT").
Buttons are verbs describing the action they perform ("Create visit",
"Dispense", "Sign in"), never generic labels like "Submit" or "OK".
Error messages say what happened and how to recover, not just that
something failed — `apps/shell/app/login/page.tsx`'s sign-in failure
reads "Sign-in failed. Check your email and password.", not just "Error"
or "Failed". No apologies ("Sorry, something went wrong") — state the
problem and, where possible, the fix; `apps/pharmacy/app/error.tsx`'s
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
`apps/medicore/components/visit-panel.test.tsx` for the fullest
example in the repo — mocked `fetch`, an empty-list assertion, a create
flow that asserts the list updates, and a second test asserting the
inline validation error. `apps/lab/components/order-list.test.tsx` is a
narrower, happy-path-only example (save-result flow, list update); no
current test asserts on toast text — treat the toast copy in section 5
as the source of truth for what a mutation shows, not the tests.

Every zone user journey is additionally covered by the Playwright smoke
test at `e2e/tests/smoke.spec.ts`, which drives a real login through all
three zones (create an OPD visit, see it queued for dispense in
Pharmacy, dispense it) across actual hard navigations between apps.

Before calling any frontend change done, run:

```bash
pnpm turbo lint type-check test build
cd e2e && npx playwright test
```

All four `turbo` tasks and the Playwright run must be green.

## 12. Env

Each app validates its required environment variables at module load
via `defineEnv` (`packages/api/src/env.ts`), a thin zod wrapper that
throws one readable error listing every invalid or missing key instead
of failing later with a cryptic `undefined` deep in a fetch call. Every
app ships a checked-in `.env.example` (see `apps/pharmacy/.env.example`)
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
{can("pharmacy.medication.write") && <CreateMedicationButton />}

<Can permission="pharmacy.dispense.fulfil">
  <DispenseButton />
</Can>
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
