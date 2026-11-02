# Staff department handoff: local verification

2026-11-02. Local synthetic QA only, through `http://helivanta.localhost:4399`,
Global API 8199 and owner API 8399. No production rollout, migration, credential
rotation, new clinical grant or issue closure. [Owner contract](../api/workforce-department-links.md).

## Normal browser evidence

The existing signed-in synthetic owner used the standalone production bundle and
normal HR setup controls. Business records were not inserted by fixture SQL.

1. Opened the retained retired HR department: history remained readable and no
   mapping form was offered for a new command.
2. Created `SYN-HANDOFF-PHARMA-20261102` as an active HR vocabulary entry, revision
   1. This is a classification, not a Staff/person master or employment activation.
3. Opened **Organization mapping**. An empty submission showed required canonical
   department and mapping-reason errors and made no successful command.
4. Selected the existing Organization Pharma department, revision 4, and entered
   an explicit synthetic reconciliation reason. The owner receipt appeared pending,
   then became **Matched snapshot** after the actual NATS/Organization observation.
5. Reloaded the whole page and recovered the same retained mapping.
6. Edited the HR vocabulary name through its normal form, creating HR revision 2.
   The original mapping stayed matched and was labeled **Historical HR revision**.
7. Explicitly mapped HR revision 2 to the same canonical structural revision. The
   UI retained mapping versions 1 and 2 with distinct HR pins and UTC observation
   times. No Staff posting or permission was created.
8. Rebuilt/reloaded the final production bundle and recovered both receipts. Empty
   form validation now uses plain language for the required reason.
9. An unsaved mapping initially disappeared on a category change. A regression
   reproduced the missing confirmation. The fixed hospital/category/record
   transitions now use the shared navigation guard: the real browser displayed
   **Discard changes to department mapping?**; **Keep editing** preserved the field
   and original category. Closing and discarding that unsaved synthetic draft left
   the two persisted mappings untouched.

Read-only verification under the existing app DB role, exact tenant, hospital and
owner subject found HR version 2, exactly two matched mappings, distinct retained
HR revisions, two immutable Organization observations, two request audits and two
observation audits. Both new migrations are recorded. The existing canonical
pharmacist remains exactly one inactive Staff record. No identity/employment,
payroll, protected file or operational access was altered by these mappings.

Private proof and screenshots are under
`~/.helivanta/staff-management-20261027/shared-staff-foundation-20261102/`:
`department-handoff-ui-persistence-proof.json`, `department-handoff-final-ui.png`,
`department-handoff-required-fields.png` and `department-handoff-navigation-guard.png`.
Private QA JSON gained IDs/evidence metadata atomically with mode 600; all existing
credentials were preserved. No credentials are repository artifacts.

## Reviewer browser acceptance

The existing reviewer signed in through the normal local login, retaining its
Pharmacist and Staff member roles and hospital-reader assignment. HR setup showed
**Read-only configuration**; Add, Edit and Organization mapping controls were
absent. The reviewer opened HR revision history and saw revisions 1 and 2. No
membership, permission, password or business record was changed for this check.
Private screenshots: `department-handoff-reviewer-readonly.png` and
`department-handoff-reviewer-history.png` in the evidence directory above.

This accepts reader HR vocabulary/history and denial of mapping controls. It does
not accept positive read-only mapping access: that API requires
`workforce.master.manage`, which this reviewer does not hold. No broader role was
added solely to satisfy a fixture.

## Automated evidence

Observed RED before implementation: missing retained mapping route (404 versus
200), missing SDK module and missing mapping UI. Further regressions reproduced:

- Audit/outbox failure mislabeled as 400: now generic 500, no surviving request or
  success audit, with read-back 404.
- Lost mutation reply followed by history refresh substituted expected version 1
  and a new command for the original version 0. The unchanged retry now keeps its
  original command identity and intent, even when refreshed history shows a commit.
- Category changes discarded an unsaved mapping. Canceling the shared confirmation
  now keeps the original category and draft fields.
- Architecture checks found raw-map presence validation and an undeclared
  single-receipt pagination exception. Required boolean presence is now represented
  by a canonical Go pointer and nil validation; wire JSON stays an explicit boolean.
  The GET exception describes one exact retained receipt, not a collection bypass.

Real OpenFGA/Postgres/NATS tests cover manager success, reader write denial,
foreign object denial, exact/concurrent/changed retries, pending conflict,
required/unknown/extra fields, SQL identity/version/removal guards, contradictory
or replayed observations, missing explicit false, source revision changes,
same-identity role revocation, authorization outage/recovery and atomic rollback.
The two-owner test uses actual durable events. Its authentication identities are
the explicit harness verifier; the browser uses real local IAM/current authority.
Neither is a claim of new scoped FGA posting relations or lifecycle enforcement.

