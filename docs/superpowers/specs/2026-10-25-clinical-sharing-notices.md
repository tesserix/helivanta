# Hospital-reviewed clinical sharing notices

Status: proposed implementation slice for #60/#556; clinical consent remains open.

Patientmaster owns hospital identity and self-authority receipts, and will own
clinical notices separately from MediConnect account notices. Consumers enter
through Global API. Verified tenant, current hospital registrar grant and separate
patient.sharing-notice.review permission are mandatory for staff actions. No legal
policy, notice text or clinical authority is seeded.

Planning assumptions (not measured capacity): 10 peak requests/s, 8 KiB commands,
100:1 reads:writes; 100 hospitals and 20 versions/year yield 2,000 rows at 12 months
and 6,000 at 36 months, approximately 100 MiB with indexes. Target 99.9%
availability and p99 300 ms excluding login. Existing Postgres/outbox incurs no
new standing service cost. These are design targets, not load/SLO certification.

PUT /v1/patients/sharing-notices/:notice creates an immutable draft containing
purpose=care_coordination, canonical locale, version_label, title, text,
withdrawal_consequence and policy_reference. POST .../:notice/approve requires
expected_version=1, review_reference and an independent hospital reviewer.
POST .../:notice/retire requires the recorded version. GET .../:notice returns
exact wording to scoped reviewers. Patient publication and clinical consent
consumption are subsequent slices: drafts must never support consent capture.

Transitions: draft v1 -> approved v2 -> retired v3, or draft v1 -> retired v2.
Identical retries return current state without reactivation; changed wording or
review evidence conflicts. Forced RLS, SQL immutability and independent actor
checks protect wording and provenance from another tenant or an ungranted insider.
A supplied hospital ID selects scope and confers no authority. Audit and opaque
ID/version-only outbox intent commit atomically; reads are audited. Evidence,
wording and actor identity never enter event payloads.

Postgres unavailable denies operations; authz unavailable denies access; NATS
unavailable retains durable outbox intent. No remote call occurs under SQL locks.
Forward-only additive migration has lock/statement limits; old binaries ignore
new tables and rollback preserves receipts. No clinical freshness requirement is
changed by this notice slice.

Rejected: account-notice reuse conflates account and clinical purposes; a new
service adds an unnecessary failure boundary and duplicates scope. IN-ID-04/06/07
and current source-register limits apply. Actual hospital/privacy review of wording
remains required: a UUID is not evidence of legal applicability by itself.
Proxy authority, signed record release, ABDM and reminders remain separate work.
