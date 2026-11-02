# Pharma shared Staff source — local acceptance

Date: 2026-11-02. Source branch: `feat/shared-staff-foundation-20261102`,
based on `40787523`. Local UI: `http://helivanta.localhost:4399/pharmacy/staff`.
Owner business requests enter Global API on port 8199. Production is unchanged.
This is acceptance of the common Staff source consumer, not pharmacist-account
story #418, department postings or dispensing authority.

## Actual browser journey

1. Fresh owner sign-in opened Pharma's Staff source. Nine existing canonical
   hospital Staff records appeared through the shared `StaffDirectory`, without
   HR edit, review, approval or dispensing controls in the Pharma table.
2. The normal “Manage records in Staff” link opened the Staff owner. Submitting
   empty onboarding showed full-name and employee-code validation errors.
3. The normal UI created `SYN-SHARED-PHARMA-20261102`, category `pharmacist`,
   employment state `inactive`. No identity evidence, employment approval,
   activation, login link, outlet grant or pharmacist verification was added.
4. Returning to Pharma and filtering by that code showed the saved canonical
   pharmacist as inactive. No second Staff record was created for Pharma.
5. Returning through the same management link changed its name to
   “Synthetic Shared Pharma Applicant Updated 20261102”. Pharma displayed that
   saved change after navigation. Read-only owner verification found exactly one
   employee code and the same canonical record at version 2, still inactive.
6. A separate local synthetic account was created as a setup fixture with the
   existing `pharmacist` membership role and no hospital/outlet/Staff grants.
   It signed in through the normal browser login. Pharma's Staff source showed
   no accessible hospital, no Staff table and none of the applicant's details.
   Source access was independently confirmed empty through the owner API.
7. The restricted session was signed out after testing. Credentials remain only
   in mode-600 private QA JSON for future local acceptance; no production account
   or grant was changed. The inactive synthetic Staff record is retained.
8. A follow-up browser check confirmed the no-match filter message, then a full
   page reload recovered the same updated canonical record and inactive state.

Private artifacts are under
`~/.helivanta/staff-management-20261027/shared-staff-foundation-20261102/`:
canonical owner proof, positive and denied screenshots, the isolated local QA
identity, runtime information and test logs. Credentials are not repository data.
No test file or dependency download was needed.

## Automated evidence

Observed red before implementation: missing Staff list loader, missing Pharma
consumer, missing hospital discovery loader, new navigation entry, and a valid
uppercase UUID rejected as a different source. Each became green. Existing
navigation catalog assertions were updated to include the new explicitly
permitted Staff-source entry; no access assertion was removed.

Commands actually run from the worktree with cached dependencies and Node 24:

```sh
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/ui test
pnpm --filter @helivanta/shell exec vitest run components/pharmacy 'app/(workspace)/workforce'
pnpm --filter @helivanta/shell exec vitest run components/pharmacy/staff-source.test.tsx
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/api lint
pnpm --filter @helivanta/ui lint
pnpm --filter @helivanta/shell exec eslint components/pharmacy/staff-source.tsx components/pharmacy/staff-source.test.tsx 'app/(workspace)/pharmacy/staff/page.tsx'
pnpm --filter @helivanta/shell exec next build --webpack
```

Results: 566 API tests; 278 shared UI tests; 654 tests across 70 Pharmacy/Workforce
files. The ten new Pharma component tests also passed after the final SDK UUID
normalization. API/shell typechecks, lint and webpack production build passed.
The build retains existing React Hook Form server-export and middleware
convention warnings; this change does not migrate those unrelated APIs.

Tests cover exact tenant/subject assertion headers and Global API paths, no-store,
abort forwarding, invalid/foreign/duplicate/private-field records, bounded pages,
cursor consistency, normalized UUID scope, owner refusals, missing pharmacy
permission, pharmacy-only access, unknown source scope, empty-page continuation,
hospital changes and removal of retained Staff after 403/404/session-change.

No backend route, permission, schema, migration or event changes in this UI slice;
contractdocs regeneration is not required for a UI-only consumer. Existing owner
handlers remain the authority. Source discovery and record reads poll through
bounded TanStack queries, fail closed on unavailable/changed authority and do not
persist private rows between account scopes.

The organization foundation is now committed in the same branch and applied to
the local synthetic QA runtime; see its [separate acceptance record](2026-11-02-organization-departments-local.md)
and [unreleased contract](../api/organization.md). It remains undeployed to
production and is not covered by this Staff-source UI acceptance.

## Existing Pharma authorization boundary — real OpenFGA

`TestCatalogueRequiresRealOpenFGAAndCurrentOutletGrant` exercises actual Pharmacy
routes with the real OpenFGA client/server, real Postgres and the shared permission
and membership middleware. The harness supplies authentication identities; it does
not replace signed-session IAM/current-authorization reconciliation acceptance.
No static permission resolver or always-member replacement supplies authority.

