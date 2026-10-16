# Statutory rule sets with maker–checker approval: PF and ESI

Issue: #1041 slice 2a (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Builds on [salary heads and structures](2026-10-16-salary-heads-structures-design.md).

## Problem

Payroll must deduct and contribute Provident Fund (EPF, EPS, EDLI) and Employees' State Insurance on the Labour Code wages the calculator already derives. The values change often, and on arbitrary days: the EPF ceiling rose mid-month on 17 September 2026. No value may be pre-loaded (ADR 0029 §1). One person must not be able to put an unchecked rate into force, so every change needs a cited source and the approval of a second person.

## Scope of this slice

- **Statutory rule sets** of kind `pf` and `esi`, one of each per hospital. Every change is a **proposal**: a full revision with its legal citation. A proposal is approved or rejected by someone other than its author, or withdrawn by its author. Only approved revisions are ever in force. They are dated to the day and immutable.
- **Coverage is explicit.** A revision either sets every parameter or states that the hospital is not covered, with a citation. A missing rule set is reported as `not_configured` and never defaulted.
- **A pure statutory calculator** in integer paise, and a **statutory preview**. The preview runs a structure through the slice 1 calculator, then applies the PF and ESI revisions in force on the date.
- **Shell:** a statutory section on `/workforce/payroll-setup` covering rule sets, proposals, decisions, history and the preview.

Not in this slice:

- Professional tax and labour welfare fund by state (slice 2b). They reuse this lifecycle, with state slab tables.
- Per-employee eligibility: the EPS age limit, excluded or international workers, voluntary higher PF, and ESI disability status from the profile. These come with employee salary assignment (slice 4). The preview takes disability as an explicit input.
- Contribution periods applied across months: ESI coverage sticks for the whole contribution period, and a payroll period that spans two revisions is split between them. Both belong to the payroll run (#1040). This slice stores the period start months and resolves revisions by day.
- The establishment-level minimum for PF admin charges. The run applies it per establishment; the preview shows each employee's share at the rate.

## Permissions

- `workforce.payroll.configure` proposes and withdraws.
- `workforce.payroll.approve` (new) approves and rejects, and never acts on the holder's own proposal. A database CHECK enforces this, and the API answers 403.
- `workforce.payroll.view` reads rule sets and runs the preview.

Every write also needs a locked hospital manager grant. Every read needs a reader or manager grant.

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}/hr/payroll`.

| Route                                  | Access                     | Behaviour                                                                                                                                                                                                                                                                                                                                                                       |
| -------------------------------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/statutory-rules`                | view + reader/manager      | Keyset pages up to 100. Each rule set carries its current approved revision and any pending proposal                                                                                                                                                                                                                                                                            |
| `PUT …/statutory-rules/{id}`           | configure + locked manager | Client-chosen id. Body: `expected_version` (approved version, 0 to create), `kind`, `name`, `covered`, `effective_on`, `citation`, `reference`, and `pf` or `esi` parameters. Creates a pending proposal. A body identical to the pending proposal or to the current revision replays. With a different proposal pending, it returns 409 `proposal_pending`. Kind never changes |
| `POST …/statutory-rules/{id}/decision` | approve + locked manager   | `proposal_id`, `expected_version`, `decision` (`approve` or `reject`) and `note` (required to reject). Self-decision is 403. A replay by the same decider returns the result                                                                                                                                                                                                    |
| `POST …/statutory-rules/{id}/withdraw` | configure + locked manager | `proposal_id` and `note`, by the proposer only (403 otherwise)                                                                                                                                                                                                                                                                                                                  |
| `GET …/statutory-rules/{id}/history`   | view + reader/manager      | Every proposal in every state, newest first                                                                                                                                                                                                                                                                                                                                     |
| `POST …/statutory-preview`             | view + reader/manager      | `structure_id`, `on`, `payable_half_days` and `disability`. Returns the structure preview plus a PF and ESI result. Nothing is stored                                                                                                                                                                                                                                           |

## Revision rules

Common fields:

- `name` (1–80).
- `effective_on`, which never precedes the current approved revision. The check runs at approval as well, because a proposal can go stale.
- `citation` (1–500): the notification, section or circular relied on.
- `reference` (0–300): a gazette number or URL.
- `covered`: when false, no parameters are given. When true, exactly the kind's parameters are given.

Money is paise and rates are parts per million. Each rule has `rounding_mode` (`nearest`, `up`, `down`) and `rounding_unit_paise` (1, 50, 100, 500, 1000), applied to every component.

**PF parameters:**

