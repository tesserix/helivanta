# Preserve unsaved shift edits

Local Shifts UI QA reproduced losing an unsaved description when navigating to
Staff directory. There was no discard confirmation. The saved shift and immutable
history were preserved; the defect is loss of the operator's unsubmitted input.

Reuse NavigationGuardProvider, useLeaveGuard and useConfirmLeave, already used by
Staff enrollment/employment forms. Register dirty and saving state in ShiftEditor.
Route navigation, cancel, hospital selection, Add/Edit and opening history must
use the same guard. Keep editing preserves fields and scope. Explicit Discard
runs only the requested action. Saving cannot be interrupted by a panel/scope
change. Successful save keeps the existing close, refresh and focus behavior.

Restoring focus before the editor unmounts can be overridden by dialog teardown.
The shift page restores its opener after editor/history state commits. Keep
editing retains the existing dialog focus return. The editor uses a fresh client
session key when a confirmed action replaces the same record/form.

This changes only client navigation. Workforce retains shift ownership and the
existing verified tenant, hospital manager, roster permission, optimistic version,
validation, revision/audit/event controls. No backend API/schema/role or policy changes,
new service/store/dependency, latency budget or runtime cost is introduced. Shift
commands retain their existing bounded owner contract. No migration is required.

Verify regressions for Cancel/Keep editing/Discard, history or edit replacement,
hospital switching and pending writes; retain existing overnight-validation,
conflict-retry and reader tests. Reproduce/verify the real local UI, then run all
web checks and CI. The Shift area remains partially accepted until its wider
multi-hospital, referenced-retirement, downstream and production QA is complete.