The test verifies:

- Pharmacist and owner catalogue capabilities work only with the exact outlet
  grant. Inventory can read, but even an outlet editor grant cannot confer the
  missing OpenFGA catalogue-write capability.
- Same-tenant unposted and foreign-tenant pharmacists get 404 for reads/writes;
  changing the outlet ID does not borrow the original grant.
- Pharmacy ownership does not disclose prescriptions (403); the pharmacist's
  independently permitted and scoped prescription list succeeds.
- Removing the pharmacist role denies reads, writes and exact retries (403) on
  the same authenticated identity. An independent Staff membership remains, so
  the test distinguishes lost capability from lost tenant membership.
- Restoring the FGA role does not override a separately revoked outlet grant
  (404). Explicit outlet restoration restores access.
- An OpenFGA outage after successful requests returns 503 for catalogue reads,
  writes and prescription reads. Recovery permits an exact retained retry.
- Final tenant/outlet-scoped database verification finds exactly one product,
  revision, catalogue audit event and catalogue outbox event. Refusals and exact
  retries do not create a second successful business change.

Mutation proof: temporarily substituting catalogue-read for catalogue-write on
the PUT route made the inventory-denial assertion fail with actual 200 versus
expected 403. The original route permission was restored immediately. The
focused race run then passed (`go test -race ./internal/modules/pharmacy -run
TestCatalogueRequiresRealOpenFGAAndCurrentOutletGrant -count=1`). An initial test
assumed DELETE returned 204; source and response establish its existing 200
receipt. The final SQL verification also needs the normal outlet session settings
because product RLS correctly hides rows without them; the test was corrected,
without weakening the handler or database policies.

This adds acceptance for existing OpenFGA-plus-SQL guards, not an outlet-grant
migration into FGA, a new permission, a model change, professional verification
or employment cutoff. No production data, policy or credentials changed. Cached
test images and dependencies were reused. The local production UI was also
reloaded: the canonical department retains four revisions and displays explicit
UTC timestamps; its counter remains attached to the canonical parent.

Final checks from `backend/`, using cached dependencies and test images:

```sh
go test -race ./internal/modules/pharmacy -count=1
go vet ./internal/modules/pharmacy
golangci-lint run ./internal/modules/pharmacy/...
go build ./...
```

All passed; the full Pharma race suite took 203.057 seconds and lint reported
zero issues. `gofmt -l` on the added test and `git diff --check` were clean.
No runtime route, grant, payload, schema or migration changed in this follow-up,
so generated contracts remain unchanged. The current local production bundle
also showed the same inactive `SYN-SHARED-PHARMA-20261102` after navigating back
to Pharma's Staff source. Browser screenshots and test logs are retained in the
private evidence directory above. GitHub-wide Go/E2E checks were still pending
at this checkpoint; the web check passed. Local results do not claim CI success.

## Remaining objective

Live GitHub stories [#417](https://github.com/tesserix/helivanta/issues/417) and
[#418](https://github.com/tesserix/helivanta/issues/418) were rechecked on
2026-11-02; both remain open. The Pharma story contains proposed schema/API/legal
assertions, not an accepted current implementation or activated legal policy.
Its product-owned specialization must use the canonical Staff reference and
current primary-source review rather than copying its draft SQL into production.

| Pharma Staff requirement | Local evidence and outstanding work |
| --- | --- |
| One canonical Staff source | UI create/edit propagation and reload persistence passed for one inactive pharmacist. |
| Independent source authorization | Pharmacy-only identity without Staff/hospital grants sees no source or Staff table. |
| Council registration and protected evidence | Not delivered by this consumer; professional profile, evidence and independent verification remain open. |
| Outlet posting and pharmacist-in-charge | Not delivered; no outlet assignment is implied by the directory. |
| Assistant versus verifier capabilities | No new capability is granted here; product-owned positive/negative operational journeys remain open. |
| Expiry, suspension and employment cutoff | Current product enforcement, retained-session and boundary/race acceptance remain open. |
| OpenFGA projection and recovery | No new operational relationship is created; projection/reconciliation acceptance remains open. |
| Lifecycle events and monitoring | Source browsing publishes no capability event; owner events, expiry monitoring and recovery remain open. |

The source is read-only and does not prove employment eligibility, pharmacy
posting, council registration, pharmacist-in-charge coverage or dispensing
permission. It is not a filtered claim that all shown Staff work at that pharmacy.
The canonical department owner, migration of HR department revision references,
Workforce postings, current lifecycle enforcement and Pharma specialization
remain open in the [complete plan](../superpowers/plans/2026-11-02-reusable-staff-foundation.md).
Standalone pharmacy Staff ownership also requires an explicit employer-scope
contract; no hospital is fabricated for it. No issue is closed by this acceptance.
