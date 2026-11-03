# Counter sale: holding the sale after an unclear post (#1533)

Resolves: #1533. Builds on the counter sale (#1386 B-1) and service charges (#1388); found in the #1531 review.

## Why

A sale id makes a retried post replay the same sale. After a post fails without a clear answer (a network drop or a 5xx), the sale may already be committed. Two things went wrong at that point:

- **Basket edits.** Changing the basket or ticking a charge cleared the price. That unmounted the payment form and dropped its id, so the next post became a second sale: double billing and double stock.
- **Tender edits.** Editing the tender before "Try again" sent a different body under the same id. The server refuses that (409, different details) but does not say what happened.

## What it does

- **Holding.** While the last post's outcome is unknown, the counter holds the sale:
  - the basket, service charges, customer and tender cannot change;
  - a status line says why.
- **Exact retry.** "Try again" resends exactly the values first submitted, under the same id.
- **Release.** Only a settled answer releases the hold:
  - the sale posts, and the invoice is shown;
  - or a 409 `not_sellable` refusal arrives, and the counter prices the basket again with a new id.

The server is unchanged. It already replays the same id with the same body and refuses a different one.

## How it holds

- `TenderForm` derives "uncertain" from the post error: any error except a 409 `not_sellable`. It reports this to `Counter`.
- `Counter` adds the flag to the existing payment hold that guards basket and charge edits.
- The tender fields sit in a `fieldset` disabled while uncertain.
- The retry posts the stored first submission rather than re-reading the form.

## Not covered

- Reloading or leaving the page while the outcome is unknown. The id is lost, and the cashier checks the sales list.
- Returns. Editing a return after an unclear failure is refused by the server (409) and cannot credit twice.
