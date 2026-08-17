# Helivanta Frontend Standards & Reusable Patterns

Date: 2026-08-04
Status: approved
Builds on: phase 2 (`2026-08-04-phase2-pharmacy-lab-zones-design.md`, merged PR #755) and the styling polish (PR #756)

## Goal

Codify how Helivanta zone apps are built so every contributor — human or AI —
produces the same patterns: one data-fetching stack, one form stack, one
UX vocabulary, machine-enforced lint rules, a generator that stamps out
correct new zones, and reference implementations to copy from.

## Decisions

- **D1 — Enforcement is layered.** Machine-enforced where possible
  (ESLint/Prettier/Vitest in CI), documented where not
  (`docs/standards/frontend.md`), and agent-enforced via `CLAUDE.md` and
  a repo skill. A generator prevents drift at zone creation time.
- **D2 — Data fetching is TanStack Query + a typed client** (`@helivanta/api`).
  No raw `fetch` + `useState` + `setInterval` in components.
- **D3 — Forms are react-hook-form + zod** with inline field errors.
  Native browser validation is disabled (`noValidate` everywhere).
- **D4 — UX vocabulary:** sonner toasts for action feedback; a
  `ConfirmDialog` (AlertDialog) reserved for destructive confirmations
  (deletes); browser `alert()`/`confirm()`/`prompt()` are banned by lint.
- **D5 — Frontend unit/component tests are Vitest + Testing Library**,
  wired into `turbo test` and CI, alongside the existing Playwright E2E.
- **D6 — Tokens only.** No hardcoded colors/radii/shadows in app code;
  everything comes from `@helivanta/ui/styles.css` + `@tesserix/web` tokens.

## Deliverables

### 1. `@helivanta/config` — shared tooling (grows from tsconfig-only)

- `eslint.config.mjs` flat-config base exported as `@helivanta/config/eslint`:
  `next/core-web-vitals`, `typescript-eslint`, `react-hooks`,
  `jsx-a11y`, plus Helivanta rules:
  - `no-alert` and `no-restricted-syntax` banning `window.alert`,
    `window.confirm`, `window.prompt`
  - `no-console` (allow `warn`/`error`)
  - `@typescript-eslint/no-explicit-any`
  - `no-restricted-syntax` banning `dangerouslySetInnerHTML` outside
    the sanitized helper (see deliverable 4)
- `prettier.config.mjs` exported as `@helivanta/config/prettier`.
- `vitest.config` preset (jsdom, Testing Library setup) exported as
  `@helivanta/config/vitest`.
- Every app gets real `lint`, `format:check`, `test` scripts; the CI
  web job runs `turbo lint type-check test build`.

### 2. `@helivanta/api` — typed data layer (new package)

- `apiFetch<T>(path, init?)`: same-origin `/api/v1` client that parses
  the platform envelope (`{"data": T}` success, `{"error", "message"}`
  failure) and throws a typed `ApiError { code, message, status }`.
- `AppProviders` (client component): mounts `QueryClientProvider` with
  Helivanta defaults and the sonner `<Toaster/>`. Every zone layout wraps
  children in it.
- `useApiQuery(key, path, opts?)` and `useApiMutation(path, opts?)`
  wrappers: mutations toast errors by default (and optionally success);
  queue pages poll via `refetchInterval` using a shared
  `POLL_INTERVAL_MS` constant (replaces hand-rolled `setInterval`).
- Typed zod env access: `env.ts` per app validates required vars at
  module load and fails fast; `NEXT_PUBLIC_` discipline documented;
  each app ships a checked-in `.env.example`.

### 3. `@helivanta/ui` — UX conventions kit (grows)

- `ConfirmDialog`: promise-based destructive-action confirmation built
  on `@tesserix/web` AlertDialog. The ONLY sanctioned confirmation UI;
  used for deletes and other irreversible actions.
- Form primitives for RHF + zod: `Form`, `FormField`, `FormError`
  rendering inline field errors; all forms set `noValidate`.
- `EmptyState` component (icon + title + hint) replacing ad-hoc empty
  `<li>`s.
- Shared `formatTime`/`formatDateTime` Intl-based helpers — consistent
  timestamp rendering for clinical data (no scattered
  `toLocaleTimeString()`).
- `sanitizeHtml` helper (dompurify) — the only allowed path to
  `dangerouslySetInnerHTML`.

### 4. App Router file conventions

Every zone app ships:

- `app/error.tsx` — friendly error boundary with a retry action
- `app/loading.tsx` — skeletons from `@tesserix/web` (no blank flashes)
- `app/not-found.tsx`
- Layout wraps pages in `AppProviders`

### 5. `docs/standards/frontend.md` — the standards document

Binding rules, each pointing at a reference implementation:

- Zone-app anatomy: port table (shell 4301, medicore 4302, pharmacy
  4303, lab 4304, next free 4305+), `basePath`, `output: "standalone"`,
  `transpilePackages`, direct-hit `/api` rewrite, shell rewrite pair,
  globals.css shape (both `@source` lines + `@helivanta/ui/styles.css`).
- Server vs client components: pages/layouts are server components;
  interactivity lives in `components/*` client components; no data
  fetching in server components against the Go API (session cookie
  flows through the browser; keep fetches client-side via @helivanta/api).
- Promotion rule: a component used (or clearly about to be used) by a
  second app moves to `@helivanta/ui`.
- Chrome rules: all sidebar/cross-zone links are plain `<a>` (hard
  navigation, phase 1 spec D3); zone nav lives only in
  `packages/ui/src/zones.ts`.
- Token discipline + the two `@tesserix/web` CSS pitfalls (documented
  in `packages/ui/styles.css`): overrides need `!important` against its
  `:not(.dark):not(.light)` branch; its UNLAYERED
  `border-color: var(--border)` base rule defeats Tailwind border color
  utilities — use the unlayered-class pattern for non-default borders.
- UX vocabulary (D4), forms (D3), data fetching (D2).
- Copy rules: sentence case, verbs on buttons matching their toast
  ("Create visit" → "Visit created"), errors say what happened and how
  to recover, no apologies.
- A11y baseline: label association on every input, `aria-current` on
  active nav, visible focus, keyboard reachability, `role="alert"` on
  error text, reduced-motion respected.
- Testing: every panel component gets a Vitest + Testing Library test
  with mocked fetch (happy path + error + empty); every zone journey is
  covered by the Playwright smoke.

### 6. Agent enforcement

- `CLAUDE.md` at hms repo root: compact binding-rules section for AI
  agents (the "musts" from the standards doc).
- `.claude/skills/hms-frontend/SKILL.md`: repo skill loaded when
  working on `apps/*` / `packages/*` frontend code; points at the
  standards doc and reference implementations.

### 7. `pnpm new-zone <name>` generator

`scripts/new-zone.mjs`: stamps `apps/<name>` from a template embedded
in the script (package.json with next free port, next.config.ts with
basePath, tsconfig, postcss, globals.css, layout with AppProviders,
error/loading/not-found, example page + panel with a Vitest test), then
prints the manual follow-ups: shell rewrite pair, `zones.ts` entry,
Makefile/README mention. Refuses names that collide or contain
uppercase/spaces.

### 8. Migration — reference implementations

Convert existing panels to the new stack as the proof and the examples
the doc links to: `visit-panel`, `ping-panel`, `dispense-list`,
`medications-panel`, `order-list`, plus the login page (RHF + zod).
Success toasts on create/dispense/result actions; mutation errors
toast; polling via React Query. Each converted panel gets its
component test.

## Out of scope

- Storybook / visual regression testing.
- i18n (note in the doc as future; copy stays in components for now).
- OpenFGA-aware permission UI (own phase).
- Mobile apps.
- Backend/Go standards (separate effort).
