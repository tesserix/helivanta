# Interim bills on the admission account (#197 slice 3)

Resolves: #197 (slice 3 of admission billing). Builds on slice 1 (#1453, accounts and
pharmacy charges) and slice 2 (#1460, bed days).

## Why

During a stay the relatives, the TPA co-ordinator and NABH all expect to see what has been
charged so far, on paper the hospital stands behind. Today the account is a running total
that changes as charges arrive: what the billing desk said this morning cannot be shown
again. An interim bill freezes the account at the moment it is issued, numbered, so it can
be shown again exactly as it was.

## Decisions (user, 2026-10-30)

1. **A statement, not a tax invoice.** Labelled "Interim bill — not a tax invoice". Medicine
   lines name pharmacy's GST invoice or credit note; bed days stay "before tax". Tax
   invoices remain #193.
2. **Numbered within the admission:** Interim bill 1, 2, 3 … contiguous, no hospital series.
3. **On demand only**, by a new permission `billing.bill.issue` (receptionist). No schedule.
4. **Only while the account is open.** A released admission waits for the final bill.
5. **No `InterimBillIssued` event**: nothing consumes it yet; adding it later is additive.

## What a bill is

- **Cumulative.** A bill lists every entry (charge and reversal, any source) recorded on the
  account and visible when it is issued. An entry not on the previous bill is marked _new_.
  An entry committed after the bill is on the next one, never lost between bills: the bill
  records entry ids, not a time cut, so an entry whose transaction was still open is not
  skipped by a timestamp.
- **Frozen.** Bills and their lines are append-only; entries are already append-only, so a
  bill's totals, per currency and per source, are derived from its lines and never change.
- **Unpriced bed days counted.** The number of live bed days without a rate when issued is
  stored on the bill and shown: such a day has no amount, and the bill says so rather than
  implying zero.
- **Nothing new, no bill.** Issuing when the account has no entry the previous bill lacks
  and the unpriced count is unchanged is refused `409 nothing_new`. That also absorbs a
  double click. An account with no entries has nothing to bill: the same `409`.

## Rules enforced in the database

- Bill numbers are the previous number plus one per admission (trigger), unique (primary
  key), and only on an open account (trigger).
- Lines are inserted only in the transaction that issued their bill (trigger compares the
  bill's `issued_at`, the issuing transaction's `now()`), and only name entries of the same
  admission (foreign key on `(tenant, admission, entry)`).
- No update or delete of bills or lines.
- Row security: read and write only at hospitals where the caller holds a billing grant;
  the issuer is the acting subject.
- Issue takes the admission's billing lock, so it never sees a bed-day reconcile half done
  and two issues number one after the other.

## Routes

| Route                                                                   | Behaviour                                                                                  |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `POST /billing/hospitals/:hospital/admissions/:admission/interim-bills` | `201` with the bill; `404` no grant or account; `409 account_closed`, `409 nothing_new`.   |
| `GET …/interim-bills`                                                   | Bills, newest first, paged.                                                                |
| `GET …/interim-bills/:number`                                           | One bill: issuer, time, totals, per-source totals, line and new counts, unpriced bed days. |
| `GET …/interim-bills/:number/lines`                                     | The bill's lines, paged, each with `new`.                                                  |

Reading needs `billing.account.read` and the grant; issuing needs `billing.bill.issue` and
the grant.

## UI

On the ward board, an occupied bed's account line gains **Bills**: the list of interim bills
and, with the permission, **Issue interim bill**. Opening a bill shows it as issued: header
with the not-a-tax-invoice label, totals, unpriced-days warning, and lines with Load more.

## Not covered

Final bill (needs a discharge-initiated event from MediCore); scheduled interim bills;
deposits and payments netted on the bill (#199); a print layout; patient name and address
on the bill (MediCore owns them; the UI shows the name through the existing lookup); GST
(#193); discounts (#200).