| Field                                      | Meaning                                                                                                             |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| `wage_basis`                               | `statutory_wages` (Labour Code wages from the calculator) or `gross`                                                |
| `contribution_basis`                       | `capped`: shares on min(wages, ceiling). `actual`: shares on wages                                                  |
| `wage_ceiling_paise`                       | Wage ceiling for the shares                                                                                         |
| `employee_rate_ppm`                        | Employee share                                                                                                      |
| `employer_rate_ppm`                        | Employer total. The EPF portion is the total minus EPS                                                              |
| `eps_rate_ppm`, `eps_wage_ceiling_paise`   | Pension portion: at most the employer rate, on min(wages, EPS ceiling). The EPS ceiling is at most the wage ceiling |
| `edli_rate_ppm`, `edli_wage_ceiling_paise` | EDLI on min(wages, EDLI ceiling)                                                                                    |
| `admin_rate_ppm`, `admin_minimum_paise`    | Admin charges on the share base. The minimum applies per establishment in the run                                   |

**ESI parameters:**

| Field                                                 | Meaning                                                                                            |
| ----------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `wage_basis`                                          | `statutory_wages` or `gross`                                                                       |
| `wage_ceiling_paise`, `disability_wage_ceiling_paise` | Coverage ceilings. The disability ceiling is at least the standard one                             |
| `employee_rate_ppm`, `employer_rate_ppm`              | Contribution rates                                                                                 |
| `employee_exempt_daily_paise`                         | Optional. At or below this average daily wage, the employee share is 0 and the employer still pays |
| `period_start_months`                                 | Non-empty set of months starting a contribution period, for example April and October              |

## Lifecycle

```
pending ──approve (other subject)──▶ approved   (gets approved_version = current + 1; rule set advances)
   ├────reject (other subject, note)──▶ rejected
   └────withdraw (proposer)──────────▶ withdrawn
```

- At most one pending proposal per rule set, enforced by a partial unique index.
- A proposal records `base_version`, and approval requires it to equal the rule set's version.
- Approved revisions resolve by day with `workforce_statutory_rule_on`: the highest approved version effective on or before the day.
- Decided proposals are immutable. A trigger allows only the single pending → terminal transition, and permits no update or delete otherwise.

## Calculator

Order of operations, every step in integer paise:

**PF.** W is the wages chosen by `wage_basis`.

1. base = `capped` ? min(W, ceiling) : W
2. employee = round(employee rate × base)
3. employer = round(employer rate × base)
4. EPS = round(EPS rate × min(W, EPS ceiling))
5. employer EPF = employer − EPS. This is never negative: rounding is monotonic, EPS rate ≤ employer rate, and EPS wages ≤ base.
6. EDLI = round(EDLI rate × min(W, EDLI ceiling))
7. admin = round(admin rate × base)

**ESI.** W is chosen by `wage_basis`; the ceiling is the disability ceiling when `disability` is true.

- When W exceeds the ceiling, the result is `above_ceiling` with zero contributions. Coverage that sticks for the period is the run's job.
- When payable days are 0, both shares are 0.
- employer = round(employer rate × W)
- employee = 0 when 2W ≤ exempt daily × payable half-days. Otherwise it is round(employee rate × W).

Each result has a `status`: `applied`, `not_covered`, `above_ceiling` or `not_configured`. It also carries the rule set and revision ids, the wage basis and wages used, and its components. Totals give the employee statutory deductions, the employer statutory cost, and net after statutory deductions.

## Data

Migration `0021_workforce_statutory_rules`:

- `workforce_statutory_rules`: kind, name, approved version and current revision, with a deferred current-revision key. Unique per hospital and kind.
- `workforce_statutory_rule_revisions`: proposals with status, the separation CHECK, and decision columns.
- `workforce_statutory_pf_params` and `workforce_statutory_esi_params`: typed columns and CHECKs, keyed to a revision of the matching kind. Inserted only in the revision's creating transaction.
- A deferred trigger that requires parameters exactly when the revision is covered.
- Forced RLS on every table.

Every write commits the change, a `workforce_audit` row, and an outbox event `helivanta.in.workforce.statutory_rule_changed.v1` together. The audit row and event carry ids, kind, status and version only, never rates, ceilings or citations.

## Verification

Real-PostgreSQL tests:

- A proposal and approval by two subjects. Self-approval is refused in the API (403) and by the CHECK when the API is bypassed.
- Rejection without a note, withdrawal by a non-proposer, and a second pending proposal.
- Stale `expected_version`, and a retroactive effective date at proposal and at approval.
- Replays of each step, and kind immutability.
- Day resolution across two approved revisions, as in the EPF change on 17 Sep.
- Covered without parameters, and parameters on an uncovered revision.
- Cross-hospital and cross-tenant 404s, and permission 403s.
- Audit rows and events that carry no rates.

Calculator boundary tests: wages at, below and above each ceiling, `capped` against `actual`, EPS against the employer total, the ESI exemption at equality, disability ceilings, zero payable days, and each rounding mode. Every guard is mutation-checked.

Page tests: proposal forms per kind with inline errors, pending decisions shown only to other approvers, withdraw shown only to the proposer, history, preview, and empty, failed and restricted states. Full Go/web gates, plus `scripts/verify-statutory-rules.mjs` with two synthetic subjects.
