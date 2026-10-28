# ADR-0048: Staff identity evidence and review authority

Status: implementation design, 2026-10-28. Related: #1399, #417, #1053,
ORG-AC-01/02/04/05/06/07/08 and the Staff lifecycle research. This does not
claim card issuance, professional registration or deployed verification.

## Owners and authority

Workforce owns identity/background/photo review cases for its existing staff
UUIDs. It must not create another person master or require a particular national
identifier. Existing protected Workforce documents own the evidence bytes and
scan/release metadata. IAM owns assignments, current membership, revocation and
OpenFGA projection. Clinical privilege and employment decisions stay with their
owners; a failed background review must not automatically terminate employment.

Extend shared IAM's supported hospital templates with identity-check manager and
reviewer templates. Each grants only its declared permission. They use the same
independent governance review, owner verification, projection acknowledgment,
membership/credential epochs and transaction guard as leave review. No new
module-private assignment store. New case operations require an explicit current
scoped grant even when the caller has a legacy tenant-admin permission.

Both roles require the actor's current active staff/account binding at that
hospital. Managers may prepare and edit drafts, provide evidence, submit and
withdraw their own submissions. Reviewers may inspect the relevant evidence,
approve/reject and revoke, but cannot decide their own proposal or a record
historically linked to their login. Every disclosure and command rechecks scope,
membership, binding, employment and source state at the owner transaction boundary.
Case review never grants a role, activates employment or establishes clinical fitness.

## Sources and states

Kinds are identity, background and photograph. Record a mandatory policy reference,
check/reference description, observed date and explicit validity instant. This is
an attributable human review under the organization’s selected policy, not an
automated government/provider verification. Unsupported external integrations do
not produce success. Sensitive identifiers belong in protected evidence, not
events or card print fields.

Create an internal employment generation on the existing Staff row. Its initial
value identifies the currently observed record, not an invented hire date or
approved employment history. The database advances it irreversibly on employment
status changes. A separate fingerprint binds the name fields, employee code and
date of birth used by identity review; unrelated contact edits do not invalidate
identity. Cases freeze both source values. Inactivation makes a case ineffective;
reactivation cannot restore a case from an earlier generation. Formal employment
periods and full cross-owner offboarding remain separate required work.

Drafts are editable with optimistic concurrency and immutable revision history.
Submission freezes the source and evidence revisions. Decisions are independent;
approved, rejected, withdrawn and revoked outcomes remain retained. Expired,
changed-source, unavailable/revoked-evidence and inactive-employment outcomes are
explicit effective states, never disguised as verified. Mutation receipts identify
the immutable command result; current validity comes from the current-state read.
Retries reauthorize and return the original receipt for an exact command ID/intent;
conflicting intent, stale versions and illegal transitions fail.

Evidence must match the exact tenant, hospital, staff and purpose, be scan-released,
and retain its byte/generation lineage. Photograph evidence is a bounded decoded
PNG/JPEG with explicit dimensions and human quality review, not biometric matching.
Evidence binding/classification cannot change under a submitted review. Security
revocation remains available and invalidates dependent use. Storage/scanning
unavailability is pending/unavailable, not a completed check. Use the existing
document scanner, generation pins, retry behavior and private disclosure path.

An active linked staff member may supply a requested photograph through a separate
self-only path, within a manager-declared submission window. They cannot edit
policy/HR review facts, browse other cases, see confidential background details,
or approve the result. Managers can collect evidence for a joiner before login
binding; that never creates login or business authority.

## Transport, bounds and rollout

Canonical Go/Zod inputs and supported template metadata have one generator source.
Keep existing leave-template behavior and strict denial of arbitrary role or
permission names. Filter/identify template families explicitly so older leave-only
consumers do not start parsing new HR proposals unintentionally. New templates
cannot become active before their projection is acknowledged; partial rolling
deployment remains denied/pending. Use additive migrations; never rewrite history.

Commands are at most 16 KiB, evidence sets at most ten files (exactly one photo),
and pages at most 200 with stable cursors. Existing file limits remain enforced.
Use the current five-second command budget, bounded private storage operations,
indexed tenant/hospital/case and pending-review queries, and one metadata-only
outbox event per committed command. No reason text, name, identifier, photograph
or storage location appears in events. Record attributable sensitive reads too.

At a planning envelope of 100 hospitals × 1,000 staff × three cases, current heads
are 300,000 rows; six retained transitions each are 1.8 million revisions before
renewals. These are sizing assumptions, not measured SLOs. Test representative
query plans and storage/scan dependencies before capacity claims. No new service,
queue or cache is needed for case review.

Rollout must include compatible IAM API/consumers and Workforce API/scanner before
exposing HR templates, with named migration/production approval and deployed UI
verification. Keep proposed grants and rendering unavailable until their actual
dependencies are ready. Reverting UI must not erase source generations, histories,
denials or accepted review evidence.

## Card issuance still required

#1399 is complete only after a separate card workflow uses current approved
identity/photo (and applicable reviewed background policy), frozen source/template/
branding revisions, issuer and independent approver, explicit validity and private
print/issue artifacts. It also needs loss, reissue, expiry, revocation, offboarding
and rehire behavior plus physical-access acknowledgments where integrated. A photo
upload or reviewed case alone is not a card or an end-to-end Staff completion.

## Source preparation read

The implemented source read is available to either a current hospital identity
manager or identity reviewer. Both need the current reviewed Staff/login binding.
The route uses normal authenticated tenant admission; Workforce checks this union
through IAM and holds the chosen grant through the read/audit transaction. No
legacy permission fallback is allowed. Unavailable authority fails closed before
trying another role; only an authoritative denial permits checking the other
scoped role. Future case mutation/decision commands keep their separate single
manager/reviewer authority; source-read eligibility does not approve any check.
