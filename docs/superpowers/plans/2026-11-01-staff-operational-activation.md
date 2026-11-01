# Staff operational activation delivery

Design: [reviewed operational activation](../specs/2026-11-01-staff-operational-activation.md).
Do not mark operational activation complete from a directory flag, template or
passing classification tool. Production remains on the separately approved
`7e5b7800` revision until a new named rollout is approved.

- [x] Trace reviewed employment, the existing epoch trigger, login-link checks,
      current scoped actors and IAM guarded transactions. Record the explicit
      source-consumption and legacy/governance boundaries in the design.
- [x] Observe the missing separate activation catalog fail through IAM's public
      HTTP interface (400 instead of 200).
- [x] Add a closed activation catalog and append-only IAM 0022 template migration;
      preserve every published older permission bundle and catalog. Observe the
      same HTTP/live-SQL regression pass.
- [x] Finish template assignment owner/SDK/Staff Access UI and denial coverage;
      new activation authority must itself receive independent assignment review.
- [x] Introduce canonical activation inputs, immutable generation/consumption
      receipts and their scoped owner API. Prove exact source/version checks,
      self/foreign-scope denial, replay and atomic failures.
- [ ] Complete deterministic source-consumption and lock-order concurrency races.
- [x] Preserve source epoch lineage across the Staff transition. Require fresh
      generation-bound digital links/assignments and disallow non-digital fallback.
- [ ] Make generation effectiveness and effective-time cutoff authoritative at
      every affected operational consumer, including external-read completion.
- [ ] Complete attributable legacy migration and explicit governance/recovery
      classifications before enabling enforcement; retain other-hospital access.
- [ ] Add normal Staff Admin/independent reviewer/recipient UI, required fields,
      immutable history, uncertain-command replay and private-data cutoff.
- [ ] Run real scanner/provider local browser journeys, signed Global API races,
      full affected tests, generators/contracts, typechecks, builds and linters.
- [ ] Reconcile progress, exact migration manifest, remaining owner cutoffs and
      issue acceptance. Commit/push/merge only verified changes; obtain specific
      production approval for the resulting revision and migrations.

The catalog regression also caught a missing registration: a generated public
template alone returned a null live SQL permission bundle until the forward
migration was wired. Both the HTTP projection and database declaration now pass;
the catalog alone is authority preparation; generation implementation is recorded below.

## Local implementation checkpoint — 2026-11-01

Backend source consumption, immutable generation receipts, IAM basis/proposal
bindings, generation-bound login enforcement and activation UI are implemented.
The signed Global API regression now passes activation, exact retry, changed
intent, duplicate consumption, UUIDv4 Staff IDs, identity restoration (including
before terms submission), actor invalidation and audit/outbox rollback scenarios.
The earlier missing PUT/404 and nullable wire-date failures were red checkpoints,
not current blockers. Canonical nullable dates and mandatory examination now match
Go and Zod. Reopening a consumed approval reports `consumed`.

The IAM hospital employment gate is acquired before credential/proposal/product
locks: shared for ordinary scoped commands, exclusive for activation. Workforce
guarded commands acquire supervision before actor/source rows. Deterministic
recipient/activation and reciprocal-denial race acceptance remains outstanding.

Release controls:

- `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED` defaults false and gates source GET
  and activation PUT with authenticated 503. It never disables current-generation
  enforcement. Production must stay disabled while the open gates remain.
- Candidate migrations: IAM 0022 activation templates, IAM 0023 employment bases,
  Workforce 0066 generations/identity counters/login bindings. They are
  forward-only; immutable evidence is retained. No production application claimed.
- Full API suite: 548 tests / 71 files. Full shell: 1,351 tests / 167 files.
  Focused UI after lint corrections: 12 tests / 2 files. API/shell typechecks/lint,
  backend build/vet/scoped lint, shell build and contractdocs generation/check pass.
  IAM race: 365.913s; Global API race: 123.458s; tenantdb VM race: 16.443s.
  The complete Workforce VM race rerun passed in 434.152s.
- Normal local non-digital browser approval, activation, mandatory-field rejection,
  consumed-source reopening and grant-withdrawal private-data cutoff pass. Fresh
  submission setup was an explicit API fixture over prior UI-created records.
  Digital-recipient/full UI-source acceptance remains open.
- The first broad tenantdb run hit an unrelated host-app port collision before
  assertions. It is not acceptance evidence. Keep all failing output in private
  QA evidence and rerun the complete affected package.

Remaining: digital-recipient/full UI-source browser acceptance, deterministic
lock races, explicit legacy/governance cohorts, every affected owner cutoff and
suspension/exit/contract-expiry/rehire acceptance. Passing API tests and a disabled
preparation release do not close operational activation or any whole Staff area.
