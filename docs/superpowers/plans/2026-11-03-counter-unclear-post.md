# Counter unclear-post hold Implementation Plan

**Goal:** after an unclear sale post, nothing can change the sale and a retry resends exactly what was sent (#1533).

**Spec:** `docs/superpowers/specs/2026-11-03-counter-unclear-post-design.md`

### Task 1: Hold and exact retry (`apps/shell/components/pharmacy/counter-sale.tsx` and its test)

- [x] **Failing test first:** "holds the basket and tender after an unclear failure, and retries what was sent (#1533)". The first post gets a 502. The test checks that:
  - the basket buttons are disabled;
  - the payment, customer, amount and buyer fields are disabled;
  - the status text shows;
  - "Try again" PUTs to the same URL with a byte-identical body.
- [x] **Implement:**
  - `TenderForm` gets `onUncertain(boolean)`;
  - `Counter` sets `held = paymentHeld || uncertain`, which guards `changeBasket`, `toggleCharge`, Remove, Add and the charge checkboxes;
  - the tender fields go in `<fieldset disabled={uncertain}>`;
  - the retry posts the stored `sent` values.
- [x] **Mutation checks:**
  - drop `uncertain` from `held`;
  - drop the fieldset `disabled`;
  - alter the resent values.

  The test fails for each.

- [x] **Gates:** `pnpm turbo lint type-check test build format:check`.
