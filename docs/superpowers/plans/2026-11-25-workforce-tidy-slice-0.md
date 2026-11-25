# Workforce tidy, slice 0: shared control and table styles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** One control class and one table style in `@helivanta/ui`, with Workforce's `inputClass` copies replaced.

**Spec:** `docs/superpowers/specs/2026-11-25-workforce-tidy-design.md` (#1261 D1, D2)

## Review Focus

1. **Focus ring visibility on the new ring.** `--ring` is `#2b4ee6` on white, which is 5.3:1. Expected: it is visible on every Workforce input.
2. **A Workforce file still matching the surface rule** only through `inputClass`. Expected: after this slice, only its real card classes remain.
3. **Layout shift from `min-h-11`** in dense rows such as the roster grid. Expected: rows still align. This needs a visual check on the hosted build.

### Task 1: `controlClass` and `tableStyles`

**Files:** create `packages/ui/src/control.ts` and `packages/ui/src/control.test.ts`; modify `packages/ui/src/index.ts`.

- [ ] Write the failing tests:
  - `controlClass` matches no `SURFACE_PATTERNS` entry;
  - it contains `focus-visible:ring-ring`, `min-h-11` and `rounded-md`, and nothing `hms-accent`;
  - `tableStyles.row` contains `h-11`, and `tableStyles.number` contains `text-right` and `tabular-nums`.
- [ ] Run them. Expected: FAIL, because the module is missing.
- [ ] Implement and export them.
- [ ] Run the tests. Expected: PASS.
- [ ] Commit.

### Task 2: Workforce uses them

**Files:** the seven Workforce `inputClass` definitions:

- `roster-policy/policy-ui.tsx`
- `roster-setup/roster-ui.ts`
- `enrollment-editor.tsx`
- `setup/page.tsx`
- `page.tsx`
- `shifts/page.tsx`
- `employment-periods-panel.tsx`, if present.

- [ ] Each definition becomes an import. The two shared modules re-export it as `inputClass`, so their 52 importers are unchanged.
- [ ] Run the shell vitest for `app/(workspace)/workforce`, lint and tsc. Expected: green, with no new lint findings. Every file stays in `SURFACE_LEGACY` until its slice.
- [ ] Commit, push, open the PR (`Refs #1261`).
