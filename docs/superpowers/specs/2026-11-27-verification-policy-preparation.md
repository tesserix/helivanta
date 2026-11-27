# Pharmacist verification policy preparation

Part of the complete #169 workflow, governed by accepted ADR-0077. This record
covers administrative draft preparation only; the parent issue is not complete
until independently authorized clinical approval, verified person identity,
review decisions, prescriber integration and the atomic sale gate work together.

The Pharmacy owner holds immutable outlet-scoped policy revision intents.
Existing `pharmacy.access.manage` plus a current editor outlet grant permits
non-patient administrative preparation. This does not identify the actor as a
clinical policy publisher or approver. Later publication must independently
resolve the accepted qualified authority and verified person evidence. Owner,
HR, supplier grants and two different account IDs are not substitutes.

Input is a label, a proposed second-check requirement, proposed review-validity
and prescriber-response intervals, a reason and an expected prior revision.
There are no clinical defaults and no emergency or approval fields. Intervals
are positive seconds bounded by representable Go duration, not recommended
clinical deadlines. Every proposed prescription policy still requires review;
there is no switch to bypass prescription verification. A clinical approver must
judge settings and applicability before any effective policy can be created.

Use one append-only revision ledger with a composite tenant/outlet parent FK,
unique retained command IDs and per-policy sequential versions. Serialize edits
with a transaction advisory lock. Exact replay by the original actor recovers
the retained revision; changed content, actor, parent or version conflicts.
Audit rows and intent commit together. Read/history disclosure is audited before
returning records, uses bounded keyset pagination, and rechecks current outlet
scope. Missing objects return 404; permission denial 403; dependency failure
remains unavailable rather than a fabricated empty policy.

All new routes remain compiled unreleased. Output always identifies a draft
with clinical authority not evaluated. No activation, clinical privilege,
patient disclosure, stock movement, financial effect or emergency supply is
created. Canonical Go/Zod models derive from pharmacy-verification.json.

Tests must cover boundary/unknown input, authoritative scope, exact and concurrent
replay, stale edits, history, immutable SQL and audit failure. Pure validation
runs locally; real database tests run in hosted CI without local Docker/Colima.
The new migration stays in an unmerged draft until the complete clinical change
and named migration approval are reviewable.
