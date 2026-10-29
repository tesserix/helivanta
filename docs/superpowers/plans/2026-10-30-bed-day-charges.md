# Bed-day charges Implementation Plan

**Goal:** Charge each admission a priced bed day per midnight in a bed (#197 slice 2).

**Spec:** docs/superpowers/specs/2026-10-30-bed-day-charges-design.md

## Tasks

1. **MediCore event.** `BedOccupancyStartedData.BedClass` (`bed_class`), set from
   `lockBeds`; PHI classification entry; `beds_test.go` asserts it in the outbox.
2. **Billing schema (0002_billing).** `billing_bed_charging` (timezone, set once,
   `applied_at`), `billing_bed_rates` (append-only, `applied_at`),
   `billing_bed_stays` (projection, `next_census_at`), `billing_bed_days`
   (one live day per admission and date; priced once, voided once); entries
   accept source `bed`; consumer list extended; system role reads only the
   due-scan columns.
3. **Reconcile + consumers.** `reconcileBedDays(tx, tenant, admission)`; started
   records the stay, ended and released reconcile. Tests: census across
   midnights and a transfer, minimum day, late end voids and reverses, a rate
   set later prices unpriced days and never reprices, replay idempotency,
   events in any order.
4. **Census worker.** `RunBedCensus` / `BedCensusOnce`, wired in `cmd/api`,
   allowlisted for `WithAllTenants`. Test drives `BedCensusOnce`.
5. **Routes.** bed-charging PUT/GET, bed-rates PUT/list; account adds
   `sources`, `bed_days`, `bed_charging`. Permission `billing.rate.manage`
   (matrix exception), unpaginated-GET allowlist for bed-charging.
6. **Frontend.** `AdmissionBalance` breakdown; `BedCharging` setup panel on the
   ward board; Vitest for both.
7. **Docs.** billing.md section, beds-and-admissions event field, contractdocs.
