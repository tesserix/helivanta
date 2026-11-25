# Public catalogue release truth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** catalogue labels follow release eligibility, backed by evidence that CI checks.

**Spec:** `docs/superpowers/specs/2026-11-25-public-catalogue-release-truth-design.md` (#1839)

## Review Focus

1. **A capability withdrawn later.** The evidence test must fail. This is proven by flipping one eligible flag in a test fixture.
2. **An evidence path with a typo.** The test must fail, not skip.
3. **The home page with an empty group.** The patient-care group drops from `/`, which `availableGroups` already handles, and stays on `/roadmap`.

### Task 1: Evidence, labels and copy

**Files:** `apps/shell/app/platform-catalog.ts`, `apps/shell/app/platform-catalog.test.ts`, `docs/product/public-platform-catalog.md`, and the tests and fixture that pin changed strings (`landing.test.tsx`, `roadmap.test.tsx`, `patient-journey.test.tsx`, `landing-content.fixture.json`).

- [ ] Write the failing test `backs every available row with eligible release pages`. It reads `contracts/release-capabilities.json` and checks D3.
- [ ] Add a pure helper `unbackedRows(groups, release)` so the withdrawal case can be shown failing on a fixture.
- [ ] Run it. Expected: FAIL, because no row has evidence yet.
- [ ] Apply the D2 rows and add `evidence` to the three qualifying rows.
- [ ] Update the tests that pinned old statuses. Remove the changed strings from the inventory fixture by name.
- [ ] Run the shell vitest, lint and tsc. Expected: green.
- [ ] Commit, push, open the PR for the product-owner review.
