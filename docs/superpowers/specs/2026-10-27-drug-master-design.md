# Drug master

Resolves #476 (slice 1 of 3). Owner: new `drugmaster` module. Consumers: MediCore
prescriptions (#83, prerequisite R2), PharmaConnect verification and dispensing (#169,
#170), SupplyConnect item definition.

## Why

Prescriptions (#83) must name a coded drug, never free text, and must keep the exact
regulatory facts that applied when they were written (IN-PH-01, IN-PH-02). Today the only
drug data is PharmaConnect's per-outlet catalogue (`pharmacy/catalogue.sql`), which has no
schedule and is a shop's stock list, not a prescribing source. #476 asks for one governed
master that every tenant reads, because reference data maintained separately per tenant
diverges, and a divergent schedule or dose is a patient-safety fault.

## What exists (verified in source, 2026-10-27)

- Every table is tenant-scoped. `tenantdb.LintRLS` (`pkg/tenantdb/db.go`) fails boot for a
  table without `tenant_id` and forced RLS, unless the table is in `lintAllowlist` with a
  written reason. Four tables are on it today.
- Dedicated database roles with column-level grants already guard narrow writers:
  `helivanta_practitioner_evidence_worker` (`practitioner/evidence_processing.sql`).
- Tenant-less events exist: `outbox_events.tenant_id` is nullable, and `events.Broadcast`
  fans out to every replica (`pkg/events/broadcast.go`).
- There is no platform-level human authority. Every permission is a tenant role
  (`pkg/authz/authz.go`).
- `pg_trgm` is not installed anywhere. Pharmacy search uses `lower(col) text_pattern_ops`
  prefix indexes.
- The licensed source for drug content is not chosen. The prescriptions spec records this as
  open (R3).

## Decisions

- **D1 One global catalogue, read-only to the app.** The `drugmaster_*` reference tables
  carry no `tenant_id`. Each goes on `lintAllowlist` with its reason. The migration revokes
  `INSERT, UPDATE, DELETE, TRUNCATE` from `hms_app`. Forced RLS gives `hms_app` a read-only
  policy, and only the steward roles (D3) can write. An app bug or injected SQL therefore
  cannot change a schedule. This is the fail-closed direction: no request-path write exists
  to get wrong. The module's tests pin the grants and forced RLS, and prove each policy by
  what each role can and cannot read or write.
- **D2 India only, for now.** Each release has `country = 'IN'`, enforced by a CHECK, the
  same way pharmacy's `india_only.sql` does it. Regulatory class is a closed set taken from
  the Drugs Rules 1945 (source S09 in `docs/domain/india/sources.md`): `none` (OTC), `G`,
  `H`, `H1`, `X`. NDPS control is a separate boolean, never folded into the letter
  (IN-PH-06). Adding a class or a country is a reviewed migration, not data. The S09
  compilation is from 2022, so this set needs a responsible pharmacist to confirm it
  against the current amendments before the first licensed release is approved. Until
  then it is a recorded open item, not a settled fact.
- **D3 Content arrives only as an approved release.** A release is one immutable dataset.
  It records:
  - its source name, licence reference, source version, file sha256, and who staged and
    who approved it.

  `cmd/drugmaster-release stage <file>` runs as the database login
  `helivanta_drug_stager`, which can only insert releases in state `staged` and their
  rows. `… approve <id>` runs as `helivanta_drug_approver`, which can only move a staged
  release to `published` (or `rejected`) and write the outbox row. Neither role inherits
  the other. A trigger rejects approval when the named approver equals the named stager.
  One stolen credential can neither stage and publish nor alter published content. That
  people hold different credentials is an operations control, written in the runbook. It
  is not a database fact.

- **D4 Three levels, all versioned, never rewritten.**
  - `drugmaster_molecules`: INN name and optional ATC code.
  - `drugmaster_formulations`: identity plus effective-dated versions.
    - Each version holds the ordered molecule and strength list (value and unit, so
      combinations work), dose form, route, regulatory class, NDPS flag and
      teleconsult-prescribable flag.
    - Each version also holds `valid_from` and the release that introduced it; there is no
      `valid_to`, because a later version ends it.
  - `drugmaster_products`: brand, manufacturer, pack size and unit of issue, GTIN, HSN code
    and status (`active | discontinued | recalled`), also versioned per release.

  A release that reclassifies a formulation adds a new version with a later `valid_from`.
  The version in force on a date is the latest `valid_from` on or before it, the later
  release winning a tie. No row is ever updated. Published rows are guarded by immutability triggers.
  A prescription stores the formulation **version id**, so a two-year-old H1 prescription
  is read under the rules that applied then.

- **D5 Reads.** All routes are tenant-authenticated under the permission
  `drugmaster.catalogue.read`, granted to doctor, nurse and pharmacist. Every list is
  keyset-paginated.
  - `GET /drugmaster/search?q=&form=&class=` matches the prefix of the INN or brand,
    normalised to lower case and stripped of punctuation. Results group by formulation and
    carry class, NDPS, teleconsult flag and strength.
    - When two results' display names are near-confusable (Levenshtein distance ≤ 2, or
      identical apart from strength), both carry `confusable_with`, so the UI must force a
      deliberate pick.
    - Prefix search uses range predicates over `text_pattern_ops` btree indexes. Unlike a
      parameterised `LIKE`, they work in every plan, and they need no extension. Trigram search is recorded
      under "Not covered".
  - `GET /drugmaster/formulations/:id?at=<date>` returns the version in force at that
    date. A date before the first `valid_from` is 404, so a past audit is never answered
    with today's rules.
  - `GET /drugmaster/products/:id` and `GET /drugmaster/products/by-gtin/:gtin` return
    product status. A recalled product is answered with `status: recalled`, never
    filtered out, because the counter must see why it cannot dispense.
- **D6 Events.** Publishing a release enqueues the tenant-less event
  `helivanta.in.drugmaster.catalogue_published.v1` (release id, sequence number, count of
  changed formulations). Each formulation whose class or NDPS flag changed also gets
  `helivanta.in.drugmaster.formulation_reclassified.v1` (formulation id, new version id,
  old and new class). Each product newly recalled or discontinued gets
  `helivanta.in.drugmaster.product_status_changed.v1` (product id, status, release id).
  Payloads hold reference data only, no PHI. Consumers re-read the catalogue at write
  time. Events only shorten staleness; correctness never depends on them, matching
  `events.Broadcast`'s stated contract.
- **D7 The catalogue ships empty.** No drug content is written from memory (AGENTS.md,
  prescriptions anti-goal). Tests load a synthetic release whose source is named
  `synthetic-test-fixture`, which the release trigger refuses outside the harness's
  `hms_test_N` databases. That fixture
  serves both the release machinery and the 60,000-product search check (D8). Until a
  licensed release is approved, search returns an empty page, and #83 drafts cannot add a
  line. This is the honest state.
- **D8 Proof of the latency target.** A test loads 60,000 synthetic products and asserts that
  p95 over 200 queries through the real handler stays under 120 ms on the test database.
  It logs the measured value (10 ms locally on 2026-10-27). It times the handler rather
  than asserting a query plan: the time is the claim, and a plan is only a proxy for it. It does not
  claim the issue's 100-requests-per-second production figure, which needs a load test
  on real infrastructure.

## Slices

1. **This slice:**
   - the catalogue schema;
   - the stager and approver roles, release machinery and CLI;
   - the read APIs;
   - the events;
   - the archtest pins;
   - the synthetic fixture and latency check;
   - docs and the regenerated contract reference.

   It unblocks #83 drafts (R2) once a licensed release is loaded.

2. **Clinical knowledge, which needs the licensed source chosen:**
   - allergy-class membership;
   - interaction pairs with severity and per-record provenance;
   - dosing constraints;
   - substitution groups with the `auto_substitutable` flag;
   - tenant formulary status (`preferred | restricted | not_stocked`, tenant-scoped, warn
     only).

   This slice supplies #83 R3.

3. **In-app stewardship.** Field-level propose and approve through the API, with four-eyes
   by authenticated subject. It needs a platform-steward authority that does not exist
   (no non-tenant permission today). That authority must be filed and designed first.

## Built differently from the first draft

- **Sealed releases (review C1).** Content can join a release only in the transaction that
  staged it. Publishing re-reads the reviewed file and refuses a release whose rows
  differ. Without this, a stager login could add unreviewed rows after review.
- **Decisions are serialised** by an advisory lock in the release trigger. The trigger, not
  the approver, sets `decided_at` and `sequence`.
- **Identities are reused only when published.** Codes and molecule keys are unique per
  release, and publication keeps published ones unique, so a rejected release binds
  nothing.
- **A future version does not block today.** Only "starts before today" is refused for
  something already published. A later release supersedes an earlier one for the same
  date.
- **Staging is batched.** It loads the published catalogue once and inserts in batches. A
  60,000-product release stages in about 8 s and verifies and publishes in about 2 s
  (local, 2026-10-27).

- **Identity rows** (molecule, formulation, product) become visible once a published
  version uses them, not when their own release is published. A rejected release may have
  created one, and a corrected release reuses it.
- **Event tests read the bus.** The tests check the events that reach subscribers, not
  `outbox_events` rows. The RLS lint allows `outbox_events` only one permissive policy,
  so the approver can write its own events but cannot read them back.

## Not covered

- **No drug content.** Choosing and licensing the Indian drug dataset is a business
  decision (the issue calls it a procurement dependency).
- **No trigram or substring search.** That needs `pg_trgm` approved on the managed Postgres.
- **No Redis cache.** Reads are indexed Postgres. Caching waits until a measurement shows
  the need.
- **No batch-level recall execution or stock.** Those belong to PharmaConnect and
  SupplyConnect.
- **No country other than India.**
- **No stewardship UI.** Release staging and approval is an operator CLI in this slice.
- **No dose units or frequency codes for prescribing** (#83 P4). Strength units are here;
  the frequency vocabulary arrives with slice 2's dosing data.
- **No molecule corrections.** A molecule's ATC code is fixed at first use. Correcting one
  needs molecule versions, which no consumer reads yet.
