# Retire the free-text dispense route — design

- **Issue:** #1680 (milestone R1 — Hospital pharmacy pilot)
- **Date:** 2026-11-16
- **Status:** implemented (branch `feat/1680-retire-free-text-dispense`)

## Problem

Every MediCore visit publishes `helivanta.in.medicore.visit_created.v1`.
Pharmacy's `pharmacy-visit-intake` consumer (`backend/internal/modules/pharmacy/consumers.go`)
turns each one into a `pharmacy_dispenses` row with `status = 'pending'`, carrying
only the visit ID and patient name: no outlet, no product, no prescription.

`POST /api/v1/pharmacy/dispenses/:id/dispense` (`dispenses.go`, `fulfil`) marks
that row dispensed with a free-text `medication` string and publishes
`helivanta.in.pharmacy.dispense_recorded.v1`. It references no catalogue product
or batch, moves no stock, checks no schedule class, prescription or outlet
licence, and writes nothing to the H1 or prescription register. A Schedule H1
drug handed over this way leaves no register entry and no stock movement, which is
the gap a drug inspection looks for.

The compliant path already exists. A counter sale (`sale.go`) can link to the
patient's OPD encounter (`encounter_id`; pharmacy's own `pharmacy_opd_encounters`
copy, #987) and enforces stock, schedule, prescription, licence and register
rules. The free-text queue is a second, unchecked way to record a hand-over.

## Decision (user, 2026-11-16)

Retire the free-text queue instead of routing it through the counter sale. A
pharmacist serves an OPD patient by posting a counter sale against that patient's
OPD encounter, the existing checked path. This deviates from the issue's wording
("a counter sale linked to that visit"). A legacy visit row has no outlet or
product, so linking it to a sale would add a second link beside the encounter.
The encounter link is the record that counts, so the decision deliberately does
not add a visit link.

## Goals

1. The free-text route can no longer record a hand-over.
2. No new pending rows are created that nobody can complete.
3. Every existing `pharmacy_dispenses` row stays readable exactly as recorded.
4. No screen, count or test implies a working dispense queue.
5. The #976 journey e2e proves the replacement path: a dispense for an OPD patient
   moves stock and lands in the register.

## Non-goals

- Changing the counter sale, its encounter link or its checks (#1477 owns the
  prescription-image gate).
- Changing MediCore's `visit_created.v1` publication. Lab's `lab-visit-intake`
  consumer still uses it.
- Linking legacy visit rows to sales, or back-filling stock or register entries
  for historical free-text dispenses. History is preserved, not rewritten
  (AGENTS.md: corrections preserve history).
