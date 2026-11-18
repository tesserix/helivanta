# #1706 implementation and acceptance plan

1. Reproduce the root/workspace raw-message disclosure with synthetic sensitive
   markers. Replace the old workspace test expectation because the accepted
   behaviour now forbids raw error text. Observe the regressions fail.
2. Implement the shared bounded report contract and browser reporter with real
   transport-boundary tests: sanitisation, same-reference correlation, duplicate
   observations, fixed allowlists, failed logging/fetch, abort and network budget.
   Preserve CSP-compatible startup validation and install runtime/rejection
   listeners without suppressing browser behaviour.
3. Implement the provider-independent safe screen and root/workspace/global
   boundaries. Test generic copy, reference correlation, retry, accessible
   landmarks, changed-error references and reporting failure.
4. Extract/test the bounded body reader; preserve CSP tests. Add the operational
   collector with strict input/origin checks, bounded streaming/deadline,
   duplicate/conflict handling, capacity limits and safe structured log bytes.
   Test private/unknown fields, foreign origin, spoofed forwarding headers,
   malformed/oversize/stalled bodies, exhaustion and dependency failures.
5. Document the exact web-owned contract and operator log lookup/fallback
   procedure. Update affected frontend/schema/contract references. No business
   route, permission, migration or canonical Go model changes are intended.
6. Extend hosted production-Shell browser coverage to trigger a synthetic render
   failure, exercise the actual boundary and collector, correlate UI reference
   with log bytes, prove marker exclusion and retry. Do not expose a production
   test-only failure route. Keep local work to tests/type checks/builds; start no
   local application, database or container.
7. Run affected full suites, lint/format/type checks, a local production build
   using Node 24, contractdocs generation/check and applicable generator checks.
   Remove only generated task build artifacts. Resolve git identity from origin,
   commit/push an issue-specific PR, and wait for all required CI before merge.
8. Release the exact merged commit through the normal signed-image/Kargo flow.
   Verify active cloud identity/target before mutation, exact image digests,
   successful promotion/migration, all three workloads 2/2 and Argo health.
9. Verify the deployed UI in the authorised browser session with a browser-only
   synthetic fault. Prove the same reference in safe operator-visible logs,
   restore the browser and verify retry. Record evidence before closing #1706 or
   proceeding to the next milestone issue.

No patient/business data, staff grants, credentials or MFA settings are changed by
this issue. Production alert routing, retention, backup drills and legal/process
acceptance remain with their own milestone issues.
