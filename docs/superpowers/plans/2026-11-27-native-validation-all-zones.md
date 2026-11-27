# No native form validation in any zone: plan

**Spec:** `docs/superpowers/specs/2026-11-27-native-validation-all-zones-design.md`

## Global constraints

- Apply spec D1–D5 to each finding of `pnpm --filter shell exec eslint app components`
  with the rule widened (D6, already in `apps/shell/eslint.config.mjs`).
- Node from `.nvmrc` (v24.21.0). Run only eslint and focused Vitest files; no
  builds, dev servers or containers.
- No new copy without listing it for product-owner approval.

## Tasks

Three groups, disjoint files, so they run in parallel:

1. **Workforce, country policy, MFA recovery and governance.** `app/(workspace)/workforce/*`,
   `components/country-policy/editor.tsx`, `components/workforce/*`.
2. **Pharmacy.** Every `components/pharmacy/*` finding, including the shared
   `currency-amount-field.tsx` and `drug-catalogue-picker.tsx`. For a shared
   component, check every caller's schema.
3. **Suppliers and supply.** `components/suppliers/*`, `components/supply/party-editor.tsx`.

Each task: fix every finding in its files, add the spec's tests, prove each new
test fails without its schema rule, and run eslint on its files plus Vitest on the
touched tests. Then the controller runs the whole shell lint and tests once, reviews
the branch, and opens the PR.
