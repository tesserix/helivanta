# Staff operational access consumer review

2026-10-31. Implements the registered-consumer inventory prerequisite of
[ADR-0049](../adr/0049-reviewed-employment-access.md), A04/A06/A07–A11 and
[the lifecycle plan](../superpowers/plans/2026-10-29-staff-employment-lifecycle.md).
This is source-review tooling. No employment guard, grant classification in a live
database, migration, account change or runtime activation is performed by it.

## Current source findings

The checked registry contains **727 registered surfaces** across module HTTP
routes, async subscriptions, broadcasts and unauthenticated allowlist entries.
Four MediCore encounter routes, ten employment-review routes and three shared
IAM session routes have explicit source-reviewed classifications; **710 remain
unclassified**. No surface has
complete employment-cutoff acceptance. Counts were checked on 2026-11-01 with the
normal and strict classification commands below.
These counts do not measure engineering completion. The three additive unpublished
activation-assignment/source/command routes remain unclassified. Existing module/review
loader classifications were rechecked after their additive registration/private
identity-counter and generation-actor admission changes; no cutoff acceptance was added.

The four encounter handlers (`open`, `list`, `get`, `transition`) all enter
`encounterHandlers.inHospital`. That helper locks `medicore_hospital_access` for
the current tenant, hospital and subject. Registered permissions and that hospital
grant do not establish a reviewed employment generation or its effective cutoff.
The classifications retain the exact route registration, relevant source hashes,
owner scope, rationale and remaining legacy/guard/signed-session/UI obligations.
This finding is limited to those four handlers; it does not certify other
MediCore routes or settle governance/recovery classifications.

The employment-review routes require compound scoped HR-intake
preparation/document authority or the separately reviewed employment-review
permission, plus an independently reviewed active actor link with no legacy
fallback. Decisions/endings preserve frozen records without operational activation. Generation-bound actors now revalidate current approved employment sources;
unbound legacy actors remain unmigrated, alongside
separately reviewed bootstrap/governance semantics and production rollout acceptance.
Normal local preparer/reviewer/scanner UI acceptance is recorded in the
[workspace matrix](2026-11-01-employment-review-ui-acceptance.md). Their source classifications confer no exemption or cutoff acceptance.

The selected-review history route reuses that current authority on every page,
with audited immutable receipts and frozen terms/evidence metadata. It grants no
current capability, private byte release or employment activation. Its source
classification was the tenth reviewed surface. Selected review-evidence release
adds the eleventh: current exact authority and frozen basis on both sides of a
bounded integrity-checked provider read, with audited release and no generic file
or operational authority. The remaining unclassified
surfaces still require review, and every runtime cutoff acceptance remains open.

Hospital capability discovery, minimal recorded-state inbox and the preparer-only
approved-person selector add three source classifications (fourteen total).
Discovery/finding scans retain bounded filtered continuation; inbox/finding data
requires current original authority, reviewed actor and sensitive-read audit.
Finding options revalidate the exact approved receipt and released evidence,
without identity-file release or general identity-review authority. Their narrow
read contexts preserve write/delete restrictions. Local browser acceptance does
not establish actor employment-generation admission or any runtime consumer cutoff.

## Shared session boundary audit — 2026-11-01

Renewal, activity and self sign-out are explicitly **mixed** surfaces. They serve
shared identity sessions used for both governance and hospital operations. No
route gains a lifecycle exemption from this classification.

- Renewal checks the current upstream active identity and tenant role binding,
  accepts only the session cookie and preserves authentication time/idle deadline.
  It has no selected hospital or reviewed employment generation.
- Activity moves the idle deadline while preserving subject, tenant and original
  authentication time. It does not require tenant membership or examine employment.
- Self sign-out remains callable after membership removal and revokes the caller's
  subject globally. Its transaction couples the watermark and invalidation intent;
  local cache invalidation follows commit. Cross-replica cache/broadcast failure
  acceptance remains distinct from hospital employment cutoff.