- Dropping the `pharmacy_dispenses` table or its audit trigger.
- Ward or IPD issue (#1682).

## Anti-goals

- A route that still returns 2xx for a free-text dispense under any flag or role.
- Rewriting pending rows to `dispensed`, `cancelled` or any new status. The
  status CHECK and the `dispensed_at` constraint stay unchanged.
- A dashboard count of zero presented as "nothing waiting" when the queue no
  longer exists.

## Design

### Backend (pharmacy module)

1. **Route answers 410.** `POST /pharmacy/dispenses/:id/dispense` stays
   registered with its existing `PermDispenseFulfil` declaration (`pharmacy.dispense.fulfil`), so a caller
   without the permission still gets 403 and a cross-tenant caller still gets
   404 semantics unchanged. Every permitted call returns `410 Gone` through
   `respond.Error(c, http.StatusGone, …)` (there is no dedicated helper), with code `dispense_retired` and a message naming the counter
   sale for an OPD encounter. It reads and writes nothing, and the handler's
   update and publish code is deleted. _Fail closed:_ the route refuses without
   inspecting the row, so no state of the row (pending, missing, dispensed) can
   reach a write. Keeping the route registered means a stale client gets an
   explicit, documented answer, not a 404 that looks like a missing row.
2. **Intake consumer removed.** `pharmacy-visit-intake` is deleted from
   `Consumers`. New visits create no pharmacy rows. The JetStream durable of that
   name stays as inert metadata. The stream uses `LimitsPolicy` with a 24 h
   `MaxAge` (`pkg/events/bus.go`, `retention.go`), so an orphaned durable holds
   no payload beyond the stream's own retention. Removing it is a release-note ops
   step (`nats consumer rm`), not code. The `processedRetention` comment in
   `pkg/events/retention.go` uses this consumer's duplicate row as its example.
   Rewrite it around a live consumer; the invariant and its test are unchanged.
3. **`dispense_recorded.v1` retired.** It has no consumers in the repository.
   Remove `SubjectDispenseRecorded`, `DispenseRecordedData` and the
   `Publishes()` entry. `TestPublishedSubjectConstants` requires every exported
   subject constant to be published, so the constant cannot stay. Regenerate the
   event registry outputs (`event-payloads.json`, reference README, inventory).
4. **Reads unchanged.** `GET` list routes (waiting and dispensed tabs, keyset
   paging) and their permission keep serving historical rows. Waiting rows
   remain `pending` forever. The API description says they are from the retired
   queue and cannot be completed.
5. **Insights.** The `GET /pharmacy/insights` pending/dispensed counts stay
   (historical). The dashboard is their only shell reader, and it stops
   presenting them as current work (below).

### Frontend (shell)

1. `/pharmacy/dispense` becomes read-only history: "Dispense" buttons and the
   fulfil mutation are removed. A notice above the list says the queue is
   retired and links to the counter sale for OPD patients. The tabs stay, with
   copy saying waiting rows were never completed in the retired queue.
2. The zone and nav label changes from "Dispense queue" to "Past dispenses"
   (`packages/ui/src/zones.ts`, its permission unchanged). The route path stays
   so links and bookmarks keep working.
3. The dashboard's dispense-task tile (`operational-dashboard.tsx`, "Current
   status of dispense tasks created in this period") is removed. The dashboard
   catalogue string `"pharmacy:Dispensing"` is updated to match.
4. `visit-panel.tsx` stops saying new visits open a pending dispense in Pharmacy.

### Tests

- Go: the route returns 410 for a pending, a dispensed and a nonexistent ID, and
  the row and outbox are unchanged afterwards (assert bytes, not status only).
  Without the permission it is 403. A `visit_created.v1` event creates no
  pharmacy row, while lab still creates its order. The existing audit-actor test
  entry for `system:pharmacy-visit-intake` is removed. Each new assertion is
  checked to fail against the old handler.
- Vitest (`renderWithProviders`): the history page renders rows without a
  Dispense action and shows the retired notice. The dashboard renders no dispense
  tile.
- e2e:
  - `journey.spec.ts` "pharmacist can dispense a visit created by admin" (#976)
    becomes: admin opens an OPD encounter, the pharmacist posts a counter sale
    against it, then asserts the stock movement and, for a scheduled line, the
    register entry.
  - `pharmacy-paging.spec.ts` seeds pending rows through visits, which no longer
    happens. It is rewritten against the read-only history if rows can be seeded
    without the retired path, otherwise removed, with paging kept under Vitest.
  - `dashboard-phone.spec.ts` targets the history page.
  - `navigation.spec.ts` uses the new label.
  - `smoke.spec.ts` selectors are kept working.

### Contracts and docs

- `docs/api/domains/pharmacy.md`: the route documents 410. The intake
  subscription and `dispense_recorded.v1` are marked retired with this issue.
- `docs/standards/backend.md` uses `pharmacy-visit-intake` and
  `SubjectDispenseRecorded` as worked examples. They are replaced with live
  examples, so the standard does not teach a removed consumer.
- `go run ./cmd/contractdocs` and `-check`, and the Staff operational access
  inventory `--check`.

## Constraints

- Branch from main; no `sale.go` change, so independent of #1477.
- No migration: no schema change is needed.
- Gates: backend and web gates run one after another (concurrent runs cause load
  timeouts).

## Calls made in the design (not stated by the user)

- Keep the route registered at 410 rather than removing it.
- Retire `dispense_recorded.v1` along with the route.
- Rename the nav entry to "Past dispenses" and keep its path.
- Remove the dashboard dispense tile rather than relabelling it.

## Left to the builder

- Exact notice copy and where the counter-sale link points.
- Whether `pharmacy-paging.spec.ts` can seed history rows, and therefore whether
  it is rewritten or removed.
- Error code wording, if `dispense_retired` collides with an existing code.

## Implementation notes

- The route runs the shared session and pharmacy-tenant precondition before the
  410, so a stale expected tenant still gets `409 session_changed`.
- `contracts/api/clinical.openapi.json` documents the 410 and no request body.
- `smoke.spec.ts` lost its pharmacy step, and `internal/journey` and one bootstrap
  test no longer depend on the intake.
- The OPD e2e sells an OTC line. An H1 or prescription register entry for a
  scheduled line is covered by backend tests, not e2e, because the e2e suite has
  no prescription and released-image fixture.

## Not covered by this slice

- Removing the orphaned `pharmacy-visit-intake` JetStream durable (ops step).
- Any visit-to-sale reporting for historical free-text dispenses.
