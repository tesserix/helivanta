# Implement the pre-employment current-person review

Follow the [source-based design](../specs/2026-10-31-pre-employment-person-review.md),
ADR-0048/0049 and the full A01–A12 tracker. This checklist is an implementation
sequence; it does not itself authorize a real finding, employment activation or
production migration. Source now implements the core backend/client commands
and original-authority-pinned evidence byte release, plus bounded hospital
discovery with independent submit/review capabilities, a summary inbox and manager-only
Staff browse/exact-ID search. The decision UI now has source/component tests;
manager preparation/evidence selection and immutable history are also source
candidates with required-field/retry/privacy/stale-head component tests. Normal
browser acceptance and remaining
failure/concurrency, real-scanner/browser acceptance and employment work are
still required before this prerequisite can close.

1. Finish the current scoped-intake candidate's normal local maker/checker/intake
   workflow and real scanner acceptance, preserving pending/recovery and cutoff
   checks. The synthetic local intake assignment is pending independent approval;
   its exact private browser evidence/receipt identifies the account and expiry.
2. Inventory source/evidence contributors and historical target subject provenance
   in the real owner schema. Prove missing provenance cannot become a successful
   independence result. Confirm immutable source/evidence pins and SQL indexes
   before defining new query predicates.
3. Add the canonical person-review command/receipt/history/effectiveness contract
   and generator. Keep existing active-employment identity-case and operational
   login-link contracts unchanged. Do not add a general HR permission/template.
4. Add one real signed Global API/IAM/Postgres/OpenFGA regression for a current
   identity manager submitting a complete inactive-joiner review with released
   identity evidence, followed by the distinct reviewer decision. First observe
   the missing owner behavior fail. Assert the target stays inactive, receives
   no login/membership/assignment and cannot self-approve.
5. Implement additive head/immutable-history/command/evidence storage and owner
   routes, current scoped actor/source guards, bounded manager/reviewer reads
   and audit/outbox atomicity. Test forced RLS and denied direct history edits,
   deletes and cross-scope reads. Use exact original account assertions.
6. Implement the complete manager/reviewer UI, including required attestations,
   policy/date/evidence fields, unresolved identity findings, source conflicts,
   same-command retries, pending outcome recovery and preserved input. Separate
   recorded digital intent from independently reviewed operational account links.
7. Exercise purpose-bound reviewer file metadata/bytes with real scanner/storage
   and original authority pins across I/O. Test assignment/link replacement,
   source/evidence changes, failed private-byte release and absence of success
   audits after denial. Do not give reviewers general document-manager authority.
8. Test submit/approve/reject/withdraw/revoke, exact/conflicting replay, expiry,
   current and historical target subjects, contributor exclusions, unknown
   provenance, no-login workers, foreign tenant/hospital, lock-wait races,
   permission/membership/credential loss and dependency/audit/outbox failures.
9. Run complete affected race/coverage, canonical checks, contractdocs normal and
   check, vet/build/lint and all web/CI gates. Record real Admin/independent-reviewer
   browser evidence separately from protocol and unit tests. Commit/merge only
   verified source; obtain approval for the named production revision/migrations.
10. Consume the exact approved current-person receipt in the independently reviewed
    employment workflow. Bind source-to-operational employment generations
    explicitly, then implement legacy/governance review and every operational
    lifecycle cutoff. Full Access/RBAC remains open until A01–A12 are proved.
