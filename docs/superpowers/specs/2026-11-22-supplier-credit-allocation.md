# Supplier-credit bill allocation and reversal

Development decision for #1784; not deployed, activated or tax/GL approved.
Extends [ADR-0075](../../adr/0075-pharmacy-supplier-returns.md) and
[matching](2026-11-21-supplier-credit-matching.md). PharmaConnect remains the single
owner of credit, supplier bills, settlements and their private history. Consumers
are the Accounts SDK/UI through Global API. No second financial master or service.

## Required behavior

Actual accepted credit can remain unused against a fully paid original bill and
be allocated to an eligible future inventory bill of the same supplier, currency
and outlet. Matching attributes returns; allocation separately consumes the actual
credit pool and reduces payable liability. Neither automatically changes stock,
acquisition cost, tax/ITC, statutory documents, bank balances or patient billing.
Use IN-RC-04/05/11 exact-money, source-history and uncertainty principles in this
supplier context; do not invent period, tax or approval-limit policies.

An allocation binds UUIDv7 command/credit/payable identities, current credit
`expected_version`, explicit `expected_available_minor`, current
`expected_payable_version`, explicit `expected_outstanding_minor`, positive INR
`amount_minor`, `occurred_on` and reason. Observed values come from authorised
projections, not manual backend-ID entry. Dates start in 1970, cannot precede the
credit document or target bill and cannot exceed the authoritative outlet date.
Amounts and observed balances are bounded at 10^12 minor units. Source licence,
stock cost or tax decisions are not inferred from financial authority.

The active credit's unallocated amount and the bill's positive outstanding
liability must both cover the allocation. Require at least one inventory source
line and exact original supplier reference/currency; salary, expense-only,
foreign, paid and unsupported sources cannot become eligible by supplying an ID.
Exclude bills containing platform-supplier receipt sources, even if a manually
recorded external supplier reference matches. That financial acknowledgement is
a separate owner protocol, not authority conferred by matching text. Mixed-source
bills fail this boundary too; validate it in both source choices and DB guards.
Do not require the original source bill itself to remain unpaid. The actual credit
document is value authority; matching does not spend the pool or multiply it.

## Owner journal and financial source of truth

Add immutable allocation/reversal journals with forced tenant/exact-outlet RLS,
actor checks, restrictive operating-purpose fences, audit and protected
projections. Preserve original commands through linked full reversal, never
delete a financial entry. A reversal restores exactly the original allocation to
the credit pool and payable liability, preserving intervening payments. It is not
a cash refund or return of medicines. Partial correction is full reversal plus a
new explicit allocation.

Acquire the credit lock, then the **existing payable advisory key** used by
`putSettlement` and `pharmacy_settlement_guard`. Both application and DB guards
must use that exact ordering/key. Payments acquire only the payable key, avoiding
a lock cycle. Under the locks recheck credit version/available pool and the bill's
version/outstanding. Different documents targeting one bill serialize on that
bill; one credit targeting several bills serializes on that credit.

The bill observation version is creation 1 plus immutable legacy settlement,
managed-allocation and allocation-reversal counts. A payment followed by its
reversal still changes it even when balances net to zero. Return matching does
not change bill versions. Publish this version in the new owner allocation
projection, preserving existing v1 payable wire fields for strict consumers.

Extend `pharmacy_payable_balances` to include net managed allocation alongside
legacy manual credits, retaining paid/credit/outstanding semantics. Extend the
**legacy settlement guard** to include that same net amount, so a payment/manual
credit cannot over-settle a bill already using managed credit. Do not redefine
legacy settlement kinds or allow negative liability. Audit existing statements,
reports, exports and their UI explanations; events alone are not integration.

Extend deferred credit proof to include every immutable allocation/reversal,
global credit version, latest actor and net allocated amount, retaining all
match/reversal/void/correction checks. A document can be voided only after both
net matching and allocation are zero. Capture immutable command balance/version
results so original receipts do not turn into today's projections on replay.

## Contracts, authority and recovery

Use existing accounts-adjust for commands, accounts-read for retained evidence,
verified tenant/actor preconditions, current membership and exact active outlet
authority. Writes and new eligible-source selectors stay payables-gated. Retained
history/recovery is baseline under fresh current authority; none bypasses Staff
operational review. Do not grant purchasing/receiving duties financial authority.
No new role or statutory financial-limit decision is introduced.

