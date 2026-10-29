# Staff employment lifecycle delivery

The complete owner research remains
[staff lifecycle R&D](../specs/2026-10-27-staff-lifecycle-rnd.md); current acceptance
is tracked in [the delivery tracker](../../qa/staff-management-progress.md).
No row below is complete merely because a UI status exists.

- [ ] Complete the account-bound/versioned basic editor prerequisite, including
      synthetic manager/reader UI and preserved inactivation/link history.
- [ ] Model employment periods and distinct employee, consultant, individual
      contractor and agency/outsourced-worker relationships. Reconcile #1035 with
      facility contractor/vendor work in #298; a service vendor is not automatically
      a Staff employment record. Contract dates/type, employer, supervisor, permitted
      hospital scope and renewal evidence need owner contracts and required fields.
- [ ] Define reviewed effective transitions: suspend, resume, notice/resignation,
      termination, contract expiry/extension and rehire. Keep security disablement,
      tenant membership removal and employment decisions separate. No fixed notice,
      pay, tax or dismissal policy is invented by the software.
- [ ] Introduce a new employment generation for rehire/resume where required,
      invalidate old bindings/assignments/cards and require new independent approval.
- [ ] Implement the accepted reviewed-employment migration policy in
      [ADR-0049](../../adr/0049-reviewed-employment-access.md); inventory legacy authority provenance;
      publish current employment authority to every actual consumer and prove cutoff
      despite old cookies, FGA tuples, queued work and missed broadcasts. Do not
      implicitly remove tenant governance or another hospital's employment.
- [ ] Complete dated postings/transfers and supervisor changes, handover owner
      acknowledgments, contract renewal and expired-contractor denial.
- [ ] Integrate current identity/background/photo evidence and revocable cards;
      retain historical evidence and separate physical-access acknowledgment.
- [ ] Complete leave/roster/attendance and payroll/final-settlement obligations,
      professional payslips/offer/exit letters, accountable exceptions and recovery.
- [ ] Run synthetic Admin, independent reviewer and worker journeys for every
      transition, foreign scope, self-review, stale/duplicate command, future cutoff,
      dependency outage and same-person/multiple-hospital case before acceptance.
- [ ] Commit, push and merge only verified slices. Review the exact combined
      deployment candidate and migrations; obtain named production approval before
      any new rollout or migration.
