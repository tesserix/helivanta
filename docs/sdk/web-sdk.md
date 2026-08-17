# Frontend Web SDK (`@tesserix/helivanta-*`)

pnpm workspace publishing scoped packages to GitHub Packages. Evolves from `design-system` / `@tesserix/web` (which remains a compatibility alias for existing consumers until migrated).
Targets: React 19 portals (Vite/Next.js), TypeScript strict. Core logic framework-agnostic; React bindings thin (architecture §4.8).

## Package structure

```
helivanta-web-sdk/
├── packages/
│   ├── core/          # @tesserix/helivanta-core        shared types, error envelope, result utils
│   ├── tokens/        # @tesserix/helivanta-tokens      design tokens, theming, RTL primitives   (#697)
│   ├── components/    # @tesserix/helivanta-components  component library                        (#698)
│   ├── auth/          # @tesserix/helivanta-auth        BFF session client + guards              (#699)
│   ├── api-client/    # @tesserix/helivanta-api         OpenAPI codegen toolchain + runtime      (#700)
│   ├── telemetry/     # @tesserix/helivanta-telemetry   vitals, errors, PHI-safe analytics       (#701)
│   ├── i18n/          # @tesserix/helivanta-i18n        i18n runtime + RTL switching             (#702)
│   ├── a11y/          # @tesserix/helivanta-a11y        a11y utilities + lint rules              (#703)
│   ├── flags/         # @tesserix/helivanta-flags       GrowthBook client                        (#704)
│   ├── data/          # @tesserix/helivanta-data        TanStack Query defaults, offline cache   (#705)
│   └── forms/         # @tesserix/helivanta-forms       schema-driven forms                      (#706)
├── apps/
│   ├── storybook/     # component workshop + a11y checks                                   (#707)
│   └── playground/    # sample app exercising every package (the SDK's own consumer)
└── tools/
    └── create-helivanta-app/ # portal scaffolder                                                 (#708)
```

## Layering

```
Layer 0: core, tokens
Layer 1: i18n, a11y, telemetry, flags        → may depend on L0
Layer 2: auth, api-client, data              → may depend on L0–L1
Layer 3: components, forms                   → may depend on L0–L2
apps/tools: anything
```

Every package ships ESM + types, side-effect-free (tree-shakeable), with an enforced bundle-size budget.

---

## Package specifications

### `core`

Error envelope types mirroring the Go `httpkit` contract, `ApiError` class, request-ID plumbing, shared branded types (`TenantId`, `RequestId`). Zero dependencies. The envelope contract is covered by a cross-repo contract test against the Go SDK's golden files (#710).

### `tokens` (#697)

Design tokens (color/type/spacing/motion) as CSS variables + TS constants; light/dark; tenant-branding overrides **validated for WCAG contrast** (invalid brand pairs rejected → default theme); logical-property (RTL-safe) primitives.
Tests: contrast validation matrix; RTL flip snapshots; dark+brand combinations.

### `components` (#698)

Form controls, table, dialog, toast, navigation, layout shells; clinical patterns (patient header, status badges, empty/loading/error states). Accessibility is a build gate: axe checks on every story. Long/localized content must not break layout (pseudo-locale snapshots).

### `auth` (#699)

Framework-agnostic session client for the auth-BFF (HttpOnly cookie model, as proven in HomeChef) + React bindings.

```ts
const session = await auth.bootstrap();            // whoami via BFF
auth.onExpiry(() => router.toLogin({ returnTo }));  // 401/419 interception
<Guard require={perm.ViewPatients}>...</Guard>      // role/permission gating, no content flash
```

Failure behaviour: expiry mid-use preserves safe in-progress state, re-login returns to context; two-tab logout propagates (storage events).

### `api-client` (#700)

OpenAPI → typed client generation (per-service packages) with the envelope, auth and request-ID wiring pre-applied. CI fails if a spec changed without regeneration; breaking server changes surface as type errors in consuming apps — never runtime surprises.

### `telemetry` (#701)

Web vitals with route context; error boundary + reporter (source-mapped, scrubbed); analytics wrapper with **clinical-screen opt-out** (clinical routes emit nothing — verified by test) and identifier scrubbing (same pattern corpus as the Go `logging` package, shared as a fixture). Endpoint down → app unaffected.

### `i18n` (#702)

Message catalogs + hooks; hardcoded-string lint rule; lazy language packs with English fallback (+ missing-key metric); RTL direction switching wired to `tokens`; number/date/currency formatting from the country profile. Pseudo-locale + RTL smoke test in CI.

### `a11y` (#703)

Focus trap/restore, roving focus, live-region announcements; eslint a11y ruleset + axe CI integration; contrast/type-scale checkers used by `tokens` validation.

### `flags` (#704)

GrowthBook wrapper with the standard attribute set, SSR-safe evaluation (no hydration flicker — tested), typed flag definitions, default-off safety and kill-switch pattern mirroring the Go `flags` package.

### `data` (#705)

Query-client defaults (retry/backoff tuned for Indian connectivity, staleness, error normalisation into `core.ApiError`), integration with generated clients and `auth`, optimistic-update helpers with exact rollback, persisted-cache groundwork for offline reads (full offline sync is a mobile-milestone non-goal here).

### `forms` (#706)

Schema-driven rendering on `components` with sync/async validation, standard error display + first-error focus, autosave/draft convention, conditional fields and multi-step flows. Reference implementation: patient registration form, E2E-tested incl. a11y.

### `storybook` + docs (#707)

Every component/story with theme + LTR/RTL toggles; getting-started and per-package guides; deployed on merge, versioned with releases; docs examples compile in CI (stale docs fail the build).

### `create-helivanta-app` (#708)

Generates a portal wired with all packages, sample authenticated page/form/list with tests, CI, Dockerfile, Helm/ArgoCD stubs. Must pass lint, tests, axe and bundle budget unmodified.

---

## Testing approach

| Level                                                      | Tooling                                        | Gate               |
| ---------------------------------------------------------- | ---------------------------------------------- | ------------------ |
| Unit (logic, scrubbing, validation)                        | vitest                                         | PR                 |
| Component + a11y                                           | Storybook + axe                                | PR                 |
| Visual/RTL snapshots                                       | Storybook snapshots (pseudo-locale, RTL, dark) | PR                 |
| Contract (envelope vs Go SDK)                              | shared golden fixtures                         | PR                 |
| E2E (playground app journeys incl. flaky-network scenario) | Playwright harness (#711)                      | PR smoke + nightly |
| Bundle budgets                                             | size-limit per package                         | PR                 |
