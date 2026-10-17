# Income-tax claims: exemptions and deductions

Issue: #1041 slice 5a (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Builds on [income-tax rule sets](2026-10-17-income-tax-rule-sets-design.md), which left declarations and old-regime deductions out.

## Problem

An employee claims exemptions and deductions from their employer on Form 124 (Income-tax Act 2025, s.392(5)(b), Income-tax Rules 2026, r.205; it replaces Form 12BB from tax year 2026-27). The employer computes TDS on salary income after those claims:

- **House rent allowance**, exempt up to the least of the allowance received, rent paid less a share of salary, and a share of salary that depends on the city class.
- **Interest on borrowing for a house**, set off as a loss from house property, up to a cap.
- **Deductions from gross total income** (the 1961 Act's chapter VI-A), each up to its own cap, with some sharing a combined cap.

Which claims a regime allows, and every cap, rate and threshold, are set by the Act and the annual Finance Act. The new regime allows almost none of them. As with every statutory value in Helivanta, nothing is pre-loaded. Each claim is entered from a cited source, and a second person approves it, as part of the regime's revision for the tax year.

This slice delivers the rules and the calculation. The staff member's own declarations come in slice 5b, and the projection that feeds monthly TDS comes in slice 6.

## Scope of this slice

- An income-tax revision (`income_tax_new` or `income_tax_old`) gains an optional list of **claim heads** and **claim groups**. The existing lifecycle applies unchanged: proposal, approval by a different subject, rejection, withdrawal, history and audit.
- Claim head kinds:
  - `deduction`: deducted from gross total income, up to an optional cap and an optional group cap.
  - `house_property_interest`: interest on borrowing for a house, set off up to a cap. At most one per revision.
  - `hra`: house rent allowance, with its three rates and the annual rent above which a landlord's PAN is required. At most one per revision. The PAN threshold is stored now because slice 5b validates declarations against it.
- The calculator applies claims in the Act's order, before rounding taxable income (see Calculation).
- `POST …/hr/payroll/income-tax-preview` accepts optional stated claims and HRA inputs. Each regime's result reports every claim's outcome, so the same figures show their effect under both regimes.
- Shell: a claims editor on each income-tax card, and claim and HRA inputs plus a per-claim breakdown on the income-tax preview.

Not in this slice:

- **Staff declarations** (amounts, landlords, lenders, evidence and approval) are slice 5b.
- **Salary-derived inputs.** The HRA received, the salary the HRA rates apply to, and the annual income come from the salary assignment in slice 6. Here they are stated inputs.
- **Leave travel**, which depends on journeys within a block of years and on actual reimbursement through the payroll run (#1040).
- Perquisites, other income reported to the employer, and relief for arrears. All of these need payroll history (#1040).
- Let-out property income, and carrying a loss forward. The employer's TDS computation sets off only the capped interest.

## Parameters

`income_tax` gains two optional lists:

| Field          | Meaning                                                                                                                                                        |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `claim_groups` | 0–8 groups, each with `code`, `label` and `cap_paise` (1 to ₹1 crore). A shared cap, for example one ceiling over several investment deductions                |
| `claims`       | 0–24 heads, each with `code`, `label`, `kind` and the kind's fields below. Codes are unique within the revision, and every group is used by at least one claim |

Claim head fields:

| Kind                      | Fields                                                                                                                                                                                                                                                             |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `deduction`               | Optional `cap_paise` (1 to ₹1 crore; absent means uncapped). Optional `cap_group`, naming a group                                                                                                                                                                  |
| `house_property_interest` | Required `cap_paise`                                                                                                                                                                                                                                               |
| `hra`                     | `hra_rent_excess_rate_ppm` (the salary share deducted from rent), `hra_metro_rate_ppm` and `hra_other_rate_ppm` (the salary share caps by city class), each 0–100%. `landlord_pan_rent_above_paise` (1 to ₹1 crore), the annual rent above which a PAN is required |

`code` uses the salary-head code format (upper case, `^[A-Z0-9_-]{1,40}$`), and `label` is 1–100 characters. A claim's fields that belong to another kind must be absent. Claims and groups are returned in entry order. An existing revision has neither list, so it allows no claims, which is how it has always computed.

## Preview input

`income-tax-preview` gains:

| Field    | Meaning                                                                                                                                                                                              |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `claims` | Optional, 0–24 `{code, amount_paise}` with codes unique and amounts 0 to ₹1,000 crore. Applied to `deduction` and `house_property_interest` heads of the same code                                   |
| `hra`    | Optional `{received_paise, salary_paise, rent_paid_paise, metro}`: annual HRA received, annual salary for HRA (basic and dearness allowance as the Act defines it), annual rent paid, and city class |

## Calculation

These steps take income I, parameters P, claims and HRA inputs. The existing steps from the income-tax spec are kept, renumbered.

1. **Band**, unchanged, including `age_required`.
2. **HRA exemption.** Without an `hra` head, or without HRA inputs, E = 0. Otherwise:
   - E = min(received, max(0, rent − round(excess_rate × salary)), round(city_rate × salary)), where city_rate is the metro or the other rate.
   - E is limited to I.
3. **Standard deduction.** D = min(`standard_deduction_paise`, I − E).
4. **House property.** L = min(claimed interest, cap, I − E − D). It is 0 without a `house_property_interest` head or claim.
5. **Gross total income.** G = I − E − D − L.
6. **Deductions.** Each `deduction` claim is limited to its own cap. Each group's claims are then limited, together, to the group cap. The excess is taken off the claims last in entry order, so the result is deterministic. The sum is C, limited to G.
7. **Taxable income.** T = round(G − C) by the taxable rounding.
8. From slab tax onwards (rebate, surcharge with marginal relief, cess and total), unchanged on T.

The result gains `exemption_paise` (E), `house_property_loss_paise` (L), `deductions_paise` (C) and `claims`. Each claim line has:

- `code` and `kind`.
- `claimed_paise` and `allowed_paise`.
- `status`, one of:
  - `allowed`: in full.
  - `capped`: by its own cap, its group, or the income left.
  - `not_allowed`: the revision has no head with that code. This fails closed: an unknown claim never reduces tax.

HRA is reported as a line with `kind: "hra"` and the head's code. HRA inputs given to a revision without an `hra` head produce an `hra` line with status `not_allowed`.

All arithmetic is integer paise, and each rate product is rounded half-up to the paisa, as before.

## Storage

Migration `0027_workforce_income_tax_claims`, append-only:

- `workforce_income_tax_claim_groups`: (`revision_id`, `position`, `code`, `label`, `cap_paise`). Unique (`revision_id`, `code`).
- `workforce_income_tax_claims`: (`revision_id`, `position`, `code`, `label`, `kind`, `cap_paise`, `cap_group`, the three HRA rates, `landlord_pan_rent_above_paise`).
  - A CHECK ties each kind to exactly its fields.
  - Unique (`revision_id`, `code`). Partial unique indexes allow one `hra` and one `house_property_interest` per revision.
  - (`revision_id`, `cap_group`) references the group.
- Both tables reference `workforce_income_tax_params`, so claims exist only on income-tax revisions. They are immutable, insertable only in the creating transaction, and under forced RLS, like the other income-tax tables.
- A deferred constraint trigger, `workforce_income_tax_claims`, refuses a covered income-tax revision that has a group no claim uses.

Revisions approved before this migration have no claims and read back and replay unchanged.

## Errors

- A malformed claim list returns `400 invalid_request` "Review the claims". This covers a duplicate code, a field from another kind, an unknown or unused group, or a second `hra` or `house_property_interest` head. Every constraint above maps to the same message.
- A duplicate claim code in the preview input returns `400 invalid_request`.

## Tests (synthetic values only)

- Calculator:
  - HRA at each of its three limits, and limited to income.
  - Interest at the cap and limited by income.
  - A deduction at its cap, and a group cap with the excess taken in entry order.
  - Deductions limited to gross total income.
  - An unknown code is `not_allowed`.
  - The rebate and surcharge apply on the reduced taxable income.
  - A revision without claims computes exactly as before.
- Handler and database: each malformed list is refused by the handler, and the table or trigger refuses the same row when inserted directly. Read-back in entry order, and replay of a revision with claims. Replay of a revision approved before the migration.
- Preview: claims under both regimes, where the new regime's revision lacks the head and reports `not_allowed`.
- Shell: the claims editor round-trips each kind, refuses a duplicate code inline, and the preview shows the per-claim lines.
