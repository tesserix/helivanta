# Page Heading Implementation Plan

**Goal:** Every workspace page opens with `PageHeading`, with no eyebrow and no second h1, and lint keeps it that way.

**Spec:** `docs/superpowers/specs/2026-10-22-page-heading-design.md` (#1274)

## Task 1: The rule (D4)

- `packages/config/eslint.config.mjs`: `WORKSPACE_SYNTAX` and `hmsWorkspacePages(files, ignores = [])`.
- `packages/config/eslint.config.test.mjs` `workspace heading rules`:
  - rejects an h1, `text-2xl`/`text-3xl`/`md:text-4xl` on a heading (literal and template), and the eyebrow in any class order;
  - keeps the design rules;
  - accepts a KPI `<p className="text-3xl tabular-nums">`, an `h2 text-xl`, an accent pill and an accent link;
  - ignores non-workspace files and tests.
- `apps/shell/eslint.config.mjs`: apply it to `app/(workspace)/**` and `components/**`, ignoring `components/login-branding.tsx`.

## Task 2: Optional title (D1)

- `packages/ui/src/page-heading.tsx`: `title?: string`; with no title there is no heading element, and the description is capped at `max-w-2xl`.
- Test `page-heading.test.tsx` "renders no heading without a title".

## Task 3: Convert (D1–D3)

- Every file the rule flags (31 files, 58 findings) moves to `PageHeading`, or drops its eyebrow. The emergency h1s become `PageHeading`.
- Tests or e2e that query a removed eyebrow or slogan, or an emergency `heading level 1`, query the new heading.
- `pnpm --filter @helivanta/shell lint` reports zero #1274 findings.

## Verify

`pnpm turbo lint type-check test build format:check`, `make lint-shell`, then e2e navigation, csp, smoke and the emergency browser tests on a production build.
