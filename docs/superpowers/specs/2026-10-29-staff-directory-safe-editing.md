# Account-bound, versioned basic Staff editing

Related scope: #1035, #417 and the A07–A10 lifecycle acceptance journeys.
This is a prerequisite slice, not implemented employment suspension/offboarding.

Workforce owns hospital-local Staff and its existing revision trigger. Basic
managers hold an explicit hospital manager grant; private enrollment additionally
requires its current permission. IAM owns accounts and assignments. Consumers
are the shell basic editor and existing legacy directory clients. No new role,
service, table, migration, clinical authority or country policy is introduced.

## Contract and consistency

Keep legacy GET/PUT responses and last-commit-wins PUT semantics. Add an opt-in
`directory_context=v1` protocol for basic Staff GET/PUT. It requires exactly one
expected tenant and subject header, matching the verified principal. Responses
add `version` and `directory_context_version: "v1"`; the client validates the
acknowledgment and exact Staff/hospital IDs before displaying an editable form.
PUT also requires the body marker and a nonnegative safe-integer
`expected_version` (0 for creation). An older owner's strict decoder rejects the
new body fields before mutation; an unacknowledged GET cannot open the editor.
Canonical protocol metadata lives in `contracts/workforce-directory.json`.

Inside the existing owner transaction, lock the current manager grant and Staff
row before checking version or writing. A stale different edit returns 409
`staff_version_conflict`; an identical current value is an idempotent replay and
retains timestamps/revision/audit count. Concurrent creates with the same UUID
cannot overwrite different content. Rejected context, malformed metadata and
stale versions have no business/audit/outbox side effects. Existing inactivation
and reviewed-link revocation remain atomic; this does not revoke all IAM,
clinical, physical-card or legacy authority and must not be labeled offboarding.

Unauthorized objects return 404, account-context mismatch 409, malformed protocol
400, and unavailable authorization retains existing fail-closed 503 behavior.
No browser assertion selects a tenant or grants authority. Reads remain no-store.

## UI and recovery

Capture the workspace account once. Existing records load the acknowledged owner
snapshot rather than initialize from a stale directory row. New records capture
one UUID and start at version 0. Account/access loss removes the workspace and
its draft. Ordinary conflicts and transient failures retain the draft. Reloading
current data requires an explicit discard confirmation; do not silently merge or
retry a different user's edit. Duplicate submissions stay disabled while pending.

## Security, scale and recovery

Assets: staff identity/employment fields and attributable history. Threats include
another tenant, a shared-workstation user after account change, a stale manager,
and concurrent editors. The boundary is the verified account plus current
hospital grant and locked owner revision; the UI is guidance only.

Reuse ADR-0009's planning assumptions: 500,000 Staff rows over 12 months,
1.5 million over 36 months, 50 peak requests/second, 99.9% availability and p99
under 500 ms. These are planning targets, not measured performance claims. New
queries are bounded primary-key/grant lookups and at most one Staff row; no list
scan, new infrastructure cost, remote call under locks or asynchronous consumer.
Keep the 8 KiB command bound. A database failure rolls back the whole command.
After an ambiguous response, retrying unchanged content reauthorizes and returns
the current identical record; differing content requires explicit reconciliation.

Alternatives rejected: requiring private HR permission contradicts basic manager
authority; changing legacy PUT globally breaks existing clients; client-only
checks cannot protect cross-account commands; a new history table duplicates the
existing revision/audit owners. Rollback stops the new editor/projection and
retains existing revisions and audit history; it performs no destructive repair.

## Acceptance

Prove stale/missing/duplicate account assertions reject before disclosure/write;
legacy compatibility; marker negotiation against old owners; exact response
identity; reader/foreign-hospital denial; stale edits; exact replay; concurrent
creation and revocation ordering; draft retention, explicit reload and fresh
snapshot; real synthetic basic-manager UI with fixture restoration. Run generated
contract checks, real PostgreSQL tests, affected race tests, web checks and CI.
