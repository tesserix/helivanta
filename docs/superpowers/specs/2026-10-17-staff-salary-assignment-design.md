# Staff salary assignment and tax-regime election

Issue: #1041 slice 4a (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Builds on [salary heads and structures](2026-10-16-salary-heads-structures-design.md), [PF and ESI rules](2026-10-16-statutory-rules-pf-esi-design.md) and [income-tax rule sets](2026-10-17-income-tax-rule-sets-design.md). Plan: [staff salary assignment](../plans/2026-10-17-staff-salary-assignment.md).

## Problem

Structures describe a template, not a person. Payroll (#1040) needs, for each staff member and each day, the structure that pays them and their own amounts for its fixed heads. It also needs the income-tax regime they elected for the tax year. A pay change is high-impact, so it needs a second person's approval. And a later change to heads or structures must never leave an assigned staff member impossible to compute.

## Scope of this slice

- **Salary assignment** per staff member: a structure plus optional per-person amounts for its fixed heads, effective-dated, with a stated reason. Proposed by one person and approved or rejected by another, the same lifecycle as statutory rules. Separation of duties is a table constraint.
- **Assignment validity at every date**: a deferred check proves that every approved assignment's structure is active and that every per-person amount belongs to a fixed line of that structure. A head or structure change that would break an assignment is refused with 409 `assignment_incomplete`.
- **Staff salary preview**: the staff member's approved assignment in force on a date, run through the same calculator, followed by the monthly statutory rules.
- **Tax-regime election** per staff member per tax year (`new` or `old`), recorded from the employee's intimation, versioned and audited. With no election, the system reports `regime_not_elected` and never assumes a default; the Act's default regime is a decision for the payroll run (#1040), with an owner review.
- **Shell:** a "Staff pay" panel on `/workforce/payroll-setup`. It chooses a staff member, then shows the assignment in force, the pending proposal, history, the regime per tax year and a preview.

Not in this slice:

- Bank account, PAN and UAN. These are financial identifiers, and the SOC 2 register already records an open decision on field-level encryption for personal data (`docs/runbooks/soc2-controls.md`). Slice 4b resolves that decision first, then stores them behind their own permission.
- Ending pay at exit. This comes with the exit workflow (#1107), which owns the last working day.
- Investment declarations and old-regime deductions, monthly TDS projection, payslip formats and HR settings: later #1041 slices and #1040.
- Matching a structure's department, designation and grade scope against the staff member. Enrollment stores those as text, not master ids, so the scope stays a guide for choosing a structure, as it is today.

## Permissions

| Action                         | Permission                    | Hospital grant |
| ------------------------------ | ----------------------------- | -------------- |
| Read assignments, elections    | `workforce.payroll.view`      | reader/manager |
| Propose or withdraw assignment | `workforce.payroll.configure` | locked manager |
| Approve or reject assignment   | `workforce.payroll.approve`   | locked manager |
| Record a regime election       | `workforce.payroll.configure` | locked manager |
| Staff salary preview           | `workforce.payroll.view`      | reader/manager |

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}/hr/payroll`.

| Route                                                | Behaviour                                                                                                                                                                                                                                                          |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET …/salary-assignments`                           | Assignments with a current or pending revision, newest first, keyset pages up to 100                                                                                                                                                                               |
| `GET …/staff/{staff}/salary-assignment`              | `current` and `pending`, or 404 when the staff member has none                                                                                                                                                                                                     |
| `PUT …/staff/{staff}/salary-assignment`              | Proposal: `expected_version` (0 for the first), `structure_id`, `effective_on`, `reason` (1–500), `amounts` (0–60 `{head_id, amount_paise}`, each head once). Replaying the pending or current content returns it unchanged. One pending proposal per staff member |
| `POST …/staff/{staff}/salary-assignment/decision`    | `proposal_id`, `expected_version`, `decision` `approve`/`reject`, `note` (required to reject). The decider must not be the proposer                                                                                                                                |
| `POST …/staff/{staff}/salary-assignment/withdraw`    | `proposal_id`, `note`. Proposer only                                                                                                                                                                                                                               |
| `GET …/staff/{staff}/salary-assignment/history`      | Every revision with its amounts, newest first                                                                                                                                                                                                                      |
| `POST …/staff/{staff}/salary-preview`                | `on`, `payable_half_days`, `disability`, optional `gender`. The approved assignment in force that day through the calculator and monthly statutory rules; 409 `not_assigned` when none                                                                             |
| `GET …/staff/{staff}/tax-regimes`                    | Elections, most recently recorded first                                                                                                                                                                                                                            |
| `PUT …/staff/{staff}/tax-regimes/{tax_year}`         | `expected_version` (0 to record), `regime`, `intimated_on` (a date inside or before the tax year), `note` (required when changing). An identical replay returns the election                                                                                       |
| `GET …/staff/{staff}/tax-regimes/{tax_year}/history` | Every version of that election                                                                                                                                                                                                                                     |

Cross-hospital and cross-tenant staff or structures are 404. A staff member who is `inactive` cannot receive a new proposal (409 `staff_inactive`), but approved history stays readable.

## Data

Migration `0024_workforce_salary_assignment`:

- `workforce_salary_assignments`: one row per staff member (`version`, `revision_id`, `effective_on`), with a deferred key to the approved revision in force, as for statutory rules.
- `workforce_salary_assignment_revisions`: `structure_id`, `effective_on`, `reason`, plus the proposal/decision columns and constraints copied from statutory revisions: one pending per staff member, decided once, content immutable, separation of duties, rejection note, withdrawal by the proposer.
- `workforce_salary_assignment_amounts`: `(revision_id, head_id, amount_paise 0–1,000,000,000)`, inserted only in the revision's creating transaction and immutable afterwards.
- `workforce_salary_assignment_on(tenant, hospital, staff, day)`.
- `workforce_salary_assignment_check(tenant, hospital, from)` visits every head, structure and approved assignment date from `from` onwards. It raises `workforce_salary_assignment_incomplete` when an approved assignment in force on a date has a missing or inactive structure, a per-person amount for a head that is not a fixed line of that structure, or full pay beyond the calculation limit. Deferred constraint triggers run it after every head and structure revision and on approval, so a change elsewhere cannot break an assignment. A proposal is checked against its own effective date when it is created. The slice 1 configuration check is unchanged.
- `workforce_staff_tax_regimes` (`staff_id, tax_year` key, `regime`, `intimated_on`, `version`) and `workforce_staff_tax_regime_revisions` (every version, immutable).
- Forced RLS on every table, writes gated by `app.workforce_payroll`, like the other payroll tables.

Every write commits the change, a `workforce_audit` row and an outbox event together: `salary_assignment_changed.v1` (staff, revision, status, version) and `tax_regime_changed.v1` (staff, tax year, version). They carry identifiers only, never amounts, structures or regimes.

## Verification

Real-PostgreSQL tests:

- The lifecycle: propose, replay, stale version, one pending, approve, reject without a note, self-approval refused by the handler and by the table, withdrawal by someone else.
- Amounts for a percentage head or a head outside the structure.
- A structure deactivated, or a fixed line removed, under an approved assignment: refused with 409, both directly and through a later-dated revision.
- An inactive staff member.
- Cross-hospital and cross-tenant 404s, and 403s for each missing permission.
- Audit rows and events that carry no amounts or regimes.
- The staff preview uses the per-person amount, falls back to the structure's amount, and returns `not_assigned` before the first effective date.
- Regime elections: record, replay, change with note, stale version, tax year format, and `intimated_on` after the tax year refused.

Calculator overrides are unit-tested. Every new guard is mutation-checked. Page tests cover the panel's empty, failed, restricted, pending and decided states, the proposal form's inline errors, and a regime change requiring a note. Full Go/web gates.
