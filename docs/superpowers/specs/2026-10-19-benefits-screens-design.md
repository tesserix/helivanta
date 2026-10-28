# Benefits screens

Issue: #1106 slice B (parent #1035, root epic #979). Builds on the [dependents and benefits API](2026-10-18-dependents-benefits-design.md), which this slice does not change.

## Problem

Slice A stores plans, dependents and two-person coverage, but HR can reach them only through the API. Nobody can record a plan, list an employee's dependents or approve coverage from the shell.

## Decision

- **One confidential page.** `/workforce/benefits` ("Benefits" in the Staff zone) declares `workforce.benefits.view`. The rail entry and every button are convenience only; the API decides.
- **No permission, no request.** Without `workforce.benefits.view` the page says benefits are confidential and sends no benefits request, so a directory reader never fetches dependents or coverage.
- **Writes follow the API's rules.** Changes show only to a locked hospital manager with `workforce.benefits.manage`. Approve and reject show only to a manager with `workforce.benefits.approve` who did not make the proposal, and withdraw only to the proposer. Inactive staff get no new dependent or enrollment. Their existing dependents stay editable and their approved coverage can still be changed, because the schema lets coverage narrow for an inactive employee, a removed dependent or a retired plan.
- **Plans panel.** It lists current plans with kind, effective dates, status and the relationships they cover. A plan is created and edited in an inline form. The code is fixed once the plan exists. Retiring asks for confirmation because it is final, and a retired plan offers no edit. History lists every revision with its author.
- **Staff benefits panel.** Choosing a staff member shows their dependents and enrollments.
  - **Dependents.** Add, edit, remove or restore a dependent, each with a required reason. A new relationship is chosen from active relationship masters by their current revision. Editing keeps the dependent's recorded revision, which the API accepts unchanged even if that master was later renamed or deactivated, so the edit never blocks a save. A phone number is optional and in international form, as the API requires.
  - **Enrollments.** Each shows its approved coverage, with the period and version, and any waiting proposal. A proposal covers the employee, or a dependent whose relationship the chosen plan covers. Further pages of plans and dependents load from inside the form. Changing an approved enrollment proposes new dates and keeps the plan and person. The approver sees the eligibility basis and decides with a note, which a rejection requires. History lists every proposal and decision.
- **Retries replay.** A new plan, dependent or enrollment keeps one id while its form is open. A proposal keeps one id for each distinct input, so a retried save returns the saved record rather than a duplicate or a `proposal_conflict`.
- **Errors stay in the form.** Validation is inline (zod, `noValidate`). A refused save shows the API's reason and keeps the input. A `version_conflict` says to close and reopen. A failed load offers a retry.
- **Nothing confidential leaves the API response.** No benefits data goes to Valkey, local storage or URLs, and query keys hold only identifiers.

## Tests

Component tests through `renderWithProviders`, against stubbed API responses with synthetic values, cover:

- The rail entry and the no-permission state.
- Reader versus manager actions.
- Plan create, edit, retirement confirmation and history.
- Dependent add, edit and remove, with relationship choices.
- Coverage proposal, with person choices limited to covered relationships.
- Approve, reject with a required note, withdraw, self-review hiding and replay.

Each test checks the request bodies the UI actually sends.

## Not in this slice

- **Slice C:** eligibility evidence files on proposals and a permissioned benefits export (with #1066), delivered by the [benefit evidence and export spec](2026-10-19-benefit-evidence-export-design.md).
- **Playwright.** The e2e environment seeds no hospital HR users or benefit grants, so these journeys are covered by component tests, as for the other workforce pages.
