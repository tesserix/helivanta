# Preserve roster setup drafts during navigation

Local administrator UI QA reproduced an unsaved roster-area name being lost by
following the in-form shift-setup link. No application or browser confirmation
appeared. The same editor owns roster areas, shift groups and weekly templates.

Reuse the existing NavigationGuardProvider, useLeaveGuard and useConfirmLeave.
Register dirty and pending state, guard Cancel and voluntary hospital/kind and
Add/Edit/history replacement, and use the existing guarded link for the in-form
shift route. A confirmed replacement starts a fresh editor session, including
same-record and same-new-form replacement. Restore opener focus after unmount.
Pending writes retain their context; successful saves close without prompting.
Forced access/account invalidation continues to clear private data immediately.

This is client input protection only. Workforce remains the owner of versioned
roster configuration and shift references. Existing hospital scope, permission,
review acknowledgement, immutable revisions, concurrency and validation remain
server enforced. No API, schema, permission, migration, service or dependency is
introduced. Browser reload uses the shared provider's existing beforeunload
behavior; no new persistence of unsubmitted personnel data is introduced.

Verify Cancel/Keep/Discard, scope/kind changes, history/replacement, shift link,
pending writes and successful saves through regression tests and local UI.
Roster setup remains audit pending until its wider saved membership, effective
versions, referenced retirement, isolation and deployed journeys are verified.
