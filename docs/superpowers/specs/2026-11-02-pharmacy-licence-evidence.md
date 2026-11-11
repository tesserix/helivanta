# Outlet licence evidence and renewal advice (#1391)

Status: implementation design; this document does not assert deployed behavior.

Rollout is split deliberately: first deliver the private-file routes, independent
worker processing and restricted storage/database boundaries; then enforce the
new-review gate and deliver its UI/renewal advice with updated browser fixtures.
The processing slice alone does not close #1391 or establish licence approval.

PharmaConnect owns outlet licence declarations and independent decisions. Private
file routes and worker processing shipped in #1528; this second slice adds the
new-review gate and licence entry/review UI. SupplyConnect already has private
licence images and dated renewal advice. No Supply tables or business handlers
become Pharmacy dependencies.

## Scope and authority

All commands use the Global API and the verified tenant, pharmacy and outlet.
The server rechecks the existing outlet grant for every command and download.
Existing `pharmacy.licence.manage` and `.review` permissions remain the write
authority. Add `pharmacy.licence.evidence.read` for the existing Pharmacy owner
role, alongside the outlet read grant. Uploading evidence does not approve a
licence. The current four-eyes
database guard remains mandatory. New evidence is Pharmacy-owned private data,
not a prescription, patient record or general document-library item.

Relevant criteria are IN-PH-06 and IN-PH-10. Renewal advice is an operational
reminder, not a legal renewal deadline or an assertion of operating authority.
This change introduces no new drug classification, retention period or licence
eligibility rule.

## Evidence lifecycle

Each immutable licence declaration can retain one evidence file. Reuse the
existing Pharmacy/Supply private-file protocol: attributable intent, exact
size/checksum upload, quarantine, generation-bound scan and guarded release.
Accept passive PNG/JPEG images up to 10 MiB, matching the actual shared content
inspector. Do not claim PDF support. A rejected file is retained; a corrected
document belongs to a new licence declaration, consistent with the existing
rejected-declaration correction model. Dependency errors permit an attributable
new scan attempt for the same immutable bytes.

Keep a distinct licence-evidence table with a foreign key to the exact tenant,
outlet and licence. Do not fabricate a prescription to reuse its image table.
File paths use the existing `pharmacy_outlet` owner prefix, with the licence ID
as record ID, under a separate `k/licence` record namespace. Identical
caller-selected licence/prescription/file UUIDs must never alias an object.
Existing prescription keys remain unchanged. The owner APIs never expose bucket
paths, raw hashes, credentials or signed download URLs.

The deployed Pharmacy evidence worker gets explicit access to this table and
new versioned scan workflow/activity declarations. It keeps its own credential,
NOINHERIT role, bounded processing and existing storage/scanner admissions.
Workflow input and events contain only opaque owner IDs and the scan attempt;
filenames, licence details and file content stay out of durable history.

## Review and compatibility

New independent reviews require available evidence for the exact declaration.
Enforce this both in the owner handler and an INSERT trigger. Link the immutable
review to the exact evidence file/generation; file release cannot be fabricated
by the application role. Upload and review serialize on the declaration so that
concurrent commands cannot change the reviewed evidence.

Preserve existing decisions and their historical operating-date interpretation.
Do not retroactively invalidate previously approved declarations. Missing evidence
for a new review produces an actionable refusal; unavailable scanning never
becomes an approval. Keep current route semantics and retain completed history.

New evidence inputs and published consumer contracts get canonical definitions.
Update owner/schema references, SDK expectations, Global API onboarding inventory
and generated route/permission/event/migration documentation together.

## UI and renewal advice

Add an outlet-scoped licence panel to Pharmacy setup: declaration entry, image
upload, scan status/retry, authorized download and independent review. Do not show
the reviewer action to the declaration author. The server remains authoritative
if UI gates are bypassed. Show rejected/unavailable states and correction steps.

Reuse Supply's existing 180/90/30-day advisory bands, expires-today, expired and
not-yet-valid states using the outlet timezone and an explicit observation time.
These are advisory bands, not new statutory deadlines. Preserve invoice/review
snapshots; calculate renewal advice only on reads. Return source-unavailable
instead of inventing a date when the owner timezone/date context is unavailable.

## Verification and rollout gates

Prove absent, pending, rejected, scan-error, wrong-outlet and wrong-tenant evidence
cannot authorize a new review, including direct SQL bypass and concurrent writes.
Exercise changed-body replay refusal, exact-byte retry, dependency failure and
worker-only release. Verify download grants and read auditing. Test advisory
boundaries and local-midnight behavior against Supply's established cases.

Update the browser fixture to upload and scan real synthetic licence evidence
before its second actor reviews. Do not insert available rows or bypass the new
gate. Verify the complete setup, review and sale/payment/return journey through
the Global API. Run canonical generators, contractdocs/check and affected tests,
lint and builds. The deployed worker needs the new processing artifact before
new-review enforcement is activated; deployment readiness alone is not synthetic
scan acceptance.
