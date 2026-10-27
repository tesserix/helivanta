# Drug master slice 1 implementation plan

**Goal:** one global, read-only, versioned drug catalogue, which content reaches only
through a two-person release.

**Spec:** `docs/superpowers/specs/2026-10-27-drug-master-design.md` (#476).

**Architecture:**

- A new `drugmaster` module holds reference tables with no `tenant_id`.
- `hms_app` can only SELECT, and only rows of published releases.
- Two NOINHERIT roles write:
  - `helivanta_drug_stager` inserts staged content;
  - `helivanta_drug_approver` decides releases and writes the outbox.
- Every content row is insert-only. The version in force on a date is the latest published
  version whose `valid_from` is on or before that date, so nothing is ever updated.

## Global constraints

- Migration `0001_drugmaster`. India only (`country='IN'`).
- Regulatory class is one of `none|G|H|H1|X`. NDPS is a separate boolean.
- Release source `synthetic-test-fixture` is refused unless
  `current_database() LIKE 'hms\_test\_%'`.
- `approved_by <> staged_by`, enforced by a CHECK and a trigger.
- Read permission: `drugmaster.catalogue.read` for doctor, nurse and pharmacist.
- Event subjects:
  - `helivanta.in.drugmaster.catalogue_published.v1`
  - `….formulation_reclassified.v1`
  - `….product_status_changed.v1`

## Review focus

1. A staged, unapproved release is invisible to `hms_app`. Test: read before and after
   approval.
2. A second release that reclassifies a formulation leaves the old version answerable for
   past dates. Test: `?at=` before and after the new `valid_from`.
3. A published release cannot be appended to or altered by any role, owner included. Test:
   insert, update and delete as stager, approver and owner each raise an error.
4. The same person cannot stage and approve one release. Test: approval with
   `approved_by = staged_by` fails and the release stays `staged`.
5. A recalled product is still returned by GTIN, with `status: recalled`. Test: a by-GTIN
   read after a status-change release.

## Tasks

1. **Schema** (`drugmaster.sql`, `module.go`, `contract/events.go`).
   - Tables:
     - `releases`;
     - `molecules` (unique normalised INN);
     - `formulations` and `formulation_versions`, unique on `(formulation_id, valid_from)`;
     - `formulation_ingredients`, with numerator and optional denominator strength in units
       `mg|g|mcg|IU|mL|%`;
     - `products` and `product_versions`, with GTIN (GS1 check digit validated in Go;
       pattern in SQL) and HSN of 4, 6 or 8 digits.
   - Vocabularies:
     - closed dose-form and route sets;
     - formulation and product identity is the source's stable `code`.
   - Access:
     - grants, forced RLS and per-role policies;
     - immutability triggers.
   - Rows also go on `tenantdb.lintAllowlist`, each with a reason.
   - Test: `schema_test.go` covers the grants (`has_table_privilege`), the immutability
     triggers and the synthetic guard.
2. **Release staging and approval** (`release.go`, `cmd/drugmaster-release`).
   - `Stage(ctx, db, Release, stagedBy) (uuid, error)` validates the file and inserts only
     changed versions.
   - `Decide(ctx, db, id, approvedBy, approve bool, reason)` publishes or rejects. On
     publish it enqueues the events in the same transaction.
   - Roles go in `dev/init-db.sql` and `testinfra/containers.go`, with DSN helpers.
   - Test: `release_test.go` covers review focus 1, 3 and 4 and the event rows written.
3. **Reads** (`reads.go`, routes).
   - Search (≥2 normalised characters, at most 20 results, confusable flags).
   - Formulation `?at=`, defaulting to today in `Asia/Kolkata`.
   - Product by id and by GTIN.
   - Add the routes to `unpaginatedGETAllowlist`, register the module in bootstrap and
     archtest, add the matrix entry, and map the Global API owner `drugmaster → platform`.
   - Test: `reads_test.go` covers review focus 2 and 5, the confusable pairs, the short
     query 400 and the 404 before first `valid_from`.
4. **Latency check.** Test: `search_latency_test.go` loads 60k synthetic products and asserts
   p95 < 120 ms over 200 handler queries. It is skipped under `-short` and logs the
   measured p95.
5. **Docs and contracts.**
   - Regenerate `contractdocs`.
   - Add a runbook section (stager and approver custody, release file format).
   - Prepare the tesserix-k8s managed roles (NOLOGIN) PR. It must merge before this one
     deploys.
