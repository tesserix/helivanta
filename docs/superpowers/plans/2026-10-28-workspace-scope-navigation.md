# Workspace scope navigation plan

1. Reproduce the CI failure's competing-navigation behavior with deterministic
   component cases before changing application code.
2. Replace automatic routing with fresh-owner native-history synchronization;
   retain explicit scope navigation and unsaved-edit decisions.
3. Verify all Pharmacy workspace tests and the real rail journey, then lint,
   typecheck, format and build. Preserve the original E2E assertion.
4. Commit/push a separate review, integrate its parent where needed and merge
   only with complete CI. Deployment remains part of the named release review.

## Validation — 2026-10-28

Three intended failures were recorded before the implementation: automatic router
transition, late restoration reclaiming another page, and overwriting a newer query.
All three now pass. The full workspace/Accounts pair passes 59 tests. Accounts'
existing navigation mock was corrected to synchronize the browser URL and search
subscription for native history, retaining all original behavioral assertions.

`mise exec node@24 -- pnpm turbo lint type-check test format:check` passed
28 tasks, including 1,033 shell tests. `pnpm turbo build` passed with the local
Global API and identity settings configured; its first invocation correctly
refused an unset production `API_URL`.

Real local admin UI created a synthetic pharmacy and outlet, restored their IDs
from an unscoped rail link, and navigated to Emergency without being reclaimed.
The new outlet denied operational access without a per-person grant. No grant
was added. This manual journey does not replace the original CI chrome-remount
assertion. The navigation PR must pass that unchanged E2E suite before merge.

No production rollout or full Staff Management completion follows from this fix.
