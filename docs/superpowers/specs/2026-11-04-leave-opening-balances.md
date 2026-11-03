# Opening leave balances before the first ledger entry

Resolves [#1537](https://github.com/tesserix/helivanta/issues/1537). Extends the
[leave management design](2026-10-15-leave-management.md) and ADR 0021.

## Problem

HR cannot record an opening balance from **Leave balances** for a staff member
whose assigned structure has no ledger entries yet. Balances are built only from
ledger sums, so a newly assigned structure reports no leave types, and the panel
shows **Correct balance** only when at least one balance exists. The correction
endpoint accepts any balance-tracked type in the hospital, so nothing ties a
correction to the staff member's assigned structure.

Go-live migrations, mid-year joiners and transfers bring a carried-over balance
that HR must enter before the first accrual posting. Without it, leave filed
before that posting is refused because the balance does not cover it.

## Definitions

- **Structure in force on a date**: the structure of the staff member's latest
  leave assignment revision effective on or before that date, the same selection
  leave filing uses. It applies whether that revision is active or inactive.
- **Structure types**: leave types whose pinned type revision in that structure
  is balance-tracked, the same set monthly posting accrues.

An inactive revision still names a structure. Excluding it would leave a
deactivated staff member's carried-in balance impossible to correct in an open
year, since a correction cannot be backdated across a closed year.

## Required behavior

1. Both balance reads list every structure type in force on the `as_of` date,
   with `half_days: 0` when there are no entries yet. Types that have ledger
   entries but are outside that structure are still listed with their sum.
   Each balance gains `in_structure` (bool, additive). Ordering stays by code.
2. A correction is refused with 400 `invalid_reference` when no assignment
   revision is in force on `effective_on`, or the leave type is not a structure
   type of the structure in force on that date. The message names the structure
   rule. Fail closed: a correction credits leave, so the type must come from an
   HR-reviewed assignment. The existing balance-tracked type-in-force check
   stays. Assignment changes and corrections already share the hospital
   `leave-ledger` advisory lock, so the check cannot race an assignment change.
3. The panel shows **Correct balance** whenever the staff member has at least
   one `in_structure` balance and offers only those types. The server stays
   authoritative when the chosen effective date has a different structure.

Unchanged: permissions, replay and conflict rules, the open-year and date
checks, audit payload (identifier and amount only), outbox event, reason
redaction and pagination.

## Verification

- Real-Postgres tests: zero balances for a newly assigned staff member; listed
  out-of-structure ledger types are flagged; correction refused outside the
  structure, before any assignment and for an unknown type; an opening credit
  inside the structure succeeds and changes the balance.
- Vitest: zero-ledger staff member gets **Correct balance** with only structure
  types offered; no button when no structure type exists.
- Local browser check with synthetic accounts on the dev stack.

## Not covered

Staff self-service leave filing (#1132), reporting-line approval routing (#1536),
bulk opening-balance import and leave encashment.