Provide bounded eligible bill/return choices with human references, supplier names
and current observations; no raw-ID form. Use the existing explicit concurrent-
index migration support for indexes on already populated bill/return tables,
separate from transactional ledger DDL. New journal indexes are created with
their empty tables. Cursor pages are capped at 50 and audited before disclosure.
A separate payables-gated credit context supplies the current credit snapshot,
database as-of timestamp, verified outlet timezone and outlet-local date. Forms
use that date rather than a browser clock. It extends the canonical credit view
on a new route; existing strict current-document responses do not change.

Same actor/command/intent returns the original receipt after subsequent activity.
Changed intent or actor conflicts; recovery by another actor is invisible. Normal
authorised history may show another recorder. Reference-only browser recovery
retains no supplier, money or private reason. A 503 means unconfirmed outcome:
recover before a fresh intent. Audit/outbox/projection failure rolls back every
write and releases no private read. Unauthorised object 404, missing permission 403,
stale/duplicate/ineligible state 409 and unavailable dependency 503 remain explicit.

Inherit ADR planning: 20 peak commands/s, 64 KiB effective command limit,
15-second deadline, unmeasured p99 targets 500 ms reads/2 s commands and existing
12/36-month return envelope 3.65/32.85 million lines. One credit and bill per command;
no synchronous external calls under locks or added infrastructure cost. Ledger
migrations are additive and irreversible after use: retain evidence and roll
forward. Deployment/activation require the existing named approval/release review.

## Acceptance and rejected alternatives

Observe a hosted missing-allocation404 baseline after real credit/paid-source and
future-bill setup. Prove both ownership models plus chains, partial/full allocation,
paid source retained value, no negative liability, same-credit/different-bill and
different-credit/same-bill races, legacy payment/manual-credit interleaving, net-zero
stale versions, immutable full reversal after payment, current actor/outlet/tenant
denial, duplicate/changed intent, audit/outbox rollback, reference recovery and
actual balance/report/export consumers. Add human source selectors, SDK and UI,
Global API/browser journeys, local builds without apps/DBs and hosted race gates.
Record deployed revision and authenticated live acceptance before closure.

Rejected: using legacy manual credit to force a paid bill below zero; duplicating
credit for each return/bill; event-only integration; deleting allocations; adding
fields silently to strict existing payable responses; and unlocked preflight-only
balance checks.

## Reversal context requirement

A paid or fully credited bill is intentionally absent from allocation choices.
The reversal screen must still load that original bill's current version and
balance through an authorised owner context; it must not reuse its historical
allocation receipt or assume a zero-net payment/reversal left the version
unchanged. The allocation context endpoint is
`/:credit/allocations/:allocation/context` under supplier credits. Its canonical
`SupplierCreditAllocationContext` returns the original allocation view, current
`SupplierCreditContext`, and current `SupplierCreditPayableContext`. The latter
permits zero outstanding; allocation choices still require a positive balance.
Both share canonical payable fields, without redefining existing v1 bill wire
models. Read under the credit lock followed by the same payable lock used by
settlements, then audit before disclosure. Require finance read permission and
exact credit/allocation/bill/outlet linkage even for retained evidence. Never use
historical receipt observations to construct a fresh reversal command.

The finance UI, full Global API/browser E2E and approved live acceptance remain
required; this owner read does not complete them.

## Finance UI acceptance prerequisites

- Finance users use the bounded dispatched-return source picker at
  `/accounts/supplier-credits/source-returns` before the first actual credit
  exists (development candidate; hosted acceptance pending). Accounts authority must not silently require granting
  physical return/stock authority or manually entering backend identifiers.
- Match reversal needs a single authorised current match/return context. The
  return's net attributed amount includes other credit documents; the original
  match receipt is historical and searching unlimited source pages is not an
  acceptable way to obtain current observations.
- Paid-bill reversal uses the allocation context above. Commands retain original
  actor/scope/identity and intent through uncertain responses; recover original
  evidence before permitting an exact retry, and re-read current context before
  creating a new command after a stale-state conflict.
- Show actual credit, return attribution, bill applications and unused pool as
  separate values. Resolve authorised recorder names or explicitly report name
  availability; do not display account identifiers as names or fabricate roles.
- New actions and source choices require Payables release admission. Retained
  history/context requires current finance authority even after flag disable,
  reversal or void. No frontend capability grants backend authority.
- Integrate application/reversal history into the bill UI and reconcile existing
  balance/report consumers without treating applications as new stock cost,
  tax/ITC treatment, a bank movement or statutory accounting approval.

These are remaining acceptance requirements, not implemented UI claims.
