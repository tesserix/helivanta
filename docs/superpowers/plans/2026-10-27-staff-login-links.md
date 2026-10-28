# Staff-login link implementation

Scope and invariants: [design](../specs/2026-10-27-staff-login-links.md).
Tracked under #1132/#1053. Keep the full Staff Connect work in the HR coverage ledger.

1. Reproduce the missing reviewed-link journey with a real-DB handler test.
2. Add canonical models/generator, additive migration, scoped handlers and private
   history. Verify membership using the existing role-reader contract; add no IAM
   table reads or tuple writes. Test each transition and failure before implementing it.
3. Revoke pending/active links atomically during staff inactivation; test races,
   uniqueness, replay, DB guards, RLS, and privacy-safe audits/outbox.
4. Add the generated API client and scoped directory panel with tests for review
   separation, retry preservation, error states and tenant/hospital changes.
5. Regenerate contract inventory; run generator checks, package/race tests, lint,
   typecheck/build and synthetic Global API/browser verification. Record outstanding
   release gates honestly; do not close #1132 until self-service leave is delivered.

Status: implementation starting; no migration or rollout executed against a live environment.
