# Professional tax: half-yearly and annual periods

Issue: #1041 (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Research: [Indian statutory landscape](../../payroll-india-statutory-reference-2026-10-16.md). Builds on [PT and LWF rule sets](2026-10-16-statutory-rules-pt-lwf-design.md), which deferred this regime.

## Problem

Some states levy professional tax on a period's income rather than a month's. Kerala and Tamil Nadu slab PT on half-yearly income, and the employer deducts it once in each half-year. Other states use an annual period. Today a PT revision can only describe monthly slabs, so a hospital in such a state has to record `covered: false` and track PT by hand. As before, no state's value is pre-loaded, every change cites its source, and a second person approves it.

## Scope of this slice

- A PT revision gains a **cadence**: `monthly` (today's behaviour, unchanged), `half_yearly` or `annual`.
- A periodic revision records the month each **period starts** in. Half-yearly periods are two six-month periods starting six months apart, for example April and October. An annual period is one twelve-month period.
- In a periodic revision, slab `from_paise`/`to_paise` bound the **period's wages**, `amount_paise` is the tax for the period, and a slab's `months` are the months the tax is **deducted**. Each period must have exactly one deduction month, so a period's tax is charged once and never twice.
- `calculatePT` takes the wages for its basis (month or period) and is unchanged in shape. A new period resolver gives the period that contains a deduction month.
- Both previews (structure and staff) report the period for a periodic PT result. Neither has wage history, so each projects the period's wages from the month it computes. The result says so (`basis: "projected"`), and the shell shows the projection with its period.
- Shell: cadence and period-start inputs on the PT card, slab labels that follow the cadence, and the period line on preview results.

Not in this slice:

- **Actual period wages.** Summing wages paid across the period, plus joiners, leavers and pay changes mid-period, needs payroll history. That history belongs to the payroll run (#1040), which will call the same calculator with `basis: "actual"`. The preview never claims an actual basis.
- Apportioning a period's tax across employers or between two revisions in force in the same period. The revision in force on the deduction day applies, as it does for monthly PT and LWF.
- PT exemptions for disability or senior citizens, as before.

## Parameters

`pt` gains two fields:

| Field                 | Meaning                                                                                                                                |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `cadence`             | Required. `monthly`, `half_yearly` or `annual`                                                                                         |
| `period_start_months` | Absent or empty for `monthly`. Two months six apart for `half_yearly` (for example `[4, 10]`). One month for `annual`. Returned sorted |

Rules, enforced in the handler and again in the database:

- A monthly revision has no period starts. A periodic revision has the right number of starts, and half-yearly starts are six months apart.
- In a periodic revision, each period has exactly one month that any slab names. A period with none would silently drop the levy for that period. A period with two would charge it twice.
- The existing completeness rule still applies to every named month and gender: the slabs partition ₹0 to infinity once.

Existing revisions are monthly. The migration gives them `cadence = 'monthly'` and no period starts, so every stored revision reads back and replays as before.

## Calculation

`ptPeriod(p, on)` returns nothing for a monthly revision. For a periodic one, it returns the period containing `on`'s month: the latest start at or before that month, wrapping into the previous calendar year when needed. It returns the period's first and last day and its length in months.

The preview:

1. Computes the month's wages on the revision's wage basis, as today.
2. For a periodic revision in a deduction month, it computes the period's wages as the month's wages × the period's length in months and labels them `projected`. Outside a deduction month, PT is `not_due` with no projection.
3. Calls `calculatePT` with those wages. Gender handling and `gender_required` are unchanged.

`statutoryResult` gains an optional `period`:

```json
"period": { "cadence": "half_yearly", "starts_on": "2026-04-01", "ends_on": "2026-09-30", "months": 6, "wages_paise": 18000000, "basis": "projected" }
```

`wages_paise` and `base_paise` keep the month's figures, so PF, ESI and LWF lines stay comparable. `period.wages_paise` is the amount the slab was matched on. The field is absent for monthly PT and for every other kind.

All amounts are integer paise. A month's wages are bounded by the calculator, so twelve times that cannot overflow `int64`.

## Storage

Migration `0026_workforce_pt_periods`, append-only:

- `workforce_statutory_pt_params` adds `cadence text NOT NULL DEFAULT 'monthly'` and `period_start_mask smallint`. A check ties them together: no mask when monthly, a mask in `{65, 130, 260, 520, 1040, 2080}` when half-yearly, and a single bit when annual. Adding columns does not fire the table's immutability trigger, and rows stay immutable afterwards.
- A deferred constraint trigger, `workforce_statutory_pt_periods`, fires on each covered PT revision insert. It fails (`23514`) unless each period has exactly one named month. The handler checks the same rule first, so the database check is a backstop.

RLS, the audit trail and the maker–checker lifecycle are inherited unchanged.

## Errors

- Wrong period starts for the cadence: `400 invalid_request` ("Review the rule"). A half-yearly or annual revision with zero or two deduction months in a period: `400 invalid_request`, with a message naming the one-deduction-month rule. The constraint name maps to the same message.

## Tests (synthetic values only)

- Calculator: period resolution for every start month, including periods that wrap the year end. Not-due outside a deduction month. Slab matching on period wages, including a band boundary. `gender_required` on period wages.
- Handler and database: monthly with starts, half-yearly with one start or starts not six months apart, annual with two starts, a period with no deduction month or with two. Each case is rejected, and the database trigger rejects a direct insert that skips the handler.
- Read-back and replay of a periodic revision, and of a pre-existing monthly one after the migration.
- Structure and staff previews: a deduction month returns `period` with `basis: "projected"` and the projected wages. A non-deduction month is `not_due`. Monthly PT has no `period`.
- Shell: the cadence switch relabels the slabs and requires period starts, the form round-trips a periodic revision, and the preview shows the projected period line.