Consequently, hospital suspension/exit/contract expiry must be checked by the
operational owner using current hospital employment, even when renewal/activity
succeeds. Offboarding must preserve independently valid other-hospital employment
and governance access. Calling global sign-out, disabling the global identity or
relying on session expiry cannot implement ADR-0049. The separate administrator
credential-revocation endpoint still needs its own authority-chain review; these
three classifications do not certify it or any other IAM route.

Source evidence pins exact registrations and handler/authentication files. The
existing IAM/authentication regression suites are verification of session behavior,
not completed lifecycle acceptance. Counts are now **17 classified / 707
unclassified / 724 total**, with **zero full employment-cutoff acceptances**.

The source-review change was checked with the complete IAM and authentication
race suites (`go -C backend test -race -p=1 -timeout=20m
./internal/modules/iam ./pkg/authn`): IAM passed in 349.931s and authentication
in 2.324s. All 15 inventory-tool tests, normal ledger/source checking, affected
Prettier checks and `git diff --check` pass. Strict classification checking
intentionally refuses the 707 remaining surfaces. No application code or schema
changed in this audit.

## Inputs and checks

The canonical registered input remains
[`docs/api/reference/inventory.json`](../api/reference/inventory.json), generated
by `backend/cmd/contractdocs`. Do not copy route lists into a new hand-maintained
master. The sparse review input is
[`staff-operational-access-reviews.json`](staff-operational-access-reviews.json).
The generated ledger is
[`staff-operational-access-inventory.json`](staff-operational-access-inventory.json).

Run from the repository root:

```sh
node --test scripts/staff-operational-access-inventory.test.mjs
node scripts/staff-operational-access-inventory.mjs
node scripts/staff-operational-access-inventory.mjs --check
node scripts/staff-operational-access-inventory.mjs --check --require-classified
```

The last command currently refuses classification completion with **707
unclassified surfaces**. Ordinary CI checks ledger/source drift and tests the
checker; it does not require an unfinished classification to pass. The strict
classification check is a prerequisite, never sufficient evidence to activate
employment enforcement. It is not wired as a runtime authorization mechanism.

No role, permission prefix, route path, `public` marker, missing tenant membership,
or worker registration automatically supplies a classification or exemption.
Each review names an explicit authority kind: hospital operational,
governance/recovery, patient/self, system continuation, public entry or mixed.
Mixed operations require further owner analysis rather than a blanket bypass.

Source or registration changes invalidate an existing review before output is
written. Re-read the affected handler and authority chain before updating its
hashes. Removed surfaces, duplicate reviews, duplicate HTTP registrations and
unknown registry fields fail checking. New registered surfaces remain explicitly
unclassified. The checker records remaining acceptance work and rejects an
`accepted` marker; source classification cannot promote a route to runtime-ready.

## Coverage outside the registry

The ledger is exhaustive for its registered input, not every possible execution
path. This source search also found background entry points needing separate
activation and authority review:

- `billing.RunBedCensus` in `backend/internal/modules/billing/bed_days.go`.
- `payments.RunReconciler` in `backend/internal/modules/payments/reconciler.go`.
- `workforce.DocumentWorkflow` / `DocumentProcessor.ProcessActivity` in
  `backend/internal/modules/workforce/document_workflow.go`.

These are source candidates, not a complete background-work inventory or proof
that each is enabled in the current deployment. Trace bootstrap/worker wiring,
Temporal workflows/activities, periodic deliveries, recovery paths and external
integrations before calling the operational-consumer inventory complete. System
completion of an already authorized action needs its own authority semantics;
a separated initiator must not automatically block legitimate settlement or
silently authorize a new command.

Reviewed employment submission/decision and exact person-finding consumption,
legacy/governance review, authoritative effective-time guards, lifecycle commands,
all owner cutoff tests, real provider/scanner acceptance and Staff Admin → reviewer
→ recipient browser journeys remain open. Existing local migration approval is
separate; this tooling does not apply migrations or switch the QA runtime.
