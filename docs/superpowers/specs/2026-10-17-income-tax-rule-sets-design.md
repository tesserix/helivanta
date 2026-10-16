# Income-tax rule sets and annual liability preview

Issue: #1041 slice 3a (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Builds on [PF and ESI](2026-10-16-statutory-rules-pf-esi-design.md) and [PT and LWF](2026-10-16-statutory-rules-pt-lwf-design.md) rule sets.

## Problem

Salary TDS (Income-tax Act 2025, s.392) is withheld against each employee's estimated annual liability under the regime they elect. Liability is computed in this order:

1. The standard deduction is subtracted from salary income.
2. Taxable income is rounded.
3. Tax is charged on the slab ladder.
4. A rebate applies up to an income threshold. Under the new regime it carries marginal relief, so crossing the threshold never costs more than the excess income.
5. Surcharge bands apply above high incomes, each with its own marginal relief.
6. Health and education cess is added on tax plus surcharge.
7. The total is rounded.

Every figure in that chain is set by the annual Finance Act and changes by tax year. The old regime also varies its exemption band by age. As with every statutory value in Helivanta, nothing is pre-loaded. Each figure is entered from the cited Act, and a second person approves it.

## Scope of this slice

- Two new kinds on the existing statutory lifecycle (proposal, approval by another subject, rejection, withdrawal, history and audit): `income_tax_new` and `income_tax_old`, one rule per hospital each.
- Each covered revision declares the `tax_year` it implements, for example `2026-27`. Its `effective_on` must fall inside that tax year, which runs from 1 April to 31 March as the Act defines it.
- **No silent roll-over.** When the revision in force on a date was written for an earlier tax year, the preview answers `tax_year_missing`. Last year's slabs are never applied to this year.
- A new `POST …/hr/payroll/income-tax-preview` reports each regime's annual liability, step by step, for a stated annual salary income and optional age.
- The shell gets cards for the two regimes, editors for the age bands, slabs and surcharge bands, and an income-tax preview panel on `/workforce/payroll-setup`.

Not in this slice:

