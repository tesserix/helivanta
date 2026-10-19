# Employee dependents and benefit enrollment

Issue: #1106 slice A (parent #1035, root epic #979). Builds on [versioned HR masters](../../adr/0016-versioned-hr-masters.md) and the review lifecycle of [credential validity](2026-10-20-credential-validity-design.md).

## Problem

Enrollment records emergency contacts but has nowhere to record an employee's dependents, the benefit plans a hospital offers, or who is covered by which plan and when. Dependents' names and birth dates and the coverage they hold are confidential HR data. Anyone who can open the staff directory must not be able to read them, and a coverage decision must not rest on one person's say-so.

## Decision

- **Three records, each versioned.** Benefit plans, dependents and enrollments each have a current row plus immutable revisions. Every change is a new revision with its author, so correction history is never overwritten.
- **Plans are effective-dated and retire once.** A plan revision has `effective_from`, an optional `effective_until` and a status of `active` or `retired`. Retiring is terminal. A retired plan takes no new enrollment and no extension.
- **Dependents reuse the relationship master.** A dependent names a revision of an active `relationship` HR master, like department and grade do on enrollment. A dependent has only a name, relationship, birth date and optional phone. There is no government identifier, no patient record and no link to clinical data.
- **Coverage needs two people.** An enrollment changes only through a proposal that someone else approves, using the credential review lifecycle: pending → approved, rejected or withdrawn, with separation of duties as table constraints. The proposal states the eligibility basis the approver checks.
- **The original plan is kept.** An enrollment pins the plan revision it was first approved under, and the database refuses to change it. Later proposals record the plan revision in force when they were made.
- **No overlapping coverage.** An exclusion constraint stops one person being covered twice by the same plan over overlapping dates. It is enforced on approved coverage, and checked when a proposal is made so the conflict is reported early.
- **Coverage stays inside the plan.** A coverage period must lie within the plan's effective dates. A plan change that would leave existing coverage outside them is refused, and so is retiring a plan while coverage is open-ended or runs past the retirement date.
- **Benefits data has its own database scope.** RLS on every benefits table requires the transaction flag `app.workforce_benefits`, which only benefits handlers set (`read`, or `write` for a locked manager), on top of the hospital grant. Directory, enrollment and payroll handlers never set it, so a mis-scoped query elsewhere returns nothing.
- **Fail closed.** If FGA or the grant check fails, the request fails with 503 or 404. A missing relationship master, an inactive staff member, a removed dependent or a retired plan refuses the write. No defaults are assumed.

## Permissions

| Action                                                    | Permission                   | Hospital grant    |
| --------------------------------------------------------- | ---------------------------- | ----------------- |
| Read plans, dependents, enrollments and history           | `workforce.benefits.view`    | reader or manager |
| Change plans and dependents; propose or withdraw coverage | `workforce.benefits.manage`  | locked manager    |
| Approve or reject coverage                                | `workforce.benefits.approve` | locked manager    |

