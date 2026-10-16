# Statutory rule sets: professional tax and labour welfare fund

Issue: #1041 slice 2b (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Builds on [PF and ESI rule sets](2026-10-16-statutory-rules-pf-esi-design.md).

## Problem

Professional tax (PT) and labour welfare fund (LWF) are state levies. Each state sets its own slabs, and they change independently. PT can differ by month: Maharashtra and Karnataka charge more in February. It can also differ by gender: Maharashtra exempts women below a higher threshold. LWF is collected only in certain months, for example June and December. It is either a fixed amount or a rate with a cap, and the employer pays its own share. Several states levy neither. As with PF and ESI, no value is pre-loaded, every change cites its source, and a second person approves it.

## Scope of this slice

- Two new rule kinds, `pt` and `lwf`, on the existing statutory lifecycle: proposal, approval by another subject, rejection, withdrawal, history and day resolution. Each hospital has one rule set of each kind. The hospital is the establishment, and its state's law applies to it.
- Each covered revision records the `state_code` it applies (ISO 3166-2:IN, for example `IN-MH`) for audit and review. The code is not used in the calculation.
- **Slab tables that are complete by construction.** For every month and gender a PT revision applies to, its slabs partition ₹0 to infinity with no gaps or overlaps. For every wage, an LWF revision's slabs do the same. An exempt band is an explicit zero slab, so "no slab matched" cannot happen.
- The preview gains PT and LWF results and an optional `gender` input.
- Shell: PT and LWF cards, a slab editor, and the new preview input on `/workforce/payroll-setup`.

Not in this slice:

- **Half-yearly and annual PT regimes** (for example Kerala and Tamil Nadu), whose slab applies to the half-year's income. They need aggregation across months, which belongs to the payroll run (#1040). This slice supports monthly PT only. A hospital in such a state records `covered: false` until that regime ships, citing the reason. No value is approximated.
- Employee eligibility from the profile, such as gender, LWF exemptions for managerial staff, and the PT exemptions for disability or senior citizens that some states grant. These come with employee salary assignment (slice 4). The preview takes gender as an input, and exempt categories are out of scope until then.
- The Art. 276 annual PT ceiling is not hard-coded, because that would pre-load a statutory value. The editor shows the highest yearly total each gender can owe, so the approver can check it.
- LWF establishment thresholds (employee counts) and remittance and challan due dates belong to the run.

## Parameters

**PT** (`pt` object, required when the revision is covered):

| Field        | Meaning                                                                                                                                                                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `state_code` | `IN-` followed by two capital letters                                                                                                                                                                                                         |
| `wage_basis` | `statutory_wages` or `gross`. The month's wages from the calculator                                                                                                                                                                           |
| `slabs`      | 1–96 slabs, each with `months` (a non-empty set from 1–12), `genders` (a non-empty set of `female`, `male` and `other`), `from_paise` (≥ 0, inclusive), `to_paise` (exclusive, or null for no upper bound) and `amount_paise` (0 to ₹100,000) |

Completeness rules:

- A month that no slab names has no PT, and the preview answers it `not_due`.
- In every month that any slab names, each of the three genders must be covered. Otherwise a person's declared gender would decide whether the levy exists at all.
- For each covered month and gender, the slabs that apply, ordered by `from_paise`, start at 0. Each starts where the previous one ends, and only the last has a null `to_paise`.

**LWF** (`lwf` object, required when the revision is covered):

| Field                   | Meaning                                                                                                                                                  |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `state_code`            | As for PT                                                                                                                                                |
| `wage_basis`            | `statutory_wages` or `gross`                                                                                                                             |
| `deduction_months`      | A non-empty set of months in which LWF is collected                                                                                                      |
| `slabs`                 | 1–24 slabs partitioning ₹0 to infinity, as above, without months or genders. For each party: `*_fixed_paise`, `*_rate_ppm` and an optional `*_cap_paise` |
| `rounding_mode`/`_unit` | As for PF and ESI, applied to the rate-based part                                                                                                        |

For each party the amount is min(fixed + round(rate × wages), cap). When there is no cap it is fixed + round(rate × wages).

## Calculator

- **PT.** W is chosen by `wage_basis`. When no slab names the month of `on`, the status is `not_due`. When the slabs for that month differ by gender and `gender` is absent, the status is `gender_required` and nothing is calculated (fail closed). Otherwise one slab matches, and the result is `pt_employee` = its amount.
- **LWF.** When the month of `on` is not a deduction month, the status is `not_due`. Otherwise one slab matches W, and the results are `lwf_employee` and `lwf_employer`.
- Both kinds can also be `not_configured` or `not_covered`, as before. Statuses that produce no components contribute nothing to the totals.

## Data

The new migration `0022_workforce_statutory_pt_lwf` is append-only:

- It widens `workforce_statutory_rules.kind` to `pf`, `esi`, `pt` and `lwf`.
- `workforce_statutory_pt_params` holds the state code and wage basis. `workforce_statutory_pt_slabs` holds `months_mask` and `genders_mask` bitmasks with the band and amount, and each slab is unique per revision by position.
- `workforce_statutory_lwf_params` holds the state code, wage basis, `deduction_mask` and rounding. `workforce_statutory_lwf_slabs` holds the band and each party's fixed, rate and cap.
- These tables are kind-pinned to their revision, immutable, insertable only in the creating transaction, and under forced RLS, the same as the PF and ESI params.
- `workforce_statutory_parameters` is replaced so it counts all four params tables.
- A new deferred constraint trigger, `workforce_statutory_slabs`, rejects a covered PT or LWF revision whose slabs are incomplete or overlapping. The API checks the same rules first and answers 400, and the trigger protects writes that bypass the API.

## Verification

Real-PostgreSQL tests:

- PT and LWF go through propose and approve, and each kind resolves by day.
- Incomplete slabs are refused by the API (400) and by the trigger when the API is bypassed. The cases covered are a gap, an overlap, a missing gender within a month, a bounded last slab and a non-zero start.
- Slab rows are immutable after commit.
- The audit row and event carry no amounts.

Calculator boundary tests:

- PT: the February amount, a gender split, `gender_required`, and `not_due` months.
- Wages exactly at a band edge, where the slab starting there wins.
- LWF: fixed amounts, rate with a cap, and non-deduction months.

Every guard is mutation-checked. Page tests cover the slab editors with inline errors, the preview with and without gender, and each status label. The full Go and web gates must pass. `scripts/verify-statutory-rules.mjs` also covers PT and LWF.