- **Per-employee regime election and declarations** (Form 124 data, HRA, chapter deductions under the old regime). These are employee data and arrive with salary assignment (slice 4). The old-regime preview therefore covers salary income less the standard deduction only, and says so.
- **Monthly TDS.** Spreading the remaining liability over the remaining months needs year-to-date paid salary and tax, which only the payroll run (#1040) has.
- **Non-salary income, and the special-rate surcharge cap on dividends and capital gains.** Salary income takes the normal surcharge, so neither applies.
- **Non-resident rules.** The preview assumes a resident individual.

## Parameters

Covered revisions carry an `income_tax` object:

| Field                                   | Meaning                                                                                                                                                                                                |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `tax_year`                              | `YYYY-YY`, where YY is the next year's last two digits. `effective_on` must fall from 1 April YYYY to 31 March YYYY+1                                                                                  |
| `standard_deduction_paise`              | 0 to ₹10 lakh, limited to salary income                                                                                                                                                                |
| `taxable_rounding_mode` / `_unit_paise` | Rounding applied to taxable income. The unit is 1, 50, 100, 500 or 1000 paise                                                                                                                          |
| `tax_rounding_mode` / `_unit_paise`     | Rounding applied to the final total                                                                                                                                                                    |
| `age_bands`                             | 1–4 bands, each with `min_age` (0–120, unique) and its own `slabs`. Exactly one band has `min_age` 0. A person uses the band with the greatest `min_age` at or below their age                         |
| `slabs` (per band)                      | 1–16 slabs partitioning ₹0 upward, as for LWF. Each has `from_paise`, an optional `to_paise` and `rate_ppm`. Tax is the sum, over slabs, of rate × the income falling inside the slab                  |
| `rebate`                                | Optional: `income_limit_paise`, `max_paise` and `marginal_relief` (bool)                                                                                                                               |
| `surcharge_bands`                       | 0–8 bands with strictly increasing `above_paise` and `rate_ppm`. The highest band whose threshold the income exceeds applies. Marginal relief is always given, because the Act gives it for every band |
| `cess_rate_ppm`                         | 0–100%. Charged on tax plus surcharge                                                                                                                                                                  |

Every amount is in paise and every rate in ppm. Calculations are integer-only, and each product is rounded half-up to the paisa before the stated roundings.

## Calculation

These steps are given income I (annual salary income), regime parameters P and an optional age A.

1. **Band.** If P has more than one age band and A is absent, the status is `age_required` and nothing is computed (fail closed). Otherwise the band is the one with the greatest `min_age` ≤ A, or the band with `min_age` 0 when there is only one band.
2. **Standard deduction.** D = min(`standard_deduction_paise`, I).
3. **Taxable income.** T = round(I − D) by the taxable rounding.
4. **Slab tax.** S = Σ over slabs of round(rate × (min(T, to) − from)), counting only slabs where T > from.
5. **Rebate.** If T ≤ `income_limit`, R = min(S, `max`). If T exceeds the limit and `marginal_relief` is set, R = min(`max`, max(0, S − (T − limit))): the tax after rebate never exceeds the income over the limit, and the rebate never exceeds its maximum. Otherwise R = 0.
6. **Surcharge.** The band is the highest whose `above` < T. Then:
   - Raw surcharge: U = round(rate × (S − R)).
   - Marginal relief: (S − R) + U must not exceed (tax at the threshold under this regime, plus the previous band's surcharge on that tax) + (T − threshold). Any excess reduces U, never below 0.
7. **Cess.** C = round(`cess_rate` × (S − R + U)).
8. **Total.** The total is S − R + U + C, rounded by the tax rounding.

The preview returns I, D, T, the band, S, R, U, C and the total for each regime. Each regime's status is one of:

- `applied`
- `not_configured`
- `not_covered`
- `tax_year_missing`
- `age_required`

## Data

The new migration `0023_workforce_income_tax` is append-only.

Lifecycle:

- It widens the kind CHECK to add the two income-tax kinds.

Tables:

- `workforce_income_tax_params` holds `tax_year`, the standard deduction, the roundings, the rebate columns and cess. A CHECK requires all three rebate columns or none.
- `workforce_income_tax_age_bands` holds (`revision_id`, `min_age`).
- `workforce_income_tax_slabs` holds (`revision_id`, `min_age`, `position`, band and rate).
- `workforce_income_tax_surcharges` holds (`revision_id`, `position`, `above`, rate).

Rules on the new tables:

- They are kind-pinned to their revision, immutable, insertable only in the creating transaction, and under forced RLS.

Triggers:

- `workforce_statutory_parameters` is replaced so it counts five params tables.
- A new deferred constraint trigger, `workforce_income_tax_complete`, runs on covered income-tax revisions. It requires `tax_year` to contain the revision's `effective_on`, a `min_age` 0 band to exist, each band's slabs to partition ₹0 upward and surcharge thresholds to strictly increase.
- A CHECK on the params row requires `tax_year` to be well formed (`YYYY-YY`, YY being the next year).

## Verification

Real-PostgreSQL tests:

- The lifecycle for both kinds.
- The API (400) and the trigger refuse each of these: a slab gap, a missing `min_age` 0 band, non-increasing surcharge thresholds, a partial rebate and an `effective_on` outside the tax year.
- Immutability.
- Audit and event payloads that carry no amounts.

Calculator boundary tests, with synthetic ladders:

- Income at a slab edge.
- The rebate exactly at the limit and ₹1 above it, with and without marginal relief.
- Surcharge marginal relief at a threshold and ₹1 above it.
- Cess.
- Rounding.
- `age_required`, the choice of age band, `tax_year_missing` and a standard deduction above income.

The tests, the shell and `scripts/verify-statutory-rules.mjs` use synthetic values only. Every guard is mutation-checked, and the full Go and web gates must pass.
