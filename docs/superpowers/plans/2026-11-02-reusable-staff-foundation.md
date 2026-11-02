# Reusable Staff delivery plan

Follow the [scope and acceptance spec](../specs/2026-11-02-reusable-staff-foundation.md).

- [x] Inspect current Staff, pharmacy organizations/outlets and ADR-0044.
- [x] Isolate work from dirty main and unfinished suspension changes.
- [x] Observe shared directory behavior tests fail before implementation.
- [x] Implement/export shared typed Staff directory presentation.
- [x] Replace the embedded Staff directory renderer; preserve protected callers.
- [x] Run shared package checks and existing Staff workspace regression tests.
- [x] Exercise the rendered current Staff journey in the browser.
- [x] Record verification and remaining department/posting/product work.
- [x] Add Pharma as the first shared canonical Staff source consumer.
- [x] Test normal UI create/edit propagation and a real pharmacy-only account without Staff access.
- [x] Record source-contract, cutoff, paging and consumer verification evidence.
- [ ] Resolve employer scope for standalone organizations.
- [x] Implement and test the unreleased organization hospital-reference protocol.
- [x] Implement bounded canonical organization departments/sub-units and normal local owner UI.
- [x] Add explicit retained HR-to-Organization mapping and manager UI/local acceptance.
- [x] Finish mapping fault matrix and independent-role browser acceptance.
- [ ] Implement Workforce postings with separate access and lifecycle enforcement
      through the [effective-postings plan](2026-11-02-effective-staff-postings.md).
- [ ] Implement Pharma Connect Staff specialization through canonical references.
- [ ] Verify complete normal-role, independent-review and negative end-to-end paths.

Do not close #417 or #418 on the strength of the UI extraction. No production
rollout or database migration is included in the initial shared-directory change.

Department delivery sequence: RED for public create/list behavior; implement
canonical model/migration/commands; RED for stale edits, subtree moves and
retirement; implement guarded immutable revisions; exercise scope, database and
outbox failure, concurrency and bounded history; add normal governance UI and
local signed Global API/browser acceptance before treating this vertical slice
as accepted. Then perform explicit HR-reference handoff and Workforce postings.
