# Staff operational access consumer review

2026-10-31. Implements the registered-consumer inventory prerequisite of
[ADR-0049](../adr/0049-reviewed-employment-access.md), A04/A06/A07–A11 and
[the lifecycle plan](../superpowers/plans/2026-10-29-staff-employment-lifecycle.md).
This is source-review tooling. No employment guard, grant classification in a live
database, migration, account change or runtime activation is performed by it.

## Current source findings

The current registry contains 654 module HTTP routes, 43 async subscriptions,
one broadcast and ten unauthenticated allowlist entries: 708 registered surfaces.
Four MediCore encounter routes and five employment-review routes have explicit
source-reviewed classifications; 699 remain unclassified. No surface has complete employment-cutoff acceptance.
These counts do not measure engineering completion.

The four encounter handlers (`open`, `list`, `get`, `transition`) all enter
`encounterHandlers.inHospital`. That helper locks `medicore_hospital_access` for
the current tenant, hospital and subject. Registered permissions and that hospital
grant do not establish a reviewed employment generation or its effective cutoff.
The classifications retain the exact route registration, relevant source hashes,
owner scope, rationale and remaining legacy/guard/signed-session/UI obligations.
This finding is limited to those four handlers; it does not certify other
MediCore routes or settle governance/recovery classifications.

The five employment-review routes require compound scoped HR-intake
preparation/document authority or the separately reviewed employment-review
permission, plus an independently reviewed active actor link with no legacy
fallback. Decisions/endings preserve frozen records without operational activation. Actor reviewed-employment admission remains unfinished, alongside
separately reviewed bootstrap/governance semantics and normal reviewer/UI/provider
acceptance. Their source classifications confer no exemption or cutoff acceptance.

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

The last command currently refuses classification completion with **699
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
