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

## Follow-up plan after main CI

1. Retain the main failure and local failed Lab navigation as evidence. Add a
   deterministic component case resolving scope during a pending approved action.
2. Track approved React transitions in the existing navigation guard and suppress
   automatic scope repair until settlement; retain owner-address checks.
3. Correct query-free route expectations while retaining exact destination paths,
   chrome identity and zero session-activity assertions. Keep an explicit scoped
   outlet → Emergency regression.
4. Run affected unit suites, full web checks/build and isolated navigation E2E.
   Do not run the stack-swap harness against unrelated local services.

### Follow-up validation evidence

- The pending-action component regression failed with scope added to `?tab=all`
  while the action was unresolved. With transition tracking it passed, including
  resuming restoration after settlement.
- The existing session-activity journey failed in three local repetitions before
  the follow-up: clicking Lab left Pharmacy in place. The initial new explicit
  outlet/marker case passed before the fix; it is retained as integration
  coverage and is not claimed as the failing-first case.
- The development-server full navigation run passed 12/14 cases; the failures
  were document overflow (121 pixels) and a login form remaining in Loading.
  Their cause was not established in that run.
- The configured production build passed both build tasks. All 14 navigation
  tests then passed against the production-mode local server, including those
  two cases, exact route destinations, marker preservation, scoped outlet
  restoration and the unchanged zero session-activity assertion.
- Initial package checks found an accessibility lint problem in the new Next
  link mock and 21 component failures from unresolved router context. Explicit
  mock anchor markup, a canonical shell `next/navigation` resolution and the
  standalone overview's router mock corrected the setup; all 57 tests in the
  three affected component files then passed, preserving their assertions.

The isolated local Playwright configuration uses port 4399, existing synthetic
per-spec users and disposable TEST fixtures. It does not run `make e2e`, re-seed
the identity provider or stop unrelated port-4301 services. Private logs and
traces retain both failures and successful verification. This does not claim
full deployment or Staff Management completion.

Final follow-up local gate: Node 24
`pnpm turbo lint type-check test format:check` passed 28/28 tasks, including
1,034 shell tests and 236 shared-UI tests. The build and all 14 production-mode
navigation tests above also passed. Remote CI and release approval remain gates.
