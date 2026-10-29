# Employment draft withdrawal

Workforce owns this private preparation record. This slice closes a draft with
an attributable reason; it does not approve employment, activate access, cancel
employment, withdraw a document, or implement the HR intake role. The intake
source audit remains in the access/review continuation plan. ADR-0049 remains
binding for future operational activation.

Reuse the unmeasured planning assumptions of 50 peak metadata requests/second,
80:20 reads:writes, 500,000/1.5 million Staff at 12/36 months, 99.9% availability
and metadata p99 below 500 ms. Commands are bounded to 4 KiB, reason to 500
characters, and reads use existing default 50/maximum 200 keyset pages. No new
service, store, worker, event consumer or runtime cost is introduced.

A current enrollment permission and explicit current hospital manager grant
are required for every command/read/receipt. Expected tenant/subject pin the UI
workspace. Missing permission returns 403, unavailable/out-of-scope object 404,
stale/reused/terminal command 409 and database failure fails closed. Do not widen
legacy helpers or grant identity, credential-review, login, clinical or payroll
powers.

Add POST /employment-periods/:period/withdraw with UUIDv7 command_id,
expected_version >= 1 and required reason. Within the existing hospital/Staff
lock order, recover exact actor/object/intent receipts first, then transition
only draft -> withdrawn with a new revision. Keep terms, original proposer,
source generations and evidence reference unchanged. Reason describes the
withdrawal; the earlier reason remains in history. A withdrawn draft cannot be
edited, restored or receive new evidence. Existing historical evidence remains
available only through its existing protected/audited disclosure boundary.
Withdrawal may close a draft after its Staff source or sponsor changed: it must
not require making an obsolete source current again. Reauthorization remains
mandatory. Competing edit/withdraw commands serialize; one wins and the other
conflicts. A repeated exact command returns the original immutable receipt.

The existing draft-only read contract remains unchanged by default: list/detail
and history return draft revisions only. Clients explicitly request
lifecycle=withdrawal-v1 to read terminal states. Unknown or duplicate lifecycle
values return 400. The new UI opts into this projection and renders the terminal
state and reason, with no edit/attach/withdraw controls. Old application code
cannot reopen a terminal draft because database triggers enforce finality.

Migration 0052_workforce_employment_withdrawal expands status checks and replaces
the owner trigger; it rewrites no business records. Database guards retain
identity, terms, actor and source on withdrawal, and append exactly one immutable
history row. Rollback retains schema/history; old readers see draft-only data,
and trigger guards deny stale writes. There is no destructive down migration.
Production migration/rollout requires approval for the exact combined revision.

Tests must prove terminal persistence/history, source/sponsor drift cancellation,
required reason, foreign scope and missing permission, stale/changed commands,
concurrent commands, retained terms, no evidence attachment, old/new reader
compatibility, database tamper rejection, and UI exact retry/conflict handling.
Verify real local Staff Admin UI and restricted reader behavior before acceptance.
