# Confirmed Pakistan test-fixture cleanup

Date: 2026-10-25. This is a one-time, explicitly requested cleanup supporting the
India-only release in [the release procedure](remove-pakistan-release.md).
It is not a general data-retention job and must never select production records
merely because their country is Pakistan.

## Classification and scope

The user requested a reviewable cleanup plan for confirmed test data and removal
of Pakistan support while India development continues. Read-only production
inspection found the same 15 blocking rows recorded in
[tesserix/kargo-manifests#60](https://github.com/tesserix/kargo-manifests/pull/60).
The affected pharmacy and supply roots have TEST-coded identifiers. The suspended
country-policy action explicitly identifies itself as fictional software-test
material and uses a test-only citation. These are not real clinical records.

The dependency closure contains 97 rows across 28 tables. This includes fixture
stock, revisions, access rows, licence evidence references and policy decisions.
The pharmacy organization and the referenced India corporate parent, its revision
and corporate structure remain. Historical audit events remain. No GCS objects,
credentials, patient records or unrelated tenant records are deleted.

| Blocking family                        | Original blocker count |
| -------------------------------------- | ---------------------: |
| Country-policy scope                   |                      1 |
| Pharmacy outlet and catalogue product  |                      2 |
| Supply parties                         |                      3 |
| Supply addresses and tax registrations |                      2 |
| Supply premises                        |                      2 |
| Supply licences                        |                      5 |

The exact scoped rows are represented by 28 SHA256 row-set checks in
[the cleanup SQL](../../scripts/ops/2026-10-25-india-test-cleanup.sql). It contains
fixture identifiers and digests, not raw row values, licence numbers, contact
information or credentials. Changes to any captured row abort the transaction.
Any remaining foreign-key dependency also aborts constraint restoration and rolls
back the entire cleanup. Do not weaken these checks to force execution.

## Recovery capture

The protected local recovery directory is:
`/Users/samyakrout/.local/state/helivanta/india-cleanup-20261025` (directory mode
0700; capture files mode 0600). It contains:

- `rows.json`: exact fixture rows and preserved parents.
- `metadata.json`: foreign keys, user-trigger states and RLS metadata.
- `pre-data.sql` and `post-data.sql`: schema-only PostgreSQL dumps, without
  ownership or grants; no database credentials.
- `restore-rows.sql`: explicit-column restore excluding generated columns.
- Rehearsal logs and protected SQL artifacts containing original values.

Keep this directory outside Git. Restrict access, retain it through verification
and the approved recovery window, then follow the retention runbook. Never paste
its contents into issues or logs. Existing backups were also reported complete;
this statement alone is not proof of a whole-cluster restore.

## Execution safeguards

Target: GCP project `tesseracthub-480811`, GKE context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`,
PostgreSQL database `helivanta`. Discovery used account `unidevidp@gmail.com` and
the current primary `helivanta-postgres-2`. Re-resolve the primary before applying;
never hard-code a former primary into an unattended job.

The SQL defaults to **ROLLBACK**. It requires psql `ON_ERROR_STOP`, uses 5-second
lock and 60-second statement timeouts, and locks target/dependent relations before
checking snapshots. The short transaction temporarily removes only the captured
foreign keys and disables non-audit user triggers on targeted tables. It restores
every foreign key (validated) and original trigger state before commit. RLS is
never disabled. The administrator session is required because these are forced
RLS tables. Normal application principals cannot run this operation.

Every removal receives an attributable `test_fixture_cleanup` audit event with
no raw row values. Existing audit triggers remain enabled. No deletion becomes
visible unless all checks and restorations succeed.

Run the checked-in SQL without variables first for a rollback rehearsal, using a
protected output file. Only the explicitly approved cleanup uses `-v apply=true`.
Pipe the file through `kubectl exec -i ... -- psql -X -U postgres -d helivanta
-v ON_ERROR_STOP=1`; do not embed credentials. Never use session_replication_role,
disable all triggers, bypass checks, or rewrite PK/PKR rows as IN/INR.

## Local validation evidence

An isolated `postgres:16-alpine` container named
`helivanta-india-cleanup-rehearsal`, with no network and no published ports,
restored the captured schema and rows. The first restore exposed a preserved
India corporate parent; the final recovery fixture includes that parent and
restores all validated constraints successfully.

Executed operational checks:

- `python3 /tmp/rehearse-helivanta-cleanup.py`: pre-data, exact rows and post-data
  restoration pass, including generated-column handling and validated FKs.
- `python3 /tmp/test-helivanta-cleanup.py`: checked-in SQL rollback and commit
  pass; 97 exact removals, 97 explicit cleanup audits, unchanged foreign-key
  definitions, trigger states and RLS flags, unchanged preserved India parents.
- Workspace, pharmacy and supply `india_only.sql` scripts execute together in a
  local transaction after cleanup and roll back successfully.

These scripts and logs reside in the protected local recovery workspace; they
exercise captured test data and are not a general CI fixture. Release checks must
also run the normal code, contract, race and end-to-end suites.

## Production verification and release gates

After an approved commit, rerun all count queries from the release procedure:
every Pakistan/PKR blocker must be zero. Verify exact cleanup audit counts, parent
preservation, constraints, trigger states and forced RLS against the capture.
Run drift detection and check API readiness; retain pre-existing audit history.
Record the operation result here before lifting the GitOps promotion hold.

Cleanup is not a deployment. Keep Kargo auto-promotion disabled until preflight,
restore evidence, reviewed image selection and compatible Global API upstream
configuration are recorded. The new MediConnect owner key must be added alongside
compatible connector/API images. Do not claim verified patient access: the
current MediConnect release implements accounts and unverified link intake only.

## Applied result — 2026-10-25

Applied the approved confirmed-test cleanup from commit `54ec0da` after the
production rollback rehearsal passed. Artifact SHA256:
`3aab10a523fb73a66a6928905a9f6b6acb46e1d8e97b12d99b252542cfe5b841`.
`python3 /tmp/apply-helivanta-cleanup.py rehearse` and `apply` both passed.
Exactly 97 captured rows were removed and 97 explicit cleanup audit events were
verified. Foreign-key definitions, user-trigger states and RLS flags match the
pre-operation snapshot. The pharmacy organization and India corporate parent
rows match their original captured values. The repeated global India-release
preflight reports zero blockers in all 12 checked tables. No stored objects or
historical audit events were deleted. Recovery artifacts and verification logs
remain in the protected directory above.

This clears the recorded country-data blocker; it does not itself lift the Kargo
hold, apply schema migrations or prove the pending application deployment.
