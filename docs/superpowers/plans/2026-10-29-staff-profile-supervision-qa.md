# Staff profile and supervision implementation sequence

Read [the scoped design](../specs/2026-10-29-staff-profile-supervision-qa.md) and the full Staff tracker. This plan does not mark Access/RBAC complete.

1. Reproduce owner GET/PUT context mismatch/missing/duplicate acceptance; capture red results and unchanged-state assertions.
2. Generate the canonical opt-in acknowledgment and centralize existing optional Workforce context validation without widening permissions.
3. Test the editor's account binding, old-API and wrong-record denial, and account-change form removal. Preserve ordinary version-conflict draft behavior.
4. Bind enrollment/choice queries and writes to the verified opening context; keep caches transient and require server acknowledgment.
5. Exercise real local admin/independent related-role UI: assign, change and clear supervisors; reject cycles/self-links; check reload and stale-tab behavior. Make inactive choices explicit without redefining lifecycle policy.
6. Record delivered production milestones and this area's actual evidence/gaps in the acceptance ledger and tracker. Document transfer/posting and removal dependencies separately.
7. Run generators, contracts, affected backend suites, full frontend gates, lint/vet/build and CI. Commit/push/merge only verified changes; prepare a named rollout including inherited changes before seeking production approval.
