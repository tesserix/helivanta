# Bind private Staff documents to the expected workspace

Status: local A06/#1053 candidate; full A01–A12 remains required.

Workforce owns private enrollment, credential and qualification files. Routes
use `scope` and `documentStaff`; the latter ignores the expected account, empty
tenant assertions and repeated tenant values. Profile/employment preparation
already uses `staffPrincipal`. Reuse that shared precondition in the common
Staff document parameter helper, before object/body/storage/transaction work.
Verified tenant, permission, manager grant, exact Staff/file binding and release
checks remain authoritative; expected headers are assertions, never authority.

Assets are personnel/evidence metadata and bytes. Stale tabs or authenticated
insiders must not continue a disclosure or command against a different selected
account/tenant using ambiguous workspace context. Omitted assertions remain
compatible; invalid, empty, repeated or mismatching assertions return no-store
`409 session_changed`. This explicitly aligns the old mismatching-tenant 404
with the shared 409 contract, independently of target existence. Actual foreign
scope remains 404 and missing permission 403. No scope or permission is widened.

Cover all ten document routes, matching/headerless requests, malformed/repeated
assertions, foreign scope, unchanged file/classification/audit/outbox and no
released-byte reads for refused requests. Use real PostgreSQL/NATS and the
existing vendor storage protocol fixture. No migration, new endpoint, provider
mutation or new architecture is needed. Header checks are bounded constant-time
with no new I/O. Reversion reopens the defect; retain this boundary on repair.

This is not employment cutoff or transaction-held scoped IAM authorization.
The intake route/phase inventory and compound enrollment/document authority
remain required before scoped HR intake and independent employment review.

## Staff UI and client binding

The document client previously sent only a tenant assertion and keyed caches by
tenant/hospital/Staff. The panel independently fetched the current tenant,
allowing it to combine a new account with an existing Staff workspace. Require
the original verified Staff workspace account as a DocumentScope input, include
subject in versioned document/classification query keys, and send both assertions
for every query, upload phase, retry, revocation and download. Credential-evidence
selection is an actual consumer and must inherit the same original account.

Remount document preparation/classification forms on account changes; abort
in-flight file work and preserve exact uncertain retries only within their original
account. Treat current account/scope/permission denial as loss of the private
workspace: hide its metadata/actions and notify the parent Staff context guard.
Keep ordinary network uncertainty separate so a retry uses its original command.
No credentials or file bytes are persisted in browser storage by this change.

UI/API regression evidence must show differing account keys, account headers,
aborted preparation before any PUT after same-tenant account change, no uncertain
classification command carried into another account, and cleared private state
on session_changed. These component/HTTP tests do not prove normal provider sign-in
or deployed acceptance; the existing browser handoffs remain separate gates.