Commands actually run with cached dependencies/test images, bounded Go concurrency,
`GOTOOLCHAIN=local GOPROXY=off`, and the existing Colima test Docker socket:

```sh
# From backend/
go test -race -timeout=20m ./internal/modules/workforce ./internal/modules/organization ./internal/bootstrap -count=1
go test -race ./internal/archtest -count=1
go vet ./internal/modules/workforce ./internal/modules/organization ./internal/bootstrap ./internal/archtest
golangci-lint run ./internal/modules/workforce/... ./internal/modules/organization/... ./internal/bootstrap/... ./internal/archtest/...
go build ./...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check

# From repository root, Node 24
node scripts/generate-organization-structure.mjs --check
node scripts/staff-operational-access-inventory.mjs --check
node --test scripts/staff-operational-access-inventory.test.mjs
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/api lint
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/shell exec vitest run components/organization components/pharmacy 'app/(workspace)/workforce'
pnpm --filter @helivanta/shell exec vitest run components/organization 'app/(workspace)/workforce/setup/page.test.tsx'
pnpm --filter @helivanta/shell exec eslint components/organization/department-handoff.tsx components/organization/department-handoff.test.tsx 'app/(workspace)/workforce/setup/page.tsx' 'app/(workspace)/workforce/setup/page.test.tsx'
pnpm --filter @helivanta/shell exec next build --webpack
```

Final full owner race runs passed: Workforce 590.079s, Organization 17.427s,
bootstrap 26.395s; full architecture race suite 79.938s. Vet/build passed and Go
lint reported zero issues. API: 588 tests. Broad affected shell: 663 tests before
the final navigation correction; all 14 affected organization/setup tests then
passed on that correction. Typechecks, scoped ESLint, production webpack build,
generator/contractdocs parity and 15 access-inventory tests passed. The final
production bundle was exercised again in the browser. Existing Vite configuration
warnings remain unrelated. Full repository coverage/CI is separate from these
scoped results; pending GitHub checks are not counted as success.

## Additional observation recovery matrix

Real Postgres/registered-consumer tests now cover retained first-answer replay
after the Organization source changes, changed request identity, malformed event
claims, omitted/null/non-boolean answers, mismatched tenant/hospital/master/source
pins, contradictory terminal replies and old replies arriving after newer mapping
versions. Organization audit/outbox faults roll back observations and replies;
Workforce observation-audit faults preserve pending state. Replaying after repair
succeeds and duplicate replies retain one business audit.

Both retained-identity comparisons were temporarily disabled independently. The
corresponding tests failed because a changed identity was accepted; production
code was restored before the green tests and vet/lint checks. Focused race suites
passed (Organization 7.951s; Workforce 9.245s). No production handler or migration
changed in this follow-up.

The CI web failure on `735bd929` was generator Prettier formatting. Formatting was
corrected, including the affected UI test. Missing isolated-worktree dependency
links were restored from an existing cache with identical manifests and lockfile;
no dependencies or test images were downloaded. Fresh
`pnpm turbo lint format:check type-check --concurrency=2 --force` passed all 21
tasks with no cache hits. Scoped Go vet/lint and generator/contractdocs parity
passed. The full follow-up race run passed: Organization 17.593s and Workforce
578.404s, using:

```sh
go test -race -timeout=20m ./internal/modules/organization ./internal/modules/workforce -count=1
go vet ./internal/modules/organization ./internal/modules/workforce
golangci-lint run ./internal/modules/organization/... ./internal/modules/workforce/...
go run ./cmd/contractdocs -check
```

`gofmt -l` on the changed Go tests and `git diff --check` were clean. Gitleaks
scanned all eight changed files with redaction enabled and found no leaks.

## Boundaries still open

Owner/manager and existing reviewer HR read-only browser journeys are accepted
above. Foreign scope has handler/component evidence. Positive read-only mapping
browser access remains unverified. The command matrix additionally passed a
stale HR revision refusal, exact historical receipt retry, authorized-actor
rebinding refusal and concurrent different-key commands committing one pending
version/audit. Removing the actor-binding check makes the regression fail (200
instead of 409); production code was restored before the full race run. The parent
Staff objective still requires actual HR-reference migration consumers, canonical effective postings,
standalone employer scope, Pharma professional/evidence review, assistant/PIC
duties, current lifecycle cutoffs and the Staff audit workspace. Mapping historical
catalog revisions does not complete any of those requirements.

Forward migrations `0070_workforce_department_links` and
`0004_organization_department_observations` are persisted with their owners and
were applied locally only. Rollback retains additive schema/history and removes
exposure; no destructive down migration is supplied. The source ledger has 759
surfaces, 17 classified and 742 unclassified. No operational acceptance was added.