`workforce.staff`, enrollment and payroll permissions grant none of these. Every read writes an audit row naming the reader.

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}`.

| Route                                                                       | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                           |
| --------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/hr/benefits/plans`                                                   | Paged; current plans                                                                                                                                                                                                                                                                                                                                                                                                                |
| `PUT …/hr/benefits/plans/{plan}`                                            | `expected_version` (0 to create), `code`, `name`, `kind`, `description`, `covers_dependents`, `covered_relationships` (master ids), `effective_from`, `effective_until`, `status`. An unchanged retry returns the current plan. 409 `version_conflict`, `plan_retired`, `coverage_outside_plan`, `duplicate_plan`                                                                                                                   |
| `GET …/hr/benefits/plans/{plan}/history`                                    | Paged revisions                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `GET …/staff/{staff}/dependents`                                            | Paged; current dependents                                                                                                                                                                                                                                                                                                                                                                                                           |
| `PUT …/staff/{staff}/dependents/{dependent}`                                | `expected_version`, `full_name`, `relationship_revision_id`, `birth_date`, `phone`, `status` (`active` or `removed`), `reason`. 409 `version_conflict`, `duplicate_dependent`, `dependent_covered`, `staff_inactive`; 400 for an inactive or non-current relationship revision                                                                                                                                                      |
| `GET …/staff/{staff}/dependents/{dependent}/history`                        | Paged revisions                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `GET …/staff/{staff}/benefit-enrollments`                                   | Paged; each enrollment with its approved coverage and any pending proposal                                                                                                                                                                                                                                                                                                                                                          |
| `PUT …/staff/{staff}/benefit-enrollments/{enrollment}/proposals/{proposal}` | `expected_version`, `plan_id` and `dependent_id` (null for the employee; both fixed after the first approval), `coverage_from`, `coverage_until`, `eligibility_basis`. Replaying the same proposal returns it. 409 `proposal_pending`, `proposal_conflict`, `version_conflict`, `plan_retired`, `coverage_outside_plan`, `coverage_overlap`, `relationship_not_covered`, `dependent_removed`, `staff_inactive`, `nothing_to_change` |
| `POST …/proposals/{proposal}/decision`                                      | `expected_version`, `decision`, `note` (required to reject). The proposer gets 409 `self_review`. Approval rechecks the plan, the dependent and overlap                                                                                                                                                                                                                                                                             |
| `POST …/proposals/{proposal}/withdraw`                                      | Proposer only                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `GET …/staff/{staff}/benefit-enrollments/{enrollment}/history`              | Paged proposals and decisions                                                                                                                                                                                                                                                                                                                                                                                                       |

Every response is `Cache-Control: no-store`. A foreign tenant or hospital gets 404, a member without the permission gets 403, and pages are capped at 100.

## Data

Migration `0033_workforce_benefits`:

- `workforce_benefit_plans` and `workforce_benefit_plan_revisions`. Codes are unique per hospital and names unique case-insensitively. Revisions are immutable, and a trigger refuses any revision after a retirement.
- `workforce_staff_dependents` and `workforce_staff_dependent_revisions`. The relationship revision is a foreign key to `workforce_hr_master_revisions` with `kind='relationship'`. An active dependent's name and birth date are unique per staff member.
- `workforce_benefit_enrollments` (approved coverage) and `workforce_benefit_enrollment_proposals`. Proposals use the credential review constraints: one pending per enrollment, decided once, separation of duties, a rejection note, and withdrawal only by the proposer. Approved coverage carries `EXCLUDE USING gist` on `(plan, member, daterange)`.
- Triggers check the parts constraints can't: coverage within the plan, a retired plan refusing new coverage, the pinned plan revision, and the dependent belonging to the same staff member and being active with a covered relationship.
- None of these tables can be deleted or truncated. Forced RLS uses `workforce_benefits_allowed(tenant, hospital, writing)`.

## Events and caching

Events go through the outbox in the business transaction, carry identifiers only, and are published to JetStream:

- `helivanta.in.workforce.benefit_plan_changed.v1` (plan, revision, version, status)
- `helivanta.in.workforce.dependent_changed.v1` (staff, dependent, revision, version, status)
- `helivanta.in.workforce.benefit_enrollment_changed.v1` (staff, enrollment, proposal, status, version)

Names, birth dates, relationships and dates never leave the API. Valkey holds nothing from this slice. Benefits data is neither cached nor used as a source of truth outside PostgreSQL, so a revoked grant takes effect on the next request.

## Compliance

- **SOC 2 CC6.1/CC6.3:** least-privilege permissions, explicit hospital grants and a separate RLS scope.
- **SOC 2 CC8.1:** two-person coverage changes.
- **HIPAA §164.312(b):** an audit row for every read and write.
- **HIPAA §164.502(b) minimum necessary:** minimal dependent fields and no clinical linkage.

Policies are synthetic. No eligibility rule decides anything automatically: the approver decides from the stated basis.

## Not in this slice

- **Slice B:** shell UI for plans, dependents and enrollment, designed in the [benefits screens spec](2026-10-19-benefits-screens-design.md).
- **Slice C:** scanned eligibility evidence files attached to proposals (the tax-evidence pattern), and a permissioned benefits export (with #1066).
- Premiums, payroll deductions, insurer integration, employee self-service (#1132) and automatic eligibility rules.
