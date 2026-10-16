# Salary heads, salary structures and the pay calculator

Issue: #1041 slice 1 (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Plan: [salary heads and structures](../plans/2026-10-16-salary-heads-structures.md).

## Problem

Legacy Salary Setup defines earning, deduction, CTC, OT and leave heads. Their fields cover month applicability, fixed or percentage values on other heads, proration by actual or fixed days, minimum payable days, rounding, caps and taxability, and inclusion in gross, CTC and arrears. Salary Template groups heads per department, designation and grade. Payroll (#1040) must compute from these reproducibly. A historical run keeps the configuration it used, and a change can never form a circular dependency or leave a template that cannot be computed.

## Scope of this slice

- **Salary heads**: earnings, employee deductions and employer contributions. Each has a fixed or percentage basis on earning heads, proration, minimum payable days, month applicability, a rounding mode and unit, an optional floor and cap, gross/CTC/arrears inclusion, taxability with a cited note when exempt, a Labour Code wage class, and its payslip label, order and zero-line display. Heads are versioned and effective-dated.
- **Salary structures** (templates): an ordered set of heads, with standard amounts for the fixed heads, optionally scoped to a department, designation and grade. Versioned and effective-dated.
- **A configuration validator in the database**: at every date from a change onwards, every head graph is acyclic, every base is an active earning, and every active structure can be computed. It runs as a deferred constraint, so no code path can skip it.
- **A deterministic calculator in integer paise**, with the order of operations fixed and documented. A synthetic **preview** of a structure takes a date and payable days, and returns lines, totals and the Labour Code wage test.
- **Shell:** `/workforce/payroll-setup` with heads, structures, history and preview.

Not in this slice (later #1041 slices; see the research doc):

- Statutory rule sets for PF, ESI, PT and LWF, with maker–checker approval (slice 2).
- Income-tax rule sets and regime election (slice 3).
- Employee salary assignment, bank/PAN/UAN fields, payslip formats and HR settings (slice 4).
- Per-shift and per-hour heads, which need approved OT and shift units from the #1040 run.
- Leave-encashment heads, which come with payroll processing.
- A lock refusing revisions effective inside a processed payroll period. #1040 adds it, as #1039 did for attendance policy.

## Permissions

`workforce.payroll.configure` writes heads and structures. `workforce.payroll.view` reads them and runs previews. Salary configuration is commercially sensitive, so reads are not `authz.Public`. Both also need the hospital grant: a manager grant to write, a reader or manager grant to read.

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}/hr/payroll`.

| Route                                   | Access                     | Behaviour                                                                                                                                                                                                         |
| --------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/salary-heads`                    | view + reader/manager      | Optional `kind`; newest first, keyset pages up to 100                                                                                                                                                             |
| `PUT …/salary-heads/{id}`               | configure + locked manager | Client-chosen id; full head, `effective_on`, `reviewed: true`, `expected_version` (0 to create). An identical replay returns the current revision. Code and kind never change; `effective_on` never moves earlier |
| `GET …/salary-heads/{id}/history`       | view + reader/manager      | Immutable revisions, newest first                                                                                                                                                                                 |
| `GET …/salary-structures`               | view + reader/manager      | Newest first, keyset pages up to 100                                                                                                                                                                              |
| `PUT …/salary-structures/{id}`          | configure + locked manager | As for heads; `lines` ordered 1–60, each `head_id` once, `amount_paise` for fixed heads only                                                                                                                      |
| `GET …/salary-structures/{id}/history`  | view + reader/manager      | Immutable revisions with their lines                                                                                                                                                                              |
| `POST …/salary-structures/{id}/preview` | view + reader/manager      | Body `on` (date), `payable_half_days` (integer 0–62, at most twice the days in that month). Read-only; nothing is stored                                                                                          |

## Head rules

- `kind`: `earning`, `deduction` or `employer_contribution`.
- `basis` `fixed` takes its amount from the structure line. `basis` `percent` takes `rate_ppm` (parts per million, 1–1,000,000; the UI enters a percent with up to 4 decimals) of the sum of 1–20 `base_head_ids`, which must be earnings.
- `proration`: `none`, `calendar_days` (payable ÷ days in the month) or `fixed_days` (payable ÷ `proration_days` of 1–31, capped at 1). Percentage heads take `none`, because their bases are already prorated.
- `minimum_payable_half_days` (integer 0–62): below it, the head is 0.
- `months`: a non-empty set of 1–12. In any other month the head is 0.
- `rounding_mode` is `nearest` (half up), `up` or `down`. `rounding_unit_paise` is 1, 50, 100, 500 or 1000. The optional `min_amount_paise` and `max_amount_paise` must be multiples of the unit, with min ≤ max.
- Inclusion: `in_gross` is for earnings only. `in_ctc` is for earnings and employer contributions; deductions never carry it. `arrears_eligible` marks heads a later correction may pay as arrears.
- `taxability`: earnings and employer contributions are `taxable` or `exempt`, and `exempt` requires a `tax_note` (1–300) citing its basis. Deductions are `not_applicable`.
- `wage_class`: earnings and employer contributions are `wages`, `excluded` or `outside`; deductions are `outside`.
- `payslip_label` (1–40), `payslip_order` (0–999), `show_when_zero`.

## Calculator

For a structure, a date and payable days, heads and the structure resolve to their revisions in force on that date (latest `effective_on` on or before it, ties to the higher version). Heads are evaluated in dependency order, and each one goes through these steps:

1. If the month is not applicable, or payable days are below the minimum, the head is 0.
2. A fixed head gives `amount × ratio`, with the ratio set by its proration. A percentage head gives `rate × Σ(rounded amounts of its bases)`.
3. Round to the unit with the mode, using exact integer arithmetic.
4. Clamp to the floor and cap.

Totals:

- gross = earnings with `in_gross`
- deductions = sum of deductions
- employer = sum of employer contributions
- net = gross − deductions. A negative net is reported as `negative_net` and never hidden.
- CTC = earnings with `in_ctc` + employer contributions with `in_ctc`

Labour Code wage test:

- wages = earnings classed `wages`
- remuneration = wages + `excluded` earnings and employer contributions
- add-back = max(0, excluded − ⌊remuneration ÷ 2⌋)
- statutory wages = wages + add-back

The test also reports the excluded share. Statutory rules (slice 2) consume statutory wages.

## Data

Migration `0020_workforce_salary_config`:

- `workforce_salary_heads` and `_revisions`, plus `workforce_salary_head_bases`. Same head/revision pattern as leave configuration: a deferred current-revision key and an advancing version.
- `workforce_salary_structures` and `_revisions`, plus `workforce_salary_structure_lines`. Scope references go to HR masters of the matching kind.
- `workforce_salary_head_on(tenant, hospital, head, day)`.
- `workforce_salary_config_check(tenant, hospital, from)`, run by deferred constraint triggers on both revision tables. It evaluates every change date from `from` and raises `workforce_salary_cycle`, `workforce_salary_base_unavailable` or `workforce_salary_structure_incomplete`, with the offending id in the detail. The API maps these to 409 with the matching `reason`.
- Forced RLS on every table.

Every write commits the change, a `workforce_audit` row and an outbox event (`salary_config_changed.v1`) together. These carry identifiers, kind and version only, never amounts.

## Verification

Real-PostgreSQL tests:

- Versioning and replay, stale versions, and code/kind immutability.
- Cycles refused directly and through a later-dated revision. A base deactivated under an active dependent. A deduction or inactive head used as a base.
- A structure missing a base, and a head changed from fixed to percent under a structure.
- Scope masters of the wrong kind.
- Cross-hospital and cross-tenant 404s, permission 403s.
- Audit rows and events that carry no amounts.

Calculator tests use synthetic boundary fixtures: proration on calendar and fixed days, the ratio cap, minimum days, month applicability, each rounding mode at the half, the floor and cap, percentage-of-prorated bases, negative net, and the 50% add-back on both sides of the threshold. Every guard is mutation-checked.

Page tests cover the head form (inline errors, a dependent field set per basis), structures, preview, history, and empty, failed and restricted states. Full Go/web gates, plus `scripts/verify-salary-config.mjs` against the synthetic tenant.
