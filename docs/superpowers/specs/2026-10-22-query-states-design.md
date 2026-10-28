# Shared loading, empty and error states

Resolves #1276. Follows #1255 (one look): "Loading uses skeletons, not spinners on blank pages."

## Problem

Five paged lists render their empty state from `items.length === 0` alone: MediCore visits, the MediCore department ping, Lab orders, Dispenses and Medications. Before the first answer and after a failed one, `items` is `[]`. So a loading or failed queue says "No lab orders yet", and a clinician reads an outage as an empty queue. The other lists check pending and error by hand, each in its own words. `RequestFailure` lives in `apps/shell`, and the workforce pages keep their own copy (`Failure`).

## Decisions

- **D1 One component.** `QueryState` in `@helivanta/ui` takes a paged query (`items`, `isPending`, `isError`, `retry`), a plural noun (`label`), the empty state's `icon`/`title`/`hint`, and a render function for the items. In order:
  - pending → a skeleton with `role="status"` and the text "Loading {label}…";
  - error with no items → `RequestFailure` "Could not load {label}." with Try again;
  - error with items (a failed poll or Load more) → the items, under an alert "Could not load all {label}. The list may be out of date." with Try again;
  - no items → `EmptyState`;
  - otherwise → the items.
- **D2 A failed load is never empty.** This is fail closed: `EmptyState` renders only after a successful answer with no rows.
- **D3 Enforced by the compiler.** `EmptyState` is no longer exported from `@helivanta/ui`; only `QueryState` renders it. An empty state that skips the pending and error checks cannot be written. All 8 current `EmptyState` sites move to `QueryState`, including the three benefits panels that were already correct.
- **D4 `RequestFailure` moves to `@helivanta/ui`.** The shell's copy is deleted, and every import moves.

## Not covered

- Lists that show a plain-text empty message after correct pending/error checks (workforce, supply, pharmacy stock) keep it. Moving them onto `QueryState` belongs to the zone tidy-ups (#1256–#1263), along with the workforce `Failure` copy.
- Single-record queries (`useApiQuery`) are unchanged.
- The failure box's own card border inside a card is #1275.
